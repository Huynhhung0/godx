// Copyright 2019 DxChain, All rights reserved.
// Use of this source code is governed by an Apache
// License 2.0 that can be found in the LICENSE file.
package dxfile

import (
	"crypto/rand"
	"fmt"
	"github.com/DxChainNetwork/godx/crypto"
	"github.com/DxChainNetwork/godx/p2p/enode"
	"os"
	"sync"
	"time"

	"github.com/DxChainNetwork/godx/common"
	"github.com/DxChainNetwork/godx/common/writeaheadlog"
	"github.com/DxChainNetwork/godx/storage/storageclient/erasurecode"
)

const (
	fileIDSize = 16

	// SectorSize is the size of a sector, which is 4MiB
	SectorSize = uint64(1 << 22)
)

type (
	DxFile struct {
		// metadata is the persist metadata
		metadata *Metadata

		// hostTable is the map of host address to whether the address is used
		hostTable hostTable

		// TODO: create a indexed list structure for the segments
		// segments is a list of segments the file is split into
		segments []*segment

		// utils field
		deleted bool
		lock    sync.RWMutex
		ID      fileID
		wal     *writeaheadlog.Wal

		// filePath is full file path
		filePath string

		//cached field
		erasureCode erasurecode.ErasureCoder
		cipherKey   crypto.CipherKey
	}

	// hostTable is the map from host address to specific host info
	hostTable map[enode.ID]bool

	// segment is the Data for a segment, which is composed of several sectors
	segment struct {
		sectors [][]*sector
		index   uint64
		offset  uint64
		stuck   bool
	}

	// sector is the Data for a single sector, which has Data of merkle root and related host address
	sector struct {
		merkleRoot  common.Hash
		hostAddress enode.ID
	}

	fileID [fileIDSize]byte
)

// New creates a new dxfile
func New(filePath string, dxPath string, sourcePath string, wal *writeaheadlog.Wal, erasureCode erasurecode.ErasureCoder, cipherKey crypto.CipherKey, fileSize uint64, fileMode os.FileMode) (*DxFile, error) {
	currentTime := uint64(time.Now().Unix())
	minSectors, numSectors, extra := erasureCodeToParams(erasureCode)
	var id fileID
	_, err := rand.Read(id[:])
	if err != nil {
		return nil, fmt.Errorf("cannot create a random id: %v", err)
	}
	md := &Metadata{
		Version:         "1.0.0",
		HostTableOffset: PageSize,
		SegmentOffset:   2 * PageSize,
		FileSize:        fileSize,
		SectorSize:      SectorSize - uint64(cipherKey.Overhead()),
		PagesPerSegment: segmentPersistNumPages(numSectors),
		LocalPath:       sourcePath,
		DxPath:          dxPath,
		CipherKeyCode:   crypto.CipherCodeByName(cipherKey.CodeName()),
		CipherKey:       cipherKey.Key(),
		TimeModify:      currentTime,
		TimeCreate:      currentTime,
		FileMode:        fileMode,
		ErasureCodeType: erasureCode.Type(),
		MinSectors:      minSectors,
		NumSectors:      numSectors,
		ECExtra:         extra,
	}
	df := &DxFile{
		metadata:    md,
		hostTable:   make(map[enode.ID]bool),
		deleted:     false,
		ID:          id,
		wal:         wal,
		filePath:    filePath,
		erasureCode: erasureCode,
		cipherKey:   cipherKey,
	}
	df.segments = make([]*segment, md.numSegments())
	for i := range df.segments {
		df.segments[i] = &segment{sectors: make([][]*sector, numSectors), index: uint64(i)}
	}
	return df, df.saveAll()
}

//func (df *DxFile) AddSector(){
//
//}
