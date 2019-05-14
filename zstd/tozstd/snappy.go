package tozstd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/klauspost/compress/huff0"
)

const (
	tagLiteral = 0x00
	tagCopy1   = 0x01
	tagCopy2   = 0x02
	tagCopy4   = 0x03
)

const (
	checksumSize    = 4
	chunkHeaderSize = 4
	magicChunk      = "\xff\x06\x00\x00" + magicBody
	magicBody       = "sNaPpY"

	// maxBlockSize is the maximum size of the input to encodeBlock. It is not
	// part of the wire format per se, but some parts of the encoder assume
	// that an offset fits into a uint16.
	//
	// Also, for the framing format (Writer type instead of Encode function),
	// https://github.com/google/snappy/blob/master/framing_format.txt says
	// that "the uncompressed data in a chunk must be no longer than 65536
	// bytes".
	maxBlockSize = 65536

	// maxEncodedLenOfMaxBlockSize equals MaxEncodedLen(maxBlockSize), but is
	// hard coded to be a const instead of a variable, so that obufLen can also
	// be a const. Their equivalence is confirmed by
	// TestMaxEncodedLenOfMaxBlockSize.
	maxEncodedLenOfMaxBlockSize = 76490

	obufHeaderLen = len(magicChunk) + checksumSize + chunkHeaderSize
	obufLen       = obufHeaderLen + maxEncodedLenOfMaxBlockSize
)

