// +build !appengine
// +build !noasm
// +build gc

package s2

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
	if avxAvailable {
		// Big blocks, use full table...
		if len(src) >= 32<<10 {
			return encodeBlockAsmAvx(dst, src)
		}
		if len(src) >= 8<<10 {
			return encodeBlockAsm12BAvx(dst, src)
		}
		if len(src) >= 2<<10 {
			return encodeBlockAsm10BAvx(dst, src)
		}
		if len(src) < minNonLiteralBlockSize {
			return 0
		}
		return encodeBlockAsm8BAvx(dst, src)
	}
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
