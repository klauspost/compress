// Copyright 2011 The Snappy-Go Authors. All rights reserved.
// Copyright 2016 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package snappy

import (
	"encoding/binary"
	"io"
	"sync"
)

// We limit how far copy back-references can go, the same as the C++ code.
const maxOffset = 1 << 15

// emitLiteral writes a literal chunk and returns the number of bytes written.
func emitLiteral(dst, lit []byte) int {
	i, n := 0, uint(len(lit)-1)
	switch {
	case n < 60:
		dst[0] = uint8(n)<<2 | tagLiteral
		i = 1
	case n < 1<<8:
		dst[0] = 60<<2 | tagLiteral
		dst[1] = uint8(n)
		i = 2
	case n < 1<<16:
		dst[0] = 61<<2 | tagLiteral
		dst[1] = uint8(n)
		dst[2] = uint8(n >> 8)
		i = 3
	case n < 1<<24:
		dst[0] = 62<<2 | tagLiteral
		dst[1] = uint8(n)
		dst[2] = uint8(n >> 8)
		dst[3] = uint8(n >> 16)
		i = 4
	case int64(n) < 1<<32:
		dst[0] = 63<<2 | tagLiteral
		dst[1] = uint8(n)
		dst[2] = uint8(n >> 8)
		dst[3] = uint8(n >> 16)
		dst[4] = uint8(n >> 24)
		i = 5
	default:
		panic("snappy: source buffer is too long")
	}
	if copy(dst[i:], lit) != len(lit) {
		panic("snappy: destination buffer is too short")
	}
	return i + len(lit)
}

// emitCopy writes a copy chunk and returns the number of bytes written.
func emitCopy(dst []byte, offset, length int) int {
	i := 0
	for length > 0 {
		x := length - 4
		if 0 <= x && x < 1<<3 && offset < 1<<11 {
			dst[i+0] = uint8(offset>>8)&0x07<<5 | uint8(x)<<2 | tagCopy1
			dst[i+1] = uint8(offset)
			i += 2
			break
		}

		x = length
		if x > 1<<6 {
			x = 1 << 6
		}
		dst[i+0] = uint8(x-1)<<2 | tagCopy2
		dst[i+1] = uint8(offset)
		dst[i+2] = uint8(offset >> 8)
		i += 3
		length -= x
	}
	return i
}

var encPool = sync.Pool{New: func() interface{} { return new(encoder) }}

// Encode returns the encoded form of src. The returned slice may be a sub-
// slice of dst if dst was large enough to hold the entire encoded block.
// Otherwise, a newly allocated slice will be returned.
// It is valid to pass a nil dst.
func Encode(dst, src []byte) []byte {
	e := encPool.Get().(*encoder)
	dst = e.encode(dst, src)
	encPool.Put(e)
	return dst
}

const tableBits = 14             // Bits used in the table
const tableSize = 1 << tableBits // Size of the table
var useSSE42 bool

type encoder struct {
	table [tableSize]int32
	cur   int
}

func (e *encoder) encode(dst, src []byte) []byte {
	if useSSE42 {
		return e.encSSE4(dst, src)
	}
	return e.enc(dst, src)
}

