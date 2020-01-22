// +build !appengine
// +build !noasm
// +build gc

package s2

import (
	"encoding/binary"
)

func EncodeGo(dst, src []byte) []byte {
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
	n := encodeBlockGo(dst[d:], src)
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
func encodeBlock(dst, src []byte) (d int) {
	if len(src) >= 32<<10 {
		return encodeBlockAsm(dst, src)
	}
	if len(src) >= 8<<10 {
		return encodeBlockAsm12B(dst, src)
	}
	if len(src) >= 2<<10 {
		return encodeBlockAsm10B(dst, src)
	}
	if len(src) < minNonLiteralBlockSize {
		return 0
	}
	return encodeBlockAsm8B(dst, src)
}
