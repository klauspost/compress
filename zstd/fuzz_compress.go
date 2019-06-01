// +build gofuzz,compress

package zstd

import (
	"bytes"
	"fmt"
)

func Fuzz(data []byte) int {
	enc, err := NewWriter(nil, WithEncoderCRC(true))
	if err != nil {
		panic(err)
	}
	encoded := enc.EncodeAll(data, nil)
	defer enc.Close()

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
