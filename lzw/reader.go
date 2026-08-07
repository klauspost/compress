// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package lzw implements the Lempel-Ziv-Welch compressed data format,
// described in T. A. Welch, “A Technique for High-Performance Data
// Compression”, Computer, 17(6) (June 1984), pp 8-19.
//
// In particular, it implements LZW as used by the GIF file format, which means
// variable-width codes up to 12 bits and the first two non-literal codes are a
// clear code and an EOF code.
//
// The TIFF file format uses a variant that increases the code width one code
// early, as does PDF's LZWDecode filter by default. Decoding those requires
// [Reader.SetAldusCompatible].
package lzw

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/internal/regmask"
)

// Order specifies the bit ordering in an LZW data stream.
type Order int

const (
	// LSB means Least Significant Bits first, as used in the GIF file format.
	LSB Order = iota
	// MSB means Most Significant Bits first, as used in the TIFF and PDF
	// file formats.
	MSB
)

const (
	maxWidth           = 12
	maxCodes           = 1 << maxWidth
	codeMask           = maxCodes - 1
	decoderInvalidCode = 0xffff

	// reserve is the output space a single code may need: a maximal expansion
	// plus the overshoot of the eight-bytes-at-a-time stores.
	reserve = maxCodes + 8
	// flushBuffer is the number of bytes decoded into output before it is
	// handed to the caller.
	flushBuffer = maxCodes
	// inputBuffer is the size of the window read from a readerAt source.
	inputBuffer = 4096
)

var errInvalidCode = errors.New("lzw: invalid code")

// A peeker is an input source that can be read without consuming, such as a
// [bufio.Reader]. It lets the decoder read whole words at a time and consume
// only the bytes the code stream needed.
type peeker interface {
	Peek(int) ([]byte, error)
	Discard(int) (int, error)
	Buffered() int
}

// A readerAt is an input source that can be read ahead of without moving its
// read position, such as a [bytes.Reader]. Its position is set to just after
// the last consumed byte whenever decoding pauses.
type readerAt interface {
	io.ReaderAt
	io.Seeker
}

// Reader is an [io.Reader] which can be used to read compressed data in the
// LZW format.
type Reader struct {
	bits     uint64
	nBits    uint
	width    uint
	litWidth uint // width in bits of literal codes
	order    Order
	aldus    bool // decode the Aldus variant, see SetAldusCompatible
	err      error

	// Exactly one of pk, at and br reads the compressed stream, see init.
	// buf is the window of input read but not yet consumed, pos the read
	// offset into it and off the position of buf[0] within at.
	pk     peeker
	at     readerAt
	br     io.ByteReader
	buf    []byte
	pos    int
	off    int64
	own    []byte // window read from at
	b1     [1]byte
	srcErr error

	// The first 1<<litWidth codes are literal codes.
	// The next two codes mean clear and EOF.
	// Other valid codes are in the range [lo, hi] where lo := clear + 2,
	// with the upper bound incrementing on each code seen.
	//
	// last is the most recently seen code, or decoderInvalidCode, lastLen is
	// the length of its expansion and lastF8 its first8 entry.
	//
	// An invariant is that hi < 1<<width.
	clear, eof, hi, last uint16
	lastLen              int
	lastF8               uint64

	// link describes each literal code and each code c in [lo, hi]: bits 0-7
	// hold the final byte of its expansion, bits 8-19 the length of its
	// expansion minus one and bits 20-31 the code for all but the final byte.
	// That code is either a literal code or another code in [lo, c).
	link [maxCodes]uint32
	// first8 holds the first eight bytes of the expansion of each of those
	// codes, in output order, zero padded. Codes expanding to eight bytes or
	// fewer are written to the output with a single store from here, so the
	// link chain only has to be walked for the bytes past the first eight.
	first8 [maxCodes]uint64

	// output is the temporary output buffer, used when Read is called with a
	// buffer too small to decode into directly. It is flushed once it holds
	// flushBuffer bytes, so that there is always room to decode an entire code.
	output [flushBuffer + reserve]byte
	toRead []byte // bytes to return from Read
}

