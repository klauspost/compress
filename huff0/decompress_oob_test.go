// Copyright 2026+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

package huff0

import (
	"bytes"
	"math/rand"
	"testing"
)

// TestDecompressOOBCanary verifies the Decompress4X/1X implementations never
// write past the declared capacity of dst. Wrong output is caught by the
// roundtrip checks elsewhere; a stray write past cap(dst) into an adjacent
// heap object is not, so dst is carved from a larger allocation whose tail is
// filled with a canary pattern and verified after every decode.
func TestDecompressOOBCanary(t *testing.T) {
	const guard = 128
	const pattern = 0xa5
	rng := rand.New(rand.NewSource(1))

	checkGuard := func(t *testing.T, buf []byte, dstSize int, mode string, trial int) {
		t.Helper()
		for i, v := range buf[dstSize:] {
			if v != pattern {
				t.Fatalf("trial %d (%s): guard byte %d past dst (size %d) overwritten: %#02x",
					trial, mode, i, dstSize, v)
			}
		}
	}

	for trial := range 1000 {
		// Vary size and entropy so both the 8-bit and full table paths, all
		// tail lengths, and streams ending near the buffer edge are hit.
		size := 1 + rng.Intn(1<<16)
		span := 1 + rng.Intn(256)
		in := make([]byte, size)
		for i := range in {
			in[i] = byte(rng.Intn(span))
		}

		var enc Scratch
		enc.Reuse = ReusePolicyNone

		if comp, _, err := Compress4X(in, &enc); err == nil {
			dec, remain, err := ReadTable(comp, nil)
			if err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, size+guard)
			for i := range buf {
				buf[i] = pattern
			}
			out, err := dec.Decoder().Decompress4X(buf[:0:size], remain)
			if err != nil {
				t.Fatalf("trial %d: Decompress4X: %v", trial, err)
			}
			if !bytes.Equal(out, in) {
				t.Fatalf("trial %d: Decompress4X roundtrip mismatch", trial)
			}
			checkGuard(t, buf, size, "4X", trial)
		}

		if comp, _, err := Compress1X(in, &enc); err == nil {
			dec, remain, err := ReadTable(comp, nil)
			if err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, size+guard)
			for i := range buf {
				buf[i] = pattern
			}
			out, err := dec.Decoder().Decompress1X(buf[:0:size], remain)
			if err != nil {
				t.Fatalf("trial %d: Decompress1X: %v", trial, err)
			}
			if !bytes.Equal(out, in) {
				t.Fatalf("trial %d: Decompress1X roundtrip mismatch", trial)
			}
			checkGuard(t, buf, size, "1X", trial)
		}
	}
}
