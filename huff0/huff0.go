package huff0

import (
	"errors"
	"fmt"
)

const (
	maxSymbolValue  = 255
	tableLogMax     = 12
	tableLogDefault = 11
	minTablelog     = 5
	cTableBound     = 129
	huffNodesLen    = 512

	// BlockSizeMax is maximum input size for a single block compressed.
	BlockSizeMax = 128 << 10
)

var (
	// ErrIncompressible is returned when input is judged to be too hard to compress.
	ErrIncompressible = errors.New("input is not compressible")

	// ErrUseRLE is returned from the compressor when the input is a single byte value repeated.
	ErrUseRLE = errors.New("input is single value repeated")
)

type Scratch struct {
	count [maxSymbolValue + 1]uint32

	// Per block parameters.
	// These can be used to override compression parameters of the block.
	// Do not touch, unless you know what you are doing.

	// Out is output buffer.
	// If the scratch is re-used before the caller is done processing the output,
	// set this field to nil.
	// Otherwise the output buffer will be re-used for next Compression/Decompression step
	// and allocation will be avoided.
	Out []byte

	// MaxSymbolValue will override the maximum symbol value of the next block.
	MaxSymbolValue uint8

	// TableLog will attempt to override the tablelog for the next block.
	TableLog uint8

	allowReuse     bool
	br             byteReader
	symbolLen      uint16 // Length of active part of the symbol table.
	maxCount       int    // count of the most probable symbol
	clearCount     bool   // clear count
	actualTableLog uint8  // Selected tablelog.
	prevTable      []cTableEntry
	cTable         []cTableEntry
	nodes          []nodeElt
}

func (s *Scratch) prepare(in []byte) (*Scratch, error) {
	if s == nil {
		s = &Scratch{}
	}
	if s.MaxSymbolValue == 0 {
		s.MaxSymbolValue = maxSymbolValue
	}
	if s.TableLog == 0 {
		s.TableLog = tableLogDefault
	}
	if s.TableLog > tableLogMax {
		return nil, fmt.Errorf("tableLog (%d) > maxTableLog (%d)", s.TableLog, tableLogMax)
	}
	if cap(s.Out) == 0 {
		s.Out = make([]byte, 0, len(in))
	}
	if cap(s.cTable) < maxSymbolValue+1 {
		s.cTable = make([]cTableEntry, 0, maxSymbolValue+1)
	}
	if cap(s.nodes) < huffNodesLen+1 {
		s.nodes = make([]nodeElt, 0, huffNodesLen+1)
	}
	s.nodes = s.nodes[:0]
	s.br.init(in)

	return s, nil
}
