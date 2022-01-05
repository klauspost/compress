// Copyright (c) 2022+ Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	S2IndexHeader   = "s2idx\x00"
	S2IndexTrailer  = "\x00xdi2s"
	maxIndexEntries = 1 << 16
)

type index struct {
	info []struct {
		compressedOffset   int64
		uncompressedOffset int64
	}
	estBlockUncomp    int64
	totalUncompressed int64
	totalCompressed   int64
}

func (i *index) Reset(maxBlock int) {
	i.estBlockUncomp = int64(maxBlock)
	// We do not write the first.
	if len(i.info) > 0 {
		i.info = i.info[:0]
	}
}

func (i *index) Close() {
	i.Reset(0)
}

// AllocInfos will allocate an empty slice of infos.
func (i *index) AllocInfos(n int) {
	if n > maxIndexEntries {
		panic("n > maxIndexEntries")
	}
	i.info = make([]struct {
		compressedOffset   int64
		uncompressedOffset int64
	}, 0, n)
}

func (i *index) Add(compressedOffset, uncompressedOffset int64) error {
	if i == nil {
		return nil
	}
	lastIdx := len(i.info) - 1
	if lastIdx >= 0 {
		latest := i.info[lastIdx]
		if latest.uncompressedOffset == uncompressedOffset {
			// Uncompressed didn't change, don't add entry,
			// but update start index.
			latest.compressedOffset = compressedOffset
			i.info[lastIdx] = latest
			return nil
		}
		if latest.uncompressedOffset > uncompressedOffset {
			return fmt.Errorf("internal error: Earlier uncompressed received (%d > %d)", latest.uncompressedOffset, uncompressedOffset)
		}
	}
	i.info = append(i.info, struct {
		compressedOffset   int64
		uncompressedOffset int64
	}{compressedOffset: compressedOffset, uncompressedOffset: uncompressedOffset})
	return nil
}

func (i *index) Reduce() {
	fmt.Println("entries:", len(i.info))
	if len(i.info) < maxIndexEntries {
		return
	}
	// Algorithm, keep 1, remove removeN entries...
	removeN := (len(i.info) + maxIndexEntries - 1) / maxIndexEntries
	src := i.info
	i.AllocInfos(len(i.info) / removeN)
	for idx := 0; idx < len(src); idx++ {
		i.info = append(i.info, src[idx])
		idx += removeN
	}
	// Update maxblock estimate.
	i.estBlockUncomp += i.estBlockUncomp * int64(removeN)
	fmt.Println("entries after:", len(i.info))
}

func (i *index) AppendTo(b []byte, uncompTotal, compTotal int64) []byte {
	i.Reduce()
	var tmp [binary.MaxVarintLen64]byte

	initSize := len(b)
	// We make the start a skippable header+size.
	b = append(b, ChunkTypeIndex, 0, 0, 0)
	b = append(b, []byte(S2IndexHeader)...)
	// Total Uncompressed size
	n := binary.PutVarint(tmp[:], uncompTotal)
	b = append(b, tmp[:n]...)
	// Total Compressed size
	n = binary.PutVarint(tmp[:], compTotal)
	b = append(b, tmp[:n]...)
	// Put EstBlockUncomp size
	n = binary.PutVarint(tmp[:], i.estBlockUncomp)
	b = append(b, tmp[:n]...)
	// Put length
	n = binary.PutVarint(tmp[:], int64(len(i.info)))
	b = append(b, tmp[:n]...)
	// Initial compressed size estimate.
	cPredict := i.estBlockUncomp / 2
	// Add each entry
	for idx, info := range i.info {
		uOff := info.uncompressedOffset
		cOff := info.compressedOffset
		if idx > 0 {
			prev := i.info[idx-1]
			uOff -= prev.uncompressedOffset + (i.estBlockUncomp)
			cOff -= prev.compressedOffset + cPredict
			// Update compressed size prediction, with half the error.
			cPredict += cOff / 2
		}
		fmt.Println(info.uncompressedOffset, "->", info.compressedOffset, "encoded:", uOff, cOff)
		n = binary.PutVarint(tmp[:], uOff)
		b = append(b, tmp[:n]...)
		n = binary.PutVarint(tmp[:], cOff)
		b = append(b, tmp[:n]...)
	}
	// Add Total Size.
	// Stored as fixed size for easier reading.
	binary.LittleEndian.PutUint32(tmp[:], uint32(len(b)-initSize+4+len(S2IndexTrailer)))
	b = append(b, tmp[:4]...)
	// Trailer
	b = append(b, []byte(S2IndexTrailer)...)

	// Update size
	chunkLen := len(b) - initSize - skippableFrameHeader
	b[initSize+1] = uint8(chunkLen >> 0)
	b[initSize+2] = uint8(chunkLen >> 8)
	b[initSize+3] = uint8(chunkLen >> 16)
	fmt.Printf("chunklen: 0x%x Uncomp:%d, Comp:%d\n", chunkLen, uncompTotal, compTotal)
	return b
}

