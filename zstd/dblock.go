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

	// Maximum possible block size (all Raw+Uncompressed).
	maxBlockSize = (1 << 21) - 1
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
	dst        []byte
	WindowSize uint64
	Type       BlockType
	RLESize    uint32
	Last       bool
	// Use less memory
	lowMem  bool
	history chan *buffer
	input   chan struct{}
	result  chan decodeOutput
}

func (b *dBlock) String() string {
	if b == nil {
		return "<nil>"
	}
	return fmt.Sprintf("Steam Size: %d, Type: %v, Last: %t, Window: %d", len(b.data), b.Type, b.Last, b.WindowSize)
}

func newDBlock(lowMem bool) *dBlock {
	b := dBlock{
		lowMem:  lowMem,
		result:  make(chan decodeOutput, 1),
		input:   make(chan struct{}, 1),
		history: make(chan *buffer, 1),
	}
	go b.startDecoder()
	return &b
}

// reset will reset the block.
// Input must be a start of a block and will be at the end of the block when returned.
func (b *dBlock) reset(br io.Reader, windowSize uint64) error {
	b.WindowSize = windowSize
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
		if b.lowMem {
			b.data = make([]byte, 0, maxBlockSize)
		} else {
			b.data = make([]byte, 0, cSize)
		}
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
	// Start decoding
	b.input <- struct{}{}
	return nil
}

// Close will release resources.
// Closed dBlock cannot be reset.
func (b *dBlock) Close() {
	close(b.input)
	close(b.history)
	close(b.result)
}

// decode will prepare decoding the block when it receives the history.
func (b *dBlock) startDecoder() {
	for {
		_, ok := <-b.input
		if !ok {
			return
		}
		fmt.Println("dBlock: Got block input")
		switch b.Type {
		case BlockTypeRLE:
			if cap(b.dst) < int(b.RLESize) {
				if b.lowMem {
					b.dst = make([]byte, b.RLESize)
				} else {
					b.dst = make([]byte, maxBlockSize)
				}
			}
			o := decodeOutput{
				d:   b,
				b:   b.dst[:b.RLESize],
				err: nil,
			}
			v := b.data[0]
			for i := range o.b {
				o.b[i] = v
			}
			<-b.history
			// TODO: add rle to hist.
			// TODO: We should check if result is closed.
			b.result <- o
		case BlockTypeRaw:
			o := decodeOutput{
				d:   b,
				b:   b.data,
				err: nil,
			}
			<-b.history
			// TODO: add block to history.
			b.result <- o
		case BlockTypeCompressed:
			_ = <-b.history
			o := decodeOutput{
				d:   b,
				b:   nil,
				err: errNotimplemented,
			}
			b.result <- o
		default:
			panic("Invalid block type")
		}
		fmt.Println("dBlock: Finished block")
	}
}
