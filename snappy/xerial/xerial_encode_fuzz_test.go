package xerial

import (
	"bytes"
	"testing"
	"github.com/klauspost/compress/internal/fuzz"
	"github.com/klauspost/compress/s2"
)

func FuzzEncoder(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/fuzz/block-corpus-raw.zip", fuzz.TypeRaw, false)

	f.Fuzz(func(t *testing.T, data []byte) {
		encoded := Encode(make([]byte, 0, len(data)/2), data)
		decoded, err := Decode(encoded)
		if err != nil {
			t.Errorf("input: %+v, encoded: %+v", data, encoded)
			t.Fatal(err)
		}
		if !bytes.Equal(decoded, data) {
			t.Fatal("mismatch")
		}

		encoded = EncodeBetter(make([]byte, 0, len(data)/2), data)
		decoded, err = Decode(encoded)
		if err != nil {
			t.Errorf("input: %+v, encoded: %+v", data, encoded)
			t.Fatal(err)
		}
		if !bytes.Equal(decoded, data) {
			t.Fatal("mismatch")
		}

		encoded = s2.EncodeSnappy(make([]byte, 0, len(data)/2), data)
		decoded, err = Decode(encoded)
		if err != nil {
			t.Errorf("input: %+v, encoded: %+v", data, encoded)
			t.Fatal(err)
		}
		if !bytes.Equal(decoded, data) {
			t.Fatal("mismatch")
		}
	})
}