// Read implements io.Reader, reading uncompressed bytes from its underlying reader.
func (r *Reader) Read(b []byte) (int, error) {
	// Decoding stops a whole maximal expansion short of the end of the buffer,
	// so returning after one pass would hand back all but the last few KB of a
	// large read. Keep going instead, and go short only once the stream itself
	// has ended: a caller that does not loop then fails loudly rather than
	// quietly losing the tail of every read.
	n := 0
	for n < len(b) {
		if len(r.toRead) > 0 {
			m := copy(b[n:], r.toRead)
			r.toRead, n = r.toRead[m:], n+m
			continue
		}
		if r.err != nil {
			break
		}
		if len(b)-n >= 2*maxCodes {
			// There is room to expand any code, so skip the output buffer.
			n += r.decodeAny(b[n:])
			continue
		}
		r.toRead = r.output[:r.decodeAny(r.output[:])]
	}
	if n > 0 || len(r.toRead) > 0 {
		return n, nil
	}
	return 0, r.err
}

func (r *Reader) decodeAny(dst []byte) int {
	if r.aldus {
		return r.decodeAldus(dst)
	}
	return r.decode(dst)
}

// decodeAldus decompresses the Aldus variant of the format, in which the code
// width grows one code early, and is otherwise like decode.
//
// It is a separate function, and a plain walk of the link chain rather than a
// copy of decode's machinery, for one reason: adding the variant to decode's
// loop cost the common path around 7% on MSB streams. The flag becomes another
// value the loop has to keep live, and it has no registers to spare. Keeping the
// two apart leaves decode compiling to exactly what it did before, and TIFF and
// PDF streams have no need of its speed.
func (r *Reader) decodeAldus(dst []byte) int {
	limit := len(dst) - reserve
	bits := r.bits
	msb := r.order == MSB
	mask := uint16(1)<<r.width - 1
	o := 0

loop:
	for {
		if r.nBits < r.width {
			if r.pos+8 <= len(r.buf) {
				if msb {
					bits |= binary.BigEndian.Uint64(r.buf[r.pos:]) >> (r.nBits & regmask.Shift64ByUint)
				} else {
					bits |= binary.LittleEndian.Uint64(r.buf[r.pos:]) << (r.nBits & regmask.Shift64ByUint)
				}
				r.pos += int(63-r.nBits) >> 3
				r.nBits |= 56
			} else {
				var ok bool
				bits, r.nBits, ok = r.fillSlow(bits, r.nBits, r.width)
				if !ok {
					break loop
				}
			}
		}
		var code uint16
		if msb {
			code = uint16(bits >> ((64 - r.width) & regmask.Shift64ByUint))
			bits <<= r.width & regmask.Shift64ByUint
		} else {
			code = uint16(bits) & mask
			bits >>= r.width & regmask.Shift64ByUint
		}
		r.nBits -= r.width

		if code-r.clear <= 1 {
			if code != r.clear {
				r.err = io.EOF
				break loop
			}
			r.width = 1 + r.litWidth
			mask = uint16(1)<<r.width - 1
			r.hi, r.last, r.lastLen = r.clear+1, decoderInvalidCode, 0
			continue
		} else if code > r.hi {
			r.err = errInvalidCode
			break loop
		}

		// Write the expansion right to left, ending at its first byte.
		c, n := code, 0
		kwkwk := code == r.hi && r.last != decoderInvalidCode
		if kwkwk {
			// code == hi expands to the last expansion followed by its head.
			c, n = r.last, r.lastLen+1
		} else {
			n = int(r.link[code&codeMask]>>8&codeMask) + 1
		}
		out := dst[o : o+n]
		i := n
		if kwkwk {
			h := r.last
			for h >= r.clear {
				h = uint16(r.link[h&codeMask] >> 20)
			}
			i--
			out[i] = uint8(h)
		}
		for c >= r.clear {
			e := r.link[c&codeMask]
			i--
			out[i] = uint8(e)
			c = uint16(e >> 20)
		}
		out[0] = uint8(c)
		o += n

		if r.last != decoderInvalidCode {
			// Save what the hi code expands to. first8 is not maintained: this
			// decoder never reads it, and a Reset redefines every entry before
			// any of them can be read again.
			r.link[r.hi&codeMask] = uint32(c) | uint32(r.lastLen)<<8 | uint32(r.last)<<20
		}
		r.last, r.lastLen = code, n
		r.hi++
		if r.hi >= mask { // one code earlier than decode
			if r.width == maxWidth {
				r.last = decoderInvalidCode
				r.hi--
			} else {
				r.width++
				mask = mask<<1 | 1
			}
		}
		if o >= limit {
			break
		}
	}

	r.bits = bits
	r.release()
	return o
}

