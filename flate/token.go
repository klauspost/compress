// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flate

import (
	"math"
)

const (
	// 2 bits:   type   0 = literal  1=EOF  2=Match   3=Unused
	// 8 bits:   xlength = length - MIN_MATCH_LENGTH
	// 22 bits   xoffset = offset - MIN_OFFSET_SIZE, or literal
	lengthShift = 22
	offsetMask  = 1<<lengthShift - 1
	typeMask    = 3 << 30
	literalType = 0 << 30
	matchType   = 1 << 30
)

// The length code for length X (MIN_MATCH_LENGTH <= X <= MAX_MATCH_LENGTH)
// is lengthCodes[length - MIN_MATCH_LENGTH]
var lengthCodes = [256]uint8{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 8,
	9, 9, 10, 10, 11, 11, 12, 12, 12, 12,
	13, 13, 13, 13, 14, 14, 14, 14, 15, 15,
	15, 15, 16, 16, 16, 16, 16, 16, 16, 16,
	17, 17, 17, 17, 17, 17, 17, 17, 18, 18,
	18, 18, 18, 18, 18, 18, 19, 19, 19, 19,
	19, 19, 19, 19, 20, 20, 20, 20, 20, 20,
	20, 20, 20, 20, 20, 20, 20, 20, 20, 20,
	21, 21, 21, 21, 21, 21, 21, 21, 21, 21,
	21, 21, 21, 21, 21, 21, 22, 22, 22, 22,
	22, 22, 22, 22, 22, 22, 22, 22, 22, 22,
	22, 22, 23, 23, 23, 23, 23, 23, 23, 23,
	23, 23, 23, 23, 23, 23, 23, 23, 24, 24,
	24, 24, 24, 24, 24, 24, 24, 24, 24, 24,
	24, 24, 24, 24, 24, 24, 24, 24, 24, 24,
	24, 24, 24, 24, 24, 24, 24, 24, 24, 24,
	25, 25, 25, 25, 25, 25, 25, 25, 25, 25,
	25, 25, 25, 25, 25, 25, 25, 25, 25, 25,
	25, 25, 25, 25, 25, 25, 25, 25, 25, 25,
	25, 25, 26, 26, 26, 26, 26, 26, 26, 26,
	26, 26, 26, 26, 26, 26, 26, 26, 26, 26,
	26, 26, 26, 26, 26, 26, 26, 26, 26, 26,
	26, 26, 26, 26, 27, 27, 27, 27, 27, 27,
	27, 27, 27, 27, 27, 27, 27, 27, 27, 27,
	27, 27, 27, 27, 27, 27, 27, 27, 27, 27,
	27, 27, 27, 27, 27, 28,
}

var offsetCodes = [256]uint32{
	0, 1, 2, 3, 4, 4, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7,
	8, 8, 8, 8, 8, 8, 8, 8, 9, 9, 9, 9, 9, 9, 9, 9,
	10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10,
	11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11,
	12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12,
	12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12,
	13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13,
	13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13,
	14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14,
	14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14,
	14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14,
	14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14,
	15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
	15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
	15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
	15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
}

type token uint32

type tokens struct {
	nLits     int
	n         uint16 // Must be able to contain maxStoreBlockSize
	tokens    [maxStoreBlockSize + 1]token
	litHist   [256]uint16 // codes 0->255
	extraHist [32]uint16  // codes 256->maxnumlit
	offHist   [32]uint16  // offset codes
}

func (t *tokens) Reset() {
	if t.n == 0 {
		return
	}
	t.n = 0
	t.nLits = 0
	for i := range t.litHist[:] {
		t.litHist[i] = 0
	}
	for i := range t.extraHist[:] {
		t.extraHist[i] = 0
	}
	for i := range t.offHist[:] {
		t.offHist[i] = 0
	}
}

