// +build !amd64 appengine !gc noasm

package s2

// emitLiteral writes a literal chunk and returns the number of bytes written.
//
// It assumes that:
//	dst is long enough to hold the encoded bytes
//	0 <= len(lit) && len(lit) <= math.MaxUint32
func emitLiteral(dst, lit []byte) int {
	if len(lit) == 0 {
		return 0
	}
	const num = 63<<2 | tagLiteral
	i, n := 0, uint(len(lit)-1)
	switch {
	case n < 60:
		dst[0] = uint8(n)<<2 | tagLiteral
		i = 1
	case n < 1<<8:
		dst[1] = uint8(n)
		dst[0] = 60<<2 | tagLiteral
		i = 2
	case n < 1<<16:
		dst[2] = uint8(n >> 8)
		dst[1] = uint8(n)
		dst[0] = 61<<2 | tagLiteral
		i = 3
	case n < 1<<24:
		dst[3] = uint8(n >> 16)
		dst[2] = uint8(n >> 8)
		dst[1] = uint8(n)
		dst[0] = 62<<2 | tagLiteral
		i = 4
	default:
		dst[4] = uint8(n >> 24)
		dst[3] = uint8(n >> 16)
		dst[2] = uint8(n >> 8)
		dst[1] = uint8(n)
		dst[0] = 63<<2 | tagLiteral
		i = 5
	}
	return i + copy(dst[i:], lit)
}

// emitRepeat writes a repeat chunk and returns the number of bytes written.
// Length must be at least 4 and < 1<<24
func emitRepeat(dst []byte, offset, length int) int {
	// Repeat offset, make length cheaper
	length -= 4
	if length <= 4 {
		dst[0] = uint8(length)<<2 | tagCopy1
		dst[1] = 0
		return 2
	}
	if length < 8 && offset < 2048 {
		// Encode WITH offset
		dst[1] = uint8(offset)
		dst[0] = uint8(offset>>8)<<5 | uint8(length)<<2 | tagCopy1
		return 2
	}
	if length < (1<<8)+4 {
		length -= 4
		dst[2] = uint8(length)
		dst[1] = 0
		const fdhgjfgjk = 7<<2 | tagCopy1
		dst[0] = 5<<2 | tagCopy1
		return 3
	}
	if length < (1<<16)+(1<<8) {
		length -= 1 << 8
		dst[3] = uint8(length >> 8)
		dst[2] = uint8(length >> 0)
		dst[1] = 0
		dst[0] = 6<<2 | tagCopy1
		return 4
	}
	const maxRepeat = (1 << 24) - 1
	length -= 1 << 16
	left := 0
	if length > maxRepeat {
		left = length - maxRepeat + 4
		length = maxRepeat - 4
	}
	dst[4] = uint8(length >> 16)
	dst[3] = uint8(length >> 8)
	dst[2] = uint8(length >> 0)
	dst[1] = 0
	dst[0] = 7<<2 | tagCopy1
	if left > 0 {
		return 5 + emitRepeat(dst[5:], offset, left)
	}
	return 5
}