func (e *encoder) enc(dst, src []byte) []byte {
	if n := MaxEncodedLen(len(src)); len(dst) < n {
		dst = make([]byte, n)
	}

	// The block starts with the varint-encoded length of the decompressed bytes.
	d := binary.PutUvarint(dst, uint64(len(src)))

	// Return early if src is short.
	if len(src) <= 4 {
		if len(src) != 0 {
			d += emitLiteral(dst[d:], src)
		}
		e.cur += 4
		return dst[:d]
	}

	// Ensure that e.cur doesn't wrap.
	if e.cur > 1<<30 {
		e.cur = 0
	}

	// Iterate over the source bytes.
	var (
		s    int          // The iterator position.
		t    int          // The last position with the same hash as s.
		lit  int          // The start position of any pending literal bytes.
		tadd = -1 - e.cur // Added to t to adjust match to offset
		sadd = 1 + e.cur  // Added to s to adjust match to offset
	)
	for s+3 < len(src) {
		// Update the hash table.
		b0, b1, b2, b3 := src[s], src[s+1], src[s+2], src[s+3]
		h := uint32(b0) | uint32(b1)<<8 | uint32(b2)<<16 | uint32(b3)<<24
		p := &e.table[(h*0x1e35a7bd)>>(32-tableBits)]
		// We need to to store values in [-1, inf) in table. To save
		// some initialization time, (re)use the table's zero value
		// and shift the values against this zero: add 1 on writes,
		// subtract 1 on reads.
		t, *p = int(*p)+tadd, int32(s+sadd)

		// We calculate the offset in the current buffer.
		// if t >= s this will be negative, when converted to a uint this will always be > maxOffset
		offset := uint(s - t - 1)

		// If t is invalid or src[s:s+4] differs from src[t:t+4], accumulate a literal byte.
		if t < 0 || offset >= (maxOffset-1) || b0 != src[t] || b1 != src[t+1] || b2 != src[t+2] || b3 != src[t+3] {
			// Skip bytes if last match was >= 32 bytes in the past.
			s += 1 + (s-lit)>>5
			continue
		}

		// Otherwise, we have a match. First, emit any pending literal bytes.
		if lit != s {
			d += emitLiteral(dst[d:], src[lit:s])
		}
		// Extend the match to be as long as possible.
		s0 := s
		s, t = s+4, t+4
		for s < len(src) && src[s] == src[t] {
			s++
			t++
		}
		// Emit the copied bytes.
		d += emitCopy(dst[d:], s-t, s-s0)
		lit = s
	}

	// Emit any final pending literal bytes and return.
	if lit != len(src) {
		d += emitLiteral(dst[d:], src[lit:])
	}

	e.cur += len(src)
	return dst[:d]
}

func (e *encoder) encSSE4(dst, src []byte) []byte {
	if n := MaxEncodedLen(len(src)); len(dst) < n {
		dst = make([]byte, n)
	}

	// The block starts with the varint-encoded length of the decompressed bytes.
	d := binary.PutUvarint(dst, uint64(len(src)))

	// Return early if src is short.
	if len(src) <= 4 {
		if len(src) != 0 {
			d += emitLiteral(dst[d:], src)
		}
		e.cur += 4
		return dst[:d]
	}

	// Ensure that e.cur doesn't wrap.
	if e.cur > 1<<30 {
		e.cur = 0
	}

	// Iterate over the source bytes.
	var (
		s    int          // The iterator position.
		t    int          // The last position with the same hash as s.
		lit  int          // The start position of any pending literal bytes.
		tadd = -1 - e.cur // Added to t to adjust match to offset
		sadd = 1 + e.cur  // Added to s to adjust match to offset
	)
	for s+3 < len(src) {
		// Update the hash table.
		h := uint32(src[s]) | uint32(src[s+1])<<8 | uint32(src[s+2])<<16 | uint32(src[s+3])<<24
		p := &e.table[(h*0x1e35a7bd)>>(32-tableBits)]
		// We need to to store values in [-1, inf) in table. To save
		// some initialization time, (re)use the table's zero value
		// and shift the values against this zero: add 1 on writes,
		// subtract 1 on reads.
		t, *p = int(*p)+tadd, int32(s+sadd)

		// We calculate the offset in the current buffer.
		// if t >= s this will be negative, when converted to a uint this will always be > maxOffset
		offset := uint(s - t - 1)

		// If t is invalid or src[s:s+4] differs from src[t:t+4], accumulate a literal byte.
		// This saves us the branch to test if t >=s, which would indicate a forward reference,
		// that is a result of e.cur wrapping.
		if t < 0 || offset >= maxOffset-1 {
			// Skip bytes if last match was >= 32 bytes in the past.
			s += 1 + (s-lit)>>5
			continue
		}

		length := len(src) - s

		// Extend the match to be as long as possible.
		match := matchLenSSE4(src[t:], src[s:], length)

		/*	match2 := matchLenSSE4Ref(src[t:], src[s:], length)

			if match != match2 {
				fmt.Printf("%v\n%v\nlen: %d\n", src[t:t+length], src[s:s+length], len(src)-s)
				s := fmt.Sprintf("got %d != %d expected", match, match2)
				panic(s)
			}
		*/
		// Return if short.
		if match < 4 {
			s += 1 + (s-lit)>>5
			continue
		}

		// Otherwise, we have a match. First, emit any pending literal bytes.
		if lit != s {
			// Skip bytes if last match was >= 32 bytes in the past.
			d += emitLiteral(dst[d:], src[lit:s])
		}

		// Emit the copied bytes.
		d += emitCopy(dst[d:], s-t, match)
		s += match
		lit = s
	}

	// Emit any final pending literal bytes and return.
	if lit != len(src) {
		d += emitLiteral(dst[d:], src[lit:])
	}

	e.cur += len(src)
	return dst[:d]
}

