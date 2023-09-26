package s2

import (
	"testing"

	"github.com/klauspost/compress/internal/fuzz"
)

func FuzzS2(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/fuzz/block-corpus-raw.zip", fuzz.TypeRaw, false)
	f.Fuzz(func(t *testing.T, data []byte) {
		concat, err := ConcatBlocks(nil, data, []byte{0})
		if err != nil || concat == nil {
			return
		}

		EstimateBlockSize(data)
		encoded := make([]byte, MaxEncodedLen(len(data)))
		if len(encoded) < MaxEncodedLen(len(data)) || minNonLiteralBlockSize > len(data) || len(data) > maxBlockSize {
			return
		}

		encodeBlockGo(encoded, data)
		encodeBlockBetterGo(encoded, data)
		encodeBlockSnappyGo(encoded, data)
		encodeBlockBetterSnappyGo(encoded, data)
		dst := encodeGo(encoded, data)
		if dst == nil {
			return
		}
	})
}
