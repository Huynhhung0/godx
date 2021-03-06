// Copyright 2019 DxChain, All rights reserved.
// Use of this source code is governed by an Apache
// License 2.0 that can be found in the LICENSE file.

package storageclient

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/DxChainNetwork/godx/storage"
	"github.com/DxChainNetwork/godx/storage/storageclient/filesystem/dxfile"
)

// uploadSegmentID is a unique identifier for each segment in the storage client
type uploadSegmentID struct {
	// Unique to each dx file
	fid dxfile.FileID

	// Index of each segment within a file
	index uint64
}

// unfinishedUploadSegment represents a segment from the dx filesystem that has not
// finished uploading, including flags of the upload progress
type unfinishedUploadSegment struct {
	// Information about the file. localPath may be the empty string if the file
	// is known not to exist locally.
	id        uploadSegmentID
	fileEntry *dxfile.FileSetEntryWithID
	threadUID int

	// Information about the segment within the file
	// 	+ index: 	array index within dxfile that represents local file in memory
	// 	+ offset: 	actual location in byte unit offset of the segment within the file
	// 	+ length: 	segment length in byte unit within local dx file
	// And these fields can help us to read from dx file
	index  uint64
	offset int64
	length uint64

	memoryNeeded   uint64 // memory needed in bytes
	memoryReleased uint64 // memory that has been returned of memoryNeeded

	sectorsMinNeedNum int // number of sectors minimum to recover file
	sectorsAllNeedNum int // number of sectors of minimum + redundant

	stuck       bool // flag whether the segment was stuck during upload
	stuckRepair bool // flag if the segment was set 'true' for repair by the stuck loop

	// The logical data is the data read from file of user
	// The physical data is all the sectors encrypted and stored on disk across the network
	logicalSegmentData  [][]byte
	physicalSegmentData [][]byte

	mu                  sync.Mutex
	sectorSlotsStatus   []bool              // 'true' in that index if a sector is either uploaded, or a worker is attempting to upload that sector
	sectorsCompletedNum int                 // number of sectors that have been successful completely uploaded
	sectorsUploadingNum int                 // number of sectors that are being uploaded, but aren't finished yet (may fail)
	released            bool                // whether this segment has been released from the active segments set
	unusedHosts         map[string]struct{} // hosts that aren't yet storing any sectors or performing any work
	workersRemain       int                 // number of inactive workers still able to upload a sector
	workerBackups       []*worker           // workers that can be used if other workers fail
}

// notifyBackupWorkers is called when a worker fails to upload a sector, meaning
// that the backup workers may now be needed to help the sector finish uploading
func (uc *unfinishedUploadSegment) notifyBackupWorkers() {
	// Copy the standby workers into a new slice and reset it since we can't
	// hold the lock while calling the managed function.
	uc.mu.Lock()
	backupWorkers := make([]*worker, len(uc.workerBackups))
	copy(backupWorkers, uc.workerBackups)
	uc.workerBackups = uc.workerBackups[:0]
	uc.mu.Unlock()

	//assignSectorTaskToWorker(backupWorkers, uc)

	for i := 0; i < len(backupWorkers); i++ {
		backupWorkers[i].signalUploadChan(uc)
	}
}

// IsSegmentUploadComplete checks some fields of the segment to determine if the segment is completed
// 1）no remain workers and no uploading task
// 2) completely upload and no uploading task
func (uc *unfinishedUploadSegment) IsSegmentUploadComplete() bool {
	if uc.sectorsCompletedNum == uc.sectorsAllNeedNum && uc.sectorsUploadingNum == 0 {
		return true
	}

	// We are no longer doing any uploads and we don't have any workers left
	if uc.workersRemain == 0 && uc.sectorsUploadingNum == 0 {
		return true
	}
	return false
}

// dispatchSegment dispatches segments to the workers in the pool in the current solution
// Now it may be that one sector will not be assigned to worker, and this doesn't have a big impact on the upload process
// But we will optimize this features and schedule strategy is more balanced and fair
func (client *StorageClient) dispatchSegment(uc *unfinishedUploadSegment) {
	// Add segment to pendingSegments map
	client.uploadHeap.mu.Lock()
	_, exists := client.uploadHeap.pendingSegments[uc.id]
	if !exists {
		client.uploadHeap.pendingSegments[uc.id] = struct{}{}
	}
	client.uploadHeap.mu.Unlock()

	// Distribute the segment to each worker in the work pool, marking the number of workers that have received the segment
	client.lock.Lock()
	uc.workersRemain += len(client.workerPool)
	workers := make([]*worker, 0, len(client.workerPool))
	for _, worker := range client.workerPool {
		workers = append(workers, worker)
	}
	client.lock.Unlock()

	client.assignSectorTaskToWorker(workers, uc)
}

