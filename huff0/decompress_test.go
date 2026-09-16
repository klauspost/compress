package huff0

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/klauspost/compress/internal/cpuinfo"
	"github.com/klauspost/compress/internal/le"
	"github.com/klauspost/compress/zip"
)

func TestDecompress1X(t *testing.T) {
	for _, test := range testfiles {
		t.Run(test.name, func(t *testing.T) {
			var s = &Scratch{}
			buf0, err := test.fn()
			if err != nil {
				t.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			b, re, err := Compress1X(buf0, s)
			if err != test.err1X {
				t.Errorf("want error %v (%T), got %v (%T)", test.err1X, test.err1X, err, err)
			}
			if err != nil {
				t.Log(test.name, err.Error())
				return
			}
			if b == nil {
				t.Error("got no output")
				return
			}
			if len(s.OutTable) == 0 {
				t.Error("got no table definition")
			}
			if re {
				t.Error("claimed to have re-used.")
			}
			if len(s.OutData) == 0 {
				t.Error("got no data output")
			}

			wantRemain := len(s.OutData)
			t.Logf("%s: %d -> %d bytes (%.2f:1) %t (table: %d bytes)", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)), re, len(s.OutTable))

			s.Out = nil
			var remain []byte
			s, remain, err = ReadTable(b, s)
			if err != nil {
				t.Error(err)
				return
			}
			var buf bytes.Buffer
			if s.matches(s.prevTable, &buf); buf.Len() > 0 {
				t.Error(buf.String())
			}
			if len(remain) != wantRemain {
				t.Fatalf("remain mismatch, want %d, got %d bytes", wantRemain, len(remain))
			}
			t.Logf("remain: %d bytes, ok", len(remain))
			dc, err := s.Decompress1X(remain)
			if err != nil {
				t.Error(err)
				return
			}
			if len(buf0) != len(dc) {
				t.Errorf(test.name+"decompressed, want size: %d, got %d", len(buf0), len(dc))
				if len(buf0) > len(dc) {
					buf0 = buf0[:len(dc)]
				} else {
					dc = dc[:len(buf0)]
				}
				if !bytes.Equal(buf0, dc) {
					if len(dc) > 1024 {
						t.Log(string(dc[:1024]))
						t.Errorf(test.name+"decompressed, got delta: \n(in)\t%02x !=\n(out)\t%02x\n", buf0[:1024], dc[:1024])
					} else {
						t.Log(string(dc))
						t.Errorf(test.name+"decompressed, got delta: (in) %v != (out) %v\n", buf0, dc)
					}
				}
				return
			}
			if !bytes.Equal(buf0, dc) {
				if len(buf0) > 1024 {
					t.Log(string(dc[:1024]))
				} else {
					t.Log(string(dc))
				}
				//t.Errorf(test.name+": decompressed, got delta: \n%s")
				t.Error(test.name + ": decompressed, got delta")
			}
			if !t.Failed() {
				t.Log("... roundtrip ok!")
			}
		})
	}
}