// MaxEncodedLen returns the maximum length of a snappy block, given its
// uncompressed length.
func MaxEncodedLen(srcLen int) int {
	// Compressed data can be defined as:
	//    compressed := item* literal*
	//    item       := literal* copy
	//
	// The trailing literal sequence has a space blowup of at most 62/60
	// since a literal of length 60 needs one tag byte + one extra byte
	// for length information.
	//
	// Item blowup is trickier to measure. Suppose the "copy" op copies
	// 4 bytes of data. Because of a special check in the encoding code,
	// we produce a 4-byte copy only if the offset is < 65536. Therefore
	// the copy op takes 3 bytes to encode, and this type of item leads
	// to at most the 62/60 blowup for representing literals.
	//
	// Suppose the "copy" op copies 5 bytes of data. If the offset is big
	// enough, it will take 5 bytes to encode the copy op. Therefore the
	// worst case here is a one-byte literal followed by a five-byte copy.
	// That is, 6 bytes of input turn into 7 bytes of "compressed" data.
	//
	// This last factor dominates the blowup, so the final estimate is:
	return 32 + srcLen + srcLen/6
}

// NewWriter returns a new Writer that compresses to w, using the framing
// format described at
// https://github.com/google/snappy/blob/master/framing_format.txt
func NewWriter(w io.Writer) *Writer {
	return &Writer{
		w:   w,
		e:   encPool.Get().(*encoder),
		enc: make([]byte, MaxEncodedLen(maxUncompressedChunkLen)),
	}
}

// Writer is an io.Writer than can write Snappy-compressed bytes.
type Writer struct {
	w           io.Writer
	e           *encoder
	err         error
	enc         []byte
	buf         [checksumSize + chunkHeaderSize]byte
	wroteHeader bool
}

// Reset discards the writer's state and switches the Snappy writer to write to
// w. This permits reusing a Writer rather than allocating a new one.
func (w *Writer) Reset(writer io.Writer) {
	w.w = writer
	w.err = nil
	w.wroteHeader = false
}

// Write satisfies the io.Writer interface.
func (w *Writer) Write(p []byte) (n int, errRet error) {
	if w.err != nil {
		return 0, w.err
	}
	if !w.wroteHeader {
		copy(w.enc, magicChunk)
		if _, err := w.w.Write(w.enc[:len(magicChunk)]); err != nil {
			w.err = err
			return n, err
		}
		w.wroteHeader = true
	}
	for len(p) > 0 {
		var uncompressed []byte
		if len(p) > maxUncompressedChunkLen {
			uncompressed, p = p[:maxUncompressedChunkLen], p[maxUncompressedChunkLen:]
		} else {
			uncompressed, p = p, nil
		}
		checksum := crc(uncompressed)

		// Compress the buffer, discarding the result if the improvement
		// isn't at least 12.5%.
		chunkType := uint8(chunkTypeCompressedData)
		chunkBody := w.e.encode(w.enc, uncompressed)
		if len(chunkBody) >= len(uncompressed)-len(uncompressed)/8 {
			chunkType, chunkBody = chunkTypeUncompressedData, uncompressed
		}

		chunkLen := 4 + len(chunkBody)
		w.buf[0] = chunkType
		w.buf[1] = uint8(chunkLen >> 0)
		w.buf[2] = uint8(chunkLen >> 8)
		w.buf[3] = uint8(chunkLen >> 16)
		w.buf[4] = uint8(checksum >> 0)
		w.buf[5] = uint8(checksum >> 8)
		w.buf[6] = uint8(checksum >> 16)
		w.buf[7] = uint8(checksum >> 24)
		if _, err := w.w.Write(w.buf[:]); err != nil {
			w.err = err
			return n, err
		}
		if _, err := w.w.Write(chunkBody); err != nil {
			w.err = err
			return n, err
		}
		n += len(uncompressed)
	}
	return n, nil
}
