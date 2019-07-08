// Copyright 2011 The Snappy-Go Authors. All rights reserved.
// Copyright (c) 2019 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"encoding/binary"
	"errors"
	"io"
)

var (
	// ErrCorrupt reports that the input is invalid.
	ErrCorrupt = errors.New("snappy: corrupt input")
	// ErrTooLarge reports that the uncompressed length is too large.
	ErrTooLarge = errors.New("snappy: decoded block is too large")
	// ErrUnsupported reports that the input isn't supported.
	ErrUnsupported = errors.New("snappy: unsupported input")

	errUnsupportedLiteralLength = errors.New("snappy: unsupported literal length")
)

// DecodedLen returns the length of the decoded block.
func DecodedLen(src []byte) (int, error) {
	v, _, err := decodedLen(src)
	return v, err
}

// decodedLen returns the length of the decoded block and the number of bytes
// that the length header occupied.
func decodedLen(src []byte) (blockLen, headerLen int, err error) {
	v, n := binary.Uvarint(src)
	if n <= 0 || v > 0xffffffff {
		return 0, 0, ErrCorrupt
	}

	const wordSize = 32 << (^uint(0) >> 32 & 1)
	if wordSize == 32 && v > 0x7fffffff {
		return 0, 0, ErrTooLarge
	}
	return int(v), n, nil
}

const (
	decodeErrCodeCorrupt                  = 1
	decodeErrCodeUnsupportedLiteralLength = 2
)

// Decode returns the decoded form of src. The returned slice may be a sub-
// slice of dst if dst was large enough to hold the entire decoded block.
// Otherwise, a newly allocated slice will be returned.
//
// The dst and src must not overlap. It is valid to pass a nil dst.
func Decode(dst, src []byte) ([]byte, error) {
	dLen, s, err := decodedLen(src)
	if err != nil {
		return nil, err
	}
	if dLen <= len(dst) {
		dst = dst[:dLen]
	} else {
		dst = make([]byte, dLen)
	}
	switch decode(dst, src[s:]) {
	case 0:
		return dst, nil
	case decodeErrCodeUnsupportedLiteralLength:
		return nil, errUnsupportedLiteralLength
	}
	return nil, ErrCorrupt
}

// NewReader returns a new Reader that decompresses from r, using the framing
// format described at
// https://github.com/google/snappy/blob/master/framing_format.txt
func NewReader(r io.Reader) *Reader {
	return &Reader{
		r:       r,
		decoded: make([]byte, maxBlockSize),
		buf:     make([]byte, maxEncodedLenOfMaxBlockSize+checksumSize),
	}
}

// Reader is an io.Reader that can read Snappy-compressed bytes.
type Reader struct {
	r       io.Reader
	err     error
	decoded []byte
	buf     []byte
	// decoded[i:j] contains decoded bytes that have not yet been passed on.
	i, j       int
	readHeader bool
}

// Reset discards any buffered data, resets all state, and switches the Snappy
// reader to read from r. This permits reusing a Reader rather than allocating
// a new one.
func (r *Reader) Reset(reader io.Reader) {
	r.r = reader
	r.err = nil
	r.i = 0
	r.j = 0
	r.readHeader = false
}

func (r *Reader) readFull(p []byte, allowEOF bool) (ok bool) {
	if _, r.err = io.ReadFull(r.r, p); r.err != nil {
		if r.err == io.ErrUnexpectedEOF || (r.err == io.EOF && !allowEOF) {
			r.err = ErrCorrupt
		}
		return false
	}
	return true
}

