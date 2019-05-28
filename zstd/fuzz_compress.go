// +build gofuzz,compress

package zstd

import (
	"bytes"
	"fmt"
)

func Fuzz(data []byte) int {
	var e Encoder
	e.Crc = true
	encoded := e.EncodeAll(data, nil)

	// Run test against out decoder
	dec, err := NewReader(nil)
	if err != nil {
		panic(err)
	}
	defer dec.Close()
	got, err := dec.DecodeAll(encoded, nil)
	if err != nil {
		panic(fmt.Sprintln(err, "got:", got, "want:", data))
	}
	if !bytes.Equal(got, data) {
		panic(fmt.Sprintln("output mismatch", got, "(got) != ", data, "(want)"))
	}
	return 1
}
