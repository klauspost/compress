// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

//go:build (amd64 || arm64) && !appengine && !noasm && gc

package zstd

import (
	"bytes"
	"io"
	"testing"
)

// TestDecodeSyncUnsafeOOB reproduces an out-of-bounds write in the unsafe
// (extended-copy) decodeSync assembly. The per-sequence space check requires
// only that outPos+ll+ml <= cap(s.out), but the unsafe copies write in 16-byte
// blocks and overrun the logical end by up to compressedBlockOverAlloc-1 bytes.
// A malformed stream whose declared content size (maxSyncLen) is a few bytes
// below what its sequences actually produce lets the decode end land inside the
// 16-byte over-allocation slack — the space check still passes, but the final
// block copy then writes past cap(s.out).
//
// The overrun is at most 15 bytes and Go's size-class rounding usually hides it
// from -race and even -asan (the slop is inside the same size class), so this
// test detects it directly: s.out is carved from a larger backing array whose
// tail is filled with a canary and checked after the decode.
func TestDecodeSyncUnsafeOOB(t *testing.T) {
	const canaryByte = 0x5a
	const canaryLen = 64

	zr := testCreateZipReader("testdata/seqs.zip", t)
	var asmPaths int
	for _, tt := range zr.File {
		var ref testSequence
		if !ref.parse(tt.Name) {
			continue
		}
		r, err := tt.Open()
		if err != nil {
			t.Fatal(err)
		}
		seqData, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		buf := bytes.NewBuffer(seqData)
		s := readDecoders(t, buf, ref)
		lits := s.literals
		hist := make([]byte, ref.win)

		reinit := func() {
			if err := s.br.init(buf.Bytes()); err != nil {
				t.Fatal(err)
			}
			if err := s.litLengths.init(s.br); err != nil {
				t.Fatal(err)
			}
			if err := s.offsets.init(s.br); err != nil {
				t.Fatal(err)
			}
			if err := s.matchLengths.init(s.br); err != nil {
				t.Fatal(err)
			}
			s.literals = lits
			s.prevOffset = ref.prevOffsets
		}

		// Learn the real decoded size N via the generic path. With cap(out)==0
		// and maxSyncLen==0, decodeSyncSimple reports unsupported and decodeSync
		// falls back to the generic loop, which appends the full output.
		reinit()
		s.out = nil
		s.maxSyncLen = 0
		if err := s.decodeSync(hist); err != nil {
			continue // not sync-decodable; skip
		}
		n := len(s.out)
		if n == 0 {
			continue
		}

		// Model a malformed stream that under-reports its size by k bytes, so the
		// decode ends inside the over-allocation slack. k in [2,15] keeps the
		// output within cap while pushing the final copy's overrun past it.
		for k := 2; k <= 15; k++ {
			maxSyncLen := n - k
			if maxSyncLen <= 0 {
				continue
			}
			capOut := maxSyncLen + compressedBlockOverAlloc
			backing := make([]byte, capOut+canaryLen)
			for i := range backing {
				backing[i] = canaryByte
			}
			reinit()
			s.out = backing[:0:capOut]
			s.maxSyncLen = uint64(maxSyncLen)

			// The whole point of this test is the unsafe (extended-copy)
			// variant; fail loudly if the buffer geometry would make
			// decodeSyncSimple select the safe copies instead.
			if s.useSafeDecodeSync() {
				t.Fatalf("%s k=%d: buffer sizing selects the safe copy variant; the unsafe path is not exercised",
					tt.Name, k)
			}

			// A well-formed decode into this buffer must either succeed or fail
			// cleanly; either way it must never write past cap(s.out).
			supported, _ := s.decodeSyncSimple(hist)
			if !supported {
				continue
			}
			asmPaths++
			for i := capOut; i < len(backing); i++ {
				if backing[i] != canaryByte {
					t.Fatalf("%s k=%d: unsafe decodeSync wrote past cap(out)=%d (first corrupted byte at +%d)",
						tt.Name, k, capOut, i-capOut)
				}
			}
		}
	}
	if asmPaths == 0 {
		t.Fatal("test did not exercise the unsafe decodeSync asm path")
	}
}
