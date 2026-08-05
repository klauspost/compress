// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lzw

import (
	"bufio"
	"bytes"
	stdlzw "compress/lzw"
	"io"
	"os"
	"testing"
)

var benchFiles = []string{
	"e.txt",                     // digits, small alphabet
	"html.txt",                  // markup
	"Mark.Twain-Tom.Sawyer.txt", // english text
	"pngdata.bin",               // binary
	"sharnd.out",                // incompressible
}

func loadFile(tb testing.TB, name string) []byte {
	tb.Helper()
	b, err := os.ReadFile("../testdata/" + name)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func lzwEncode(tb testing.TB, raw []byte, order Order, litWidth int) []byte {
	tb.Helper()
	var buf bytes.Buffer
	w := NewWriter(&buf, order, litWidth)
	if _, err := w.Write(raw); err != nil {
		tb.Fatal(err)
	}
	if err := w.Close(); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func drain(tb testing.TB, r io.Reader, dst []byte) int {
	n := 0
	for {
		got, err := r.Read(dst)
		n += got
		if err != nil {
			if err != io.EOF {
				tb.Fatal(err)
			}
			return n
		}
	}
}

type resetter interface {
	io.Reader
	Reset(io.Reader, Order, int)
}

// wrapSrc controls what kind of io.Reader the decoder sees.
type srcKind int

const (
	srcBytes srcKind = iota // *bytes.Reader: io.ByteReader, no read-ahead allowed
	srcBufio                // *bufio.Reader
	srcPlain                // bare io.Reader, decoder buffers it itself
)

type plainReader struct{ r io.Reader }

func (p plainReader) Read(b []byte) (int, error) { return p.r.Read(b) }

func makeSrc(kind srcKind, br *bytes.Reader) io.Reader {
	switch kind {
	case srcBufio:
		return bufio.NewReader(br)
	case srcPlain:
		return plainReader{br}
	}
	return br
}

// resetSrc rewinds the source between iterations. The wrapper itself is reused so
// that allocating it is not measured.
func resetSrc(src io.Reader, br *bytes.Reader, comp []byte) {
	br.Reset(comp)
	if bufr, ok := src.(*bufio.Reader); ok {
		bufr.Reset(br)
	}
}

func benchDecode(b *testing.B, comp []byte, rawLen int, order Order, litWidth int, kind srcKind, std bool) {
	dst := make([]byte, 64<<10)
	br := bytes.NewReader(comp)
	src := makeSrc(kind, br)
	b.SetBytes(int64(rawLen))
	b.ReportAllocs()
	b.ResetTimer()

	if std {
		r := stdlzw.NewReader(src, stdlzw.Order(order), litWidth).(*stdlzw.Reader)
		for i := 0; i < b.N; i++ {
			resetSrc(src, br, comp)
			r.Reset(src, stdlzw.Order(order), litWidth)
			if got := drain(b, r, dst); got != rawLen {
				b.Fatalf("got %d bytes, want %d", got, rawLen)
			}
		}
		return
	}
	var r resetter = newReader(src, order, litWidth)
	for i := 0; i < b.N; i++ {
		resetSrc(src, br, comp)
		r.Reset(src, order, litWidth)
		if got := drain(b, r, dst); got != rawLen {
			b.Fatalf("got %d bytes, want %d", got, rawLen)
		}
	}
}

func benchAll(b *testing.B, std bool) {
	for _, name := range benchFiles {
		raw := loadFile(b, name)
		b.Run(name, func(b *testing.B) {
			comp := lzwEncode(b, raw, LSB, 8)
			benchDecode(b, comp, len(raw), LSB, 8, srcBytes, std)
		})
	}
	raw := loadFile(b, "Mark.Twain-Tom.Sawyer.txt")
	b.Run("MSB", func(b *testing.B) {
		comp := lzwEncode(b, raw, MSB, 8)
		benchDecode(b, comp, len(raw), MSB, 8, srcBytes, std)
	})
	b.Run("bufio", func(b *testing.B) {
		comp := lzwEncode(b, raw, LSB, 8)
		benchDecode(b, comp, len(raw), LSB, 8, srcBufio, std)
	})
	b.Run("plain", func(b *testing.B) {
		comp := lzwEncode(b, raw, LSB, 8)
		benchDecode(b, comp, len(raw), LSB, 8, srcPlain, std)
	})
	b.Run("litWidth5", func(b *testing.B) {
		trimmed := make([]byte, len(raw))
		for i, v := range raw {
			trimmed[i] = v & 0x1f
		}
		comp := lzwEncode(b, trimmed, LSB, 5)
		benchDecode(b, comp, len(trimmed), LSB, 5, srcBytes, std)
	})
	// Many short streams: dominated by per-stream setup cost.
	b.Run("small1k", func(b *testing.B) {
		small := raw[:1024]
		comp := lzwEncode(b, small, LSB, 8)
		benchDecode(b, comp, len(small), LSB, 8, srcBytes, std)
	})
}

func BenchmarkDecodeNew(b *testing.B) { benchAll(b, false) }
func BenchmarkDecodeStd(b *testing.B) { benchAll(b, true) }

// nopWriter is a bare io.Writer, so the encoder has to do its own buffering.
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

func benchEncode(b *testing.B, raw []byte, order Order, litWidth int, std bool) {
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	if std {
		w := stdlzw.NewWriter(nopWriter{}, stdlzw.Order(order), litWidth).(*stdlzw.Writer)
		for i := 0; i < b.N; i++ {
			w.Reset(nopWriter{}, stdlzw.Order(order), litWidth)
			if _, err := w.Write(raw); err != nil {
				b.Fatal(err)
			}
			if err := w.Close(); err != nil {
				b.Fatal(err)
			}
		}
		return
	}
	w := newWriter(nopWriter{}, order, litWidth)
	for i := 0; i < b.N; i++ {
		w.Reset(nopWriter{}, order, litWidth)
		if _, err := w.Write(raw); err != nil {
			b.Fatal(err)
		}
		if err := w.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func benchEncodeAll(b *testing.B, std bool) {
	for _, name := range benchFiles {
		raw := loadFile(b, name)
		b.Run(name, func(b *testing.B) { benchEncode(b, raw, LSB, 8, std) })
	}
	raw := loadFile(b, "Mark.Twain-Tom.Sawyer.txt")
	b.Run("MSB", func(b *testing.B) { benchEncode(b, raw, MSB, 8, std) })
	b.Run("litWidth5", func(b *testing.B) {
		trimmed := make([]byte, len(raw))
		for i, v := range raw {
			trimmed[i] = v & 0x1f
		}
		benchEncode(b, trimmed, LSB, 5, std)
	})
	b.Run("small1k", func(b *testing.B) { benchEncode(b, raw[:1024], LSB, 8, std) })
}

func BenchmarkEncodeNew(b *testing.B) { benchEncodeAll(b, false) }
func BenchmarkEncodeStd(b *testing.B) { benchEncodeAll(b, true) }
