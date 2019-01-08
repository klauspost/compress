package zstd

import "fmt"

// fsePredef are the predefined fse tables as defined here:
// https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md#default-distributions
var fsePredef [3]fseDecoder

type baseOffset struct {
	baseLine uint32
	addBits  uint8
}

var symbolTableX [3][]baseOffset

func fillBase(dst []baseOffset, base uint32, bits ...uint8) {
	if len(bits) != len(dst) {
		panic(fmt.Sprintf("len(dst) (%d) != len(bits) (%d)", len(dst), len(bits)))
	}
	for i, bit := range bits {
		dst[i] = baseOffset{
			baseLine: base,
			addBits:  bit,
		}
		base += 1 << bit
	}
}

func init() {
	// Literals length codes
	tmp := make([]baseOffset, 36)
	for i := range tmp[:16] {
		tmp[i] = baseOffset{
			baseLine: uint32(i),
			addBits:  0,
		}
	}
	fillBase(tmp[16:], 16, 1, 1, 1, 1, 2, 2, 3, 3, 4, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16)
	symbolTableX[0] = tmp

	// Match length codes
	tmp = make([]baseOffset, 53)
	for i := range tmp[:32] {
		tmp[i] = baseOffset{
			baseLine: uint32(i) + 3,
			addBits:  0,
		}
	}
	fillBase(tmp[32:], 35, 1, 1, 1, 1, 2, 2, 3, 3, 4, 4, 5, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16)
	symbolTableX[1] = tmp

	// Offset codes
	tmp = make([]baseOffset, 32)
	for i := range tmp[:32] {
		tmp[i] = baseOffset{
			baseLine: 1 << uint(i),
			addBits:  uint8(i),
		}
	}
	symbolTableX[2] = tmp

	for i := range fsePredef[:] {
		f := &fsePredef[i]
		switch i {
		case 0:
			f.actualTableLog = 6
			copy(f.norm[:], []int16{4, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1,
				2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 2, 1, 1, 1, 1, 1,
				-1, -1, -1, -1})
			f.symbolLen = 36
		case 1:
			f.actualTableLog = 6
			copy(f.norm[:], []int16{
				1, 4, 3, 2, 2, 2, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
				1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
				1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, -1, -1,
				-1, -1, -1, -1, -1})
			f.symbolLen = 53
		case 2:
			f.actualTableLog = 5
			copy(f.norm[:], []int16{
				1, 1, 1, 1, 1, 1, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
				1, 1, 1, 1, 1, 1, 1, 1, -1, -1, -1, -1, -1})
			f.symbolLen = 29
		}
		if err := f.buildDtable(); err != nil {
			panic(fmt.Errorf("building table %d: %v", i, err))
		}
		if err := f.transform(symbolTableX[i]); err != nil {
			panic(fmt.Errorf("building table %d: %v", i, err))
		}
	}
}
