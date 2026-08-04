// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lzw

import (
	"bufio"
	"bytes"
	stdlzw "compress/lzw"
	"io"
	"math/rand"
	"strings"
	"testing"
)

// byteReader is an io.ByteReader that supports nothing else, like the block
// reader image/gif feeds to the decoder.
type byteReader struct {
	b []byte
	i int
}

func (r *byteReader) ReadByte() (byte, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	r.i++
	return r.b[r.i-1], nil
}

// Read is never expected to be called: such a source must be read one byte at
// a time so that nothing past the compressed stream is consumed.
func (r *byteReader) Read([]byte) (int, error) { panic("lzw read ahead of an io.ByteReader") }

func (r *byteReader) rest() []byte { return r.b[r.i:] }

// TestSourceNotOverRead checks that a source implementing io.ByteReader is left
// positioned just after the compressed stream, which callers such as image/gif
// rely on to find what follows it.
func TestSourceNotOverRead(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	trailer := []byte("TRAILING BYTES")
	for _, size := range []int{0, 1, 5, 1000, 100000} {
		raw := make([]byte, size)
		for i := range raw {
			raw[i] = byte(rng.Intn(7))
		}
		for _, order := range []Order{LSB, MSB} {
			comp := lzwEncode(t, raw, order, 8)
			in := append(append([]byte{}, comp...), trailer...)

			for _, chunk := range []int{1, 100, 1 << 16} {
				// *bytes.Reader: io.ByteReader, io.ReaderAt and io.Seeker.
				br := bytes.NewReader(in)
				got, err := readAll(NewReader(br, order, 8), chunk)
				if err != io.EOF || !bytes.Equal(got, raw) {
					t.Fatalf("bytes.Reader size=%d order=%d: %d bytes, %v", size, order, len(got), err)
				}
				if left, _ := io.ReadAll(br); !bytes.Equal(left, trailer) {
					t.Errorf("bytes.Reader size=%d order=%d chunk=%d: left %q, want %q", size, order, chunk, left, trailer)
				}

				// *strings.Reader takes the same path.
				sr := strings.NewReader(string(in))
				if _, err := readAll(NewReader(sr, order, 8), chunk); err != io.EOF {
					t.Fatalf("strings.Reader: %v", err)
				}
				if left, _ := io.ReadAll(sr); !bytes.Equal(left, trailer) {
					t.Errorf("strings.Reader size=%d order=%d chunk=%d: left %q, want %q", size, order, chunk, left, trailer)
				}

				// *bufio.Reader is peeked and discarded.
				bufr := bufio.NewReader(bytes.NewReader(in))
				if _, err := readAll(NewReader(bufr, order, 8), chunk); err != io.EOF {
					t.Fatalf("bufio.Reader: %v", err)
				}
				if left, _ := io.ReadAll(bufr); !bytes.Equal(left, trailer) {
					t.Errorf("bufio.Reader size=%d order=%d chunk=%d: left %q, want %q", size, order, chunk, left, trailer)
				}

				// A bare io.ByteReader is read one byte at a time.
				byr := &byteReader{b: in}
				if _, err := readAll(NewReader(byr, order, 8), chunk); err != io.EOF {
					t.Fatalf("byteReader: %v", err)
				}
				if left := byr.rest(); !bytes.Equal(left, trailer) {
					t.Errorf("byteReader size=%d order=%d chunk=%d: left %q, want %q", size, order, chunk, left, trailer)
				}

				// *bytes.Buffer implements io.ByteReader only.
				bb := bytes.NewBuffer(in)
				if _, err := readAll(NewReader(bb, order, 8), chunk); err != io.EOF {
					t.Fatalf("bytes.Buffer: %v", err)
				}
				if left := bb.Bytes(); !bytes.Equal(left, trailer) {
					t.Errorf("bytes.Buffer size=%d order=%d chunk=%d: left %q, want %q", size, order, chunk, left, trailer)
				}
			}
		}
	}
}

// TestSourceMatchesStd checks that every source kind consumes exactly as many
// bytes as compress/lzw does, including for truncated and corrupt input.
func TestSourceMatchesStd(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	streams := [][]byte{}
	for _, size := range []int{0, 3, 700, 40000} {
		raw := make([]byte, size)
		for i := range raw {
			raw[i] = byte(rng.Intn(5))
		}
		comp := lzwEncode(t, raw, LSB, 8)
		streams = append(streams, comp, comp[:len(comp)/2])
	}
	for range 200 {
		streams = append(streams, randBytes(rng, rng.Intn(200)))
	}
	for i, in := range streams {
		if len(in) == 0 {
			continue
		}
		want := bytes.NewReader(in)
		readAll(stdlzw.NewReader(want, stdlzw.LSB, 8), 4096)
		wantLeft := want.Len()

		for _, name := range []string{"bytes.Reader", "bufio.Reader", "byteReader"} {
			var left int
			switch name {
			case "bytes.Reader":
				br := bytes.NewReader(in)
				readAll(NewReader(br, LSB, 8), 4096)
				left = br.Len()
			case "bufio.Reader":
				br := bytes.NewReader(in)
				bufr := bufio.NewReader(br)
				readAll(NewReader(bufr, LSB, 8), 4096)
				// Whatever bufio still holds has not been consumed either.
				left = br.Len() + bufr.Buffered()
			case "byteReader":
				byr := &byteReader{b: in}
				readAll(NewReader(byr, LSB, 8), 4096)
				left = len(byr.rest())
			}
			if left != wantLeft {
				t.Fatalf("stream %d (%d bytes) %s: %d bytes left, want %d", i, len(in), name, left, wantLeft)
			}
		}
	}
}