// TestBitReaderShiftedRestoreFromAsm covers the conversion from the state
// the Decompress4X asm loops leave behind (sentinel container, signed
// distance from the stream start) back to the Go bit reader invariant.
func TestBitReaderShiftedRestoreFromAsm(t *testing.T) {
	in := make([]byte, 24)
	for i := range in {
		in[i] = byte(0x10 * (i + 1))
	}
	window := func(off int) uint64 { return le.Load64(in, off) }
	tests := []struct {
		name         string
		off          int
		consumed     uint // sentinel position in the container
		wantOff      uint
		wantBitsRead uint8
		wantValue    uint64
		wantErr      bool
	}{
		{name: "in stream, no bits consumed", off: 5, consumed: 0, wantOff: 5, wantBitsRead: 0, wantValue: window(5)},
		{name: "in stream, 13 bits consumed", off: 9, consumed: 13, wantOff: 9, wantBitsRead: 13, wantValue: window(9) << 13},
		{name: "at stream start, 62 bits consumed", off: 0, consumed: 62, wantOff: 0, wantBitsRead: 62, wantValue: window(0) << 62},
		{name: "window 3 bytes below start", off: -3, consumed: 10, wantOff: 0, wantBitsRead: 34, wantValue: window(0) << 34},
		{name: "window 7 bytes below start, exactly drained", off: -7, consumed: 8, wantOff: 0, wantBitsRead: 64, wantValue: 0},
		{name: "window 7 bytes below start, one bit too many", off: -7, consumed: 9, wantErr: true},
		{name: "window 8 bytes below start", off: -8, consumed: 0, wantErr: true},
		{name: "window past the end of the stream", off: 17, consumed: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The container's data bits are deliberately garbage: only the
			// sentinel position may be trusted, the window is re-read.
			b := bitReaderShifted{in: in, off: uint(tt.off), value: 0xdeadbeefcafef00d<<(tt.consumed+1) | 1<<tt.consumed}
			err := b.restoreFromAsm()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("got off=%d bitsRead=%d value=%#x, want error", b.off, b.bitsRead, b.value)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if b.off != tt.wantOff || b.bitsRead != tt.wantBitsRead || b.value != tt.wantValue {
				t.Errorf("got off=%d bitsRead=%d value=%#x, want off=%d bitsRead=%d value=%#x",
					b.off, b.bitsRead, b.value, tt.wantOff, tt.wantBitsRead, tt.wantValue)
			}
			if tt.wantOff == 0 && tt.wantBitsRead == 64 && b.remaining() != 0 {
				t.Errorf("remaining() = %d, want 0", b.remaining())
			}
		})
	}
}

