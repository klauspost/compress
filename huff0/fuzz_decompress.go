// +build gofuzz,decompress

package huff0

func Fuzz(data []byte) int {
	s, rem, err := ReadTable(data, nil)
	if err != nil {
		return 0
	}
	_, err1 := s.Decompress1X(rem)
	_, err4 := s.Decompress4X(rem, BlockSizeMax)
	if err1 != nil && err4 != nil {
		return 0
	}
	return 1
}
