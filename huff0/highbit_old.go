// +build !go1.9

package huff0

var deBruijnClz = [...]uint8{0, 9, 1, 10, 13, 21, 2, 29,
	11, 14, 16, 18, 22, 25, 3, 30,
	8, 12, 20, 28, 15, 17, 24, 7,
	19, 27, 23, 6, 26, 5, 4, 31}

// highBit32 returns the highest set bit
func highBit32(v uint32) (n uint32) {
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	return uint32(deBruijnClz[(v*0x07C4ACDD)>>27])
}