func (i *index) Load(b []byte) ([]byte, error) {
	if len(b) <= 4+len(S2IndexHeader)+len(S2IndexTrailer) {
		return b, io.ErrUnexpectedEOF
	}
	if b[0] != ChunkTypeIndex {
		return b, ErrCorrupt
	}
	chunkLen := int(b[1]) | int(b[2])<<8 | int(b[3])<<16
	b = b[4:]

	// Validate we have enough...
	if len(b) < chunkLen {
		return b, io.ErrUnexpectedEOF
	}
	if !bytes.Equal(b[:len(S2IndexHeader)], []byte(S2IndexHeader)) {
		return b, ErrUnsupported
	}
	b = b[len(S2IndexHeader):]

	// Total Uncompressed
	if v, n := binary.Varint(b); n <= 0 {
		return b, ErrCorrupt
	} else {
		i.totalUncompressed = v
		b = b[n:]
	}

	// Total Uncompressed
	if v, n := binary.Varint(b); n <= 0 {
		return b, ErrCorrupt
	} else {
		i.totalUncompressed = v
		b = b[n:]
	}

	// Read EstBlockUncomp
	if v, n := binary.Varint(b); n <= 0 {
		return b, ErrCorrupt
	} else {
		if v < 0 {
			return b, ErrUnsupported
		}
		i.estBlockUncomp = v
		b = b[n:]
	}

	var entries int
	if v, n := binary.Varint(b); n <= 0 {
		return b, ErrCorrupt
	} else {
		if v < 0 || v > maxIndexEntries {
			return b, ErrUnsupported
		}
		entries = int(v)
		b = b[n:]
	}
	if cap(i.info) < entries {
		i.AllocInfos(entries)
	}
	i.info = i.info[:entries]

	// Initial compressed size estimate.
	cPredict := i.estBlockUncomp / 2
	// Add each entry
	for idx := range i.info {
		var uOff, cOff int64
		if v, n := binary.Varint(b); n <= 0 {
			return b, ErrCorrupt
		} else {
			if v > maxIndexEntries {
				return b, ErrUnsupported
			}
			uOff = v
			b = b[n:]
		}
		if v, n := binary.Varint(b); n <= 0 {
			return b, ErrCorrupt
		} else {
			if v > maxIndexEntries {
				return b, ErrUnsupported
			}
			cOff = v
			b = b[n:]
		}

		if idx > 0 {
			// Update compressed size prediction, with half the error.
			cPredictNew := cPredict + cOff/2

			prev := i.info[idx-1]
			uOff += prev.uncompressedOffset + (i.estBlockUncomp)
			cOff += prev.compressedOffset + cPredict
			cPredict = cPredictNew
		}
		fmt.Println(uOff, "->", cOff)
		i.info[idx] = struct {
			compressedOffset   int64
			uncompressedOffset int64
		}{compressedOffset: cOff, uncompressedOffset: uOff}
	}
	if len(b) < 4+len(S2IndexTrailer) {
		return b, io.ErrUnexpectedEOF
	}
	// Skip size...
	b = b[4:]

	// Check trailer...
	if !bytes.Equal(b[:len(S2IndexTrailer)], []byte(S2IndexTrailer)) {
		return b, ErrCorrupt
	}
	return b[len(S2IndexTrailer):], nil
}

func (i *index) LoadStream(rs io.ReadSeeker) error {
	// Go to end.
	_, err := rs.Seek(-10, io.SeekEnd)
	if err != nil {
		return err
	}
	var tmp [10]byte
	_, err = io.ReadFull(rs, tmp[:])
	if err != nil {
		return err
	}
	// Check trailer...
	if !bytes.Equal(tmp[4:4+len(S2IndexTrailer)], []byte(S2IndexTrailer)) {
		return ErrUnsupported
	}
	sz := binary.LittleEndian.Uint32(tmp[:4])
	if sz > maxChunkSize+skippableFrameHeader {

	}
	_, err = rs.Seek(-int64(sz), io.SeekEnd)
	if err != nil {
		return err
	}

	// Read index.
	buf := make([]byte, sz)
	_, err = io.ReadFull(rs, buf)
	if err != nil {
		return err
	}
	_, err = i.Load(buf)
	return err
}
