package tozstd

import "math/bits"

type seq struct {
	litLen   uint32
	offset   uint32
	matchLen uint32
}

type seqCodes struct {
	litLen   []uint8
	offset   []uint8
	matchLen []uint8

	llEnc, ofEnc, mlEnc    *fseEncoder
	llPrev, ofPrev, mlPrev *fseEncoder
}

func (s *seqCodes) initSize(sequences int) {
	// maybe move stuff here
}

func highBit(val uint32) (n uint32) {
	return uint32(bits.Len32(val) - 1)
}

var llCodeTable = [64]byte{0, 1, 2, 3, 4, 5, 6, 7,
	8, 9, 10, 11, 12, 13, 14, 15,
	16, 16, 17, 17, 18, 18, 19, 19,
	20, 20, 20, 20, 21, 21, 21, 21,
	22, 22, 22, 22, 22, 22, 22, 22,
	23, 23, 23, 23, 23, 23, 23, 23,
	24, 24, 24, 24, 24, 24, 24, 24,
	24, 24, 24, 24, 24, 24, 24, 24}

const maxLLCode = 35

// llBitsTable translates from ll code to number of bits.
// TODO: We should probably combine these in a single bigger array to avoid bounds checks
//  or combine this into the fse table.
var llBitsTable = [maxLLCode + 1]byte{
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1, 2, 2, 3, 3,
	4, 6, 7, 8, 9, 10, 11, 12,
	13, 14, 15, 16}

// llCode returns the code that represents the literal length requested.
func llCode(litLength uint32) uint8 {
	const llDeltaCode = 19
	if litLength <= 63 {
		// TODO: check if compiler omits bounds check
		return llCodeTable[litLength]
	}
	return uint8(highBit(litLength)) + llDeltaCode
}

var mlCodeTable = [128]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31,
	32, 32, 33, 33, 34, 34, 35, 35, 36, 36, 36, 36, 37, 37, 37, 37,
	38, 38, 38, 38, 38, 38, 38, 38, 39, 39, 39, 39, 39, 39, 39, 39,
	40, 40, 40, 40, 40, 40, 40, 40, 40, 40, 40, 40, 40, 40, 40, 40,
	41, 41, 41, 41, 41, 41, 41, 41, 41, 41, 41, 41, 41, 41, 41, 41,
	42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42,
	42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42}

const maxMLCode = 52

// mlBitsTable translates from ml code to number of bits.
var mlBitsTable = [maxMLCode + 1]byte{
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1, 2, 2, 3, 3,
	4, 4, 5, 7, 8, 9, 10, 11,
	12, 13, 14, 15, 16}

// note : mlBase = matchLength - MINMATCH;
// because it's the format it's stored in seqStore->sequences
func mlCode(mlBase uint32) uint8 {
	const mlDeltaCode = 36
	if mlBase <= 127 {
		// TODO: check if compiler omits bounds check
		return mlCodeTable[mlBase]
	}
	return uint8(highBit(mlBase)) + mlDeltaCode
}

func ofCode(offset uint32) uint8 {
	// A valid offset will always be > 0.
	return uint8(highBit(offset))
}
