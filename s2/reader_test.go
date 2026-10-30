// Copyright (c) 2019+ Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"bytes"
	"io"
	"math/rand"
	"testing"
)

func TestLeadingSkippableBlock(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.AddSkippableBlock(0x80, []byte("skippable block")); err != nil {
		t.Fatalf("w.AddSkippableBlock: %v", err)
	}
	if _, err := w.Write([]byte("some data")); err != nil {
		t.Fatalf("w.Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("w.Close: %v", err)
	}
	r := NewReader(&buf)
	var sb []byte
	r.SkippableCB(0x80, func(sr io.Reader) error {
		var err error
		sb, err = io.ReadAll(sr)
		return err
	})
	if _, err := r.Read([]byte{}); err != nil {
		t.Errorf("empty read failed: %v", err)
	}
	if !bytes.Equal(sb, []byte("skippable block")) {
		t.Errorf("didn't get correct data from skippable block: %q", string(sb))
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("r.Read: %v", err)
	}
	if !bytes.Equal(data, []byte("some data")) {
		t.Errorf("didn't get correct compressed data: %q", string(data))
	}
}

func TestReaderSkipCRC(t *testing.T) {
	// Returns a stream of 4 KiB chunks and the start of the chunk holding
	// uncompressed offset 4096. Random data is written as uncompressed chunks,
	// text as compressed chunks.
	stream := func(t *testing.T, data []byte) ([]byte, int) {
		var buf bytes.Buffer
		w := NewWriter(&buf, WriterBlockSize(4<<10), WriterConcurrency(1))
		if _, err := w.Write(data); err != nil {
			t.Fatalf("w.Write: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("w.Close: %v", err)
		}
		b := buf.Bytes()
		// Stream identifier, first data chunk, second data chunk.
		second := 4 + len(magicBody)
		second += 4 + (int(b[second+1]) | int(b[second+2])<<8 | int(b[second+3])<<16)
		return b, second
	}
	random := make([]byte, 16<<10)
	rng := rand.New(rand.NewSource(0))
	rng.Read(random)
	text := bytes.Repeat([]byte("compressible text "), 1<<10)

	for _, tc := range []struct {
		data []byte
		typ  byte
	}{
		{random, chunkTypeUncompressedData},
		{text, chunkTypeCompressedData},
	} {
		data, typ := tc.data, tc.typ
		b, chunk := stream(t, data)
		if b[chunk] != typ {
			t.Fatalf("chunk type %#x, want %#x", b[chunk], typ)
		}
		// Flip a bit in the stored checksum of the second chunk.
		b[chunk+4] ^= 1

		// Skip into the corrupted chunk. Either Skip or the Read of the
		// remaining chunk data must report the CRC mismatch.
		r := NewReader(bytes.NewReader(b))
		err := r.Skip(4096 + 100)
		if err == nil {
			_, err = io.ReadAll(r)
		}
		if err != ErrCRC {
			t.Errorf("chunk type %#x: Skip+Read: want %v, got %v", typ, ErrCRC, err)
		}

		// With CRC checks disabled Skip must accept the chunk, like Read.
		r = NewReader(bytes.NewReader(b), ReaderIgnoreCRC())
		if err := r.Skip(4096 + 100); err != nil {
			t.Errorf("chunk type %#x: Skip with ReaderIgnoreCRC: %v", typ, err)
		}
		got, err := io.ReadAll(r)
		if err != nil {
			t.Errorf("chunk type %#x: Read with ReaderIgnoreCRC: %v", typ, err)
		}
		if !bytes.Equal(got, data[4096+100:]) {
			t.Errorf("chunk type %#x: Read with ReaderIgnoreCRC: wrong data", typ)
		}
	}
}
