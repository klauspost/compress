// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lzw_test

import (
	"bytes"
	stdlzw "compress/lzw"
	"io"
	"math/rand"
	"os"
	"testing"

	"github.com/klauspost/compress/lzw"
)

// TestRefOrderValues checks the assumption that lets the reference decoder in
// ref_aldus_test.go be driven with an order value taken from this package.
func TestRefOrderValues(t *testing.T) {
	if int(lzw.LSB) != int(LSB) || int(lzw.MSB) != int(MSB) {
		t.Fatalf("order values differ: LSB %d/%d, MSB %d/%d", lzw.LSB, LSB, lzw.MSB, MSB)
	}
}

// refEncode compresses raw with a deliberately simple LZW encoder. When aldus is
// true it grows the code width one code early, the variant that
// Reader.SetAldusCompatible selects. It exists to produce streams for the
// decoder tests without using this package's own Writer, and is written for
// clarity rather than speed.
func refEncode(raw []byte, order lzw.Order, litWidth int, aldus bool) []byte {
	const maxCodeWidth = 12
	const maxCode = 1<<maxCodeWidth - 1
	var out []byte
	var bits uint32
	var nBits uint
	width := uint(litWidth) + 1

	emit := func(code uint32) {
		if order == lzw.MSB {
			bits |= code << (32 - width - nBits)
			nBits += width
			for nBits >= 8 {
				out = append(out, byte(bits>>24))
				bits <<= 8
				nBits -= 8
			}
			return
		}
		bits |= code << nBits
		nBits += width
		for nBits >= 8 {
			out = append(out, byte(bits))
			bits >>= 8
			nBits -= 8
		}
	}

	clear := uint32(1) << litWidth
	eof := clear + 1
	hi, overflow := clear+1, uint32(1)<<width
	table := map[uint32]uint32{}

	// bumpHi advances the implied next code exactly as the decoder advances its
	// own, reporting whether the table filled up and a clear code was sent.
	bumpHi := func() bool {
		hi++
		grow := hi == overflow
		if aldus {
			grow = hi+1 == overflow
		}
		// The width never grows past the maximum: at that point the table is
		// simply full until a clear code is sent.
		if grow && width < maxCodeWidth {
			width++
			overflow <<= 1
		}
		if hi == maxCode {
			emit(clear)
			width = uint(litWidth) + 1
			hi, overflow = clear+1, uint32(1)<<width
			table = map[uint32]uint32{}
			return true
		}
		return false
	}

	emit(clear)
	if len(raw) > 0 {
		cur := uint32(raw[0])
		for _, b := range raw[1:] {
			key := cur<<8 | uint32(b)
			if c, ok := table[key]; ok {
				cur = c
				continue
			}
			emit(cur)
			cur = uint32(b)
			if bumpHi() {
				continue
			}
			table[key] = hi
		}
		emit(cur)
		bumpHi()
	}
	emit(eof)
	if nBits > 0 {
		if order == lzw.MSB {
			out = append(out, byte(bits>>24))
		} else {
			out = append(out, byte(bits))
		}
	}
	return out
}