// Read satisfies the io.Reader interface.
func (r *Reader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	for {
		if r.i < r.j {
			n := copy(p, r.decoded[r.i:r.j])
			r.i += n
			return n, nil
		}
		if !r.readFull(r.buf[:4], true) {
			return 0, r.err
		}
		chunkType := r.buf[0]
		if !r.readHeader {
			if chunkType != chunkTypeStreamIdentifier {
				r.err = ErrCorrupt
				return 0, r.err
			}
			r.readHeader = true
		}
		chunkLen := int(r.buf[1]) | int(r.buf[2])<<8 | int(r.buf[3])<<16
		if chunkLen > len(r.buf) {
			r.err = ErrUnsupported
			return 0, r.err
		}

		// The chunk types are specified at
		// https://github.com/google/snappy/blob/master/framing_format.txt
		switch chunkType {
		case chunkTypeCompressedData:
			// Section 4.2. Compressed data (chunk type 0x00).
			if chunkLen < checksumSize {
				r.err = ErrCorrupt
				return 0, r.err
			}
			buf := r.buf[:chunkLen]
			if !r.readFull(buf, false) {
				return 0, r.err
			}
			checksum := uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
			buf = buf[checksumSize:]

			n, err := DecodedLen(buf)
			if err != nil {
				r.err = err
				return 0, r.err
			}
			if n > len(r.decoded) {
				r.err = ErrCorrupt
				return 0, r.err
			}
			if _, err := Decode(r.decoded, buf); err != nil {
				r.err = err
				return 0, r.err
			}
			if crc(r.decoded[:n]) != checksum {
				r.err = ErrCorrupt
				return 0, r.err
			}
			r.i, r.j = 0, n
			continue

		case chunkTypeUncompressedData:
			// Section 4.3. Uncompressed data (chunk type 0x01).
			if chunkLen < checksumSize {
				r.err = ErrCorrupt
				return 0, r.err
			}
			buf := r.buf[:checksumSize]
			if !r.readFull(buf, false) {
				return 0, r.err
			}
			checksum := uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
			// Read directly into r.decoded instead of via r.buf.
			n := chunkLen - checksumSize
			if n > len(r.decoded) {
				r.err = ErrCorrupt
				return 0, r.err
			}
			if !r.readFull(r.decoded[:n], false) {
				return 0, r.err
			}
			if crc(r.decoded[:n]) != checksum {
				r.err = ErrCorrupt
				return 0, r.err
			}
			r.i, r.j = 0, n
			continue

		case chunkTypeStreamIdentifier:
			// Section 4.1. Stream identifier (chunk type 0xff).
			if chunkLen != len(magicBody) {
				r.err = ErrCorrupt
				return 0, r.err
			}
			if !r.readFull(r.buf[:len(magicBody)], false) {
				return 0, r.err
			}
			ok := true
			for i := 0; i < len(magicBody); i++ {
				if r.buf[i] != magicBody[i] {
					ok = false
					break
				}
			}
			if !ok {
				for i := 0; i < len(magicBodySnappy); i++ {
					if r.buf[i] != magicBodySnappy[i] {
						r.err = ErrCorrupt
						return 0, r.err
					}
				}
			}
			continue
		}

		if chunkType <= 0x7f {
			// Section 4.5. Reserved unskippable chunks (chunk types 0x02-0x7f).
			r.err = ErrUnsupported
			return 0, r.err
		}
		// Section 4.4 Padding (chunk type 0xfe).
		// Section 4.6. Reserved skippable chunks (chunk types 0x80-0xfd).
		if !r.readFull(r.buf[:chunkLen], false) {
			return 0, r.err
		}
	}
}

