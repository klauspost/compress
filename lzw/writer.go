// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lzw

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/internal/regmask"
)

// A writer is a buffered, flushable writer. Such a destination did its own
// buffering back when this package wrote to it a byte at a time, and Close
// flushes it for compatibility with that.
type writer interface {
	io.ByteWriter
	Flush() error
}

const (
	// A code is a 12 bit value, stored as a uint32 when encoding to avoid
	// type conversions when shifting bits.
	maxCode     = 1<<12 - 1
	invalidCode = 1<<32 - 1
	// There are 1<<12 possible codes, which is an upper bound on the number of
	// valid hash table entries at any given point in time. tableSize is 4x that.
	tableBits = 12 + 2
	tableSize = 1 << tableBits
	tableMask = tableSize - 1
	// hashMul spreads the 20 bit keys over the table by Fibonacci hashing: the
	// top tableBits of the product are taken. Folding the key onto itself
	// instead clusters badly when the input uses few distinct byte values,
	// since then only a few of the low bits of a key ever vary.
	hashMul = 0x9E3779B1
	// A hash table entry is a uint32. Zero is an invalid entry since the
	// lower 12 bits of a valid entry must be a non-literal code.
	invalidEntry = 0
	// outFlush is the number of pending output bytes at which the output buffer
	// is written out. Codes are added to it eight bytes at a time, so it has
	// room for that much beyond outFlush.
	outFlush = 4096
	outSize  = outFlush + 8
)

// Writer is an LZW compressor. It writes the compressed form of the data
// to an underlying writer (see [NewWriter]).
type Writer struct {
	// w is the writer that compressed bytes are written to.
	w io.Writer
	// litWidth is the width in bits of literal codes.
	litWidth uint
	// order, bits, nBits and width are the state for converting a code stream
	// into a byte stream. bits holds nBits bits not yet written out, at the
	// bottom of the word for LSB and at the top for MSB.
	order Order
	nBits uint
	width uint
	bits  uint64
	// hi is the code implied by the next code emission.
	// overflow is the code at which hi overflows the code width.
	hi, overflow uint32
	// savedCode is the accumulated code at the end of the most recent Write
	// call. It is equal to invalidCode if there was no such call.
	savedCode uint32
	// err is the first error encountered during writing. Closing the writer
	// will make any future Write calls return errClosed
	err error
	// n is the number of bytes of out that are pending.
	n   int
	out [outSize]byte
	// table is the hash table from 20-bit keys to 12-bit values. Each table
	// entry contains key<<12|val and collisions resolve by linear probing.
	// The keys consist of a 12-bit code prefix and an 8-bit byte suffix.
	// The values are a 12-bit code.
	table [tableSize]uint32
}

// emit writes the code c to the output buffer, flushing it if it has filled up.
func (w *Writer) emit(c uint32) error {
	// Whole bytes are taken from the accumulator into out. The store always
	// writes eight bytes, of which only the whole bytes are kept: the rest are
	// overwritten by the next code.
	if w.order == MSB {
		w.bits |= uint64(c) << ((64 - w.width - w.nBits) & regmask.Shift64ByUint)
		w.nBits += w.width
		binary.BigEndian.PutUint64(w.out[w.n:], w.bits)
		w.bits <<= (w.nBits & ^uint(7)) & regmask.Shift64ByUint
	} else {
		w.bits |= uint64(c) << (w.nBits & regmask.Shift64ByUint)
		w.nBits += w.width
		binary.LittleEndian.PutUint64(w.out[w.n:], w.bits)
		w.bits >>= (w.nBits & ^uint(7)) & regmask.Shift64ByUint
	}
	w.n += int(w.nBits >> 3)
	w.nBits &= 7
	if w.n < outFlush {
		return nil
	}
	return w.flush()
}

// flush writes the pending output to the underlying writer.
func (w *Writer) flush() error {
	if w.n == 0 {
		return nil
	}
	n, err := w.w.Write(w.out[:w.n])
	if err == nil && n < w.n {
		err = io.ErrShortWrite
	}
	w.n = 0
	return err
}

// errOutOfCodes is an internal error that means that the writer has run out
// of unused codes and a clear code needs to be sent next.
var errOutOfCodes = errors.New("lzw: out of codes")

// incHi increments e.hi and checks for both overflow and running out of
// unused codes. In the latter case, incHi sends a clear code, resets the
// writer state and returns errOutOfCodes.
func (w *Writer) incHi() error {
	w.hi++
	if w.hi == w.overflow {
		w.width++
		w.overflow <<= 1
	}
	if w.hi == maxCode {
		return w.sendClear()
	}
	return nil
}

// sendClear sends a clear code and resets the writer state. It always returns
// an error, errOutOfCodes if the clear code was sent successfully.
func (w *Writer) sendClear() error {
	clearCode := uint32(1) << w.litWidth
	if err := w.emit(clearCode); err != nil {
		return err
	}
	w.width = w.litWidth + 1
	w.hi = clearCode + 1
	w.overflow = clearCode << 1
	clear(w.table[:])
	return errOutOfCodes
}

