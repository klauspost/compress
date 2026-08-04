// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lzw

import (
	"bufio"
	"bytes"
	stdlzw "compress/lzw"
	"fmt"
	"io"
	"math/rand"
	"testing"
)

// readAll reads r in chunks of the given size, returning what was decoded and
// the terminating error.
func readAll(r io.Reader, chunk int) ([]byte, error) {
	var out []byte
	buf := make([]byte, chunk)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, err
		}
		if n == 0 {
			return out, io.ErrNoProgress
		}
	}
}

// chunkSizes exercise the intermediate output buffer (small) and decoding
// straight into the caller's buffer (large).
var chunkSizes = []int{1, 3, 100, 4096, 8192, 1 << 16}

// checkAgainstStd decodes in with both this package and compress/lzw and
// requires that the decoded bytes and the terminating error match.
func checkAgainstStd(t *testing.T, name string, in []byte, order Order, litWidth int) {
	t.Helper()
	want, wantErr := readAll(stdlzw.NewReader(bytes.NewReader(in), stdlzw.Order(order), litWidth), 4096)
	for _, chunk := range chunkSizes {
		got, gotErr := readAll(NewReader(bytes.NewReader(in), order, litWidth), chunk)
		if !bytes.Equal(got, want) {
			t.Fatalf("%s (order=%d litWidth=%d chunk=%d): got %d bytes, want %d bytes\n got: %x\nwant: %x",
				name, order, litWidth, chunk, len(got), len(want), trunc(got), trunc(want))
		}
		if errString(gotErr) != errString(wantErr) {
			t.Fatalf("%s (order=%d litWidth=%d chunk=%d): error %v, want %v",
				name, order, litWidth, chunk, gotErr, wantErr)
		}
	}
}

// errString is used for comparison: compress/lzw allocates a fresh error for
// an invalid code, so the values are never identical.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func trunc(b []byte) []byte {
	if len(b) > 64 {
		return b[:64]
	}
	return b
}

func TestStdCompatFiles(t *testing.T) {
	for _, name := range append([]string{"gettysburg.txt", "pi.txt"}, benchFiles...) {
		raw := loadFile(t, name)
		for _, order := range []Order{LSB, MSB} {
			for litWidth := 2; litWidth <= 8; litWidth++ {
				masked := make([]byte, len(raw))
				for i, v := range raw {
					masked[i] = v & byte(1<<litWidth-1)
				}
				in := lzwEncode(t, masked, order, litWidth)
				checkAgainstStd(t, name, in, order, litWidth)
			}
		}
	}
}

// TestStdCompatSynthetic covers inputs that stress the dictionary: long runs
// (maximal expansions and the KwKwK case), repeated clear codes and data that
// pushes the code width to its maximum.
func TestStdCompatSynthetic(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	inputs := map[string][]byte{
		"empty":  {},
		"one":    {0x42},
		"zeros":  make([]byte, 1<<20),
		"random": randBytes(rng, 1<<19),
		"runs":   runs(rng, 1<<19),
		"lowentropy": func() []byte {
			b := make([]byte, 1<<19)
			for i := range b {
				b[i] = byte(rng.Intn(3))
			}
			return b
		}(),
	}
	for name, raw := range inputs {
		for _, order := range []Order{LSB, MSB} {
			in := lzwEncode(t, raw, order, 8)
			checkAgainstStd(t, name, in, order, 8)
			// Truncations exercise the unexpected-EOF paths.
			for _, cut := range []int{1, 2, 3, len(in) / 3, len(in) / 2, len(in) - 1} {
				if cut > 0 && cut < len(in) {
					checkAgainstStd(t, fmt.Sprintf("%s-cut%d", name, cut), in[:cut], order, 8)
				}
			}
		}
	}
}

func randBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	rng.Read(b)
	return b
}

// runs returns data made of long repeats, which produces maximal length
// dictionary entries.
func runs(rng *rand.Rand, n int) []byte {
	b := make([]byte, 0, n)
	for len(b) < n {
		v := byte(rng.Intn(256))
		for i := rng.Intn(5000); i >= 0 && len(b) < n; i-- {
			b = append(b, v)
		}
	}
	return b
}

