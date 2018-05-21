package zstd

import (
	"bufio"
	"errors"
	"io"
)

type BlockType uint8

const (
	BlockTypeRaw BlockType = iota
	BlockTypeRLE
	BlockTypeCompressed
	BlockTypeReserved
)

var (
	// ErrReservedBlockType is returned when a reserved block type is found.
	// Typically this indicates wrong or corrupted input.
	ErrReservedBlockType = errors.New("invalid input: reserved block type encountered")

	// ErrReservedBlockType is returned when a reserved block type is found.
	// Typically this indicates wrong or corrupted input.
	ErrCompressedSizeTooBig = errors.New("invalid input: compressed size too big")
)

type dBlock struct {
	data       []byte
	WindowSize uint64
	Type       BlockType
	Last       bool
}

func newDBlock(br *bufio.Reader, windowSize uint64) (*dBlock, error) {
	d := dBlock{WindowSize: windowSize}
	return &d, d.reset(br)
}

// reset will reset the block.
// Input must be a start of a block and will be at the end of the block when returned.
func (b *dBlock) reset(br *bufio.Reader) error {
	var tmp [4]byte
	_, err := io.ReadFull(br, tmp[:3])
	if err != nil {
		return err
	}
	bh := uint32(tmp[0]) | (uint32(tmp[1]) << 8) | (uint32(tmp[2]) << 16)
	b.Last = bh&1 != 0
	b.Type = BlockType((bh >> 1) & 3)
	if b.Type == BlockTypeReserved {
		return ErrReservedBlockType
	}
	// find compressed size.
	cSize := int(bh >> 3)
	if cSize > (128<<10) || uint64(cSize) > b.WindowSize {
		return ErrCompressedSizeTooBig
	}

	// Read block data.
	if cap(b.data) < cSize {
		b.data = make([]byte, 0, 128<<10)
	}
	b.data = b.data[:cSize]
	// Read all.
	_, err = io.ReadFull(br, b.data)
	if err != nil {
		return err
	}
	return nil
}
