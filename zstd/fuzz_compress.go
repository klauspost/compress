// +build gofuzz,compress

package zstd

import (
	"bytes"
	"fmt"
	"io/ioutil"
)

func Fuzz(data []byte) int {
	var dst bytes.Buffer
	enc, err := NewWriter(&dst, WithEncoderCRC(true))
	if err != nil {
		panic(err)
	}
	encoded := enc.EncodeAll(data, nil)
	n, err := enc.Write(data)
	if err != nil {
		panic(err)
	}
	if n != len(data) {
		panic(fmt.Sprintln("Short write, got:", n, "want:", len(data)))
	}
	enc.Close()

	// Run test against out decoder
	dec, err := NewReader(&dst)
	if err != nil {
		panic(err)
	}
	defer dec.Close()
	got, err := dec.DecodeAll(encoded, nil)
	if err != nil {
		panic(fmt.Sprintln("DecodeAll error:", err, "got:", got, "want:", data))
	}
	if !bytes.Equal(got, data) {
		panic(fmt.Sprintln("DecodeAll output mismatch", got, "(got) != ", data, "(want)"))
	}

	// Run streamed test against out decoder
	got, err = ioutil.ReadAll(dec)
	if err != nil {
		panic(fmt.Sprintln("Reader:", err, "got:", got, "want:", data))
	}
	if !bytes.Equal(got, data) {
		panic(fmt.Sprintln("Reader output mismatch", got, "(got) != ", data, "(want)"))
	}
	return 1
}