// TestStdCompatCorrupt feeds arbitrary bytes to both decoders.
func TestStdCompatCorrupt(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for i := range 2000 {
		in := randBytes(rng, rng.Intn(300))
		for _, order := range []Order{LSB, MSB} {
			for _, litWidth := range []int{2, 5, 8} {
				checkAgainstStd(t, fmt.Sprintf("corrupt%d", i), in, order, litWidth)
			}
		}
	}
	// Streams of a single repeated byte walk the dictionary in unusual ways.
	for _, fill := range []byte{0x00, 0x01, 0x7f, 0x80, 0xfe, 0xff} {
		for _, n := range []int{1, 2, 17, 1000, 100000} {
			in := bytes.Repeat([]byte{fill}, n)
			for _, order := range []Order{LSB, MSB} {
				for _, litWidth := range []int{2, 5, 8} {
					checkAgainstStd(t, fmt.Sprintf("fill%02x-%d", fill, n), in, order, litWidth)
				}
			}
		}
	}
}

// FuzzReader requires that decoding matches compress/lzw byte for byte, error
// for error and, for every kind of source, in how much of it was consumed.
func FuzzReader(f *testing.F) {
	for _, tt := range lzwTests {
		f.Add([]byte(tt.compressed))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		for _, order := range []Order{LSB, MSB} {
			for litWidth := 2; litWidth <= 8; litWidth++ {
				ref := bytes.NewReader(in)
				want, wantErr := readAll(stdlzw.NewReader(ref, stdlzw.Order(order), litWidth), 4096)
				wantUsed := len(in) - ref.Len()

				for _, chunk := range []int{1, 4096, 1 << 16} {
					for _, kind := range []string{"bytes", "bufio", "byte"} {
						var src io.Reader
						var used func() int
						switch kind {
						case "bytes":
							s := bytes.NewReader(in)
							src, used = s, func() int { return len(in) - s.Len() }
						case "bufio":
							s := bytes.NewReader(in)
							b := bufio.NewReader(s)
							src, used = b, func() int { return len(in) - s.Len() - b.Buffered() }
						case "byte":
							s := &byteReader{b: in}
							src, used = s, func() int { return s.i }
						}
						got, gotErr := readAll(NewReader(src, order, litWidth), chunk)
						if !bytes.Equal(got, want) || errString(gotErr) != errString(wantErr) {
							t.Fatalf("order=%d litWidth=%d chunk=%d src=%s: got %d bytes %v, want %d bytes %v",
								order, litWidth, chunk, kind, len(got), gotErr, len(want), wantErr)
						}
						if u := used(); u != wantUsed {
							t.Fatalf("order=%d litWidth=%d chunk=%d src=%s: consumed %d bytes, want %d",
								order, litWidth, chunk, kind, u, wantUsed)
						}
					}
				}
			}
		}
	})
}