func drainAll(r io.Reader) ([]byte, error) {
	var out []byte
	buf := make([]byte, 4096)
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

func ourReader(in []byte, order lzw.Order, litWidth int, aldus bool) io.ReadCloser {
	r := lzw.NewReader(bytes.NewReader(in), order, litWidth).(*lzw.Reader)
	r.SetAldusCompatible(aldus)
	return r
}

func testFiles(tb testing.TB) map[string][]byte {
	tb.Helper()
	out := map[string][]byte{}
	for _, name := range []string{"gettysburg.txt", "e.txt", "html.txt", "Mark.Twain-Tom.Sawyer.txt", "pngdata.bin"} {
		b, err := os.ReadFile("../testdata/" + name)
		if err != nil {
			tb.Fatal(err)
		}
		out[name] = b
	}
	rng := rand.New(rand.NewSource(11))
	random := make([]byte, 1<<18)
	rng.Read(random)
	out["random"] = random
	out["empty"] = nil
	out["one"] = []byte{3}
	out["runs"] = bytes.Repeat([]byte{1, 1, 1, 2, 2, 3}, 40000)
	return out
}

func mask(raw []byte, litWidth int) []byte {
	out := make([]byte, len(raw))
	for i, v := range raw {
		out[i] = v & byte(1<<litWidth-1)
	}
	return out
}

// TestRefEncoderIsValidLZW checks refEncode itself: with the standard width rule
// its output must decode correctly with compress/lzw, so that a failure of the
// Aldus tests below can be attributed to the Aldus rule rather than to the
// reference encoder being wrong.
func TestRefEncoderIsValidLZW(t *testing.T) {
	for name, file := range testFiles(t) {
		for _, order := range []lzw.Order{lzw.LSB, lzw.MSB} {
			for litWidth := 2; litWidth <= 8; litWidth++ {
				raw := mask(file, litWidth)
				in := refEncode(raw, order, litWidth, false)
				got, err := drainAll(stdlzw.NewReader(bytes.NewReader(in), stdlzw.Order(order), litWidth))
				if err != io.EOF || !bytes.Equal(got, raw) {
					t.Fatalf("%s order=%d litWidth=%d: compress/lzw got %d bytes (%v), want %d",
						name, order, litWidth, len(got), err, len(raw))
				}
			}
		}
	}
}

// TestAldusMatchesReference decodes Aldus streams with both the reference
// decoder copied from x/image and this package's, and requires that both
// reproduce the input.
func TestAldusMatchesReference(t *testing.T) {
	for name, file := range testFiles(t) {
		for _, order := range []lzw.Order{lzw.LSB, lzw.MSB} {
			for litWidth := 2; litWidth <= 8; litWidth++ {
				raw := mask(file, litWidth)
				in := refEncode(raw, order, litWidth, true)

				ref, err := drainAll(NewReader(bytes.NewReader(in), Order(order), litWidth))
				if err != io.EOF || !bytes.Equal(ref, raw) {
					t.Fatalf("%s order=%d litWidth=%d: reference decoder got %d bytes (%v), want %d",
						name, order, litWidth, len(ref), err, len(raw))
				}
				got, err := drainAll(ourReader(in, order, litWidth, true))
				if err != io.EOF || !bytes.Equal(got, raw) {
					t.Fatalf("%s order=%d litWidth=%d: got %d bytes (%v), want %d",
						name, order, litWidth, len(got), err, len(raw))
				}
			}
		}
	}
}

// TestAldusIsNotStandard checks that the flag actually selects a different
// dialect, so that the tests above are not passing for a trivial reason.
func TestAldusIsNotStandard(t *testing.T) {
	// Long enough to reach the first width change at code 511.
	raw := mask([]byte(bytes.Repeat([]byte("Aldus and standard LZW diverge here. "), 400)), 8)
	aldusIn := refEncode(raw, lzw.MSB, 8, true)
	stdIn := refEncode(raw, lzw.MSB, 8, false)
	if bytes.Equal(aldusIn, stdIn) {
		t.Fatal("the two variants encoded to the same bytes")
	}
	got, err := drainAll(ourReader(aldusIn, lzw.MSB, 8, false))
	if err == io.EOF && bytes.Equal(got, raw) {
		t.Fatal("decoded an Aldus stream as standard LZW without noticing")
	}
	got, err = drainAll(ourReader(stdIn, lzw.MSB, 8, true))
	if err == io.EOF && bytes.Equal(got, raw) {
		t.Fatal("decoded a standard stream as Aldus without noticing")
	}
}

// TestAldusSurvivesReset checks the documented lifetime of the setting.
func TestAldusSurvivesReset(t *testing.T) {
	raw := mask([]byte(bytes.Repeat([]byte("reset keeps the dialect "), 500)), 8)
	in := refEncode(raw, lzw.MSB, 8, true)

	r := lzw.NewReader(bytes.NewReader(in), lzw.MSB, 8).(*lzw.Reader)
	r.SetAldusCompatible(true)
	for i := range 3 {
		r.Reset(bytes.NewReader(in), lzw.MSB, 8)
		got, err := drainAll(r)
		if err != io.EOF || !bytes.Equal(got, raw) {
			t.Fatalf("after %d resets: got %d bytes (%v), want %d", i, len(got), err, len(raw))
		}
	}
	r.SetAldusCompatible(false)
	r.Reset(bytes.NewReader(in), lzw.MSB, 8)
	if got, err := drainAll(r); err == io.EOF && bytes.Equal(got, raw) {
		t.Fatal("still decoding as Aldus after the setting was turned off")
	}
}

// refEmitCodes writes the given codes using the Aldus width schedule, tracking
// the width exactly as a decoder does. It is for building streams by hand that
// no conforming encoder would produce.
func refEmitCodes(codes []uint32, order lzw.Order, litWidth int) []byte {
	var out []byte
	var bits uint32
	var nBits uint
	width := uint(litWidth) + 1
	clearCode := uint32(1) << litWidth
	hi, overflow := clearCode+1, uint32(1)<<width

	for _, c := range codes {
		if order == lzw.MSB {
			bits |= c << (32 - width - nBits)
			nBits += width
			for nBits >= 8 {
				out = append(out, byte(bits>>24))
				bits <<= 8
				nBits -= 8
			}
		} else {
			bits |= c << nBits
			nBits += width
			for nBits >= 8 {
				out = append(out, byte(bits))
				bits >>= 8
				nBits -= 8
			}
		}
		switch {
		case c == clearCode:
			width = uint(litWidth) + 1
			hi, overflow = clearCode+1, uint32(1)<<width
		case c == clearCode+1: // EOF
		default:
			hi++
			if hi+1 == overflow && width < 12 {
				width++
				overflow <<= 1
			}
		}
	}
	if nBits > 0 {
		if order == lzw.MSB {
			out = append(out, byte(bits>>24))
		} else {
			out = append(out, byte(bits))
		}
	}
	return out
}

// TestAldusMaxWidthCode covers the one place this decoder deliberately differs
// from the reference. With the Aldus rule the table stops growing at code 4094,
// so code 4095 is never assigned and a stream using it is corrupt. This decoder
// rejects it, which also keeps hi from climbing past the maximum and eventually
// wrapping a uint16. The reference lets hi climb instead, so it accepts the code
// and expands the entry that was never assigned. Reaching this needs 3838 codes,
// far more than FuzzAldus will produce, hence the hand built stream.
func TestAldusMaxWidthCode(t *testing.T) {
	// 254 + 512 + 1024 + 2048 codes take hi from 257 to 4095, the point at which
	// the table is full.
	codes := []uint32{256}
	for range 254 + 512 + 1024 + 2048 {
		codes = append(codes, 0)
	}
	codes = append(codes, 4095, 257)
	in := refEmitCodes(codes, lzw.MSB, 8)

	got, gotErr := drainAll(ourReader(in, lzw.MSB, 8, true))
	if gotErr == nil || gotErr.Error() != "lzw: invalid code" {
		t.Fatalf("got %d bytes, %v; want a rejection of code 4095", len(got), gotErr)
	}
	if want := 254 + 512 + 1024 + 2048; len(got) != want {
		t.Fatalf("decoded %d bytes before the bad code, want %d", len(got), want)
	}
	// Recorded for contrast: the reference expands the unassigned entry, whose
	// zeroed prefix and suffix yield two bytes of nothing, and reports success.
	ref, refErr := drainAll(NewReader(bytes.NewReader(in), MSB, 8))
	if refErr != io.EOF || len(ref) != len(got)+2 {
		t.Logf("reference behaviour changed: %d bytes, %v (ours: %d bytes, %v)",
			len(ref), refErr, len(got), gotErr)
	}
}

// FuzzAldus requires that Aldus decoding matches the reference decoder byte for
// byte and error for error, including for corrupt and truncated input.
func FuzzAldus(f *testing.F) {
	f.Add(refEncode([]byte("TOBEORNOTTOBEORTOBEORNOT"), lzw.MSB, 8, true), uint8(1))
	f.Add(refEncode(bytes.Repeat([]byte{7}, 20000), lzw.MSB, 8, true), uint8(1))
	f.Fuzz(func(t *testing.T, in []byte, cfg uint8) {
		order, litWidth := lzw.Order(cfg&1), int(cfg>>1)%7+2
		want, wantErr := drainAll(NewReader(bytes.NewReader(in), Order(order), litWidth))
		got, gotErr := drainAll(ourReader(in, order, litWidth, true))
		if !bytes.Equal(got, want) || errText(gotErr) != errText(wantErr) {
			t.Fatalf("order=%d litWidth=%d: got %d bytes %v, want %d bytes %v\n got: %x\nwant: %x",
				order, litWidth, len(got), gotErr, len(want), wantErr, head(got), head(want))
		}
	})
}

// TestReadFillsBuffer checks that a Read given room to decode into directly
// hands back a full buffer while the stream still holds that much. An io.Reader
// is not obliged to, but decoding stops a whole maximal expansion short of the
// end of the buffer, so returning that count would leave a caller that does not
// loop with all but the last few KB of its data — right enough to pass a glance
// and wrong at the tail. Buffers too small to decode into directly are excluded:
// they are served from the output buffer, and a short read there is as plain as
// the one compress/lzw returns.
func TestReadFillsBuffer(t *testing.T) {
	raw := mask(testFiles(t)["Mark.Twain-Tom.Sawyer.txt"], 8)
	for _, aldus := range []bool{false, true} {
		in := refEncode(raw, lzw.MSB, 8, aldus)
		for _, size := range []int{8192, 1 << 15, 100000, len(raw) + 1000} {
			r := ourReader(in, lzw.MSB, 8, aldus)
			buf := make([]byte, size)
			for off := 0; off < len(raw); {
				n, err := r.Read(buf)
				if want := min(size, len(raw)-off); n != want {
					t.Fatalf("aldus=%v size=%d off=%d: read %d bytes (%v), want %d",
						aldus, size, off, n, err, want)
				}
				if !bytes.Equal(buf[:n], raw[off:off+n]) {
					t.Fatalf("aldus=%v size=%d off=%d: wrong bytes", aldus, size, off)
				}
				off += n
				if err != nil {
					break
				}
			}
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func head(b []byte) []byte {
	if len(b) > 48 {
		return b[:48]
	}
	return b
}

func BenchmarkDecodeAldus(b *testing.B) {
	raw, err := os.ReadFile("../testdata/Mark.Twain-Tom.Sawyer.txt")
	if err != nil {
		b.Fatal(err)
	}
	for _, aldus := range []bool{false, true} {
		name := "standard"
		if aldus {
			name = "aldus"
		}
		b.Run(name, func(b *testing.B) {
			in := refEncode(raw, lzw.MSB, 8, aldus)
			dst := make([]byte, 64<<10)
			src := bytes.NewReader(in)
			r := lzw.NewReader(src, lzw.MSB, 8).(*lzw.Reader)
			r.SetAldusCompatible(aldus)
			b.SetBytes(int64(len(raw)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				src.Reset(in)
				r.Reset(src, lzw.MSB, 8)
				total := 0
				for {
					got, err := r.Read(dst)
					total += got
					if err != nil {
						if err != io.EOF {
							b.Fatal(err)
						}
						break
					}
				}
				if total != len(raw) {
					b.Fatalf("decoded %d bytes, want %d", total, len(raw))
				}
			}
		})
	}
}