const (
	chunkTypeCompressedData   = 0x00
	chunkTypeUncompressedData = 0x01
	chunkTypePadding          = 0xfe
	chunkTypeStreamIdentifier = 0xff
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

var crcTable = crc32.MakeTable(crc32.Castagnoli)

// crc implements the checksum specified in section 3 of
// https://github.com/google/snappy/blob/master/framing_format.txt
func crc(b []byte) uint32 {
	c := crc32.Update(0, crcTable, b)
	return uint32(c>>15|c<<17) + 0xa282ead8
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

// Snappy can read Snappy-compressed streams and convert them to zstd.
type Snappy struct {
	r   io.Reader
	err error
	//decoded []byte
	buf []byte
	// decoded[i:j] contains decoded bytes that have not yet been passed on.
	//i, j       int
	//readHeader bool

	//compress chan *block
	//write    chan *block
	block *block
}

func (r *Snappy) readFull(p []byte, allowEOF bool) (ok bool) {
	if _, r.err = io.ReadFull(r.r, p); r.err != nil {
		if r.err == io.ErrUnexpectedEOF || (r.err == io.EOF && !allowEOF) {
			r.err = ErrCorrupt
		}
		return false
	}
	return true
}

func (r *Snappy) Convert(in io.Reader, w io.Writer) (int64, error) {
	r.err = nil
	r.r = in
	if r.block == nil {
		r.block = &block{}
		r.block.init()
	}
	if len(r.buf) != maxEncodedLenOfMaxBlockSize+checksumSize {
		r.buf = make([]byte, maxEncodedLenOfMaxBlockSize+checksumSize)
	}
	r.block.litEnc.Reuse = huff0.ReusePolicyNone
	var written int64
	var readHeader bool
	{
		var header []byte
		var n int
		header, r.err = frameHeader{WindowSize: maxBlockSize}.appendTo(r.buf[:0])

		n, r.err = w.Write(header)
		if r.err != nil {
			return written, r.err
		}
		written += int64(n)
	}

	for {
		if !r.readFull(r.buf[:4], true) {
			// Add empty last block
			r.block.reset()
			r.block.last = true
			err := r.block.encodeLits()
			if err != nil {
				return written, err
			}
			n, err := w.Write(r.block.output)
			if err != nil {
				return written, err
			}
			written += int64(n)

			return written, r.err
		}
		chunkType := r.buf[0]
		if !readHeader {
			if chunkType != chunkTypeStreamIdentifier {
				println("chunkType != chunkTypeStreamIdentifier", chunkType)
				r.err = ErrCorrupt
				return written, r.err
			}
			readHeader = true
		}
		chunkLen := int(r.buf[1]) | int(r.buf[2])<<8 | int(r.buf[3])<<16
		if chunkLen > len(r.buf) {
			r.err = ErrUnsupported
			return written, r.err
		}

		// The chunk types are specified at
		// https://github.com/google/snappy/blob/master/framing_format.txt
		switch chunkType {
		case chunkTypeCompressedData:
			// Section 4.2. Compressed data (chunk type 0x00).
			if chunkLen < checksumSize {
				println("chunkLen < checksumSize", chunkLen, checksumSize)
				r.err = ErrCorrupt
				return written, r.err
			}
			buf := r.buf[:chunkLen]
			if !r.readFull(buf, false) {
				return written, r.err
			}
			//checksum := uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
			buf = buf[checksumSize:]

			n, hdr, err := decodedLen(buf)
			if err != nil {
				r.err = err
				return written, r.err
			}
			buf = buf[hdr:]
			if n > maxBlockSize {
				println("n > maxBlockSize", n, maxBlockSize)
				r.err = ErrCorrupt
				return written, r.err
			}
			r.block.reset()
			if err := decode(r.block, buf); err != nil {
				r.err = err
				return written, r.err
			}
			err = r.block.encode()
			if err != nil {
				return written, err
			}
			n, r.err = w.Write(r.block.output)
			if r.err != nil {
				return written, err
			}
			written += int64(n)

			/*
				if crc(r.decoded[:n]) != checksum {
					r.err = ErrCorrupt
					return 0, r.err
				}
				r.i, r.j = 0, n
			*/
			continue

		case chunkTypeUncompressedData:
			fmt.Println("Uncompressed, chunklen", chunkLen)

			// Section 4.3. Uncompressed data (chunk type 0x01).
			if chunkLen < checksumSize {
				println("chunkLen < checksumSize", chunkLen, checksumSize)
				r.err = ErrCorrupt
				return written, r.err
			}
			r.block.reset()
			buf := r.buf[:checksumSize]
			if !r.readFull(buf, false) {
				return written, r.err
			}
			checksum := uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
			// Read directly into r.decoded instead of via r.buf.
			n := chunkLen - checksumSize
			if n > maxBlockSize {
				println("n > maxBlockSize", n, maxBlockSize)
				r.err = ErrCorrupt
				return written, r.err
			}
			r.block.literals = r.block.literals[:n]
			if !r.readFull(r.block.literals, false) {
				return written, r.err
			}
			if crc(r.block.literals) != checksum {
				println("literals crc mismatch")
				r.err = ErrCorrupt
				return written, r.err
			}
			err := r.block.encodeLits()
			if err != nil {
				return written, err
			}
			n, r.err = w.Write(r.block.output)
			if r.err != nil {
				return written, err
			}
			written += int64(n)
			continue

		case chunkTypeStreamIdentifier:
			println("stream id", chunkLen, len(magicBody))
			// Section 4.1. Stream identifier (chunk type 0xff).
			if chunkLen != len(magicBody) {
				println("chunkLen != len(magicBody)", chunkLen, len(magicBody))
				r.err = ErrCorrupt
				return written, r.err
			}
			if !r.readFull(r.buf[:len(magicBody)], false) {
				return written, r.err
			}
			for i := 0; i < len(magicBody); i++ {
				if r.buf[i] != magicBody[i] {
					println("r.buf[i] != magicBody[i]", r.buf[i], magicBody[i], i)
					r.err = ErrCorrupt
					return written, r.err
				}
			}
			continue
		}

		if chunkType <= 0x7f {
			// Section 4.5. Reserved unskippable chunks (chunk types 0x02-0x7f).
			r.err = ErrUnsupported
			return written, r.err
		}
		// Section 4.4 Padding (chunk type 0xfe).
		// Section 4.6. Reserved skippable chunks (chunk types 0x80-0xfd).
		if !r.readFull(r.buf[:chunkLen], false) {
			return written, r.err
		}
	}
}

// decode writes the decoding of src to dst. It assumes that the varint-encoded
// length of the decompressed bytes has already been read, and that len(dst)
// equals that length.
//
// It returns 0 on success or a decodeErrCodeXxx error code on failure.
func decode(dst *block, src []byte) error {
	//decodeRef(make([]byte, maxBlockSize), src)
	var s, length int
	lits := dst.extraLits
	var offset uint32
	for s < len(src) {
		switch src[s] & 0x03 {
		case tagLiteral:
			x := uint32(src[s] >> 2)
			switch {
			case x < 60:
				s++
			case x == 60:
				s += 2
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					println("uint(s) > uint(len(src)", s, src)
					return ErrCorrupt
				}
				x = uint32(src[s-1])
			case x == 61:
				s += 3
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					println("uint(s) > uint(len(src)", s, src)
					return ErrCorrupt
				}
				x = uint32(src[s-2]) | uint32(src[s-1])<<8
			case x == 62:
				s += 4
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					println("uint(s) > uint(len(src)", s, src)
					return ErrCorrupt
				}
				x = uint32(src[s-3]) | uint32(src[s-2])<<8 | uint32(src[s-1])<<16
			case x == 63:
				s += 5
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					println("uint(s) > uint(len(src)", s, src)
					return ErrCorrupt
				}
				x = uint32(src[s-4]) | uint32(src[s-3])<<8 | uint32(src[s-2])<<16 | uint32(src[s-1])<<24
			}
			if x > maxBlockSize {
				println("x > maxBlockSize", x, maxBlockSize)
				return ErrCorrupt
			}
			length = int(x) + 1
			if length <= 0 {
				println("length <= 0 ", length)

				return errUnsupportedLiteralLength
			}
			//if length > maxBlockSize-d || uint32(length) > len(src)-s {
			//	return ErrCorrupt
			//}

			dst.literals = append(dst.literals, src[s:s+length]...)
			//println(length, "literals")
			lits += length
			s += length
			continue

		case tagCopy1:
			s += 2
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				println("uint(s) > uint(len(src)", s, len(src))
				return ErrCorrupt
			}
			length = 4 + int(src[s-2])>>2&0x7
			offset = uint32(src[s-2])&0xe0<<3 | uint32(src[s-1])

		case tagCopy2:
			s += 3
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				println("uint(s) > uint(len(src)", s, len(src))
				return ErrCorrupt
			}
			length = 1 + int(src[s-3])>>2
			offset = uint32(src[s-2]) | uint32(src[s-1])<<8

		case tagCopy4:
			s += 5
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				println("uint(s) > uint(len(src)", s, len(src))
				return ErrCorrupt
			}
			length = 1 + int(src[s-5])>>2
			offset = uint32(src[s-4]) | uint32(src[s-3])<<8 | uint32(src[s-2])<<16 | uint32(src[s-1])<<24
		}

		if offset <= 0 || dst.size+lits < int(offset) /*|| length > len(dst)-d */ {
			println("offset <= 0 || dst.size+lits < int(offset)", offset, dst.size+lits, int(offset), dst.size, lits)

			return ErrCorrupt
		}
		// Copy from an earlier sub-slice of dst to a later sub-slice. Unlike
		// the built-in copy function, this byte-by-byte copy always runs
		// forwards, even if the slices overlap. Conceptually, this is:
		//
		// d += forwardCopy(dst[d:d+length], dst[d-offset:])
		//for end := d + length; d != end; d++ {
		//	dst[d] = dst[d-offset]
		//}

		//println(length, "match", offset)

		dst.sequences = append(dst.sequences, seq{
			litLen: uint32(lits),
			// TODO: Allow repeat offsets.
			offset:   offset + 3,
			matchLen: uint32(length) - zstdMinMatch,
		})
		dst.size += length + lits
		lits = 0
	}
	dst.extraLits = lits
	//if d != len(dst) {
	//	return ErrCorrupt
	//}
	return nil
}

