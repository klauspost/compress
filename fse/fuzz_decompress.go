// +build gofuzz,decompress

package fse

import "strings"

func Fuzz(data []byte) int {
	dec, err := Decompress(data, nil)
	if err != nil && strings.Contains(err.Error(), "DecompressLimit") {
		panic(err)
	}
	if err != nil {
		return 0
	}
	if len(dec) == 0 {
		panic("no output")
	}
	return 1
}
