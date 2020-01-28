// Copyright (c) 2019 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
)

func TestEncoderRegression(t *testing.T) {
	data, err := ioutil.ReadFile("testdata/enc_regressions.zip")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	// Same as fuzz test...
	test := func(t *testing.T, data []byte) {
		dec := NewReader(nil)
		enc := NewWriter(nil, WriterConcurrency(2), WriterPadding(255), WriterBlockSize(128<<10))
		encBetter := NewWriter(nil, WriterConcurrency(2), WriterPadding(255), WriterBetterCompression(), WriterBlockSize(512<<10))

		comp := Encode(make([]byte, MaxEncodedLen(len(data))), data)
		decoded, err := Decode(nil, comp)
		if err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(data, decoded) {
			t.Error("block decoder mismatch")
			return
		}
		if mel := MaxEncodedLen(len(data)); len(comp) > mel {
			t.Error(fmt.Errorf("MaxEncodedLen Exceed: input: %d, mel: %d, got %d", len(data), mel, len(comp)))
			return
		}
		// Test writer and use "better":
		var buf bytes.Buffer
		encBetter.Reset(&buf)
		n, err := encBetter.Write(data)
		if err != nil {
			t.Error(err)
			return
		}
		if n != len(data) {
			t.Error(fmt.Errorf("Write: Short write, want %d, got %d", len(data), n))
			return
		}
		err = encBetter.Close()
		if err != nil {
			t.Error(err)
			return
		}
		// Calling close twice should not affect anything.
		err = encBetter.Close()
		if err != nil {
			t.Error(err)
			return
		}
		comp = buf.Bytes()
		if len(comp)%255 != 0 {
			t.Error(fmt.Errorf("wanted size to be mutiple of %d, got size %d with remainder %d", 255, len(comp), len(comp)%255))
			return
		}
		dec.Reset(&buf)
		got, err := ioutil.ReadAll(dec)
		if err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(data, got) {
			t.Error("block (reset) decoder mismatch")
			return
		}
		// Test Reset on both and use ReadFrom instead.
		input := bytes.NewBuffer(data)
		buf = bytes.Buffer{}
		enc.Reset(&buf)
		n2, err := enc.ReadFrom(input)
		if err != nil {
			t.Error(err)
			return
		}
		if n2 != int64(len(data)) {
			t.Error(fmt.Errorf("ReadFrom: Short read, want %d, got %d", len(data), n2))
			return
		}
		err = enc.Close()
		if err != nil {
			t.Error(err)
			return
		}
		if buf.Len()%255 != 0 {
			t.Error(fmt.Errorf("wanted size to be mutiple of %d, got size %d with remainder %d", 255, buf.Len(), buf.Len()%255))
			return
		}
		dec.Reset(&buf)
		got, err = ioutil.ReadAll(dec)
		if err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(data, got) {
			t.Error("frame (reset) decoder mismatch")
			return
		}
	}
	for _, tt := range zr.File {
		if !strings.HasSuffix(t.Name(), "") {
			continue
		}
		t.Run(tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			b, err := ioutil.ReadAll(r)
			if err != nil {
				t.Error(err)
				return
			}
			test(t, b)
		})
	}
}

func TestWriterPadding(t *testing.T) {
	n := 100
	if testing.Short() {
		n = 5
	}
	rng := rand.New(rand.NewSource(0x1337))
	d := NewReader(nil)

	for i := 0; i < n; i++ {
		padding := (rng.Int() & 0xffff) + 1
		src := make([]byte, (rng.Int()&0xfffff)+1)
		for i := range src {
			src[i] = uint8(rng.Uint32()) & 3
		}
		var dst bytes.Buffer
		e := NewWriter(&dst, WriterPadding(padding))
		// Test the added padding is invisible.
		_, err := io.Copy(e, bytes.NewBuffer(src))
		if err != nil {
			t.Fatal(err)
		}
		err = e.Close()
		if err != nil {
			t.Fatal(err)
		}
		err = e.Close()
		if err != nil {
			t.Fatal(err)
		}

		if dst.Len()%padding != 0 {
			t.Fatalf("wanted size to be mutiple of %d, got size %d with remainder %d", padding, dst.Len(), dst.Len()%padding)
		}
		var got bytes.Buffer
		d.Reset(&dst)
		_, err = io.Copy(&got, d)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(src, got.Bytes()) {
			t.Fatal("output mismatch")
		}

		// Try after reset
		dst.Reset()
		e.Reset(&dst)
		_, err = io.Copy(e, bytes.NewBuffer(src))
		if err != nil {
			t.Fatal(err)
		}
		err = e.Close()
		if err != nil {
			t.Fatal(err)
		}
		if dst.Len()%padding != 0 {
			t.Fatalf("wanted size to be mutiple of %d, got size %d with remainder %d", padding, dst.Len(), dst.Len()%padding)
		}

		got.Reset()
		d.Reset(&dst)
		_, err = io.Copy(&got, d)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(src, got.Bytes()) {
			t.Fatal("output mismatch after reset")
		}
	}
}
