package xerial

import (
	"bytes"
	"testing"

	"github.com/klauspost/compress/internal/fuzz"
	"github.com/klauspost/compress/s2"
)

func FuzzDecode(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/FuzzDecoder.zip", fuzz.TypeGoFuzz, false)
	const limit = 1 << 20
	dst := make([]byte, 0, limit)
	f.Fuzz(func(t *testing.T, data []byte) {
		got, _ := DecodeCapped(dst[:0], data)
		if len(got) > cap(dst) {
			t.Fatalf("cap exceeded: %d > %d", len(got), cap(dst))
		}
	})
}

func FuzzEncode(f *testing.F) {
	fuzz.AddFromZip(f, "../../s2/testdata/enc_regressions.zip", fuzz.TypeRaw, false)
	fuzz.AddFromZip(f, "../../s2/testdata/fuzz/block-corpus-raw.zip", fuzz.TypeRaw, testing.Short())
	fuzz.AddFromZip(f, "../../s2/testdata/fuzz/block-corpus-enc.zip", fuzz.TypeGoFuzz, testing.Short())

	f.Fuzz(func(t *testing.T, data []byte) {
		t.Run("standard", func(t *testing.T) {
			encoded := Encode(make([]byte, 0, len(data)/2), data)
			decoded, err := Decode(encoded)
			if err != nil {
				t.Errorf("input: %+v, encoded: %+v", data, encoded)
				t.Fatal(err)
			}
			if !bytes.Equal(decoded, data) {
				t.Fatal("mismatch")
			}

		})
		t.Run("better", func(t *testing.T) {
			encoded := EncodeBetter(make([]byte, 0, len(data)/2), data)
			decoded, err := Decode(encoded)
			if err != nil {
				t.Errorf("input: %+v, encoded: %+v", data, encoded)
				t.Fatal(err)
			}
			if !bytes.Equal(decoded, data) {
				t.Fatal("mismatch")
			}
		})
		t.Run("snappy", func(t *testing.T) {
			encoded := s2.EncodeSnappy(make([]byte, 0, len(data)/2), data)
			decoded, err := Decode(encoded)
			if err != nil {
				t.Errorf("input: %+v, encoded: %+v", data, encoded)
				t.Fatal(err)
			}
			if !bytes.Equal(decoded, data) {
				t.Fatal("mismatch")
			}
		})
	})
}