// decode decompresses bytes from r into dst and returns the number of bytes
// written. len(dst) must be greater than reserve.
func (r *Reader) decode(dst []byte) int {
	limit := len(dst) - reserve
	// Only the bit accumulator and the byte order are worth a local. The loop
	// needs too many values live at once for the rest to stay in registers too,
	// and the spill code the compiler then emits costs more than reading them
	// back out of r: hoisting all of them is around 15% slower on text.
	bits := r.bits
	msb := r.order == MSB
	// mask holds 1<<width - 1. Keeping it rather than deriving it from the width
	// each time round the loop saves both the shift and the guard the compiler
	// would need against it overshifting. Every shift below is masked for the
	// same reason: all of them are provably shorter than a word. It also stands
	// in for the code at which hi overflows the width, which is mask+1.
	mask := uint16(1)<<r.width - 1
	o := 0

loop:
	for {
		if r.nBits < r.width {
			if r.pos+8 <= len(r.buf) {
				if msb {
					bits |= binary.BigEndian.Uint64(r.buf[r.pos:]) >> (r.nBits & regmask.Shift64ByUint)
				} else {
					bits |= binary.LittleEndian.Uint64(r.buf[r.pos:]) << (r.nBits & regmask.Shift64ByUint)
				}
				r.pos += int(63-r.nBits) >> 3
				r.nBits |= 56
			} else {
				var ok bool
				bits, r.nBits, ok = r.fillSlow(bits, r.nBits, r.width)
				if !ok {
					break loop
				}
			}
		}
		var code uint16
		if msb {
			code = uint16(bits >> ((64 - r.width) & regmask.Shift64ByUint))
			bits <<= r.width & regmask.Shift64ByUint
		} else {
			code = uint16(bits) & mask
			bits >>= r.width & regmask.Shift64ByUint
		}
		r.nBits -= r.width

		// f8 is the first8 entry of code, that is the first eight bytes of its
		// expansion, and n is the length of the expansion. Literal codes have
		// entries of their own, so that they need no test of their own here:
		// only the clear and EOF codes are singled out, with a test that is
		// almost always false and so costs next to nothing.
		var f8 uint64
		var n int
		if code-r.clear <= 1 {
			if code != r.clear {
				r.err = io.EOF
				break loop
			}
			r.width = 1 + r.litWidth
			mask = uint16(1)<<r.width - 1
			r.hi, r.last, r.lastLen = r.clear+1, decoderInvalidCode, 0
			continue
		} else if code > r.hi {
			r.err = errInvalidCode
			break loop
		} else if code == r.hi && r.last != decoderInvalidCode {
			// code == hi is a special case which expands to the last expansion
			// followed by the head of the last expansion.
			n = r.lastLen + 1
			if n <= 8 {
				f8 = r.lastF8 | (r.lastF8&0xff)<<(8*uint(r.lastLen))
				binary.LittleEndian.PutUint64(dst[o:], f8)
			} else if o >= r.lastLen {
				// The last expansion is still right there in the output.
				copy(dst[o:o+r.lastLen], dst[o-r.lastLen:o])
				dst[o+r.lastLen] = uint8(r.lastF8)
				f8 = r.lastF8
			} else {
				out := dst[o : o+n]
				out[n-1] = uint8(r.lastF8)
				e := r.link[r.last&codeMask]
				for i := n - 1; i > 8; i-- {
					out[i-1] = uint8(e)
					e = r.link[e>>20&codeMask]
				}
				f8 = r.lastF8
				binary.LittleEndian.PutUint64(out, f8)
			}
		} else {
			e := r.link[code&codeMask]
			n = int(e>>8&codeMask) + 1
			f8 = r.first8[code&codeMask]
			if n <= 8 {
				binary.LittleEndian.PutUint64(dst[o:], f8)
			} else if e>>20 == uint32(r.last) && o >= r.lastLen {
				// This code extends the last one by a byte, and the last
				// expansion is still right there in the output.
				copy(dst[o:o+r.lastLen], dst[o-r.lastLen:o])
				dst[o+r.lastLen] = uint8(e)
			} else {
				// Walk the link chain writing suffixes right to left, until
				// only the first eight bytes are left to write.
				out := dst[o : o+n]
				for i := n; i > 8; i-- {
					out[i-1] = uint8(e)
					e = r.link[e>>20&codeMask]
				}
				binary.LittleEndian.PutUint64(out, f8)
			}
		}
		o += n

		if r.last != decoderInvalidCode {
			// Save what the hi code expands to: the last expansion followed by
			// the first byte of this one.
			r.link[r.hi&codeMask] = uint32(f8&0xff) | uint32(r.lastLen)<<8 | uint32(r.last)<<20
			r.first8[r.hi&codeMask] = r.lastF8 | (f8&0xff)<<(8*uint(r.lastLen))
		}
		r.last, r.lastLen, r.lastF8 = code, n, f8
		r.hi++
		if r.hi > mask {
			if r.width == maxWidth {
				r.last = decoderInvalidCode
				// Undo the hi++ a few lines above, so that (1) we maintain
				// the invariant that hi < 1<<width, and (2) hi does not
				// eventually overflow a uint16.
				r.hi--
			} else {
				r.width++
				mask = mask<<1 | 1
			}
		}
		if o >= limit {
			break
		}
	}

	r.bits = bits
	r.release()
	return o
}

