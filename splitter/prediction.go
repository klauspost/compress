package splitter

import "math"

// Split blocks like ZPAQ: (public domain)
type prediction struct {
	h           uint32 // rolling hash for finding fragment boundaries
	c1          byte   // last byte
	maxFragment int
	minFragment int
	maxHash     uint32
	o1          [256]byte // order 1 context -> predicted byte
	buffer      []byte
	off         int
	out         chan<- []byte
	sb          sendBack
}

// Split blocks. Typically block size will be maxSize / 4
// Minimum block size is maxSize/64.
//
// The break point is content dependent.
// Any insertions, deletions, or edits that occur before the start of the 32+ byte dependency window
// don't affect the break point.
// This makes it likely for two files to still have identical fragments far away from any edits.
func newPredictionWriter(maxSize uint, out chan<- []byte) *prediction {
	fragment := math.Log2(float64(maxSize) / (64 * 64))
	mh := math.Exp2(22 - fragment)
	return &prediction{
		maxFragment: int(maxSize),
		minFragment: int(maxSize / 64),
		maxHash:     uint32(mh),
		buffer:      make([]byte, maxSize),
		out:         out,
	}
}

// h is a 32 bit hash that depends on the last 32 bytes that were mispredicted by the order 1 model o1[].
// h < maxhash therefore occurs with probability 2^-16, giving an average fragment size of 64K.
// The variable size dependency window works because one constant is odd (correct prediction, no shift),
// and the other is even but not a multiple of 4 (missed prediction, 1 bit shift left).
// This is different from a normal Rabin filter, which uses a large fixed-sized dependency window
// and two multiply operations, one at the window entry and the inverse at the window exit.
func (z *prediction) write(b []byte) (int, error) {
	// Transfer to local variables ~30% faster.
	c1 := z.c1
	h := z.h
	for _, c := range b {
		if c == z.o1[c1] {
			h = (h + uint32(c) + 1) * 314159265
		} else {
			h = (h + uint32(c) + 1) * 271828182
		}
		z.o1[c1] = c
		c1 = c
		z.buffer[z.off] = c
		z.off++

		// At a break point? Send it off!
		if (z.off >= z.minFragment && h < z.maxHash) || z.off >= z.maxFragment {
			z.split()
		}
	}
	z.h = h
	z.c1 = c1
	return len(b), nil
}

// Split content, so a new block begins with next write
func (z *prediction) split() {
	if z.off == 0 {
		return
	}
	out := z.sb.getBuffer(z.off)
	copy(out, z.buffer[:z.off])
	z.out <- out
	z.off = 0
	z.h = 0
	z.c1 = 0
}
