package s2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const SeekIndexV1Header = "s33k01"

type blockInfo struct {
	offset int64 // Absolute offset
	uSize  int32 // Uncompressed size
	cSize  int32 // Compressed size
}

type seekIndex struct {
	blocks []blockInfo
}

func (s seekIndex) encode() []byte {
	estSize := skippableFrameHeader + 4 + 2*len(SeekIndexV1Header) + len(s.blocks)*8
	res := append(make([]byte, 0, estSize), chunkTypePadding, 0, 0, 0)
	res = append(res, []byte(SeekIndexV1Header)...)
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], uint64(len(s.blocks)))
	res = append(res, tmp[:n]...)
	for i, block := range s.blocks {
		if i > 0 {
			// Delta encode the rest.
			block.offset -= s.blocks[i-1].offset
			block.uSize -= s.blocks[i-1].uSize
			block.cSize -= s.blocks[i-1].cSize
		}
		n = binary.PutVarint(tmp[:], block.offset)
		res = append(res, tmp[:n]...)
		n = binary.PutVarint(tmp[:], int64(block.uSize))
		res = append(res, tmp[:n]...)
		n = binary.PutVarint(tmp[:], int64(block.cSize))
		res = append(res, tmp[:n]...)
	}
	// Append current length as LE uint32
	binary.LittleEndian.PutUint32(tmp[:4], uint32(len(res)))
	res = append(res, tmp[:4]...)
	// End with "header" to confirm.
	res = append(res, []byte(SeekIndexV1Header)...)
	f := uint32(len(res) - skippableFrameHeader)
	if f > maxSkippableChuckSize {
		return nil
	}
	// Add chunk length.
	res[1] = uint8(f)
	res[2] = uint8(f >> 8)
	res[3] = uint8(f >> 16)

	return res
}

func (s seekIndex) validate() error {
	for i, block := range s.blocks {
		if i > 0 {
			if block.offset >= s.blocks[i-1].offset {
				return errors.New("block offset not in order")
			}
			if s.blocks[i-1].offset+int64(s.blocks[i-1].cSize) > block.offset {
				return errors.New("overlapping blocks detected")
			}
		}
		if block.offset <= 0 {
			return errors.New("offset <= 0")
		}
		if block.uSize > maxBlockSize || block.uSize < 0 {
			return errors.New("uSize has invalid size")
		}
		if block.cSize > int32(MaxEncodedLen(maxBlockSize)) || block.cSize < 0 {
			return errors.New("cSize has invalid size")
		}
	}
	return nil
}

// addBlock will add a block.
// blocks must be added in order.
func (s *seekIndex) addBlock(i blockInfo) {
	s.blocks = append(s.blocks, i)
}

func decodeSeekIndex(b []byte) (*seekIndex, error) {
	if len(b) <= len(SeekIndexV1Header)*2+4+skippableFrameHeader {
		return nil, io.ErrUnexpectedEOF
	}
	if b[0] != chunkTypePadding {
		return nil, errors.New("unknown chunk type")
	}
	chunkSize := int(b[1]) | int(b[2])<<8 | int(b[3])<<16
	b = b[4:]
	if len(b) < chunkSize {
		return nil, io.ErrUnexpectedEOF
	}
	// Truncate any extra data given...
	b = b[:chunkSize]

	// Check front header
	if !bytes.Equal(b[:len(SeekIndexV1Header)], []byte(SeekIndexV1Header)) {
		return nil, errors.New("unknown header")
	}

	// Check end of payload.
	wantSize := uint32(len(b) - len(SeekIndexV1Header) - 4)
	bEnd := b[wantSize:]
	if !bytes.Equal(bEnd[4:], []byte(SeekIndexV1Header)) {
		return nil, io.ErrUnexpectedEOF
	}
	gotSize := binary.LittleEndian.Uint32(bEnd[:4])
	if gotSize != wantSize {
		return nil, fmt.Errorf("unexpected size, expected %d, got %d", wantSize, gotSize)
	}

	// Read blocks
	blocks, n := binary.Uvarint(b)
	if n <= 0 {
		return nil, io.ErrUnexpectedEOF
	}
	b = b[n:]
	s := seekIndex{blocks: make([]blockInfo, blocks)}

	for i := range s.blocks {
		var block blockInfo
		block.offset, n = binary.Varint(b)
		if n <= 0 {
			return nil, io.ErrUnexpectedEOF
		}
		b = b[n:]
		var sz int64
		sz, n = binary.Varint(b)
		if n <= 0 {
			return nil, io.ErrUnexpectedEOF
		}
		b = b[n:]
		if sz > math.MaxInt32 || sz < math.MinInt32 {
			return nil, errors.New("uSize overflow")
		}
		block.uSize = int32(sz)
		sz, n = binary.Varint(b)
		if n <= 0 {
			return nil, io.ErrUnexpectedEOF
		}
		b = b[n:]
		if sz > math.MaxInt32 || sz < math.MinInt32 {
			return nil, errors.New("size overflow")
		}
		block.cSize = int32(sz)

		if i > 0 {
			block.offset += s.blocks[i-i].offset
			block.cSize += s.blocks[i-i].cSize
			block.uSize += s.blocks[i-i].uSize
		}

		s.blocks[i] = block
	}
	return &s, nil
}
