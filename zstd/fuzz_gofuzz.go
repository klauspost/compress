// +build gofuzz

package zstd

import (
	"bytes"
	"fmt"
	"sync"
)

var dec *Decoder
var encs [speedLast]*Encoder
var mu sync.Mutex

func init() {
	var err error
	dec, err = NewReader(nil, WithDecoderConcurrency(1))
	if err != nil {
		panic(err)
	}
	for level := EncoderLevel(speedNotSet + 1); level < speedLast; level++ {
		encs[level], err = NewWriter(nil, WithEncoderCRC(false), WithEncoderLevel(level), WithEncoderConcurrency(1))
	}
}

func FuzzCompress(data []byte) int {
	mu.Lock()
	defer mu.Unlock()
	// Run test against out decoder
	for level := EncoderLevel(speedNotSet + 1); level < speedLast; level++ {
		var dst bytes.Buffer
		enc := encs[level]
		//enc, err := NewWriter(nil, WithEncoderCRC(true), WithEncoderLevel(level), WithEncoderConcurrency(1))
		encoded := enc.EncodeAll(data, make([]byte, 0, len(data)))
		enc.Reset(&dst)

		n, err := enc.Write(data)
		if err != nil {
			panic(err)
		}
		if n != len(data) {
			panic(fmt.Sprintln("Level", level, "Short write, got:", n, "want:", len(data)))
		}
		err = enc.Close()
		if err != nil {
			panic(err)
		}
		got, err := dec.DecodeAll(encoded, make([]byte, 0, len(data)))
		if err != nil {
			panic(fmt.Sprintln("Level", level, "DecodeAll error:", err, "got:", got, "want:", data, "encoded", encoded))
		}
		if !bytes.Equal(got, data) {
			panic(fmt.Sprintln("Level", level, "DecodeAll output mismatch", got, "(got) != ", data, "(want)", "encoded", encoded))
		}

		encoded = dst.Bytes()
		got, err = dec.DecodeAll(encoded, make([]byte, 0, len(data)))
		if err != nil {
			panic(fmt.Sprintln("Level", level, "DecodeAll (buffer) error:", err, "got:", got, "want:", data, "encoded", encoded))
		}
		if !bytes.Equal(got, data) {
			panic(fmt.Sprintln("Level", level, "DecodeAll (buffer) output mismatch", got, "(got) != ", data, "(want)", "encoded", encoded))
		}
	}
	return 1
}
