// +build gofuzz,compress

package huff0

func Fuzz(data []byte) int {
	_, _, err := Compress1X(data, nil)
	if err == ErrIncompressible || err == ErrUseRLE {
		return 0
	}
	if err != nil {
		panic(err)
	}
	return 1
}
