// +build gofuzz,decompress

package zstd

func Fuzz(data []byte) int {
	dec, err := NewReader(nil)
	if err != nil {
		return 0
	}
	defer dec.Close()
	_, err = dec.DecodeAll(data, nil)
	if err != nil {
		return 0
	}
	return 1
}
