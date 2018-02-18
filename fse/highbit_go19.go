// +build go1.9

package fse

import "math/bits"

func highBits(val uint32) (n uint32) {
	return uint32(bits.Len32(val) - 1)
}