// assignSectorTaskToWorker will assign non uploaded sector to worker
func (client *StorageClient) assignSectorTaskToWorker(workers []*worker, uc *unfinishedUploadSegment) {
	for _, w := range workers {
		if w.isReady(uc) {
			w.pendingSegments = append(w.pendingSegments, uc)
			select {
			case w.uploadChan <- struct{}{}:
			default:
			}
		}
	}
}

// downloadLogicalSegmentData will fetch the logical segment data by sending a
// download to the storage client's downloader, and then assign the data to the field
func (client *StorageClient) downloadLogicalSegmentData(segment *unfinishedUploadSegment) error {
	downloadLength := segment.length
	if segment.index == uint64(segment.fileEntry.NumSegments()-1) && segment.fileEntry.FileSize()%segment.length != 0 {
		downloadLength = segment.fileEntry.FileSize() % segment.length
	}

	// Create the download
	buf := newDownloadBuffer(segment.length, segment.fileEntry.SectorSize())
	snap, err := segment.fileEntry.Snapshot()
	if err != nil {
		return fmt.Errorf("cannot create the snapshot: %v", err)
	}

	d, err := client.newDownload(downloadParams{
		destination:     buf,
		destinationType: "buffer",
		file:            snap,

		latencyTarget: 200e3, // No need to rush latency on repair downloads.
		length:        downloadLength,
		needsMemory:   false, // We already requested memory, the download memory fits inside of that.
		offset:        uint64(segment.offset),
		overdrive:     0, // No need to rush the latency on repair downloads.
		priority:      0, // Repair downloads are completely de-prioritized.
	})
	if err != nil {
		return err
	}

	// Register some cleanup for when the download is done.
	d.onComplete(func(_ error) error {
		// Update the access time when the download is done
		return segment.fileEntry.DxFile.SetTimeAccess(time.Now())
	})

	// Set the in-memory buffer to nil just to be safe in case of a memory leak.
	defer func() {
		d.destination = nil
	}()

	// Wait for the download to complete.
	select {
	case <-d.completeChan:
	case <-client.tm.StopChan():
		return errors.New("repair download interrupted by stop call")
	}
	if d.Err() != nil {
		buf.buf = nil
		return d.Err()
	}
	segment.logicalSegmentData = [][]byte(buf.buf)
	return nil
}

