package zstd

import (
	"bytes"
	"io"
	"math/rand"
	"testing"
)

// hideLen wraps a reader so the decoder cannot detect a *bytes.Reader and take
// the small-buffer sync path; this forces the concurrent streaming decode path.
type hideLen struct{ r io.Reader }

func (h hideLen) Read(p []byte) (int, error) { return h.r.Read(p) }

// TestDecoderDictReuseAfterLargeStream verifies that streaming-decoding a
// dictionary-compressed frame whose output exceeds the window does not corrupt
// the registered dictionary for later decodes on the same decoder.
func TestDecoderDictReuseAfterLargeStream(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	dictContent := make([]byte, 8<<10)
	rng.Read(dictContent)
	const dictID = 12345

	// The payload starts with the dictionary content (so the encoder emits a
	// match into the dictionary) followed by a compressible body large enough
	// that decoded history exceeds the window while compressed blocks are still
	// being executed.
	var payload []byte
	for i := 0; i < 301; i++ {
		payload = append(payload, dictContent...)
	}

	enc, err := NewWriter(nil, WithEncoderDictRaw(dictID, dictContent), WithWindowSize(256<<10))
	if err != nil {
		t.Fatal(err)
	}
	frame := enc.EncodeAll(payload, nil)
	enc.Close()

	dec, err := NewReader(nil, WithDecoderDictRaw(dictID, dictContent))
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	for i := 0; i < 3; i++ {
		if err := dec.Reset(hideLen{bytes.NewReader(frame)}); err != nil {
			t.Fatalf("reset %d: %v", i, err)
		}
		got, err := io.ReadAll(dec)
		if err != nil {
			t.Fatalf("decode %d of a valid dictionary frame failed: %v", i, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("decode %d: output mismatch: got %d bytes, want %d", i, len(got), len(payload))
		}
	}
}
