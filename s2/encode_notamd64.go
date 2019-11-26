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
