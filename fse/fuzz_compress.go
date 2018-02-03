// +build gofuzz,compress

package fse

import (
	"bytes"
)

func Fuzz(data []byte) int {
	comp, err := Compress(data, nil)
	if err == ErrIncompressible || err == ErrUseRLE {
		return 0
	}
	if err != nil {
		panic(err)
	}
	dec, err := Decompress(comp, nil)
	if err != nil {
		panic(err)
	}
	if !bytes.Equal(data, dec) {
		panic("decoder mismatch")
	}
	return 1
}