// TestBitReaderShiftedPrepareForAsm covers the entry normalization: a
// stream ending in 0x01 starts with a whole byte consumed, which the asm
// loops cannot take, and short streams are refused by canUseAsm.
func TestBitReaderShiftedPrepareForAsm(t *testing.T) {
	tests := []struct {
		name         string
		in           []byte
		wantOK       bool
		wantOff      uint
		wantBitsRead uint8
	}{
		{name: "marker in top bit", in: append(make([]byte, 20), 0x80), wantOK: true, wantOff: 13, wantBitsRead: 1},
		{name: "marker in bit 3", in: append(make([]byte, 20), 0x08), wantOK: true, wantOff: 13, wantBitsRead: 5},
		{name: "marker in bit 0 slides the window", in: append(make([]byte, 20), 0x01), wantOK: true, wantOff: 12, wantBitsRead: 0},
		{name: "too short", in: append(make([]byte, 10), 0x80), wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bitReaderShifted
			if err := b.init(tt.in); err != nil {
				t.Fatal(err)
			}
			before := b.remaining()
			if ok := b.canUseAsm(); ok != tt.wantOK {
				t.Fatalf("canUseAsm() = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			b.prepareForAsm()
			if b.off != tt.wantOff || b.bitsRead != tt.wantBitsRead {
				t.Errorf("got off=%d bitsRead=%d, want off=%d bitsRead=%d", b.off, b.bitsRead, tt.wantOff, tt.wantBitsRead)
			}
			if b.remaining() != before {
				t.Errorf("remaining changed from %d to %d", before, b.remaining())
			}
			if want := le.Load64(tt.in, b.off) << b.bitsRead; b.value != want {
				t.Errorf("value = %#x, want %#x", b.value, want)
			}
		})
	}
}

func TestDecompress1XRegression(t *testing.T) {
	data, err := os.ReadFile("testdata/decompress1x_regression.zip")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range zr.File {
		if tt.UncompressedSize64 == 0 {
			continue
		}
		rc, err := tt.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}

		t.Run(tt.Name, func(t *testing.T) {
			s, rem, err := ReadTable(data, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Decompress1X(rem)
			if err == nil {
				t.Fatal("expected error to be returned")
			}

			t.Logf("returned error: %s", err)
		})
	}
}

func TestDecompress4X(t *testing.T) {
	testDecompress4X(t)
}

// TestDecompress4XCorruptStaysInBounds feeds Decompress4X streams that
// never drain (random bits under a valid table) into buffers with guard
// bytes after the declared capacity. The decoder must report corruption
// without writing past the capacity, for every table size the asm loops
// specialize on. This is the scenario that the output-iteration bound of
// the asm loops exists for: a stream whose read pointer sits high keeps the
// input bound large, so only the output bound stops the loop.
func TestDecompress4XCorruptStaysInBounds(t *testing.T) {
	testDecompressCorruptStaysInBounds(t, true)
}

// TestDecompress4XCorruptStaysInBoundsNoBMI2 is the same through the
// generic asm twin on amd64 (a no-op elsewhere).
func TestDecompress4XCorruptStaysInBoundsNoBMI2(t *testing.T) {
	defer cpuinfo.DisableBMI2()()
	testDecompressCorruptStaysInBounds(t, true)
}

// TestDecompress1XCorruptStaysInBounds is TestDecompress4XCorruptStaysInBounds
// for the single-stream loops, which have the same batch bound.
func TestDecompress1XCorruptStaysInBounds(t *testing.T) {
	testDecompressCorruptStaysInBounds(t, false)
}

// TestDecompress1XCorruptStaysInBoundsNoBMI2 is the same through the
// generic asm twin on amd64 (a no-op elsewhere).
func TestDecompress1XCorruptStaysInBoundsNoBMI2(t *testing.T) {
	defer cpuinfo.DisableBMI2()()
	testDecompressCorruptStaysInBounds(t, false)
}

// decodeGuarded decodes into a buffer of exactly dstSize bytes of capacity
// followed by guard bytes, and fails the test if the guard bytes change.
// This is the only way a Go test can observe an assembly overrun: the
// allocator rounds small buffers up to a size class, so an overrun of a
// plain make([]byte, n) lands in slack that nothing checks.
func decodeGuarded(t *testing.T, dstSize int, decode func(dst []byte) ([]byte, error)) ([]byte, error) {
	t.Helper()
	const guard = 4096
	buf := make([]byte, dstSize+guard)
	for i := dstSize; i < len(buf); i++ {
		buf[i] = 0xAA
	}
	out, err := decode(buf[:0:dstSize])
	for i := dstSize; i < len(buf); i++ {
		if buf[i] != 0xAA {
			t.Fatalf("dstSize %d: wrote past the output capacity at +%d", dstSize, i-dstSize)
		}
	}
	return out, err
}

func decompress4XGuarded(t *testing.T, dec *Decoder, dstSize int, src []byte) ([]byte, error) {
	t.Helper()
	return decodeGuarded(t, dstSize, func(dst []byte) ([]byte, error) { return dec.Decompress4X(dst, src) })
}

func decompress1XGuarded(t *testing.T, dec *Decoder, dstSize int, src []byte) ([]byte, error) {
	t.Helper()
	return decodeGuarded(t, dstSize, func(dst []byte) ([]byte, error) { return dec.Decompress1X(dst, src) })
}

func testDecompressCorruptStaysInBounds(t *testing.T, fourStreams bool) {
	// A uniform alphabet of 2^k symbols yields exactly k-bit codes; the
	// skewed 256-symbol sample yields codes up to the maximum length.
	tables := []struct {
		name           string
		symbols        int
		skewed         bool
		minLog, maxLog uint8
	}{
		{name: "tablelog-2", symbols: 4, minLog: 2, maxLog: 4},
		{name: "tablelog-4", symbols: 16, minLog: 4, maxLog: 4},
		{name: "tablelog-6", symbols: 64, minLog: 5, maxLog: 8},
		{name: "tablelog-7", symbols: 128, minLog: 7, maxLog: 8},
		{name: "tablelog-11", symbols: 256, skewed: true, minLog: 9, maxLog: 11},
	}
	sizes := []int{800, 801, 1000, 4096, 65536, 262143}
	if !fourStreams {
		// The single-stream loops also take small outputs, down to the
		// one-batch minimum, and drain the rest in Go.
		sizes = append([]int{15, 16, 17, 31, 100}, sizes...)
	}
	seed := uint32(0x12345678)
	next := func() byte {
		seed = seed*1664525 + 1013904223
		return byte(seed >> 24)
	}
	for _, tt := range tables {
		t.Run(tt.name, func(t *testing.T) {
			sample := make([]byte, 1<<16)
			for i := range sample {
				v := int(next())
				if tt.skewed {
					v = v * int(next()) * int(next()) / (256 * 256)
				}
				sample[i] = byte(v % tt.symbols)
			}
			s := &Scratch{}
			if tt.skewed {
				s.TableLog = tt.maxLog
			}
			b, _, err := Compress4X(sample, s)
			if err != nil {
				t.Fatal(err)
			}
			dec, _, err := ReadTable(b, nil)
			if err != nil {
				t.Fatal(err)
			}
			if dec.actualTableLog < tt.minLog || dec.actualTableLog > tt.maxLog {
				t.Fatalf("table log %d, want %d..%d", dec.actualTableLog, tt.minLog, tt.maxLog)
			}
			for _, size := range sizes {
				// Valid input first: a roundtrip of a fresh sample under the
				// same table, with the output capacity exactly the size.
				in := make([]byte, size)
				for i := range in {
					in[i] = sample[int(next())|int(next())<<8]
				}
				enc := &Scratch{Reuse: ReusePolicyMust}
				enc.TransferCTable(dec)
				var comp []byte
				var reused bool
				if fourStreams {
					comp, reused, err = Compress4X(in, enc)
				} else {
					comp, reused, err = Compress1X(in, enc)
				}
				if err != nil {
					t.Fatalf("size %d: %v", size, err)
				}
				if !reused {
					t.Fatalf("size %d: table was not reused", size)
				}
				var out []byte
				if fourStreams {
					out, err = decompress4XGuarded(t, dec.Decoder(), size, comp)
				} else {
					out, err = decompress1XGuarded(t, dec.Decoder(), size, comp)
				}
				if err != nil {
					t.Fatalf("size %d: %v", size, err)
				}
				if !bytes.Equal(out, in) {
					t.Fatalf("size %d: roundtrip mismatch", size)
				}

				// Then corrupt input: streams as long as the output, far more
				// bits than the output can hold, every byte random, marker in
				// the top bit of the last byte. Four equal streams as long as
				// the jump table can express, or one stream.
				var src []byte
				if fourStreams {
					stream := min(size, 65535)
					src = make([]byte, 6+4*stream)
					for i := range 3 {
						src[i*2] = byte(stream)
						src[i*2+1] = byte(stream >> 8)
					}
					for i := 6; i < len(src); i++ {
						src[i] = next()
					}
					for i := range 4 {
						src[6+(i+1)*stream-1] |= 0x80
					}
					_, err = decompress4XGuarded(t, dec.Decoder(), size, src)
				} else {
					src = make([]byte, size)
					for i := range src {
						src[i] = next()
					}
					src[len(src)-1] |= 0x80
					_, err = decompress1XGuarded(t, dec.Decoder(), size, src)
				}
				if err == nil {
					t.Errorf("size %d: expected a corruption error", size)
				}
			}
		})
	}
}

// TestDecompress4XStreamEndsIn01 finds inputs whose compressed streams end
// in the byte 0x01 (marker in bit 0, so the reader starts with a whole byte
// consumed) and roundtrips them through Decompress4X with guard bytes, for
// tables both above and below 8 bits. This is the end-to-end counterpart of
// TestBitReaderShiftedPrepareForAsm.
func TestDecompress4XStreamEndsIn01(t *testing.T) {
	testDecompressStreamEndsIn01(t, true)
}

// TestDecompress1XStreamEndsIn01 is TestDecompress4XStreamEndsIn01 through
// Decompress1X.
func TestDecompress1XStreamEndsIn01(t *testing.T) {
	testDecompressStreamEndsIn01(t, false)
}

func testDecompressStreamEndsIn01(t *testing.T, fourStreams bool) {
	seed := uint32(0xC0FFEE)
	next := func() byte {
		seed = seed*1664525 + 1013904223
		return byte(seed >> 24)
	}
	// Four symbols get 2-bit codes, so 752 symbols per stream (3008 bytes)
	// end every stream on a byte boundary and the marker lands in bit 0
	// deterministically. The larger alphabets have variable code lengths,
	// so those cases search random inputs for streams that end that way.
	for _, tc := range []struct{ symbols, length int }{{4, 3008}, {64, 3000}, {256, 3000}} {
		symbols := tc.symbols
		found := 0
		for attempt := 0; attempt < 4000 && found < 3; attempt++ {
			in := make([]byte, tc.length)
			for i := range in {
				v := int(next()) * int(next()) / 256
				in[i] = byte(v % symbols)
			}
			s := &Scratch{}
			var comp []byte
			var err error
			if fourStreams {
				comp, _, err = Compress4X(in, s)
			} else {
				comp, _, err = Compress1X(in, s)
			}
			if err != nil {
				continue
			}
			dec, streams, err := ReadTable(comp, nil)
			if err != nil {
				t.Fatal(err)
			}
			hit := false
			if fourStreams {
				// Walk the jump table to find each stream's final byte.
				start := 6
				for i := range 4 {
					end := len(streams)
					if i < 3 {
						end = start + (int(streams[i*2]) | int(streams[i*2+1])<<8)
					}
					if streams[end-1] == 0x01 {
						hit = true
					}
					start = end
				}
			} else {
				hit = streams[len(streams)-1] == 0x01
			}
			if !hit {
				continue
			}
			found++
			var out []byte
			if fourStreams {
				out, err = decompress4XGuarded(t, dec.Decoder(), len(in), streams)
			} else {
				out, err = decompress1XGuarded(t, dec.Decoder(), len(in), streams)
			}
			if err != nil {
				t.Fatalf("symbols %d attempt %d: %v", symbols, attempt, err)
			}
			if !bytes.Equal(out, in) {
				t.Fatalf("symbols %d attempt %d: roundtrip mismatch", symbols, attempt)
			}
		}
		if found == 0 {
			t.Fatalf("symbols %d: no stream ending in 0x01 found", symbols)
		}
	}
}

// TestDecompress4XNoBMI2 runs the same roundtrips through the generic asm
// twin on amd64 (a no-op elsewhere).
func TestDecompress4XNoBMI2(t *testing.T) {
	defer cpuinfo.DisableBMI2()()
	testDecompress4X(t)
}

func testDecompress4X(t *testing.T) {
	for _, test := range testfiles {
		t.Run(test.name, func(t *testing.T) {
			for _, tl := range []uint8{0, 5, 6, 7, 8, 9, 10, 11} {
				t.Run(fmt.Sprintf("tablelog-%d", tl), func(t *testing.T) {
					var s = &Scratch{}
					s.TableLog = tl
					buf0, err := test.fn()
					if err != nil {
						t.Fatal(err)
					}
					if len(buf0) > BlockSizeMax {
						buf0 = buf0[:BlockSizeMax]
					}
					b, re, err := Compress4X(buf0, s)
					if err != test.err4X {
						t.Errorf("want error %v (%T), got %v (%T)", test.err1X, test.err1X, err, err)
					}
					if err != nil {
						t.Log(test.name, err.Error())
						return
					}
					if b == nil {
						t.Error("got no output")
						return
					}
					if len(s.OutTable) == 0 {
						t.Error("got no table definition")
					}
					if re {
						t.Error("claimed to have re-used.")
					}
					if len(s.OutData) == 0 {
						t.Error("got no data output")
					}

					wantRemain := len(s.OutData)
					t.Logf("%s: %d -> %d bytes (%.2f:1) %t (table: %d bytes)", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)), re, len(s.OutTable))

					s.Out = nil
					var remain []byte
					s, remain, err = ReadTable(b, s)
					if err != nil {
						t.Error(err)
						return
					}
					var buf bytes.Buffer
					if s.matches(s.prevTable, &buf); buf.Len() > 0 {
						t.Error(buf.String())
					}
					if len(remain) != wantRemain {
						t.Fatalf("remain mismatch, want %d, got %d bytes", wantRemain, len(remain))
					}
					t.Logf("remain: %d bytes, ok", len(remain))
					dc, err := s.Decompress4X(remain, len(buf0))
					if err != nil {
						t.Error(err)
						return
					}
					if len(buf0) != len(dc) {
						t.Errorf(test.name+"decompressed, want size: %d, got %d", len(buf0), len(dc))
						if len(buf0) > len(dc) {
							buf0 = buf0[:len(dc)]
						} else {
							dc = dc[:len(buf0)]
						}
						if !bytes.Equal(buf0, dc) {
							if len(dc) > 1024 {
								t.Log(string(dc[:1024]))
								t.Errorf(test.name+"decompressed, got delta: \n(in)\t%02x !=\n(out)\t%02x\n", buf0[:1024], dc[:1024])
							} else {
								t.Log(string(dc))
								t.Errorf(test.name+"decompressed, got delta: (in) %v != (out) %v\n", buf0, dc)
							}
						}
						return
					}
					if !bytes.Equal(buf0, dc) {
						if len(buf0) > 1024 {
							t.Log(string(dc[:1024]))
						} else {
							t.Log(string(dc))
						}
						//t.Errorf(test.name+": decompressed, got delta: \n%s")
						t.Error(test.name + ": decompressed, got delta")
					}
					if !t.Failed() {
						t.Log("... roundtrip ok!")
					}

				})
			}
		})
	}
}

