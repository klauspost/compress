// +build gofuzz,decompress

package zstd

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/DataDog/zstd"
)

func Fuzz(data []byte) int {
	/*
		cc := make(chan struct{})
		defer close(cc)
		go func() {
			c := time.After(5 * time.Second)
			select {
			case <-cc:
				return
			case <-c:
				buf := make([]byte, 1<<20)
				stacklen := runtime.Stack(buf, true)
				msg := fmt.Sprintf("=== Timeout, assuming deadlock ===\n*** goroutine dump...\n%s\n*** end\n", string(buf[:stacklen]))
				panic(msg)
			}
		}()
	*/
	if false {
		dec, err := NewReader(bytes.NewBuffer(data), WithDecoderConcurrency(1))
		if err != nil {
			return 0
		}
		defer dec.Close()
		_, err = io.Copy(ioutil.Discard, dec)
		switch err {
		case nil, ErrCRCMismatch:
			return 1
		}
		return 0
	} else if false {
		dec, err := NewReader(nil, WithDecoderLowmem(true), WithDecoderConcurrency(1), WithDecoderMaxMemory(10<<20))
		if err != nil {
			panic(err)
		}
		defer dec.Close()
		_, err = dec.DecodeAll(data, nil)
		switch err {
		case nil, ErrCRCMismatch:
			return 1
		}
		return 0
	}

	// Run against reference decoder
	dec, err := NewReader(nil, WithDecoderLowmem(true), WithDecoderConcurrency(1), WithDecoderMaxMemory(10<<20))
	if err != nil {
		panic(err)
	}
	defer dec.Close()
	got, err := dec.DecodeAll(data, nil)
	if err == ErrDecoderSizeExceeded {
		// Don't run me out of memory.
		return 0
	}

	ref, refErr := zstd.Decompress(nil, data)

	switch {
	case err == nil:
		if refErr != nil {
			panic(fmt.Errorf("decoder returned no error, but reference returned %v", refErr))
		}
		if !bytes.Equal(ref, got) {
			panic("output mismatch")
		}
		return 1
	case refErr == nil:
		if err != nil {
			panic(fmt.Errorf("reference returned no error, but got %v", err))
		}
	}
	return 0
}
