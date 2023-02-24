// Copyright (c) 2022+ Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"bytes"
	"encoding/binary"
	"sync"
)

const (
	// MinDictSize is the minimum dictionary size when repeat has been read.
	MinDictSize = 16

	// MaxDictSize is the maximum dictionary size when repeat has been read.
	MaxDictSize = 65536

	// MaxDictSrcOffset is the maximum offset where a dictionary entry can start.
	MaxDictSrcOffset = 65535
)

// Dict contains a dictionary that can be used for encoding and decoding s2
type Dict struct {
	dict   []byte
	repeat int // Repeat as index of dict

	fast, better, best sync.Once
	fastTable          *[1 << 14]uint16

	betterTableShort *[1 << 14]uint16
	betterTableLong  *[1 << 17]uint16

	bestTableShort *[1 << 16]uint32
	bestTableLong  *[1 << 19]uint32
}

// NewDict will read a dictionary.
// It will return nil if the dictionary is invalid.
func NewDict(dict []byte) *Dict {
	if len(dict) == 0 {
		return nil
	}
	var d Dict
	// Repeat is the first value of the dict
	r, n := binary.Uvarint(dict)
	if n <= 0 {
		return nil
	}
	dict = dict[n:]
	d.dict = dict
	if cap(d.dict) < len(d.dict)+16 {
		d.dict = append(make([]byte, 0, len(d.dict)+16), d.dict...)
	}
	if len(dict) < MinDictSize || len(dict) > MaxDictSize {
		return nil
	}
	d.repeat = int(r)
	if d.repeat > len(dict) {
		return nil
	}
	return &d
}

// Bytes will return a serialized version of the dictionary.
// The output can be sent to NewDict.
func (d *Dict) Bytes() []byte {
	dst := make([]byte, binary.MaxVarintLen16+len(d.dict))
	return append(dst[:binary.PutUvarint(dst, uint64(d.repeat))], d.dict...)
}

// MakeDict will create a dictionary.
// 'data' must be between MinDictSize and MaxDictSize (inclusive).
// If searchStart is set the start repeat value will be set to the last
// match of this content.
// If no matches are found, it will attempt to find shorter matches.
// This content should match the typical start of a block.
// If at least 4 bytes cannot be matched, repeat is set to start of block.
func MakeDict(data []byte, searchStart []byte) *Dict {
	if len(data) == 0 {
		return nil
	}
	var d Dict
	dict := data
	d.dict = dict
	if cap(d.dict) < len(d.dict)+16 {
		d.dict = append(make([]byte, 0, len(d.dict)+16), d.dict...)
	}
	if len(dict) < MinDictSize || len(dict) > MaxDictSize {
		return nil
	}

	// Find the longest match possible, last entry if multiple.
	for s := len(searchStart); s > 4; s-- {
		if idx := bytes.LastIndex(data, searchStart[:s]); idx >= 0 && idx <= len(data)-8 {
			d.repeat = idx
			break
		}
	}

	return &d
}

func (d *Dict) initFast() {
	d.fast.Do(func() {
		const (
			tableBits    = 14
			maxTableSize = 1 << tableBits
		)

		var table [maxTableSize]uint16
		// We stop so any entry of length 8 can always be read.
		for i := 0; i < len(d.dict)-8-2; i += 3 {
			x0 := load64(d.dict, i)
			h0 := hash6(x0, tableBits)
			h1 := hash6(x0>>8, tableBits)
			h2 := hash6(x0>>16, tableBits)
			table[h0] = uint16(i)
			table[h1] = uint16(i + 1)
			table[h2] = uint16(i + 2)
		}
		d.fastTable = &table
	})
}

func (d *Dict) initBetter() {
	d.best.Do(func() {
		const (
			// Long hash matches.
			lTableBits    = 17
			maxLTableSize = 1 << lTableBits

			// Short hash matches.
			sTableBits    = 14
			maxSTableSize = 1 << sTableBits
		)

		var lTable [maxLTableSize]uint16
		var sTable [maxSTableSize]uint16

		// We stop so any entry of length 8 can always be read.
		for i := 0; i < len(d.dict)-8; i++ {
			cv := load64(d.dict, i)
			lTable[hash7(cv, lTableBits)] = uint16(i)
			sTable[hash4(cv, sTableBits)] = uint16(i)
		}
		d.betterTableShort = &sTable
		d.betterTableLong = &lTable
	})
}

func (d *Dict) initBest() {
	d.best.Do(func() {
		const (
			// Long hash matches.
			lTableBits    = 19
			maxLTableSize = 1 << lTableBits

			// Short hash matches.
			sTableBits    = 16
			maxSTableSize = 1 << sTableBits
		)

		var lTable [maxLTableSize]uint32
		var sTable [maxSTableSize]uint32

		// We stop so any entry of length 8 can always be read.
		for i := 0; i < len(d.dict)-8; i++ {
			cv := load64(d.dict, i)
			hashL := hash8(cv, lTableBits)
			hashS := hash4(cv, sTableBits)
			candidateL := lTable[hashL]
			candidateS := sTable[hashS]
			lTable[hashL] = uint32(i) | candidateL<<16
			sTable[hashS] = uint32(i) | candidateS<<16
		}
		d.bestTableShort = &sTable
		d.bestTableLong = &lTable
	})
}