// TestRoundtrip1XFuzzNoBMI2 runs the same roundtrips through the generic
// asm twin on amd64 (a no-op elsewhere).
func TestRoundtrip1XFuzzNoBMI2(t *testing.T) {
	defer cpuinfo.DisableBMI2()()
	testRoundtrip1XFuzz(t)
}

func TestRoundtrip1XFuzz(t *testing.T) {
	testRoundtrip1XFuzz(t)
}

func testRoundtrip1XFuzz(t *testing.T) {
	for _, test := range testfilesExtended {
		t.Run(test.name, func(t *testing.T) {
			var s = &Scratch{}
			buf0, err := test.fn()
			if err != nil {
				t.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			b, re, err := Compress1X(buf0, s)
			if err != nil {
				if err == ErrIncompressible || err == ErrUseRLE || err == ErrTooBig {
					t.Log(test.name, err.Error())
					return
				}
				t.Error(test.name, err.Error())
				return
			}
			if b == nil {
				t.Error("got no output")
				return
			}
			if len(s.OutTable) == 0 {
				t.Error("got no table definition")
			}
			if re {
				t.Error("claimed to have re-used.")
			}
			if len(s.OutData) == 0 {
				t.Error("got no data output")
			}

			wantRemain := len(s.OutData)
			t.Logf("%s: %d -> %d bytes (%.2f:1) %t (table: %d bytes)", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)), re, len(s.OutTable))

			s.Out = nil
			var remain []byte
			s, remain, err = ReadTable(b, s)
			if err != nil {
				t.Error(err)
				return
			}
			var buf bytes.Buffer
			if s.matches(s.prevTable, &buf); buf.Len() > 0 {
				t.Error(buf.String())
			}
			if len(remain) != wantRemain {
				t.Fatalf("remain mismatch, want %d, got %d bytes", wantRemain, len(remain))
			}
			t.Logf("remain: %d bytes, ok", len(remain))
			dc, err := s.Decompress1X(remain)
			if err != nil {
				t.Error(err)
				return
			}
			if len(buf0) != len(dc) {
				t.Errorf(test.name+"decompressed, want size: %d, got %d", len(buf0), len(dc))
				if len(buf0) > len(dc) {
					buf0 = buf0[:len(dc)]
				} else {
					dc = dc[:len(buf0)]
				}
				if !bytes.Equal(buf0, dc) {
					if len(dc) > 1024 {
						t.Log(string(dc[:1024]))
						t.Errorf(test.name+"decompressed, got delta: \n(in)\t%02x !=\n(out)\t%02x\n", buf0[:1024], dc[:1024])
					} else {
						t.Log(string(dc))
						t.Errorf(test.name+"decompressed, got delta: (in) %v != (out) %v\n", buf0, dc)
					}
				}
				return
			}
			if !bytes.Equal(buf0, dc) {
				if len(buf0) > 1024 {
					t.Log(string(dc[:1024]))
				} else {
					t.Log(string(dc))
				}
				//t.Errorf(test.name+": decompressed, got delta: \n%s")
				t.Error(test.name + ": decompressed, got delta")
			}
			if !t.Failed() {
				t.Log("... roundtrip ok!")
			}
		})
	}
}

