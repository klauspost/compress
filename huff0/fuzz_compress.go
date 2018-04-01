// +build gofuzz,compress

package huff0

import "bytes"

func Fuzz(data []byte) int {
	comp, _, err := Compress1X(data, nil)
	if err == ErrIncompressible || err == ErrUseRLE {
		return 0
	}
	if err != nil {
		panic(err)
	}
	var s = &Scratch{}
	s, remain, err := s.ReadTable(comp)
	if err != nil {
		panic(err)
	}
	out, err := s.Decompress1X(remain)
	if err != nil {
		panic(err)
	}
	if !bytes.Equal(out, data) {
		panic("decompression mismatch")
	}
	return 1
}
