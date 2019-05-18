// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package tozstd

import (
	"fmt"
)

var (
	// fsePredefEnc are the predefined fse tables as defined here:
	// https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md#default-distributions
	// These values are already transformed.
	fsePredefEnc [3]fseEncoder

	// maxTableSymbol is the biggest supported symbol for each table type
	// https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md#the-codes-for-literals-lengths-match-lengths-and-offsets
	bitTables = [3][]byte{tableLiteralLengths: llBitsTable[:], tableOffsets: nil, tableMatchLengths: mlBitsTable[:]}
)

type tableIndex uint8

const (
	// indexes for fsePredefEnc and symbolTableX
	tableLiteralLengths tableIndex = 0
	tableOffsets        tableIndex = 1
	tableMatchLengths   tableIndex = 2
)

func init() {
	// Fill predefined compression encoders
	// https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md#default-distributions
	for i := range fsePredefEnc[:] {
		f := &fsePredefEnc[i]
		switch tableIndex(i) {
		case tableLiteralLengths:
			// https://github.com/facebook/zstd/blob/ededcfca57366461021c922720878c81a5854a0a/lib/decompress/zstd_decompress_block.c#L243
			f.actualTableLog = 6
			copy(f.norm[:], []int16{4, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1,
				2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 2, 1, 1, 1, 1, 1,
				-1, -1, -1, -1})
			f.symbolLen = 36
		case tableOffsets:
			// https://github.com/facebook/zstd/blob/ededcfca57366461021c922720878c81a5854a0a/lib/decompress/zstd_decompress_block.c#L281
			f.actualTableLog = 5
			copy(f.norm[:], []int16{
				1, 1, 1, 1, 1, 1, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
				1, 1, 1, 1, 1, 1, 1, 1, -1, -1, -1, -1, -1})
			f.symbolLen = 29
		case tableMatchLengths:
			//https://github.com/facebook/zstd/blob/ededcfca57366461021c922720878c81a5854a0a/lib/decompress/zstd_decompress_block.c#L304
			f.actualTableLog = 6
			copy(f.norm[:], []int16{
				1, 4, 3, 2, 2, 2, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
				1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
				1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, -1, -1,
				-1, -1, -1, -1, -1})
			f.symbolLen = 53
		}
		if err := f.buildCTable(); err != nil {
			panic(fmt.Errorf("building table %v: %v", tableIndex(i), err))
		}
		f.setBits(bitTables[i])
		f.preDefined = true
	}
}