// retrieveDataAndDispatchSegment will fetch the logical data for a segment, encode
// the physical data for the segment, and then distribute them.
func (client *StorageClient) retrieveDataAndDispatchSegment(segment *unfinishedUploadSegment) {
	select {
	case <-client.tm.StopChan():
		return
	default:
	}

	err := client.tm.Add()

	if err != nil {
		return
	}
	defer client.tm.Done()

	ec, err := segment.fileEntry.ErasureCode()
	if err != nil {
		return
	}

	erasureCodingMemory := segment.fileEntry.SectorSize() * uint64(ec.MinSectors())
	var sectorCompletedMemory uint64
	for i := 0; i < len(segment.sectorSlotsStatus); i++ {
		if segment.sectorSlotsStatus[i] {
			sectorCompletedMemory += storage.SectorSize
		}
	}

	defer client.cleanupUploadSegment(segment)

	// Retrieve the logical data for the segment
	err = client.retrieveLogicalSegmentData(segment)
	if err != nil {
		// retrieve logical data failed, interrupt upload and release memory
		segment.logicalSegmentData = nil
		segment.workersRemain = 0
		client.memoryManager.Return(erasureCodingMemory + sectorCompletedMemory)
		segment.memoryReleased += erasureCodingMemory + sectorCompletedMemory
		client.log.Error("retrieve logical data of a segment failed:", err)
		return
	}

	// Encode the physical sectors from content bytes of file
	var segmentBytes []byte
	for _, b := range segment.logicalSegmentData {
		segmentBytes = append(segmentBytes, b...)
	}
	segment.physicalSegmentData, err = ec.Encode(segmentBytes)
	if err != nil {
		segment.workersRemain = 0
		client.memoryManager.Return(sectorCompletedMemory)
		segment.memoryReleased += sectorCompletedMemory
		for i := 0; i < len(segment.physicalSegmentData); i++ {
			segment.physicalSegmentData[i] = nil
		}
		client.log.Error("Erasure encode physical data of a segment failed", "err", err)
		return
	}

	segment.logicalSegmentData = nil
	client.memoryManager.Return(erasureCodingMemory)
	segment.memoryReleased += erasureCodingMemory

	// Sanity check that at least as many physical data sectors as sector slots
	if len(segment.physicalSegmentData) < len(segment.sectorSlotsStatus) {
		client.log.Info("not enough physical sectors to match the upload sector slots of the file")
		return
	}
	key, err := segment.fileEntry.CipherKey()
	if err != nil {
		return
	}
	// Loop through the sectorSlots and encrypt any that are needed
	// If the sector has been used, set physicalSegmentData nil and gc routine will collect this memory
	for i := 0; i < len(segment.sectorSlotsStatus); i++ {
		if segment.sectorSlotsStatus[i] {
			segment.physicalSegmentData[i] = nil
		} else {
			cipherData, err := key.Encrypt(segment.physicalSegmentData[i])
			if err != nil {
				segment.physicalSegmentData[i] = nil
				client.log.Error("encrypt segment after erasure encode failed", "err", err)
			} else {
				segment.physicalSegmentData[i] = cipherData
			}
		}
	}

	if sectorCompletedMemory > 0 {
		client.memoryManager.Return(sectorCompletedMemory)
		segment.memoryReleased += sectorCompletedMemory
	}
	client.dispatchSegment(segment)
}

// retrieveLogicalSegmentData will get the raw data from disk if possible otherwise queueing a download
func (client *StorageClient) retrieveLogicalSegmentData(segment *unfinishedUploadSegment) error {
	numRedundantSectors := float64(segment.sectorsAllNeedNum - segment.sectorsMinNeedNum)
	minMissingSectorsToDownload := int(numRedundantSectors * RemoteRepairDownloadThreshold)
	needDownload := segment.sectorsCompletedNum+minMissingSectorsToDownload < segment.sectorsAllNeedNum

	// Download the segment if it's not on disk.
	if segment.fileEntry.LocalPath() == "" && needDownload {
		return client.downloadLogicalSegmentData(segment)
	} else if segment.fileEntry.LocalPath() == "" {
		return errors.New("file not available locally")
	}

	// Try to read the file content from disk. If failed, go through needDownload
	osFile, err := os.Open(string(segment.fileEntry.LocalPath()))
	if err != nil && needDownload {
		return client.downloadLogicalSegmentData(segment)
	} else if err != nil {
		return errors.New("failed to open file locally")
	}
	defer osFile.Close()

	buf := newDownloadBuffer(segment.length, segment.fileEntry.SectorSize())
	sr := io.NewSectionReader(osFile, segment.offset, int64(segment.length))
	_, err = buf.ReadFrom(sr)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF && needDownload {
		client.log.Error("failed to read file, downloading instead", "err", err)
		return client.downloadLogicalSegmentData(segment)
	} else if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		client.log.Error("failed to read file locally", "err", err)
		return errors.New("failed to read file locally")
	}
	segment.logicalSegmentData = buf.buf

	return nil
}