const zstdMinMatch = 3

// decode writes the decoding of src to dst. It assumes that the varint-encoded
// length of the decompressed bytes has already been read, and that len(dst)
// equals that length.
//
// It returns 0 on success or a decodeErrCodeXxx error code on failure.
func decodeRef(dst, src []byte) (res int) {
	defer func() {
		if res != 0 {
			fmt.Println("reference corrupted")
		}
	}()
	const decodeErrCodeCorrupt = 1
	var d, s, offset, length int
	for s < len(src) {
		switch src[s] & 0x03 {
		case tagLiteral:
			x := uint32(src[s] >> 2)
			switch {
			case x < 60:
				s++
			case x == 60:
				s += 2
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return decodeErrCodeCorrupt
				}
				x = uint32(src[s-1])
			case x == 61:
				s += 3
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return decodeErrCodeCorrupt
				}
				x = uint32(src[s-2]) | uint32(src[s-1])<<8
			case x == 62:
				s += 4
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return decodeErrCodeCorrupt
				}
				x = uint32(src[s-3]) | uint32(src[s-2])<<8 | uint32(src[s-1])<<16
			case x == 63:
				s += 5
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return decodeErrCodeCorrupt
				}
				x = uint32(src[s-4]) | uint32(src[s-3])<<8 | uint32(src[s-2])<<16 | uint32(src[s-1])<<24
			}
			length = int(x) + 1
			if length <= 0 {
				return decodeErrCodeCorrupt
			}
			if length > len(dst)-d || length > len(src)-s {
				return decodeErrCodeCorrupt
			}
			copy(dst[d:], src[s:s+length])
			println(length, "literal (REF)")

			d += length
			s += length
			continue

		case tagCopy1:
			s += 2
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				return decodeErrCodeCorrupt
			}
			length = 4 + int(src[s-2])>>2&0x7
			offset = int(uint32(src[s-2])&0xe0<<3 | uint32(src[s-1]))

		case tagCopy2:
			s += 3
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				return decodeErrCodeCorrupt
			}
			length = 1 + int(src[s-3])>>2
			offset = int(uint32(src[s-2]) | uint32(src[s-1])<<8)

		case tagCopy4:
			s += 5
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				return decodeErrCodeCorrupt
			}
			length = 1 + int(src[s-5])>>2
			offset = int(uint32(src[s-4]) | uint32(src[s-3])<<8 | uint32(src[s-2])<<16 | uint32(src[s-1])<<24)
		}

		if offset <= 0 || d < offset || length > len(dst)-d {
			return decodeErrCodeCorrupt
		}
		// Copy from an earlier sub-slice of dst to a later sub-slice. Unlike
		// the built-in copy function, this byte-by-byte copy always runs
		// forwards, even if the slices overlap. Conceptually, this is:
		//
		// d += forwardCopy(dst[d:d+length], dst[d-offset:])

		println(length, "match (REF)", offset)

		for end := d + length; d != end; d++ {
			dst[d] = dst[d-offset]
		}
	}
	if d != len(dst) {
		return decodeErrCodeCorrupt
	}
	return 0
}
