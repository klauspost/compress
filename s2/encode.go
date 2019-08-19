// Copyright 2011 The Snappy-Go Authors. All rights reserved.
// Copyright (c) 2019 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"encoding/binary"
	"errors"
	"io"
	"math/bits"
	"runtime"
	"sync"
)

// Encode returns the encoded form of src. The returned slice may be a sub-
// slice of dst if dst was large enough to hold the entire encoded block.
// Otherwise, a newly allocated slice will be returned.
//
// The dst and src must not overlap. It is valid to pass a nil dst.
//
// The blocks will require the same amount of memory to decode as encoding,
// and does not make for concurrent decoding.
// Also note that blocks do not contain CRC information, so corruption may be undetected.
//
// If you need to encode larger amounts of data, consider using
// the streaming interface which gives all of these features.
func Encode(dst, src []byte) []byte {
	if n := MaxEncodedLen(len(src)); n < 0 {
		panic(ErrTooLarge)
	} else if len(dst) < n {
		dst = make([]byte, n)
	}

	// The block starts with the varint-encoded length of the decompressed bytes.
	d := binary.PutUvarint(dst, uint64(len(src)))

	if len(src) == 0 {
		return dst[:d]
	}
	if len(src) < minNonLiteralBlockSize {
		d += emitLiteral(dst[d:], src)
		return dst[:d]
	}
	n := encodeBlock(dst[d:], src)
	if n > 0 {
		d += n
		return dst[:d]
	}
	// Not compressible
	d += emitLiteral(dst[d:], src)
	return dst[:d]
}

// inputMargin is the minimum number of extra input bytes to keep, inside
// encodeBlock's inner loop. On some architectures, this margin lets us
// implement a fast path for emitLiteral, where the copy of short (<= 16 byte)
// literals can be implemented as a single load to and store from a 16-byte
// register. That literal's actual length can be as short as 1 byte, so this
// can copy up to 15 bytes too much, but that's OK as subsequent iterations of
// the encoding loop will fix up the copy overrun, and this inputMargin ensures
// that we don't overrun the dst and src buffers.
const inputMargin = 8

// minNonLiteralBlockSize is the minimum size of the input to encodeBlock that
// will be accepted by the encoder.
const minNonLiteralBlockSize = 32

// maxExtraLength is the maximum extra that will be emitted from a 4 byte match.
// The copy will be at most 5 bytes (offset > 64k, length 4), but subtract 4 from the match.
// The literal encoding will be at most 5 bytes.
const maxExtraLength = 5 - 4 + 5

// MaxEncodedLen returns the maximum length of a snappy block, given its
// uncompressed length.
//
// It will return a negative value if srcLen is too large to encode.
func MaxEncodedLen(srcLen int) int {
	n := uint64(srcLen)
	if n > 0xffffffff {
		// Also includes negative.
		return -1
	}
	// Size of the varint encoded block size.
	varSize := (bits.Len64(n) + 1) * 9 / 8

	// The encoder will never output blocks that are bigger.
	// This means that for each block, the maximum size will be
	// srcLen + (maximum size of literal encoding == 5)
	n = (n + maxBlockSize - 1) / maxBlockSize
	n *= maxBlockSize + 5
	n += uint64(varSize)
	if n > 0xffffffff {
		return -1
	}
	return int(n)
}

var errClosed = errors.New("s2: Writer is closed")

// NewWriter returns a new Writer that compresses to w, using the
// framing format described at
// https://github.com/google/snappy/blob/master/framing_format.txt
//
// The Writer returned buffers writes. Users must call Close to guarantee all
// data has been forwarded to the underlying io.Writer. They may also call
// Flush zero or more times before calling Close.
func NewWriter(w io.Writer) *Writer {
	w2 := Writer{
		w:           w,
		ibuf:        make([]byte, 0, maxBlockSize),
		obuf:        make([]byte, obufLen),
		concurrency: runtime.GOMAXPROCS(0),
	}
	if w != nil {
		w2.output = make(chan result, w2.concurrency)
	}
	return &w2
}

// Writer is an io.Writer that can write Snappy-compressed bytes.
type Writer struct {
	w   io.Writer
	err error

	// ibuf is a buffer for the incoming (uncompressed) bytes.
	//
	// Its use is optional. For backwards compatibility, Writers created by the
	// NewWriter function have ibuf == nil, do not buffer incoming bytes, and
	// therefore do not need to be Flush'ed or Close'd.
	ibuf []byte

	// obuf is a buffer for the outgoing (compressed) bytes.
	obuf []byte

	// wroteStreamHeader is whether we have written the stream header.
	wroteStreamHeader bool

	concurrency int
	output      chan result
	buffers     sync.Pool
}

type result struct {
	wg  sync.WaitGroup
	b   []byte
	err error
}

// Reset discards the writer's state and switches the Snappy writer to write to
// w. This permits reusing a Writer rather than allocating a new one.
func (w *Writer) Reset(writer io.Writer) {
	w.output = make(chan result, w.concurrency)
	w.w = writer
	w.err = nil
	w.ibuf = w.ibuf[:0]
	w.wroteStreamHeader = false
}