func TestRoundtrip4XFuzz(t *testing.T) {
	for _, test := range testfilesExtended {
		t.Run(test.name, func(t *testing.T) {
			var s = &Scratch{}
			buf0, err := test.fn()
			if err != nil {
				t.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			b, re, err := Compress4X(buf0, s)
			if err != nil {
				if err == ErrIncompressible || err == ErrUseRLE || err == ErrTooBig {
					t.Log(test.name, err.Error())
					return
				}
				t.Error(test.name, err.Error())
				return
			}
			if b == nil {
				t.Error("got no output")
				return
			}
			if len(s.OutTable) == 0 {
				t.Error("got no table definition")
			}
			if re {
				t.Error("claimed to have re-used.")
			}
			if len(s.OutData) == 0 {
				t.Error("got no data output")
			}

			wantRemain := len(s.OutData)
			t.Logf("%s: %d -> %d bytes (%.2f:1) %t (table: %d bytes)", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)), re, len(s.OutTable))

			s.Out = nil
			var remain []byte
			s, remain, err = ReadTable(b, s)
			if err != nil {
				t.Error(err)
				return
			}
			var buf bytes.Buffer
			if s.matches(s.prevTable, &buf); buf.Len() > 0 {
				t.Error(buf.String())
			}
			if len(remain) != wantRemain {
				t.Fatalf("remain mismatch, want %d, got %d bytes", wantRemain, len(remain))
			}
			t.Logf("remain: %d bytes, ok", len(remain))
			dc, err := s.Decompress4X(remain, len(buf0))
			if err != nil {
				t.Error(err)
				return
			}
			if len(buf0) != len(dc) {
				t.Errorf(test.name+"decompressed, want size: %d, got %d", len(buf0), len(dc))
				if len(buf0) > len(dc) {
					buf0 = buf0[:len(dc)]
				} else {
					dc = dc[:len(buf0)]
				}
				if !bytes.Equal(buf0, dc) {
					if len(dc) > 1024 {
						t.Log(string(dc[:1024]))
						t.Errorf(test.name+"decompressed, got delta: \n(in)\t%02x !=\n(out)\t%02x\n", buf0[:1024], dc[:1024])
					} else {
						t.Log(string(dc))
						t.Errorf(test.name+"decompressed, got delta: (in) %v != (out) %v\n", buf0, dc)
					}
				}
				return
			}
			if !bytes.Equal(buf0, dc) {
				if len(buf0) > 1024 {
					t.Log(string(dc[:1024]))
				} else {
					t.Log(string(dc))
				}
				//t.Errorf(test.name+": decompressed, got delta: \n%s")
				t.Error(test.name + ": decompressed, got delta")
			}
			if !t.Failed() {
				t.Log("... roundtrip ok!")
			}
		})
	}
}

