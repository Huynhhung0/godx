// Copyright 2019 DxChain, All rights reserved.
// Use of this source code is governed by an Apache
// License 2.0 that can be found in the LICENSE file.

package newstoragemanager

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/DxChainNetwork/godx/common"
	"github.com/DxChainNetwork/godx/common/writeaheadlog"
	"github.com/DxChainNetwork/godx/crypto/merkle"
	"github.com/DxChainNetwork/godx/storage"
	"github.com/syndtr/goleveldb/leveldb/util"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddSector(t *testing.T) {
	sm := newTestStorageManager(t, "", newDisrupter())
	path := randomFolderPath(t, "")
	size := uint64(1 << 25)
	if err := sm.addStorageFolder(path, size); err != nil {
		t.Fatal(err)
	}
	// Create the sector
	data := randomBytes(storage.SectorSize)
	root := merkle.Root(data)
	if err := sm.addSector(root, data); err != nil {
		t.Fatal(err)
	}
	// Post add sector check
	// The sector shall be stored in db
	sectorID := sm.calculateSectorID(root)
	err := checkSectorExist(sectorID, sm, data, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = checkFoldersHasExpectedSectors(sm, 1); err != nil {
		t.Fatal(err)
	}
	// Create a virtual sector
	if err := sm.addSector(root, data); err != nil {
		t.Fatal(err)
	}
	err = checkSectorExist(sectorID, sm, data, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err = checkFoldersHasExpectedSectors(sm, 1); err != nil {
		t.Fatal(err)
	}
	sm.shutdown(t, 10*time.Millisecond)
	if err = checkWalTxnNum(filepath.Join(sm.persistDir, walFileName), 0); err != nil {
		t.Fatal(err)
	}
}

// TestDisruptedPhysicalAddSector test the case of disrupted during add physical sectors
func TestDisruptedPhysicalAddSector(t *testing.T) {
	tests := []struct {
		keyWord string
	}{
		{"physical process normal"},
		{"physical prepare normal"},
	}
	for _, test := range tests {
		d := newDisrupter().register(test.keyWord, func() bool { return true })
		sm := newTestStorageManager(t, test.keyWord, d)
		path := randomFolderPath(t, test.keyWord)
		size := uint64(1 << 25)
		if err := sm.addStorageFolder(path, size); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		// Create the sector
		data := randomBytes(storage.SectorSize)
		root := merkle.Root(data)
		if err := sm.addSector(root, data); err == nil {
			t.Fatalf("test %v: disrupting does not give error", test.keyWord)
		}
		id := sm.calculateSectorID(root)
		if err := checkSectorNotExist(id, sm); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		if err := checkFoldersHasExpectedSectors(sm, 0); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		sm.shutdown(t, 10*time.Millisecond)
		if err := checkWalTxnNum(filepath.Join(sm.persistDir, walFileName), 0); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
	}
}

// TestDisruptedVirtualAddSector test the case of disrupted during add virtual sectors
func TestDisruptedVirtualAddSector(t *testing.T) {
	tests := []struct {
		keyWord string
	}{
		{"virtual process normal"},
		{"virtual prepare normal"},
	}
	for _, test := range tests {
		d := newDisrupter().register(test.keyWord, func() bool { return true })
		sm := newTestStorageManager(t, test.keyWord, d)
		path := randomFolderPath(t, test.keyWord)
		size := uint64(1 << 25)
		if err := sm.addStorageFolder(path, size); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		// Create the sector
		data := randomBytes(storage.SectorSize)
		root := merkle.Root(data)
		if err := sm.addSector(root, data); err != nil {
			t.Fatalf("test %v: first add sector give error: %v", test.keyWord, err)
		}
		if err := sm.addSector(root, data); err == nil {
			t.Fatalf("test %v: second add sector does not give error: %v", test.keyWord, err)
		}
		id := sm.calculateSectorID(root)
		if err := checkSectorExist(id, sm, data, 1); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		if err := checkFoldersHasExpectedSectors(sm, 1); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		sm.shutdown(t, 10*time.Millisecond)
		if err := checkWalTxnNum(filepath.Join(sm.persistDir, walFileName), 0); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
	}
}

// TestAddSectorStop test the scenario of stop and recover
func TestAddSectorStopRecoverPhysical(t *testing.T) {
	tests := []struct {
		keyWord string
	}{
		{"physical prepare normal stop"},
		{"physical process normal stop"},
	}
	for _, test := range tests {
		d := newDisrupter().register(test.keyWord, func() bool { return true })
		sm := newTestStorageManager(t, test.keyWord, d)
		path := randomFolderPath(t, test.keyWord)
		size := uint64(1 << 25)
		if err := sm.addStorageFolder(path, size); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		data := randomBytes(storage.SectorSize)
		root := merkle.Root(data)
		if err := sm.addSector(root, data); err != nil {
			t.Fatalf("test %v: errStop should not give error: %v", test.keyWord, err)
		}
		id := sm.calculateSectorID(root)
		sm.shutdown(t, 100*time.Millisecond)
		// The update should not be released
		if err := checkWalTxnNum(filepath.Join(sm.persistDir, walFileName), 1); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		newSM, err := New(sm.persistDir)
		if err != nil {
			t.Fatalf("cannot create a new sm: %v", err)
		}
		if err = newSM.Start(); err != nil {
			t.Fatal(err)
		}
		// wait for the update to complete
		<-time.After(100 * time.Millisecond)
		if err := checkSectorNotExist(id, newSM); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		if err := checkFoldersHasExpectedSectors(newSM, 0); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		newSM.shutdown(t, 100*time.Millisecond)
		if err := checkWalTxnNum(filepath.Join(sm.persistDir, walFileName), 0); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
	}
}

// TestAddSectorsStopRecoverVirtual test stopped during adding virtual sectors
func TestAddSectorsStopRecoverVirtual(t *testing.T) {
	tests := []struct {
		keyWord string
	}{
		{"virtual process normal stop"},
		{"virtual prepare normal stop"},
	}
	for _, test := range tests {
		d := newDisrupter().register(test.keyWord, func() bool { return true })
		sm := newTestStorageManager(t, test.keyWord, d)
		path := randomFolderPath(t, test.keyWord)
		size := uint64(1 << 25)
		if err := sm.addStorageFolder(path, size); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		data := randomBytes(storage.SectorSize)
		root := merkle.Root(data)
		id := sm.calculateSectorID(root)
		if err := sm.addSector(root, data); err != nil {
			t.Fatalf("test %v: add physical sector should not give error: %v", test.keyWord, err)
		}
		if err := sm.addSector(root, data); err != nil {
			t.Fatalf("test %v: errStop should not give error: %v", test.keyWord, err)
		}
		sm.shutdown(t, 100*time.Millisecond)
		// The update should not be released
		if err := checkWalTxnNum(filepath.Join(sm.persistDir, walFileName), 1); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		newSM, err := New(sm.persistDir)
		if err != nil {
			t.Fatalf("cannot create a new sm: %v", err)
		}
		if err = newSM.Start(); err != nil {
			t.Fatal(err)
		}
		// wait for the update to complete
		<-time.After(100 * time.Millisecond)
		if err := checkSectorExist(id, newSM, data, 1); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		if err := checkFoldersHasExpectedSectors(newSM, 1); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
		newSM.shutdown(t, 100*time.Millisecond)
		if err := checkWalTxnNum(filepath.Join(sm.persistDir, walFileName), 0); err != nil {
			t.Fatalf("test %v: %v", test.keyWord, err)
		}
	}
}

// TestAddSectorConcurrent test the scenario of multiple goroutines add sector at the same time
func TestAddSectorConcurrent(t *testing.T) {

}

// checkSectorExist checks whether the sector exists
func checkSectorExist(id sectorID, sm *storageManager, data []byte, count uint64) (err error) {
	sector, err := sm.db.getSector(id)
	if err != nil {
		return err
	}
	if sector.count != count {
		return fmt.Errorf("sector count not expected. Got %v, Expect %v", sector.count, count)
	}
	//fmt.Println(1)
	folderID := sector.folderID
	folderPath, err := sm.db.getFolderPath(folderID)
	if err != nil {
		return err
	}
	//fmt.Println(2)
	// DB folder should have expected data
	dbFolder, err := sm.db.loadStorageFolder(folderPath)
	if err != nil {
		return err
	}
	if dbFolder.storedSectors < 1 {
		return fmt.Errorf("folders has no stored sectors")
	}
	if err = dbFolder.setUsedSectorSlot(sector.index); err == nil {
		return fmt.Errorf("folder's %d entry shall be occupied", sector.index)
	}
	//fmt.Println(3)
	// DB folder should have the map from folder id to sector id
	key := makeFolderSectorKey(folderID, id)
	exist, err := sm.db.lvl.Has(key, nil)
	if err != nil {
		return err
	}
	if !exist {
		return fmt.Errorf("folder id to sector id not exist")
	}
	//fmt.Println(4)
	// memoryFolder
	mmFolder, err := sm.folders.get(folderPath)
	if err != nil {
		return err
	}
	defer mmFolder.lock.Unlock()
	//fmt.Println(4.1)
	if err = mmFolder.setUsedSectorSlot(sector.index); err == nil {
		return fmt.Errorf("folders %d entry shall be occupied", sector.index)
	}
	if mmFolder.storedSectors != 1 {
		return fmt.Errorf("folders has not 1 occupied sector %v", dbFolder.storedSectors)
	}
	//fmt.Println(5)
	// check whether the sector data is saved correctly
	b := make([]byte, storage.SectorSize)
	n, err := mmFolder.dataFile.ReadAt(b, int64(sector.index*storage.SectorSize))
	if err != nil {
		return err
	}
	if uint64(n) != storage.SectorSize {
		return fmt.Errorf("read size not equal to sectorSize. Got %v, Expect %v", n, storage.SectorSize)
	}
	if !bytes.Equal(data, b) {
		return fmt.Errorf("data not correctly saved on disk")
	}
	//fmt.Println(6)
	c := make(chan struct{})
	go func() {
		sm.sectorLocks.lockSector(sector.id)
		defer sm.sectorLocks.unlockSector(sector.id)

		close(c)
	}()
	<-time.After(10 * time.Millisecond)
	select {
	case <-c:
	default:
		err = errors.New("sector still locked")
	}
	if err != nil {
		return err
	}
	return nil
}

// checkFoldersHasExpectedSectors checks both in-memory folders and db folders
// has expected number of stored segments
func checkFoldersHasExpectedSectors(sm *storageManager, expect int) (err error) {
	if err = checkExpectStoredSectors(sm.folders.sfs, expect); err != nil {
		return err
	}
	folders, err := sm.db.loadAllStorageFolders()
	if err != nil {
		return err
	}
	if err = checkExpectStoredSectors(folders, expect); err != nil {
		return err
	}
	iter := sm.db.lvl.NewIterator(util.BytesPrefix([]byte(prefixFolderSector)), nil)
	var count int
	for iter.Next() {
		count++
	}
	if count != expect {
		return fmt.Errorf("folders has unexpected stored sectors. Expect %v, Got %v", expect, count)
	}
	return nil
}

// checkSectorNotExist checks whether the sector exists in storage folder
// if exist, return an error
func checkSectorNotExist(id sectorID, sm *storageManager) (err error) {
	exist, err := sm.db.hasSector(id)
	if err != nil {
		return err
	}
	if exist {
		return fmt.Errorf("sector %x shall not exist in storage manager", id)
	}
	iter := sm.db.lvl.NewIterator(util.BytesPrefix([]byte(prefixFolderSector)), nil)
	for iter.Next() {
		key := string(iter.Key())
		if strings.HasSuffix(key, common.Bytes2Hex(id[:])) {
			return fmt.Errorf("sector is registered in folder: %v", key)
		}
	}
	return nil
}

// checkWalTxnNum checks the number of returned transactions is of the expect number
func checkWalTxnNum(path string, numTxn int) (err error) {
	wal, txns, err := writeaheadlog.New(path)
	if err != nil {
		return err
	}
	if len(txns) != numTxn {
		return fmt.Errorf("reopen wal give %v transactions. Expect %v", len(txns), numTxn)
	}
	_, err = wal.CloseIncomplete()
	if err != nil {
		return
	}
	return
}

// checkExpectNumSectors checks whether the total number of sectors in folders are
// consistent with totalNumSectors
func checkExpectStoredSectors(folders map[string]*storageFolder, totalStoredSectors int) (err error) {
	var sum uint64
	for _, sf := range folders {
		sum += sf.storedSectors
	}
	if sum != uint64(totalStoredSectors) {
		return fmt.Errorf("totalStoredSectors not expected. Expect %v, Got %v", totalStoredSectors, sum)
	}
	return nil
}

func randomBytes(size uint64) []byte {
	b := make([]byte, size)
	rand.Read(b)
	return b
}
