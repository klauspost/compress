package zstd

import (
	"errors"
	"fmt"
	"io"
)

type BlockType uint8

//go:generate stringer -type=BlockType

const (
	BlockTypeRaw BlockType = iota
	BlockTypeRLE
	BlockTypeCompressed
	BlockTypeReserved

	// maxCompressedBlockSize is the biggest allowed compressed block size (128KB)
	maxCompressedBlockSize = 128 << 10
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
	RLESize    uint32
	Last       bool
}

func (b *dBlock) String() string {
	if b == nil {
		return "<nil>"
	}
	return fmt.Sprintf("Steam Size: %d, Type: %v, Last: %t, Window: %d", len(b.data), b.Type, b.Last, b.WindowSize)
}

func newDBlock(br io.Reader, windowSize uint64) (*dBlock, error) {
	d := dBlock{WindowSize: windowSize}
	return &d, d.reset(br)
}

// reset will reset the block.
// Input must be a start of a block and will be at the end of the block when returned.
func (b *dBlock) reset(br io.Reader) error {
	var tmp [4]byte
	_, err := io.ReadFull(br, tmp[:3])
	if err != nil {
		if debug {
			fmt.Println("Reading block header:", err)
		}
		return err
	}
	bh := uint32(tmp[0]) | (uint32(tmp[1]) << 8) | (uint32(tmp[2]) << 16)
	b.Last = bh&1 != 0
	b.Type = BlockType((bh >> 1) & 3)
	// find size.
	cSize := int(bh >> 3)
	switch b.Type {
	case BlockTypeReserved:
		return ErrReservedBlockType
	case BlockTypeRLE:
		b.RLESize = uint32(cSize)
		cSize = 1
	//TODO: Add simple path
	case BlockTypeCompressed:
		if debug {
			fmt.Println("Data size on stream:", cSize)
		}
		b.RLESize = 0
		if cSize > maxCompressedBlockSize || uint64(cSize) > b.WindowSize {
			if debug {
				fmt.Printf("compressed block too big: %+v\n", b)
			}
			return ErrCompressedSizeTooBig
		}
	default:
		b.RLESize = 0
	}

	// Read block data.
	if cap(b.data) < cSize {
		b.data = make([]byte, 0, cSize)
	}
	b.data = b.data[:cSize]
	// Read all.
	_, err = io.ReadFull(br, b.data)
	if err != nil {
		if debug {
			fmt.Println("Reading block:", err)
		}
		return err
	}
	return nil
}
