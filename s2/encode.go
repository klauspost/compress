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

// EncodeBetter returns the encoded form of src. The returned slice may be a sub-
// slice of dst if dst was large enough to hold the entire encoded block.
// Otherwise, a newly allocated slice will be returned.
//
// EncodeBetter compresses better than Encode but typically with a
// 10-40% speed decrease on both compression and decompression.
//
// The dst and src must not overlap. It is valid to pass a nil dst.
//
// The blocks will require the same amount of memory to decode as encoding,
// and does not make for concurrent decoding.
// Also note that blocks do not contain CRC information, so corruption may be undetected.
//
// If you need to encode larger amounts of data, consider using
// the streaming interface which gives all of these features.
func EncodeBetter(dst, src []byte) []byte {
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
	n := encodeBlockBetter(dst[d:], src)
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
func NewWriter(w io.Writer, opts ...WriterOption) *Writer {
	w2 := Writer{
		ibuf:        make([]byte, 0, maxBlockSize),
		concurrency: runtime.GOMAXPROCS(0),
	}
	for _, opt := range opts {
		if err := opt(&w2); err != nil {
			w2.errState = err
			return &w2
		}
	}
	w2.paramsOK = true
	w2.buffers.New = func() interface{} {
		return make([]byte, obufLen)
	}
	w2.Reset(w)
	return &w2
}

// Writer is an io.Writer that can write Snappy-compressed bytes.
type Writer struct {
	errMu    sync.Mutex
	errState error

	// ibuf is a buffer for the incoming (uncompressed) bytes.
	ibuf []byte

	// wroteStreamHeader is whether we have written the stream header.
	wroteStreamHeader bool
	paramsOK          bool
	better            bool

	concurrency int
	output      chan chan result
	buffers     sync.Pool
	writerWg    sync.WaitGroup
	writer      io.Writer
}

type result []byte

// err returns the previously set error.
// If no error has been set it is set to err if not nil.
func (w *Writer) err(err error) error {
	w.errMu.Lock()
	errSet := w.errState
	if errSet == nil && err != nil {
		w.errState = err
		errSet = err
	}
	w.errMu.Unlock()
	return errSet
}

// Reset discards the writer's state and switches the Snappy writer to write to
// w. This permits reusing a Writer rather than allocating a new one.
func (w *Writer) Reset(writer io.Writer) {
	if !w.paramsOK {
		return
	}
	if w.output != nil {
		close(w.output)
		w.writerWg.Wait()
		w.output = nil
	}
	w.errState = nil
	w.ibuf = w.ibuf[:0]
	w.wroteStreamHeader = false
	if writer == nil {
		return
	}
	if w.concurrency == 1 {
		w.writer = writer
		return
	}
	toWrite := make(chan chan result, w.concurrency)
	w.output = toWrite
	w.writerWg.Add(1)

	// Start a writer goroutine that will write all output in order.
	go func() {
		defer w.writerWg.Done()
		for write := range toWrite {
			in := <-write
			if len(in) > 0 {
				if w.err(nil) == nil {
					// Don't expose data from previous buffers.
					in = in[:len(in):len(in)]
					// Write to output.
					n, err := writer.Write(in)
					if err == nil && n != len(in) {
						err = io.ErrShortBuffer
					}
					_ = w.err(err)
				}
				if cap(in) >= obufLen {
					w.buffers.Put([]byte(in))
				}
			}
			// close the incoming write request.
			// This can be used for synchronizing flushes.
			close(write)
		}
	}()
}

// Write satisfies the io.Writer interface.
func (w *Writer) Write(p []byte) (nRet int, errRet error) {
	// The remainder of this method is based on bufio.Writer.Write from the
	// standard library.
	for len(p) > (cap(w.ibuf)-len(w.ibuf)) && w.err(nil) == nil {
		var n int
		if len(w.ibuf) == 0 {
			// Large write, empty buffer.
			// Write directly from p to avoid copy.
			n, _ = w.write(p)
		} else {
			n = copy(w.ibuf[len(w.ibuf):cap(w.ibuf)], p)
			w.ibuf = w.ibuf[:len(w.ibuf)+n]
			w.write(w.ibuf)
			w.ibuf = w.ibuf[:0]
		}
		nRet += n
		p = p[n:]
	}
	if err := w.err(nil); err != nil {
		return nRet, err
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
	if len(w.ibuf) > 0 {
		err := w.Flush()
		if err != nil {
			return 0, err
		}
	}
	w.ibuf = w.ibuf[:maxBlockSize]
	for {
		n2, err := io.ReadFull(r, w.ibuf)
		if err != nil {
			if err == io.ErrUnexpectedEOF {
				err = io.EOF
			}
			if err != io.EOF {
				return n, w.err(err)
			}
		}
		if n2 == 0 {
			break
		}
		n += int64(n2)
		w.ibuf = w.ibuf[:n2]
		n3, err2 := w.write(w.ibuf)
		if w.err(err2) != nil {
			break
		}
		if n3 != n2 {
			return n, w.err(errors.New("internal error: size uncompressed size mismatch"))
		}
		if err != nil {
			// We got EOF and wrote everything
			break
		}
	}
	w.ibuf = w.ibuf[:0]
	return n, w.err(nil)
}

func (w *Writer) write(p []byte) (nRet int, errRet error) {
	if err := w.err(nil); err != nil {
		return 0, err
	}
	if w.concurrency == 1 {
		return w.writeSync(p)
	}
	for len(p) > 0 {
		if !w.wroteStreamHeader {
			w.wroteStreamHeader = true
			hWriter := make(chan result)
			w.output <- hWriter
			hWriter <- []byte(magicChunk)
		}

		var uncompressed []byte
		if len(p) > maxBlockSize {
			uncompressed, p = p[:maxBlockSize], p[maxBlockSize:]
		} else {
			uncompressed, p = p, nil
		}

		// Copy input.
		// If the block is incompressible, this is used for the result.
		inbuf := w.buffers.Get().([]byte)[:len(uncompressed)+obufHeaderLen]
		obuf := w.buffers.Get().([]byte)[:obufLen]
		copy(inbuf[obufHeaderLen:], uncompressed)
		uncompressed = inbuf[obufHeaderLen:]

		output := make(chan result)
		w.output <- output
		go func() {
			checksum := crc(uncompressed)

			// Set to uncompressed.
			chunkType := uint8(chunkTypeUncompressedData)
			chunkLen := 4 + len(uncompressed)

			// Attempt compressing.
			n := binary.PutUvarint(obuf[obufHeaderLen:], uint64(len(uncompressed)))
			var n2 int
			if w.better {
				n2 = encodeBlockBetter(obuf[obufHeaderLen+n:], uncompressed)
			} else {
				n2 = encodeBlock(obuf[obufHeaderLen+n:], uncompressed)
			}

			if n2 > 0 {
				chunkType = uint8(chunkTypeCompressedData)
				chunkLen = 4 + n + n2
				obuf = obuf[:obufHeaderLen+n+n2]
				w.buffers.Put(inbuf)
				inbuf = nil
			} else {
				// Discard output buffer.
				w.buffers.Put(obuf)
				obuf = inbuf
			}

			// Fill in the per-chunk header that comes before the body.
			obuf[0] = chunkType
			obuf[1] = uint8(chunkLen >> 0)
			obuf[2] = uint8(chunkLen >> 8)
			obuf[3] = uint8(chunkLen >> 16)
			obuf[4] = uint8(checksum >> 0)
			obuf[5] = uint8(checksum >> 8)
			obuf[6] = uint8(checksum >> 16)
			obuf[7] = uint8(checksum >> 24)

			// Queue final output.
			output <- obuf
		}()
		nRet += len(uncompressed)
	}
	return nRet, nil
}

func (w *Writer) writeSync(p []byte) (nRet int, errRet error) {
	if err := w.err(nil); err != nil {
		return 0, err
	}
	for len(p) > 0 {
		if !w.wroteStreamHeader {
			w.wroteStreamHeader = true
			n, err := w.writer.Write([]byte(magicChunk))
			if err != nil {
				return 0, w.err(err)
			}
			if n != len(magicChunk) {
				return 0, w.err(io.ErrShortWrite)
			}
		}

		var uncompressed []byte
		if len(p) > maxBlockSize {
			uncompressed, p = p[:maxBlockSize], p[maxBlockSize:]
		} else {
			uncompressed, p = p, nil
		}

		obuf := w.buffers.Get().([]byte)[:obufLen]
		checksum := crc(uncompressed)

		// Set to uncompressed.
		chunkType := uint8(chunkTypeUncompressedData)
		chunkLen := 4 + len(uncompressed)

		// Attempt compressing.
		n := binary.PutUvarint(obuf[obufHeaderLen:], uint64(len(uncompressed)))
		var n2 int
		if w.better {
			n2 = encodeBlockBetter(obuf[obufHeaderLen+n:], uncompressed)
		} else {
			n2 = encodeBlock(obuf[obufHeaderLen+n:], uncompressed)
		}

		if n2 > 0 {
			chunkType = uint8(chunkTypeCompressedData)
			chunkLen = 4 + n + n2
			obuf = obuf[:obufHeaderLen+n+n2]
		} else {
			obuf = obuf[:8]
		}

		// Fill in the per-chunk header that comes before the body.
		obuf[0] = chunkType
		obuf[1] = uint8(chunkLen >> 0)
		obuf[2] = uint8(chunkLen >> 8)
		obuf[3] = uint8(chunkLen >> 16)
		obuf[4] = uint8(checksum >> 0)
		obuf[5] = uint8(checksum >> 8)
		obuf[6] = uint8(checksum >> 16)
		obuf[7] = uint8(checksum >> 24)

		n, err := w.writer.Write(obuf)
		if err != nil {
			return 0, w.err(err)
		}
		if n != len(obuf) {
			return 0, w.err(io.ErrShortWrite)
		}
		if chunkType == chunkTypeUncompressedData {
			// Write uncompressed data.
			n, err := w.writer.Write(uncompressed)
			if err != nil {
				return 0, w.err(err)
			}
			if n != len(uncompressed) {
				return 0, w.err(io.ErrShortWrite)
			}
		}
		w.buffers.Put(obuf)
		// Queue final output.
		nRet += len(uncompressed)
	}
	return nRet, nil
}

// Flush flushes the Writer to its underlying io.Writer.
func (w *Writer) Flush() error {
	if err := w.err(nil); err != nil {
		return err
	}

	// Queue any data still in input buffer.
	if len(w.ibuf) != 0 {
		_, err := w.write(w.ibuf)
		w.ibuf = w.ibuf[:0]
		err = w.err(err)
		if err != nil {
			return err
		}
	}
	if w.output == nil {
		return w.err(nil)
	}

	// Send empty buffer
	res := make(chan result)
	w.output <- res
	// Block until this has been picked up.
	res <- nil
	// When it is closed, we have flushed.
	<-res
	return w.err(nil)
}

// Close calls Flush and then closes the Writer.
func (w *Writer) Close() error {
	err := w.Flush()
	_ = w.err(errClosed)
	return err
}

// WriterOption is an option for creating a encoder.
type WriterOption func(*Writer) error

// WriterConcurrency will set the concurrency,
// meaning the maximum number of decoders to run concurrently.
// The value supplied must be at least 1.
// By default this will be set to GOMAXPROCS.
func WriterConcurrency(n int) WriterOption {
	return func(w *Writer) error {
		if n <= 0 {
			return errors.New("concurrency must be at least 1")
		}
		w.concurrency = n
		return nil
	}
}

// WriterBetterCompression will enable better compression.
// EncodeBetter compresses better than Encode but typically with a
// 10-40% speed decrease on both compression and decompression.
func WriterBetterCompression() WriterOption {
	return func(w *Writer) error {
		w.better = true
		return nil
	}
}