func BenchmarkDecompress1XTable(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		if test.err1X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			var s = &Scratch{}
			s.Reuse = ReusePolicyNone
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			compressed, _, err := Compress1X(buf0, s)
			if err != test.err1X {
				b.Fatal("unexpected error:", err)
			}
			s.Out = nil
			s, remain, _ := ReadTable(compressed, s)
			s.Decompress1X(remain)
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				s, remain, err := ReadTable(compressed, s)
				if err != nil {
					b.Fatal(err)
				}
				_, err = s.Decompress1X(remain)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecompress1XNoTable(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		if test.err1X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			for _, sz := range []int{1e2, 1e4, BlockSizeMax} {
				b.Run(fmt.Sprintf("%d", sz), func(b *testing.B) {
					var s = &Scratch{}
					s.Reuse = ReusePolicyNone
					buf0, err := test.fn()
					if err != nil {
						b.Fatal(err)
					}
					for len(buf0) < sz {
						buf0 = append(buf0, buf0...)
					}
					if len(buf0) > sz {
						buf0 = buf0[:sz]
					}
					compressed, _, err := Compress1X(buf0, s)
					if err != test.err1X {
						if err == ErrUseRLE {
							b.Skip("RLE")
							return
						}
						b.Fatal("unexpected error:", err)
					}
					s.Out = nil
					s, remain, _ := ReadTable(compressed, s)
					s.Decompress1X(remain)
					b.ResetTimer()
					b.ReportAllocs()
					b.SetBytes(int64(len(buf0)))
					for i := 0; i < b.N; i++ {
						_, err = s.Decompress1X(remain)
						if err != nil {
							b.Fatal(err)
						}
					}
					b.ReportMetric(float64(s.actualTableLog), "log")
					b.ReportMetric(100*float64(len(compressed))/float64(len(buf0)), "pct")
				})
			}
		})
	}
}

