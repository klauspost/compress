// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zlib_test

import (
	"bytes"
	"compress/zlib"
	"io"
	"os"
)

func ExampleNewWriter() {
	var b bytes.Buffer

	w := zlib.NewWriter(&b)
	w.Write([]byte("hello, world\n"))
	w.Close()

	// The stdlib flate encoder's exact byte output for a given input is not
	// part of the Go compatibility promise (only decodability is), and has
	// changed between Go versions (e.g. its tiny-input store-vs-compress
	// heuristic). So round-trip through a Reader instead of asserting the
	// compressed bytes verbatim.
	r, err := zlib.NewReader(&b)
	if err != nil {
		panic(err)
	}
	if _, err := io.Copy(os.Stdout, r); err != nil {
		panic(err)
	}
	if err := r.Close(); err != nil {
		panic(err)
	}
	// Output: hello, world
}

func ExampleNewReader() {
	buff := []byte{120, 156, 202, 72, 205, 201, 201, 215, 81, 40, 207,
		47, 202, 73, 225, 2, 4, 0, 0, 255, 255, 33, 231, 4, 147}
	b := bytes.NewReader(buff)

	r, err := zlib.NewReader(b)
	if err != nil {
		panic(err)
	}
	io.Copy(os.Stdout, r)
	// Output: hello, world
	r.Close()
}