// Write satisfies the io.Writer interface.
func (w *Writer) Write(p []byte) (nRet int, errRet error) {
	// The remainder of this method is based on bufio.Writer.Write from the
	// standard library.

	for len(p) > (cap(w.ibuf)-len(w.ibuf)) && w.err == nil {
		var n int
		if len(w.ibuf) == 0 {
			// Large write, empty buffer.
			// Write directly from p to avoid copy.
			n, _ = w.write(p)
		} else {
			n = copy(w.ibuf[len(w.ibuf):cap(w.ibuf)], p)
			w.ibuf = w.ibuf[:len(w.ibuf)+n]
			w.Flush()
		}
		nRet += n
		p = p[n:]
	}
	if w.err != nil {
		return nRet, w.err
	}
	n := copy(w.ibuf[len(w.ibuf):cap(w.ibuf)], p)
	w.ibuf = w.ibuf[:len(w.ibuf)+n]
	nRet += n
	return nRet, nil
}

// ReadFrom implements the io.ReaderFrom interface.
// Using this is typically more efficient since it avoids a memory copy.
// ReadFrom reads data from r until EOF or error.
// The return value n is the number of bytes read.
// Any error except io.EOF encountered during the read is also returned.
func (w *Writer) ReadFrom(r io.Reader) (n int64, err error) {
	w.ibuf = w.ibuf[0:cap(w.ibuf)]
	for {
		n2, err := io.ReadFull(r, w.ibuf)
		if err != nil {
			if err == io.ErrUnexpectedEOF {
				err = io.EOF
			}
			if err != io.EOF {
				w.err = err
				break
			}
		}
		if n2 == 0 {
			break
		}
		n += int64(n2)
		w.ibuf = w.ibuf[:n2]
		n3, err2 := w.write(w.ibuf)
		if err2 != nil {
			w.err = err2
			break
		}
		if n3 != n2 {
			w.err = errors.New("internal error: size uncompressed size mismatch")
			break
		}
		if err != nil {
			// We got EOF and wrote everything
			break
		}
	}
	w.ibuf = w.ibuf[:0]
	return n, w.err
}

func (w *Writer) write(p []byte) (nRet int, errRet error) {
	if w.err != nil {
		return 0, w.err
	}
	for len(p) > 0 {
		obufStart := len(magicChunk)
		if !w.wroteStreamHeader {
			w.wroteStreamHeader = true
			copy(w.obuf, magicChunk)
			obufStart = 0
		}

		var uncompressed []byte
		if len(p) > maxBlockSize {
			uncompressed, p = p[:maxBlockSize], p[maxBlockSize:]
		} else {
			uncompressed, p = p, nil
		}
		checksum := crc(uncompressed)

		// Set to uncompressed.
		chunkType := uint8(chunkTypeUncompressedData)
		chunkLen := 4 + len(uncompressed)
		obufEnd := obufHeaderLen

		// Attempt compressing.
		n := binary.PutUvarint(w.obuf[obufHeaderLen:], uint64(len(uncompressed)))
		n2 := encodeBlock(w.obuf[obufHeaderLen+n:], uncompressed)
		if n2 > 0 {
			chunkType = uint8(chunkTypeCompressedData)
			chunkLen = 4 + n + n2
			obufEnd = obufHeaderLen + n + n2
		}

		// Fill in the per-chunk header that comes before the body.
		w.obuf[len(magicChunk)+0] = chunkType
		w.obuf[len(magicChunk)+1] = uint8(chunkLen >> 0)
		w.obuf[len(magicChunk)+2] = uint8(chunkLen >> 8)
		w.obuf[len(magicChunk)+3] = uint8(chunkLen >> 16)
		w.obuf[len(magicChunk)+4] = uint8(checksum >> 0)
		w.obuf[len(magicChunk)+5] = uint8(checksum >> 8)
		w.obuf[len(magicChunk)+6] = uint8(checksum >> 16)
		w.obuf[len(magicChunk)+7] = uint8(checksum >> 24)

		if _, err := w.w.Write(w.obuf[obufStart:obufEnd]); err != nil {
			w.err = err
			return nRet, err
		}
		if chunkType == chunkTypeUncompressedData {
			if _, err := w.w.Write(uncompressed); err != nil {
				w.err = err
				return nRet, err
			}
		}
		nRet += len(uncompressed)
	}
	return nRet, nil
}

// Flush flushes the Writer to its underlying io.Writer.
func (w *Writer) Flush() error {
	if w.err != nil {
		return w.err
	}
	if len(w.ibuf) == 0 {
		return nil
	}
	_, w.err = w.write(w.ibuf)
	w.ibuf = w.ibuf[:0]
	return w.err
}

// Close calls Flush and then closes the Writer.
func (w *Writer) Close() error {
	w.Flush()
	ret := w.err
	if w.err == nil {
		w.err = errClosed
	}
	return ret
}