// cleanupUploadSegment will check the state of the segment and perform any
// cleanup required. This can include returning memory and releasing the segment
// from the map of active segments in the segment heap.
func (client *StorageClient) cleanupUploadSegment(uc *unfinishedUploadSegment) {
	uc.mu.Lock()
	sectorsAvailable := 0
	var memoryReleased uint64
	// Release any unnecessary sectors if they are not uploading or uploaded
	for i := 0; i < len(uc.sectorSlotsStatus); i++ {
		if uc.sectorSlotsStatus[i] {
			continue
		}

		if sectorsAvailable >= uc.workersRemain {
			memoryReleased += storage.SectorSize
			uc.physicalSegmentData[i] = nil

			// Mark this sector as true when released memory
			uc.sectorSlotsStatus[i] = true
		} else {
			sectorsAvailable++
		}
	}

	// Segment needs to be removed if it is complete, but hasn't been released
	segmentComplete := uc.IsSegmentUploadComplete()
	released := uc.released

	// If required, remove the segment from the set of repairing segments.
	if segmentComplete && !released {
		uc.released = true
		client.updateUploadSegmentStuckStatus(uc)
		client.uploadHeap.mu.Lock()
		delete(client.uploadHeap.pendingSegments, uc.id)
		client.uploadHeap.mu.Unlock()
	}

	uc.memoryReleased += uint64(memoryReleased)
	totalMemoryReleased := uc.memoryReleased
	uc.mu.Unlock()

	if sectorsAvailable > 0 {
		uc.notifyBackupWorkers()
	}

	// If required, return the memory to the storage client.
	if memoryReleased > 0 {
		client.memoryManager.Return(memoryReleased)
	}

	// Sanity check - all memory should be released if the segment is complete.
	if segmentComplete && totalMemoryReleased != uc.memoryNeeded {
		client.log.Info("No workers remaining, but not all memory released", "workersRemain", uc.workersRemain, "sectorsUploadingNum", uc.sectorsUploadingNum, "memoryReleased", uc.memoryReleased, "memoryNeeded", uc.memoryNeeded)
	}

}

// setStuckAndClose sets the unfinishedUploadSegment's stuck status
func (client *StorageClient) setStuckAndClose(uc *unfinishedUploadSegment, stuck bool) error {
	err := uc.fileEntry.SetStuckByIndex(int(uc.index), stuck)
	if err != nil {
		return fmt.Errorf("unable to update Segment stuck status for file %v: %v", uc.fileEntry.DxPath(), err)
	}

	go client.fileSystem.InitAndUpdateDirMetadata(uc.fileEntry.DxPath())

	//err = uc.fileEntry.Close()
	//if err != nil {
	//	return fmt.Errorf("unable to close dx file %v", uc.fileEntry.DxPath())
	//}
	return nil
}

// updateUploadSegmentStuckStatus checks to see if the repair was
// successful and then updates the segment's stuck status
func (client *StorageClient) updateUploadSegmentStuckStatus(uc *unfinishedUploadSegment) {
	// Grab necessary information from upload segment under lock
	index := uc.id.index
	stuck := uc.stuck
	sectorsCompleteNum := uc.sectorsCompletedNum
	sectorsNeedNum := uc.sectorsAllNeedNum
	stuckRepair := uc.stuckRepair

	// Determine if repair was successful
	successfulRepair := (1-RemoteRepairDownloadThreshold)*float64(sectorsNeedNum) <= float64(sectorsCompleteNum)

	// Check if client shut down
	var clientOffline bool
	select {
	case <-client.tm.StopChan():
		clientOffline = true
	default:
		if storage.ENV == storage.EnvTest {
			clientOffline = false
		} else {
			// Check that the storage client is still online
			if !client.Online() {
				clientOffline = true
			}
		}
	}

	// If the repair was unsuccessful and there was a client closed then return
	if !successfulRepair && clientOffline {
		client.log.Info("repair unsuccessful for segment due to client shut down", "unfinishedSegmentID", uc.id)
		return
	}

	if !successfulRepair {
		client.log.Info("repair unsuccessful, marking segment", "unfinishedSegmentID", uc.id, "completePercent", float64(sectorsCompleteNum)/float64(sectorsNeedNum))
	} else {
		client.log.Info("repair successful, marking segment as non-stuck", "unfinishedSegmentID", uc.id)
	}

	if err := uc.fileEntry.SetStuckByIndex(int(index), !successfulRepair); err != nil {
		client.log.Error("could not set segment stuck status for file", "unfinishedSegmentID", uc.id, "dxpath", uc.fileEntry.DxPath(), "err", err)
	}

	dxPath := uc.fileEntry.DxPath()

	if err := client.fileSystem.InitAndUpdateDirMetadata(dxPath); err != nil {
		client.log.Error("update dir meta data failed", "err", err)
	}

	// Check to see if the segment was stuck and now is successfully repaired by the stuck loop
	if stuck && successfulRepair && stuckRepair {
		// Signal the stuck loop that the Segment was successfully repaired
		client.log.Info("Stuck segment successfully repaired", "unfinishedSegmentID", uc.id)
		select {
		case <-client.tm.StopChan():
			client.log.Info("storage client shut down before the stuck loop was signalled that the stuck repair was successful")
			return
		case client.uploadHeap.stuckSegmentSuccess <- dxPath:
		}
	}
}