// Reading whole words at a time pulls in bytes whose bits have not been
// consumed yet. They are still held in the bit accumulator, so nBits/8 of the
// pos bytes taken from the window count as unread. An invariant of the peeker
// path, which cannot hand bytes back once they are discarded, is that
// pos >= nBits/8.

// fillSlow tops up bits from the tail of the input window, refilling it as
// needed. It reports false and sets r.err if the stream ended.
func (r *Reader) fillSlow(bits uint64, nBits, width uint) (uint64, uint, bool) {
	for nBits < width {
		if r.pos >= len(r.buf) && !r.refill(nBits) {
			err := r.srcErr
			if err == nil || err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			r.err = err
			return bits, nBits, false
		}
		x := r.buf[r.pos]
		r.pos++
		if r.order == MSB {
			bits |= uint64(x) << ((56 - nBits) & regmask.Shift64ByUint)
		} else {
			bits |= uint64(x) << (nBits & regmask.Shift64ByUint)
		}
		nBits += 8
	}
	return bits, nBits, true
}

// refill replaces the input window with one starting at the first byte that has
// not been consumed. It reports whether the new window holds a byte that has
// not been read yet, recording the reason in r.srcErr if not.
func (r *Reader) refill(nBits uint) bool {
	switch {
	case r.pk != nil:
		skip := int(nBits) / 8
		if d := r.pos - skip; d > 0 {
			r.pk.Discard(d)
		}
		r.pos = skip
		// Never ask for more than is already buffered plus the one byte we
		// need, so that we block no longer than a ReadByte would have.
		n := r.pk.Buffered()
		if n <= skip {
			n = skip + 1
		}
		var err error
		r.buf, err = r.pk.Peek(n)
		if len(r.buf) <= skip {
			r.srcErr = err
			return false
		}
	case r.at != nil:
		if r.own == nil {
			r.own = make([]byte, inputBuffer)
		}
		// Reading at an offset leaves the source's position alone, so the
		// window can start past the bytes still held in the accumulator.
		r.off += int64(r.pos)
		r.pos = 0
		n, err := r.at.ReadAt(r.own, r.off)
		r.buf = r.own[:n]
		if n == 0 {
			r.srcErr = err
			return false
		}
	default:
		// Nothing may be read ahead, so the accumulator never holds a whole
		// unconsumed byte here.
		x, err := r.br.ReadByte()
		if err != nil {
			r.srcErr = err
			return false
		}
		r.b1[0] = x
		r.buf, r.pos = r.b1[:], 0
	}
	return true
}

