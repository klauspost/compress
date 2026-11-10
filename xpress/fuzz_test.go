package xpress

import (
	"testing"
)

// FuzzXpressPlain runs the fuzzed input through both decompressors and asserts
// that neither panics and that neither ever produces more than MaxSize
// bytes of output. The seed corpus is the compressed side of the MS-XCA
// test vectors.
func FuzzXpressPlain(f *testing.F) {
	for _, v := range xpressVectors {
		f.Add(v.data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		pre := make([]byte, len(data)&(MaxSize-1))
		out, err := AppendDecompressed(pre, data)
		if len(out)-len(pre) > MaxSize {
			t.Fatalf("plain LZ77: %d bytes of output exceeds MaxSize", len(out))
		}
		if err != nil && len(out) != len(pre) {
			t.Fatalf("plain LZ77: error, but output modified %d != %d", len(out), len(pre))
		}
	})
}

// FuzzXpressHuffman is for the Huffman variant, which
// requires the uncompressed size as a separate argument.
func FuzzXpressHuffman(f *testing.F) {
	for _, v := range xpressVectors {
		f.Add(v.data, len(v.expected))
	}
	f.Fuzz(func(t *testing.T, data []byte, dsize int) {
		pre := make([]byte, dsize&(MaxSize-1))
		out, err := AppendHDecompressed(pre, data, dsize)
		if len(out)-len(pre) > MaxSize {
			t.Fatalf("LZ77+Huffman: %d bytes of output exceeds MaxSize", len(out))
		}
		if err != nil && len(out) != len(pre) {
			t.Fatalf("LZ77+Huff: error, but output modified %d != %d", len(out), len(pre))
		}
		if err == nil && len(out)-len(pre) != dsize {
			t.Fatalf("LZ77+Huff: %d bytes of output, want %d", len(out)-len(pre), dsize)
		}
	})
}