// Skip will skip n bytes forward in the decompressed output.
// For larger skips this consumes less CPU and is faster than reading output and discarding it.
// CRC is not checked on skipped compressed blocks.
// io.ErrUnexpectedEOF is returned if the stream ends before all bytes have been skipped.
// If a decoding error is encountered subsequent calls to Read will also fail.
func (r *Reader) Skip(n int64) error {
	if n < 0 {
		return errors.New("attempted negative skip")
	}
	if r.err != nil {
		return r.err
	}
	if r.i < r.j {
		// Skip in buffer.
		// decoded[i:j] contains decoded bytes that have not yet been passed on.
		left := int64(r.j - r.i)
		if left >= n {
			r.i += int(n)
			return nil
		}
		r.i, r.j = 0, 0
	}
	// Buffer empty.
	for n > 0 {
		if !r.readFull(r.buf[:4], true) {
			if r.err == io.EOF {
				r.err = io.ErrUnexpectedEOF
			}
			return r.err
		}
		chunkType := r.buf[0]
		if !r.readHeader {
			if chunkType != chunkTypeStreamIdentifier {
				r.err = ErrCorrupt
				return r.err
			}
			r.readHeader = true
		}
		chunkLen := int(r.buf[1]) | int(r.buf[2])<<8 | int(r.buf[3])<<16
		if chunkLen > len(r.buf) {
			r.err = ErrUnsupported
			return r.err
		}

		// The chunk types are specified at
		// https://github.com/google/snappy/blob/master/framing_format.txt
		switch chunkType {
		case chunkTypeCompressedData:
			// Section 4.2. Compressed data (chunk type 0x00).
			if chunkLen < checksumSize {
				r.err = ErrCorrupt
				return r.err
			}
			buf := r.buf[:chunkLen]
			if !r.readFull(buf, false) {
				return r.err
			}
			checksum := uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
			buf = buf[checksumSize:]

			dLen, err := DecodedLen(buf)
			if err != nil {
				r.err = err
				return r.err
			}
			if dLen > len(r.decoded) {
				r.err = ErrCorrupt
				return r.err
			}
			if int64(dLen) > n {
				if _, err := Decode(r.decoded, buf); err != nil {
					r.err = err
					return r.err
				}
				if crc(r.decoded[:dLen]) != checksum {
					r.err = ErrCorrupt
					return r.err
				}
			} else {
				n -= int64(dLen)
			}
			// We should now have enough data.
			// Skip partial block, should only recurse once.
			r.i, r.j = 0, dLen
			return r.Skip(n)
		case chunkTypeUncompressedData:
			// Section 4.3. Uncompressed data (chunk type 0x01).
			if chunkLen < checksumSize {
				r.err = ErrCorrupt
				return r.err
			}
			buf := r.buf[:checksumSize]
			if !r.readFull(buf, false) {
				return r.err
			}
			checksum := uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
			// Read directly into r.decoded instead of via r.buf.
			n := chunkLen - checksumSize
			if n > len(r.decoded) {
				r.err = ErrCorrupt
				return r.err
			}
			if !r.readFull(r.decoded[:n], false) {
				return r.err
			}
			if crc(r.decoded[:n]) != checksum {
				r.err = ErrCorrupt
				return r.err
			}
			r.i, r.j = 0, n
			continue

		case chunkTypeStreamIdentifier:
			// Section 4.1. Stream identifier (chunk type 0xff).
			if chunkLen != len(magicBody) {
				r.err = ErrCorrupt
				return r.err
			}
			if !r.readFull(r.buf[:len(magicBody)], false) {
				return r.err
			}
			ok := true
			for i := 0; i < len(magicBody); i++ {
				if r.buf[i] != magicBody[i] {
					ok = false
					break
				}
			}
			if !ok {
				for i := 0; i < len(magicBodySnappy); i++ {
					if r.buf[i] != magicBodySnappy[i] {
						r.err = ErrCorrupt
						return r.err
					}
				}
			}
			continue
		}

		if chunkType <= 0x7f {
			// Section 4.5. Reserved unskippable chunks (chunk types 0x02-0x7f).
			r.err = ErrUnsupported
			return r.err
		}
		// Section 4.4 Padding (chunk type 0xfe).
		// Section 4.6. Reserved skippable chunks (chunk types 0x80-0xfd).
		if !r.readFull(r.buf[:chunkLen], false) {
			return r.err
		}

		return io.ErrUnexpectedEOF
	}
	return nil
}