func BenchmarkDecompress4XNoTable(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		if test.err4X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			for _, sz := range []int{1e2, 1e4, BlockSizeMax} {
				b.Run(fmt.Sprintf("%d", sz), func(b *testing.B) {
					var s = &Scratch{}
					s.Reuse = ReusePolicyNone
					buf0, err := test.fn()
					if err != nil {
						b.Fatal(err)
					}
					for len(buf0) < sz {
						buf0 = append(buf0, buf0...)
					}
					if len(buf0) > sz {
						buf0 = buf0[:sz]
					}
					compressed, _, err := Compress4X(buf0, s)
					if err != test.err4X {
						if err == ErrUseRLE {
							b.Skip("RLE")
							return
						}
						b.Fatal("unexpected error:", err)
					}
					s.Out = nil
					s, remain, _ := ReadTable(compressed, s)
					s.Decompress4X(remain, len(buf0))
					b.ResetTimer()
					b.ReportAllocs()
					b.SetBytes(int64(len(buf0)))
					for i := 0; i < b.N; i++ {
						_, err = s.Decompress4X(remain, len(buf0))
						if err != nil {
							b.Fatal(err)
						}
					}
					b.ReportMetric(float64(s.actualTableLog), "log")
					b.ReportMetric(100*float64(len(compressed))/float64(len(buf0)), "pct")

				})
			}
		})
	}
}

func BenchmarkDecompress4XNoTableTableLog8(b *testing.B) {
	for _, tt := range testfiles[:1] {
		test := tt
		if test.err4X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			var s = &Scratch{}
			s.Reuse = ReusePolicyNone
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			s.TableLog = 8
			compressed, _, err := Compress4X(buf0, s)
			if err != test.err1X {
				b.Fatal("unexpected error:", err)
			}
			s.Out = nil
			s, remain, _ := ReadTable(compressed, s)
			s.Decompress4X(remain, len(buf0))
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				_, err = s.Decompress4X(remain, len(buf0))
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecompress4XTable(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		if test.err4X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			var s = &Scratch{}
			s.Reuse = ReusePolicyNone
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			compressed, _, err := Compress4X(buf0, s)
			if err != test.err1X {
				b.Fatal("unexpected error:", err)
			}
			s.Out = nil
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				s, remain, err := ReadTable(compressed, s)
				if err != nil {
					b.Fatal(err)
				}
				_, err = s.Decompress4X(remain, len(buf0))
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