// stdEncode compresses raw with compress/lzw, writing it in the given chunk
// sizes.
func stdEncode(tb testing.TB, raw []byte, order Order, litWidth int, chunk int) []byte {
	tb.Helper()
	var buf bytes.Buffer
	w := stdlzw.NewWriter(&buf, stdlzw.Order(order), litWidth)
	for len(raw) > 0 {
		n := min(chunk, len(raw))
		if _, err := w.Write(raw[:n]); err != nil {
			tb.Fatal(err)
		}
		raw = raw[n:]
	}
	if err := w.Close(); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func lzwEncodeChunked(tb testing.TB, raw []byte, order Order, litWidth int, chunk int) []byte {
	tb.Helper()
	var buf bytes.Buffer
	w := NewWriter(&buf, order, litWidth)
	for len(raw) > 0 {
		n := min(chunk, len(raw))
		if _, err := w.Write(raw[:n]); err != nil {
			tb.Fatal(err)
		}
		raw = raw[n:]
	}
	if err := w.Close(); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// TestWriterMatchesStd requires that compression produces exactly the same
// bytes as compress/lzw, whatever sizes the input arrives in.
func TestWriterMatchesStd(t *testing.T) {
	for _, name := range append([]string{"gettysburg.txt", "pi.txt"}, benchFiles...) {
		raw := loadFile(t, name)
		for _, order := range []Order{LSB, MSB} {
			for litWidth := 2; litWidth <= 8; litWidth++ {
				masked := make([]byte, len(raw))
				for i, v := range raw {
					masked[i] = v & byte(1<<litWidth-1)
				}
				want := stdEncode(t, masked, order, litWidth, len(masked)+1)
				for _, chunk := range []int{1, 7, 1000, len(masked) + 1} {
					got := lzwEncodeChunked(t, masked, order, litWidth, chunk)
					if !bytes.Equal(got, want) {
						t.Fatalf("%s (order=%d litWidth=%d chunk=%d): %d bytes, want %d",
							name, order, litWidth, chunk, len(got), len(want))
					}
				}
			}
		}
	}
}

// TestWriterFlushesBufferedDestination checks that Close still flushes a
// destination that does its own buffering, and reaches a plain one.
func TestWriterFlushesBufferedDestination(t *testing.T) {
	data := bytes.Repeat([]byte("flush me "), 2000)
	want := lzwEncode(t, data, LSB, 8)

	var sink bytes.Buffer
	bw := bufio.NewWriter(&sink)
	w := NewWriter(bw, LSB, 8)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sink.Bytes(), want) {
		t.Errorf("bufio.Writer: got %d bytes, want %d", sink.Len(), len(want))
	}

	// io.Discard is neither a byte writer nor flushable.
	if err := NewWriter(io.Discard, LSB, 8).Close(); err != nil {
		t.Errorf("io.Discard: %v", err)
	}
}

// TestWriterShortWrite checks that a destination that cannot take everything is
// reported rather than silently dropped.
func TestWriterShortWrite(t *testing.T) {
	w := NewWriter(shortWriter{}, LSB, 8)
	_, err1 := w.Write(bytes.Repeat([]byte("abcdefghij"), 5000))
	err2 := w.Close()
	if err1 == nil && err2 == nil {
		t.Fatal("got nil errors, want a write error")
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

// FuzzWriter requires that compression matches compress/lzw byte for byte and
// that what it produces decodes back to the input.
func FuzzWriter(f *testing.F) {
	f.Add([]byte("TOBEORNOTTOBEORTOBEORNOT"), uint8(0), uint8(3))
	f.Add(bytes.Repeat([]byte{1, 2}, 5000), uint8(1), uint8(200))
	f.Fuzz(func(t *testing.T, raw []byte, cfg uint8, chunk uint8) {
		order, litWidth := Order(cfg&1), int(cfg>>1)%7+2
		masked := make([]byte, len(raw))
		for i, v := range raw {
			masked[i] = v & byte(1<<litWidth-1)
		}
		want := stdEncode(t, masked, order, litWidth, len(masked)+1)
		got := lzwEncodeChunked(t, masked, order, litWidth, int(chunk)+1)
		if !bytes.Equal(got, want) {
			t.Fatalf("order=%d litWidth=%d chunk=%d: %d bytes, want %d",
				order, litWidth, chunk, len(got), len(want))
		}
		back, err := readAll(NewReader(bytes.NewReader(got), order, litWidth), 4096)
		if err != io.EOF || !bytes.Equal(back, masked) {
			t.Fatalf("roundtrip: %d bytes, %v, want %d bytes", len(back), err, len(masked))
		}
	})
}

// FuzzRoundtrip reaches the decoder paths that only well formed streams take,
// such as maximal expansions and repeated clear codes.
func FuzzRoundtrip(f *testing.F) {
	f.Add([]byte("TOBEORNOTTOBEORTOBEORNOT"), uint8(0))
	f.Add(bytes.Repeat([]byte{7}, 10000), uint8(1))
	f.Fuzz(func(t *testing.T, raw []byte, cfg uint8) {
		order, litWidth := Order(cfg&1), int(cfg>>1)%7+2
		masked := make([]byte, len(raw))
		for i, v := range raw {
			masked[i] = v & byte(1<<litWidth-1)
		}
		var buf bytes.Buffer
		w := NewWriter(&buf, order, litWidth)
		if _, err := w.Write(masked); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		for _, chunk := range []int{1, 9000, 1 << 17} {
			got, err := readAll(NewReader(bytes.NewReader(buf.Bytes()), order, litWidth), chunk)
			if err != io.EOF {
				t.Fatalf("chunk=%d: %v", chunk, err)
			}
			if !bytes.Equal(got, masked) {
				t.Fatalf("chunk=%d: got %d bytes, want %d", chunk, len(got), len(masked))
			}
		}
	})
}
