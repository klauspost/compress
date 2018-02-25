package huff0

import (
	"errors"
	"fmt"

	"github.com/klauspost/compress/fse"
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
	prevTable      cTable
	cTable         cTable
	nodes          []nodeElt
	bw             [4]*bitWriter
	tmpOut         [4][]byte
	fse            *fse.Scratch
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
	if s.fse == nil {
		s.fse = &fse.Scratch{}
	}
	s.nodes = s.nodes[:0]
	s.br.init(in)

	return s, nil
}

type cTable []cTableEntry

func (c cTable) write(s *Scratch) error {
	var (
		// precomputed conversion table
		bitsToWeight   [tableLogMax + 1]byte
		huffWeight     [maxSymbolValue]byte // TODO: Move to scratch?
		huffLog        = s.actualTableLog
		maxSymbolValue = uint8(s.symbolLen - 1)
	)
	const (
		maxFSETableLog = 6
	)
	// convert to weight
	bitsToWeight[0] = 0
	for n := uint8(1); n < huffLog+1; n++ {
		bitsToWeight[n] = huffLog + 1 - n
	}

	// Acquire histogram for FSE.
	hist, finished := s.fse.Histogram()
	hist = hist[:256]
	for i := range hist {
		hist[i] = 0
	}
	huffMax := uint8(0)
	for n := uint8(0); n < maxSymbolValue; n++ {
		v := bitsToWeight[c[n].nBits]
		huffWeight[n] = v
		hist[v]++
		if v > huffMax {
			huffMax = v
		}
	}
	// FSE compress if feasible.
	if maxSymbolValue >= 2 {
		huffMaxCnt := uint32(0)
		for _, v := range hist[:int(huffMax+1)] {
			if v > huffMaxCnt {
				huffMaxCnt = v
			}
		}
		finished(huffMax, int(huffMaxCnt))
		s.fse.TableLog = maxFSETableLog
		b, err := fse.Compress(huffWeight[:maxSymbolValue], s.fse)
		if err == nil && len(b) < int(s.symbolLen>>1) {
			s.Out = append(s.Out, uint8(len(b)))
			s.Out = append(s.Out, b...)
			return nil
		}
	}
	// write raw values as 4-bits (max : 15)
	if maxSymbolValue > (256 - 128) {
		// should not happen : likely means source cannot be compressed
		return ErrIncompressible
	}
	op := s.Out
	// special case, pack weights 4 bits/weight.
	op = append(op, 128|(maxSymbolValue-1))
	// be sure it doesn't cause msan issue in final combination
	huffWeight[maxSymbolValue] = 0
	for n := uint16(0); n < uint16(maxSymbolValue); n += 2 {
		op = append(op, (huffWeight[n]<<4)|huffWeight[n+1])
	}
	s.Out = op
	return nil
}

// estimateSize returns the estimated size in bytes of the input represented in the
// histogram supplied.
func (c cTable) estimateSize(hist []uint32) int {
	nbBits := uint32(7)
	for i, v := range c[:len(hist)] {
		nbBits += uint32(v.nBits) * hist[i]
	}
	return int(nbBits >> 3)
}
