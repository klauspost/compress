// Copyright (c) 2019+ Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"bytes"
	"fmt"
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

func TestSkipOversizedChunk(t *testing.T) {
	// An uncompressed chunk larger than the configured maximum block size
	// must be rejected with ErrCorrupt, by Read as well as by Skip.
	stream := []byte{0xff, 0x06, 0x00, 0x00, 's', 'N', 'a', 'P', 'p', 'Y'}
	stream = append(stream, 0x01, 0xff, 0xff, 0x00) // uncompressed chunk of 65535 bytes

	for _, tc := range []struct {
		name string
		op   func(*Reader) error
	}{
		{"Read", func(r *Reader) error { _, err := r.Read(make([]byte, 1)); return err }},
		{"Skip", func(r *Reader) error { return r.Skip(1) }},
	} {
		r := NewReader(bytes.NewReader(stream), ReaderMaxBlockSize(1024))
		if err := tc.op(r); err != ErrCorrupt {
			t.Errorf("%s: got %v, want ErrCorrupt", tc.name, err)
		}
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

func TestReaderResetStreamType(t *testing.T) {
	encode := func(data []byte, opts ...WriterOption) []byte {
		t.Helper()
		var buf bytes.Buffer
		opts = append(opts, WriterConcurrency(1))
		w := NewWriter(&buf, opts...)
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	previous := encode([]byte("previous Snappy stream"), WriterSnappyCompat())
	data := bytes.Repeat([]byte("S2 data after reset "), 8192)
	for _, uncompressed := range []bool{false, true} {
		opts := []WriterOption{WriterBlockSize(256 << 10)}
		name := "compressed"
		wantType := byte(chunkTypeCompressedData)
		if uncompressed {
			opts = append(opts, WriterUncompressed())
			name = "uncompressed"
			wantType = chunkTypeUncompressedData
		}
		stream := encode(data, opts...)
		headerLen := len(magicChunk)
		if stream[headerLen] != wantType {
			t.Fatalf("%s chunk type: got %x, want %x", name, stream[headerLen], wantType)
		}
		for _, concurrent := range []bool{false, true} {
			operation := "Read"
			if concurrent {
				operation = "DecodeConcurrent"
			}
			t.Run(name+"/"+operation, func(t *testing.T) {
				r := NewReader(bytes.NewReader(previous), ReaderIgnoreStreamIdentifier())
				if _, err := io.ReadAll(r); err != nil {
					t.Fatal(err)
				}
				// A fresh reader already accepts this identifier-free S2 stream.
				fresh, err := io.ReadAll(NewReader(bytes.NewReader(stream[headerLen:]), ReaderIgnoreStreamIdentifier()))
				if err != nil || !bytes.Equal(fresh, data) {
					t.Fatalf("fresh reader: %v, decoded %d bytes", err, len(fresh))
				}
				r.Reset(bytes.NewReader(stream[headerLen:]))
				var got []byte
				if concurrent {
					var dst bytes.Buffer
					_, err = r.DecodeConcurrent(&dst, 2)
					got = dst.Bytes()
				} else {
					got, err = io.ReadAll(r)
				}
				if err != nil {
					t.Fatalf("decode S2 after resetting Snappy reader: %v", err)
				}
				if !bytes.Equal(got, data) {
					t.Fatal("decoded data mismatch")
				}
				// A new Snappy identifier must still enforce the smaller block limit.
				invalidSnappy := append([]byte(magicChunkSnappy), stream[headerLen:]...)
				r.Reset(bytes.NewReader(invalidSnappy))
				if concurrent {
					_, err = r.DecodeConcurrent(io.Discard, 2)
				} else {
					_, err = io.ReadAll(r)
				}
				if err != ErrCorrupt {
					t.Fatalf("oversized Snappy block after reset: got %v, want %v", err, ErrCorrupt)
				}
			})
		}
	}
}

func TestReaderSkipStreamType(t *testing.T) {
	encode := func(data []byte, opts ...WriterOption) []byte {
		t.Helper()
		var buf bytes.Buffer
		w := NewWriter(&buf, append(opts, WriterConcurrency(1))...)
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(bytes.Repeat([]byte("payload "), 16384)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	for _, uncompressed := range []bool{false, true} {
		opts := []WriterOption{WriterBlockSize(256 << 10)}
		if uncompressed {
			opts = append(opts, WriterUncompressed())
		}
		s2stream := encode([]byte("x"), opts...)
		// Keep the same two blocks, changing only the advertised stream type.
		snappystream := append([]byte(magicChunkSnappy), s2stream[len(magicChunk):]...)
		for _, snappy := range []bool{false, true} {
			stream, previous := s2stream, snappystream
			if snappy {
				stream, previous = snappystream, s2stream
			}
			for _, mode := range []string{"fresh", "reset", "concatenated"} {
				t.Run(fmt.Sprintf("uncompressed=%v/snappy=%v/%s", uncompressed, snappy, mode), func(t *testing.T) {
					r := NewReader(bytes.NewReader(stream))
					var prefix [1]byte
					switch mode {
					case "reset":
						r.Reset(bytes.NewReader(previous))
						if _, err := io.ReadFull(r, prefix[:]); err != nil {
							t.Fatal(err)
						}
						r.Reset(bytes.NewReader(stream))
					case "concatenated":
						// Only consume the first small block of the opposite stream type.
						firstLen := int(previous[len(magicChunk)+1]) | int(previous[len(magicChunk)+2])<<8 | int(previous[len(magicChunk)+3])<<16
						joined := append(append([]byte{}, previous[:len(magicChunk)+4+firstLen]...), stream...)
						r.Reset(bytes.NewReader(joined))
						if _, err := io.ReadFull(r, prefix[:]); err != nil {
							t.Fatal(err)
						}
					}
					if err := r.Skip(1); err != nil {
						t.Fatal(err)
					}
					got, err := io.ReadAll(r)
					if snappy {
						if err != ErrCorrupt {
							t.Fatalf("oversized identified Snappy block: got %v, want %v", err, ErrCorrupt)
						}
					} else if err != nil || !bytes.Equal(got, bytes.Repeat([]byte("payload "), 16384)) {
						t.Fatalf("valid S2 block: decoded %d bytes, error %v", len(got), err)
					}
				})
			}
		}
	}
}
