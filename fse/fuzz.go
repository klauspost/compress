// +build gofuzz

package fse

import (
	"bytes"
)

func Fuzz(data []byte) int {
	if false {
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
	dec, err := Decompress(comp, nil)
	if err != nil {
		return 0
	}
	return 1
}
