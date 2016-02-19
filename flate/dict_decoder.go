// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flate

type dictionary struct {
	// Invariant: len(hist) <= size
	size int    // Sliding window size
	hist []byte // Sliding window history, dynamically grown to match size

	// Invariant: 0 <= rdPos <= wrPos <= len(hist)
	wrPos int  // Current output position in buffer
	rdPos int  // Have emitted hist[:rdPos] already
	full  bool // Has a full window length been written yet?
}

func (dd *dictionary) Init(size int, dict []byte) {
	*dd = dictionary{hist: dd.hist}

	dd.size = size
	if len(dd.hist) < size {
		dd.hist = make([]byte, size)
	}
	dd.hist = dd.hist[:size]

	if len(dict) > len(dd.hist) {
		dict = dict[len(dict)-len(dd.hist):]
	}
	dd.wrPos = copy(dd.hist, dict)
	if dd.wrPos == len(dd.hist) {
		dd.wrPos = 0
		dd.full = true
	}
	dd.rdPos = dd.wrPos
}

// HistSize reports the total amount of historical data in the dictionary.
func (dd *dictionary) HistSize() int {
	if dd.full {
		return dd.size
	}
	return dd.wrPos
}

// AvailRead reports the number of bytes that can be flushed by ReadFlush.
func (dd *dictionary) AvailRead() int {
	return dd.wrPos - dd.rdPos
}

// AvailWrite reports the available amount of output buffer space.
func (dd *dictionary) AvailWrite() int {
	return len(dd.hist) - dd.wrPos
}

// WriteSlice returns a slice of the available buffer to write data to.
//
// This invariant will be kept: len(s) <= AvailSize()
func (dd *dictionary) WriteSlice() []byte {
	return dd.hist[dd.wrPos:]
}

// WriteMark advances the writer pointer by cnt.
//
// This invariant must be kept: 0 <= cnt <= AvailSize()
func (dd *dictionary) WriteMark(cnt int) {
	dd.wrPos += cnt
}

// AddByte adds a byte to the dictionary.
// Not used since inlined version is faster *sigh*.
func (dd *dictionary) AddByte(v byte) bool {
	dd.hist[dd.wrPos] = byte(v)
	dd.wrPos++
	return len(dd.hist)-dd.wrPos == 0
}

// WriteCopy copies a string at a given (distance, length) to the output.
// This returns the number of bytes copied and may be less than the requested
// length if the available space in the output buffer is too small.
//
// This invariant must be kept: 0 <= dist <= HistSize()
func (dd *dictionary) WriteCopy(dist, length int) int {
	wrBase := dd.wrPos
	wrEnd := dd.wrPos + length
	if wrEnd > len(dd.hist) {
		wrEnd = len(dd.hist)
	}

	// Copy non-overlapping section after destination.
	rdPos := dd.wrPos - dist
	if rdPos < 0 {
		rdPos += len(dd.hist)
		dd.wrPos += copy(dd.hist[dd.wrPos:wrEnd], dd.hist[rdPos:])
		rdPos = 0
	}

	// Copy overlapping section before destination.
	for dd.wrPos < wrEnd {
		dd.wrPos += copy(dd.hist[dd.wrPos:wrEnd], dd.hist[rdPos:dd.wrPos])
	}
	return dd.wrPos - wrBase
}

// ReadFlush returns a slice of the historical buffer that is ready to be
// emitted to the user. A call to ReadFlush is only valid after all of the data
// from a previous call to ReadFlush has been consumed.
func (dd *dictionary) ReadFlush() []byte {
	toRead := dd.hist[dd.rdPos:dd.wrPos]
	dd.rdPos = dd.wrPos
	if dd.wrPos == len(dd.hist) {
		dd.wrPos, dd.rdPos = 0, 0
		dd.full = true
	}
	return toRead
}