func (t *tokens) Fill() {
	if t.n == 0 {
		return
	}
	for i := range t.litHist[:] {
		if t.litHist[i] == 0 {
			t.litHist[i] = 1
		}
	}
	for i := range t.extraHist[:maxNumLit-256] {
		if t.extraHist[i] == 0 {
			t.extraHist[i] = 1
		}
	}
	for i := range t.offHist[:offsetCodeCount] {
		if t.offHist[i] == 0 {
			t.offHist[i] = 1
		}
	}
}

func indexTokens(in []token) tokens {
	var t tokens
	for _, tok := range in {
		if tok < matchType {
			t.tokens[t.n] = tok
			t.litHist[tok]++
			t.n++
			continue
		}
		t.AddMatch(uint32(tok.length()), tok.offset())
	}
	return t
}

// emitLiteral writes a literal chunk and returns the number of bytes written.
func emitLiteral(dst *tokens, lit []byte) {
	ol := int(dst.n)
	for i, v := range lit {
		dst.tokens[(i+ol)&maxStoreBlockSize] = token(v)
		dst.litHist[v]++
	}
	dst.n += uint16(len(lit))
	dst.nLits += len(lit)
}

func (t *tokens) AddLiteral(lit byte) {
	t.tokens[t.n] = token(lit)
	t.litHist[lit]++
	t.n++
	t.nLits++
}

// EstimatedBits will return an minimum size estimated by an *optimal*
// compression of the block.
// The size of the block
func (t *tokens) EstimatedBits() int {
	shannon := float64(0)
	bits := int(0)
	nMatches := 0
	if t.nLits > 0 {
		invTotal := 1.0 / float64(t.nLits)
		for _, v := range t.litHist[:] {
			if v > 0 {
				n := float64(v)
				shannon += math.Ceil(-math.Log2(n*invTotal) * n)
			}
		}
		// Just add 15 for EOB
		shannon += 15
		for _, v := range t.extraHist[1 : maxNumLit-256] {
			if v > 0 {
				n := float64(v)
				shannon += math.Ceil(-math.Log2(n*invTotal) * n)
				bits += int(lengthExtraBits[v&31]) * int(v)
				nMatches += int(v)
			}
		}
	}
	if nMatches > 0 {
		invTotal := 1.0 / float64(nMatches)
		for _, v := range t.offHist[:offsetCodeCount] {
			if v > 0 {
				n := float64(v)
				shannon += math.Ceil(-math.Log2(n*invTotal) * n)
				bits += int(offsetExtraBits[v&31]) * int(n)
			}
		}
	}

	return int(shannon) + bits
}

// AddMatch adds a match to the tokens.
// This function is very sensitive to inlining and right on the border.
func (t *tokens) AddMatch(xlength uint32, xoffset uint32) {
	t.tokens[t.n] = token(matchType | xlength<<lengthShift | xoffset)
	t.offHist[offsetCode(xoffset)&31]++
	t.extraHist[(1+lengthCodes[uint8(xlength)])&31]++
	t.nLits++
	t.n++
}

func (t *tokens) AddEOB() {
	t.tokens[t.n] = token(endBlockMarker)
	t.extraHist[0]++
	t.n++
}

func (t *tokens) Slice() []token {
	return t.tokens[:t.n]
}

// Convert a literal into a literal token.
func literalToken(literal uint32) token { return token(literalType + literal) }

// Returns the type of a token
func (t token) typ() uint32 { return uint32(t) & typeMask }

// Returns the literal of a literal token
func (t token) literal() uint8 { return uint8(t) }

// Returns the extra offset of a match token
func (t token) offset() uint32 { return uint32(t) & offsetMask }

func (t token) length() uint8 { return uint8(t >> lengthShift) }

// The code is never more than 8 bits, but is returned as uint32 for convenience.
func lengthCode(len uint8) uint32 { return uint32(lengthCodes[len]) }

// Returns the offset code corresponding to a specific offset
func offsetCode(off uint32) uint32 {
	if off < uint32(len(offsetCodes)) {
		return offsetCodes[uint8(off)]
	} else if off < uint32(len(offsetCodes))<<8-1 {
		return offsetCodes[uint8(off>>7)] + 14
	} else {
		return offsetCodes[uint8(off>>14)] + 28
	}
}