// Write writes a compressed representation of p to w's underlying writer.
func (w *Writer) Write(p []byte) (n int, err error) {
	if w.err != nil {
		return 0, w.err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if maxLit := uint8(1<<w.litWidth - 1); maxLit != 0xff {
		for _, x := range p {
			if x > maxLit {
				w.err = errors.New("lzw: input byte too large for the litWidth")
				return 0, w.err
			}
		}
	}
	n = len(p)
	code := w.savedCode
	if code == invalidCode {
		// This is the first write; send a clear code.
		// https://www.w3.org/Graphics/GIF/spec-gif89a.txt Appendix F
		// "Variable-Length-Code LZW Compression" says that "Encoders should
		// output a Clear code as the first code of each image data stream".
		//
		// LZW compression isn't only used by GIF, but it's cheap to follow
		// that directive unconditionally.
		clear := uint32(1) << w.litWidth
		if err := w.emit(clear); err != nil {
			return 0, err
		}
		// After the starting clear code, the next code sent (for non-empty
		// input) is always a literal code.
		code, p = uint32(p[0]), p[1:]
	}
loop:
	for _, x := range p {
		literal := uint32(x)
		key := code<<8 | literal
		// If there is a hash table hit for this key then we continue the loop
		// and do not emit a code yet.
		h := (key * hashMul) >> (32 - tableBits)
		for t := w.table[h]; t != invalidEntry; t = w.table[h] {
			if key == t>>12 {
				code = t & maxCode
				continue loop
			}
			h = (h + 1) & tableMask
		}
		// Otherwise, write the current code, and literal becomes the start of
		// the next emitted code. h indexes the free entry that the probe above
		// stopped at, which is where key belongs.
		if w.err = w.emit(code); w.err != nil {
			return 0, w.err
		}
		code = literal
		// Increment w.hi, the next implied code. If we run out of codes, reset
		// the writer state (including clearing the hash table) and continue.
		w.hi++
		if w.hi == w.overflow {
			w.width++
			w.overflow <<= 1
		}
		if w.hi == maxCode {
			if err1 := w.sendClear(); err1 != errOutOfCodes {
				w.err = err1
				return 0, w.err
			}
			continue
		}
		// Otherwise, insert key -> w.hi into the map that w.table represents.
		w.table[h] = key<<12 | w.hi
	}
	w.savedCode = code
	return n, nil
}

// Close closes the [Writer], flushing any pending output. It does not close
// w's underlying writer.
func (w *Writer) Close() error {
	if w.err != nil {
		if w.err == errClosed {
			return nil
		}
		return w.err
	}
	// Make any future calls to Write return errClosed.
	w.err = errClosed
	// Write the savedCode if valid.
	if w.savedCode != invalidCode {
		if err := w.emit(w.savedCode); err != nil {
			return err
		}
		if err := w.incHi(); err != nil && err != errOutOfCodes {
			return err
		}
	} else {
		// Write the starting clear code, as w.Write did not.
		clear := uint32(1) << w.litWidth
		if err := w.emit(clear); err != nil {
			return err
		}
	}
	// Write the eof code.
	eof := uint32(1)<<w.litWidth + 1
	if err := w.emit(eof); err != nil {
		return err
	}
	// Write the final bits.
	if w.nBits > 0 {
		if w.order == MSB {
			w.out[w.n] = uint8(w.bits >> 56)
		} else {
			w.out[w.n] = uint8(w.bits)
		}
		w.n++
	}
	if err := w.flush(); err != nil {
		return err
	}
	if bw, ok := w.w.(writer); ok {
		return bw.Flush()
	}
	return nil
}

// Reset clears the [Writer]'s state and allows it to be reused again
// as a new [Writer].
func (w *Writer) Reset(dst io.Writer, order Order, litWidth int) {
	w.w = nil
	w.bits, w.nBits, w.n = 0, 0, 0
	w.err = nil
	clear(w.table[:])
	w.init(dst, order, litWidth)
}

// NewWriter creates a new [io.WriteCloser].
// Writes to the returned [io.WriteCloser] are compressed and written to w.
// It is the caller's responsibility to call Close on the WriteCloser when
// finished writing.
// The number of bits to use for literal codes, litWidth, must be in the
// range [2,8] and is typically 8. Input bytes must be less than 1<<litWidth.
//
// It is guaranteed that the underlying type of the returned [io.WriteCloser]
// is a *[Writer].
func NewWriter(w io.Writer, order Order, litWidth int) io.WriteCloser {
	return newWriter(w, order, litWidth)
}

func newWriter(dst io.Writer, order Order, litWidth int) *Writer {
	w := new(Writer)
	w.init(dst, order, litWidth)
	return w
}

func (w *Writer) init(dst io.Writer, order Order, litWidth int) {
	if order != LSB && order != MSB {
		w.err = errors.New("lzw: unknown order")
		return
	}
	if litWidth < 2 || 8 < litWidth {
		w.err = fmt.Errorf("lzw: litWidth %d out of range", litWidth)
		return
	}
	w.w = dst
	lw := uint(litWidth)
	w.order = order
	w.width = 1 + lw
	w.litWidth = lw
	w.hi = 1<<lw + 1
	w.overflow = 1 << (lw + 1)
	w.savedCode = invalidCode
}
