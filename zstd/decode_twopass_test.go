package zstd

import (
	"bytes"
	"io"
	"math"
	"math/rand"
	"testing"
)

// twoPassTestInput builds ~6 MiB whose matches reach across the whole
// window: random vocabulary text, then copies from up to 4 MiB back.
func twoPassTestInput() []byte {
	rng := rand.New(rand.NewSource(7))
	vocab := make([][]byte, 4096)
	for i := range vocab {
		w := make([]byte, 3+rng.Intn(8))
		for j := range w {
			w[j] = byte('a' + rng.Intn(26))
		}
		vocab[i] = w
	}
	buf := make([]byte, 0, 6<<20)
	for len(buf) < 4<<20 {
		buf = append(buf, vocab[rng.Intn(len(vocab))]...)
		buf = append(buf, " ,.\n"[rng.Intn(4)])
	}
	for len(buf) < 6<<20 {
		for i := rng.Intn(4); i > 0; i-- {
			buf = append(buf, byte(rng.Intn(256)))
		}
		n := 4 + rng.Intn(60)
		off := 1 + rng.Intn(4<<20)
		src := len(buf) - off
		buf = append(buf, buf[src:src+n]...)
	}
	return buf
}

// forceTwoPass makes the synchronous decoder take the two-pass path on every
// block (on) or the one-pass path (off) whatever the data, and returns a
// function restoring the thresholds.
func forceTwoPass(on bool) func() {
	a, b := decodeTwoPassMinWindow, twoPassMinFarShare
	if on {
		decodeTwoPassMinWindow, twoPassMinFarShare = 0, 0
	} else {
		decodeTwoPassMinWindow, twoPassMinFarShare = math.MaxInt, 257
	}
	return func() { decodeTwoPassMinWindow, twoPassMinFarShare = a, b }
}

// TestSequenceDecsUseTwoPass checks the two-pass decision against each kind
// of offset table.
func TestSequenceDecsUseTwoPass(t *testing.T) {
	defer forceTwoPass(false)()
	defer func(v int) { twoPassFarCode = v }(twoPassFarCode)
	decodeTwoPassMinWindow, twoPassMinFarShare, twoPassFarCode = 1<<20, 46, 17

	// fseTable: log-8 table with far slots on codes 17-19 (two of them -1),
	// the rest on code 3.
	fseTable := func(far int) *fseDecoder {
		f := &fseDecoder{actualTableLog: 8, symbolLen: 20}
		f.norm[3] = int16(256 - far)
		f.norm[twoPassFarCode] = int16(far - 2)
		f.norm[twoPassFarCode+1] = -1
		f.norm[twoPassFarCode+2] = -1
		return f
	}
	rle := func(code uint8) *fseDecoder {
		return &fseDecoder{actualTableLog: 0, maxBits: code}
	}
	predefined := &fseDecoder{preDefined: true}

	tests := []struct {
		name   string
		fse    *fseDecoder
		window int
		want   bool
	}{
		{"fse at threshold", fseTable(46), 1 << 20, true},
		{"fse at threshold small window", fseTable(46), 1<<20 - 1, false},
		{"fse below threshold", fseTable(45), 1 << 30, false},
		{"fse far only", fseTable(256), 1 << 20, true},
		{"fse none far", fseTable(2), 1 << 30, false},
		{"rle large window", rle(uint8(twoPassFarCode - 1)), 1 << 20, true},
		{"rle small window", rle(uint8(twoPassFarCode)), 1<<20 - 1, false},
		{"predefined small window", predefined, 1<<20 - 1, false},
		{"predefined large window", predefined, 1 << 20, true},
		{"no table small window", nil, 1<<20 - 1, false},
		{"no table large window", nil, 1 << 20, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := sequenceDecs{windowSize: tt.window}
			s.offsets.fse = tt.fse
			if got := s.useTwoPass(); got != tt.want {
				t.Errorf("useTwoPass = %v, want %v (share %d)", got, tt.want, tt.fse.codeShare(twoPassFarCode))
			}
		})
	}
}

// TestDecodeTwoPassMatchesOnePass forces both the two-pass and the one-pass
// path on any architecture and checks every DecodeAll and Reader shape, plus
// rejection of corrupt input, on each.
func TestDecodeTwoPassMatchesOnePass(t *testing.T) {
	input := twoPassTestInput()
	frames := map[string][]byte{}
	for _, lvl := range []EncoderLevel{SpeedDefault, SpeedBestCompression} {
		enc, err := NewWriter(nil, WithEncoderLevel(lvl), WithWindowSize(8<<20))
		if err != nil {
			t.Fatal(err)
		}
		frames[lvl.String()] = enc.EncodeAll(input, nil)
		enc.Close()
	}

	paths := []struct {
		name string
		on   bool
	}{
		{"two-pass+prefetch", true},
		{"one-pass", false},
	}
	for _, p := range paths {
		for name, comp := range frames {
			t.Run(p.name+"/"+name, func(t *testing.T) {
				defer forceTwoPass(p.on)()

				dec, err := NewReader(nil, WithDecoderConcurrency(1))
				if err != nil {
					t.Fatal(err)
				}
				defer dec.Close()

				got, err := dec.DecodeAll(comp, nil)
				if err != nil {
					t.Fatal("DecodeAll:", err)
				}
				if !bytes.Equal(got, input) {
					t.Fatal("DecodeAll output differs from input")
				}

				// A non-empty dst puts the history in a separate buffer.
				prefix := []byte("prefix bytes that are not part of the frame")
				got, err = dec.DecodeAll(comp, append([]byte(nil), prefix...))
				if err != nil {
					t.Fatal("DecodeAll with prefix:", err)
				}
				if !bytes.Equal(got, append(append([]byte(nil), prefix...), input...)) {
					t.Fatal("DecodeAll with prefix: output differs from input")
				}

				// Exactly sized dst.
				got, err = dec.DecodeAll(comp, make([]byte, 0, len(input)))
				if err != nil {
					t.Fatal("DecodeAll exact:", err)
				}
				if !bytes.Equal(got, input) {
					t.Fatal("DecodeAll exact: output differs from input")
				}

				for _, n := range []int{1, 2} {
					rdr, err := NewReader(bytes.NewReader(comp), WithDecoderConcurrency(n))
					if err != nil {
						t.Fatal(err)
					}
					got, err = io.ReadAll(rdr)
					rdr.Close()
					if err != nil {
						t.Fatalf("Reader concurrency %d: %v", n, err)
					}
					if !bytes.Equal(got, input) {
						t.Fatalf("Reader concurrency %d: output differs from input", n)
					}
				}

				// Corrupt input must error, not panic.
				bad := append([]byte(nil), comp...)
				for i := len(bad) / 3; i < len(bad); i += len(bad) / 7 {
					bad[i] ^= 0x5a
				}
				if _, err := dec.DecodeAll(bad, nil); err == nil {
					t.Fatal("corrupted frame decoded without error")
				}
				if _, err := dec.DecodeAll(comp[:len(comp)*2/3], nil); err == nil {
					t.Fatal("truncated frame decoded without error")
				}
			})
		}
	}
}