// release leaves the source positioned just after the last consumed byte, so
// that a caller reading whatever follows the compressed stream, as image/gif
// does, sees the same thing it would from a byte at a time decoder.
func (r *Reader) release() {
	skip := int(r.nBits) / 8
	if r.srcErr != nil {
		// The source is exhausted; a byte at a time decoder would have
		// consumed the trailing bytes it could not turn into a code too.
		skip = 0
	}
	switch {
	case r.pk != nil:
		if d := r.pos - skip; d > 0 {
			r.pk.Discard(d)
		}
		r.buf, r.pos = nil, skip
	case r.at != nil:
		r.at.Seek(r.off+int64(r.pos-skip), io.SeekStart)
	}
}

var errClosed = errors.New("lzw: reader/writer is closed")

// Close closes the [Reader] and returns an error for any future read operation.
// It does not close the underlying [io.Reader].
func (r *Reader) Close() error {
	r.err = errClosed // in case any Reads come along
	return nil
}

// SetAldusCompatible selects the variant of LZW that Aldus implemented for the
// TIFF file format, which increases the code width one code early: the "off by
// one" difference from the TIFF 5.0 specification that libtiff documents and
// keeps for compatibility. The same variant is what PDF's LZWDecode filter
// specifies by default, through its /EarlyChange value of 1.
//
// Set it before the first Read. It is configuration rather than stream state, so
// it survives [Reader.Reset], including for a pooled [Reader].
//
// Leaving it off costs nothing: the two variants are decoded by separate code.
func (r *Reader) SetAldusCompatible(b bool) {
	r.aldus = b
}

// Reset clears the [Reader]'s state and allows it to be reused again
// as a new [Reader]. It does not change whether the [Reader] is Aldus
// compatible.
func (r *Reader) Reset(src io.Reader, order Order, litWidth int) {
	r.bits, r.nBits = 0, 0
	r.err, r.srcErr = nil, nil
	r.toRead = nil
	r.pk, r.at, r.br = nil, nil, nil
	r.buf, r.pos, r.off = nil, 0, 0
	r.init(src, order, litWidth)
}

// NewReader creates a new [io.ReadCloser].
// Reads from the returned [io.ReadCloser] read and decompress data from r.
// If r does not also implement [io.ByteReader],
// the decompressor may read more data than necessary from r.
// It is the caller's responsibility to call Close on the ReadCloser when
// finished reading.
// The number of bits to use for literal codes, litWidth, must be in the
// range [2,8] and is typically 8. It must equal the litWidth
// used during compression.
//
// It is guaranteed that the underlying type of the returned [io.ReadCloser]
// is a *[Reader].
func NewReader(r io.Reader, order Order, litWidth int) io.ReadCloser {
	return newReader(r, order, litWidth)
}

func newReader(src io.Reader, order Order, litWidth int) *Reader {
	r := new(Reader)
	r.init(src, order, litWidth)
	return r
}

func (r *Reader) init(src io.Reader, order Order, litWidth int) {
	if order != LSB && order != MSB {
		r.err = errors.New("lzw: unknown order")
		return
	}
	if litWidth < 2 || 8 < litWidth {
		r.err = fmt.Errorf("lzw: litWidth %d out of range", litWidth)
		return
	}

	// Pick the cheapest way to read the stream that does not consume more of
	// src than the code stream needs.
	switch t := src.(type) {
	case peeker:
		r.pk = t
	case io.ByteReader:
		if at, ok := src.(readerAt); ok {
			if off, err := at.Seek(0, io.SeekCurrent); err == nil {
				r.at, r.off = at, off
				break
			}
		}
		r.br = t
	default:
		if src != nil {
			// Reading ahead of src is allowed, as it is not an io.ByteReader.
			r.pk = bufio.NewReader(src)
		}
	}

	r.order = order
	lw := uint(litWidth)
	sameLit := r.litWidth == lw
	r.litWidth = lw
	r.width = 1 + lw
	r.clear = uint16(1) << lw
	r.eof, r.hi = r.clear+1, r.clear+1
	r.last = decoderInvalidCode
	r.lastLen, r.lastF8 = 0, 0
	// The literal entries only depend on the code, so a stream of the same
	// litWidth as the last one finds them already there: the codes a stream
	// defines start at clear+2, past its own literal range. A narrower stream
	// defines codes inside a wider one's literal range, though, so a change of
	// litWidth means redoing them.
	if !sameLit {
		for c := 0; c < int(r.clear); c++ {
			r.link[c] = uint32(c)
			r.first8[c] = uint64(c)
		}
	}
}
