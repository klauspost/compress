package splitter

import "math"

// Split blocks based on entropy distribution.
type entropy struct {
	h           uint32 // rolling hash for finding fragment boundaries
	maxFragment int
	minFragment int
	maxHash     uint32
	hist        [256]uint16 // histogram of current accumulated
	histLen     int
	avgHist     uint16
	buffer      []byte
	off         int
	sb          sendBack
	out         chan<- []byte
}

// Split blocks. Typically block size will be maxSize / 4
// Minimum block size is maxSize/32.
//
// The break point is content dependent.
// Any insertions, deletions, or edits that occur before the start of the 32+ byte dependency window
// don't affect the break point.
// This makes it likely for two files to still have identical fragments far away from any edits.
func newEntropyWriter(maxSize uint, out chan<- []byte) *entropy {
	fragment := math.Log2(float64(maxSize) / (64 * 64))
	mh := math.Exp2(22 - fragment)
	e := &entropy{
		maxFragment: int(maxSize),
		minFragment: int(maxSize / 32),
		maxHash:     uint32(mh),
		out:         out,
		buffer:      make([]byte, maxSize),
	}
	if e.minFragment > 65535 {
		e.minFragment = 65535
	}
	if e.minFragment < 512 {
		e.minFragment = 512
	}
	e.avgHist = uint16(e.minFragment / 255)
	return e
}

// h is a 32 bit hash that depends on the last 32 bytes that were mispredicted by the order 1 model o1[].
// h < maxhash therefore occurs with probability 2^-16, giving an average fragment size of 64K.
// The variable size dependency window works because one constant is odd (correct prediction, no shift),
// and the other is even but not a multiple of 4 (missed prediction, 1 bit shift left).
// This is different from a normal Rabin filter, which uses a large fixed-sized dependency window
// and two multiply operations, one at the window entry and the inverse at the window exit.
func (e *entropy) write(b []byte) (int, error) {
	inLen := len(b)
	if e.histLen < e.minFragment {
		b2 := b
		if len(b2)+e.histLen > e.minFragment {
			b2 = b2[:e.minFragment-e.histLen]
		}
		off := e.off
		for i := range b2 {
			v := b2[i]
			e.hist[v]++
			e.buffer[off+i] = v
		}
		e.histLen += len(b2)
		e.off += len(b2)
		b = b[len(b2):]
	}
	if len(b) == 0 {
		return inLen, nil
	}

	h := e.h
	for _, c := range b {
		if e.hist[c] >= e.avgHist {
			h = (h + uint32(c) + 1) * 314159265
		} else {
			h = (h + uint32(c) + 1) * 271828182
		}
		e.buffer[e.off] = c
		e.off++

		// At a break point? Send it off!
		if (e.off >= e.minFragment && h < e.maxHash) || e.off >= e.maxFragment {
			e.split()
		}
	}
	e.h = h
	return inLen, nil
}

// Split content, so a new block begins with next write
func (e *entropy) split() {
	if e.off == 0 {
		return
	}

	out := e.sb.getBuffer(e.off)
	copy(out, e.buffer[:e.off])
	e.out <- out
	e.off = 0
	e.h = 0
	e.histLen = 0
	for i := range e.hist {
		e.hist[i] = 0
	}
}
