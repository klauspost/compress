package flate

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"testing"
)

type testFatal interface {
	Fatal(args ...any)
}

// loadTestTokens will load test tokens.
// First block from enwik9, varint encoded.
func loadTestTokens(t testFatal) *tokens {
	b, err := os.ReadFile("testdata/tokens.bin")
	if err != nil {
		t.Fatal(err)
	}
	var tokens tokens
	err = tokens.FromVarInt(b)
	if err != nil {
		t.Fatal(err)
	}
	return &tokens
}

func Test_tokens_EstimatedBits(t *testing.T) {
	tok := loadTestTokens(t)
	// The estimated size, update if method changes.
	const expect = 221057
	n := tok.EstimatedBits()
	var buf bytes.Buffer
	wr := newHuffmanBitWriter(&buf)
	wr.writeBlockDynamic(tok, true, nil, true)
	if wr.err != nil {
		t.Fatal(wr.err)
	}
	wr.flush()
	t.Log("got:", n, "actual:", buf.Len()*8, "(header not part of estimate)")
	if n != expect {
		t.Error("want:", expect, "bits, got:", n)
	}
}

func Benchmark_tokens_EstimatedBits(b *testing.B) {
	tok := loadTestTokens(b)
	b.ResetTimer()
	// One "byte", one token iteration.
	b.SetBytes(1)
	for i := 0; i < b.N; i++ {
		_ = tok.EstimatedBits()
	}
}

// TestCompressArchDependent is a regression test for
// https://github.com/klauspost/pgzip/issues/72: compressing this particular
// input at level 3 used to produce a different (though still valid) byte
// stream on arm64 than on amd64, because EstimatedBits() summed
// FMA-contractable multiply-adds into its running total. A
// tiny per-architecture rounding difference in that sum was occasionally
// enough to flip the table-reuse decision, changing everything encoded
// after that point.
//
// The input is stored gzipped, with stdlib compress/gzip reading it so that a
// break in this package cannot quietly change what is being compressed.
func TestCompressArchDependent(t *testing.T) {
	f, err := os.Open("testdata/issue72-arch-dependent.bin.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	in, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	const wantSHA256 = "485a8c79e4888759521afde1ea9d2cf2e3d21a621000ed0183c502d8a8a843d0"

	var buf bytes.Buffer
	w, err := NewWriter(&buf, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(in); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	got := sha256.Sum256(buf.Bytes())
	if gotSHA256 := hex.EncodeToString(got[:]); gotSHA256 != wantSHA256 {
		t.Fatalf("compressed sha256 = %s, want %s (compressed output is architecture-dependent)", gotSHA256, wantSHA256)
	}
}
