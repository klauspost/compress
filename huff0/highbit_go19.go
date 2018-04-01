// +build go1.9

package huff0

import "math/bits"

func highBit32(val uint32) (n uint32) {
	return uint32(bits.Len32(val) - 1)
}
