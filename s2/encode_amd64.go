// +build !appengine
// +build !noasm
// +build gc

package s2

import "encoding/binary"

func init() {
	avxAvailable = cpu.avx()
}

// encodeBlock encodes a non-empty src to a guaranteed-large-enough dst. It
// assumes that the varint-encoded length of the decompressed bytes has already
// been written.
//
// It also assumes that:
//	len(dst) >= MaxEncodedLen(len(src)) &&
// 	minNonLiteralBlockSize <= len(src) && len(src) <= maxBlockSize
func encodeBlock(dst, src []byte) (d int) {
	const (
		// Use 12 bit table when less than...
		limit12B = 16 << 10
		// Use 10 bit table when less than...
		limit10B = 4 << 10
		// Use 8 bit table when less than...
		limit8B = 512
	)
	if avxAvailable {
		// Big blocks, use full table...
		if len(src) >= limit12B {
			return encodeBlockAsmAvx(dst, src)
		}
		if len(src) >= limit10B {
			return encodeBlockAsm12BAvx(dst, src)
		}
		if len(src) >= limit8B {
			return encodeBlockAsm10BAvx(dst, src)
		}
		if len(src) < minNonLiteralBlockSize {
			return 0
		}
		return encodeBlockAsm8BAvx(dst, src)
	}
	if len(src) >= limit12B {
		return encodeBlockAsm(dst, src)
	}
	if len(src) >= limit10B {
		return encodeBlockAsm12B(dst, src)
	}
	if len(src) >= limit8B {
		return encodeBlockAsm10B(dst, src)
	}
	if len(src) < minNonLiteralBlockSize {
		return 0
	}
	return encodeBlockAsm8B(dst, src)
}

// EncodeSnappy returns the encoded form of src. The returned slice may be a sub-
// slice of dst if dst was large enough to hold the entire encoded block.
// Otherwise, a newly allocated slice will be returned.
//
// The output is Snappy compatible and will likely decompress faster.
//
// The dst and src must not overlap. It is valid to pass a nil dst.
//
// The blocks will require the same amount of memory to decode as encoding,
// and does not make for concurrent decoding.
// Also note that blocks do not contain CRC information, so corruption may be undetected.
//
// If you need to encode larger amounts of data, consider using
// the streaming interface which gives all of these features.
func EncodeSnappy(dst, src []byte) []byte {
	if n := MaxEncodedLen(len(src)); n < 0 {
		panic(ErrTooLarge)
	} else if cap(dst) < n {
		dst = make([]byte, n)
	} else {
		dst = dst[:n]
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

	n := encodeBlockSnappy(dst[d:], src)
	if n > 0 {
		d += n
		return dst[:d]
	}
	// Not compressible
	d += emitLiteral(dst[d:], src)
	return dst[:d]
}

// encodeBlock encodes a non-empty src to a guaranteed-large-enough dst. It
// assumes that the varint-encoded length of the decompressed bytes has already
// been written.
//
// It also assumes that:
//	len(dst) >= MaxEncodedLen(len(src)) &&
// 	minNonLiteralBlockSize <= len(src) && len(src) <= maxBlockSize
func encodeBlockSnappy(dst, src []byte) (d int) {
	const (
		// Use 12 bit table when less than...
		limit12B = 16 << 10
		// Use 10 bit table when less than...
		limit10B = 4 << 10
		// Use 8 bit table when less than...
		limit8B = 512
	)
	if avxAvailable {
		// Big blocks, use full table...
		if len(src) >= limit12B {
			return encodeSnappyBlockAsmAvx(dst, src)
		}
		if len(src) >= limit10B {
			return encodeSnappyBlockAsm12BAvx(dst, src)
		}
		if len(src) >= limit8B {
			return encodeSnappyBlockAsm10BAvx(dst, src)
		}
		if len(src) < minNonLiteralBlockSize {
			return 0
		}
		return encodeSnappyBlockAsm8BAvx(dst, src)
	}
	if len(src) >= limit12B {
		return encodeSnappyBlockAsm(dst, src)
	}
	if len(src) >= limit10B {
		return encodeSnappyBlockAsm12B(dst, src)
	}
	if len(src) >= limit8B {
		return encodeSnappyBlockAsm10B(dst, src)
	}
	if len(src) < minNonLiteralBlockSize {
		return 0
	}
	return encodeSnappyBlockAsm8B(dst, src)
}
