package xpress

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestXpressPlainLZ77(t *testing.T) {
	for _, v := range xpressVectors {
		if v.huff {
			continue
		}
		out, err := AppendDecompressed(nil, v.data)
		if err != nil {
			t.Errorf("%s: %v", v.name, err)
			continue
		}
		if string(out) != string(v.expected) {
			t.Errorf("%s: mismatched output (%d bytes)", v.name, len(out))
		}
	}
}

func TestXpressHuffman(t *testing.T) {
	for _, v := range xpressVectors {
		if !v.huff {
			continue
		}
		out, err := AppendHDecompressed(nil, v.data, len(v.expected))
		if err != nil {
			t.Errorf("%s: %v", v.name, err)
			continue
		}
		if string(out) != string(v.expected) {
			t.Errorf("%s: mismatched output (%d bytes)", v.name, len(out))
		}
	}
}

func TestXpressTruncated(t *testing.T) {
	var byName = func(name string) xpressVector {
		for _, v := range xpressVectors {
			if strings.HasPrefix(v.name, name) {
				return v
			}
		}
		t.Fatalf("vector %q not found", name)
		return xpressVector{}
	}

	// Plain LZ77: cut off mid-match.
	plain := byName("Plain1")
	_, err := AppendDecompressed(nil, plain.data[:8])
	if !errors.Is(err, errTruncated) {
		t.Errorf("plain: expected truncated stream, got %v", err)
	}

	// Huffman: fewer than the mandatory 256 table bytes.
	huff0 := byName("Huff0")
	_, err = AppendHDecompressed(nil, huff0.data[:100], 360)
	if !errors.Is(err, errTruncated) {
		t.Errorf("huffman: expected truncated stream on short table, got %v", err)
	}

	// Huffman: declare a larger output than the stream contains. The
	// stream's end-of-data symbol lands mid-loop with fewer than 3
	// bytes remaining, so the decoder reports a corrupt stream.
	huff5 := byName("Huff5")
	_, err = AppendHDecompressed(nil, huff5.data, 22)
	if !errors.Is(err, errCorrupt) {
		t.Errorf("huffman: expected corrupt stream on oversized declare, got %v", err)
	}
}

func TestXpressHuffmanMatchPastSize(t *testing.T) {
	// Huff7 is a literal followed by one match of 70000 bytes. A smaller
	// declared size must not be overrun by that match.
	for _, v := range xpressVectors {
		if !strings.HasPrefix(v.name, "Huff7") {
			continue
		}
		for _, size := range []int{2, 1000, len(v.expected) - 1} {
			out, err := AppendHDecompressed(nil, v.data, size)
			if !errors.Is(err, errCorrupt) {
				t.Errorf("size %d: expected corrupt stream, got %v with %d bytes", size, err, len(out))
			}
		}
		return
	}
	t.Fatal("vector Huff7 not found")
}

func TestXpressExpansionRatio(t *testing.T) {
	// A match with a 4GB raw length via the LE32 extension must be
	// rejected by the decompressed-size cap. The flag group is stored
	// little-endian, so bit 31 (a match) is the last byte 0x40.
	in := []byte{
		0x00, 0x00, 0x00, 0x40, // flag group: literal, then match
		0x41,       // literal 'A'
		0x07, 0x00, // match: offset 1, length nibble 7
		0xFF,       // nibble 15: raw length byte follows
		0xFF,       // raw length byte 255: LE16 follows
		0x00, 0x00, // -> LE16 0: LE32 follows
		0xFF, 0xFF, 0xFF, 0xFF, // -> LE32 (4GB - 1)
	}
	_, err := AppendDecompressed(nil, in)
	if !errors.Is(err, errTooLarge) {
		t.Errorf("plain: expected compression ratio error, got %v", err)
	}
}

func TestXpressCorruptCode(t *testing.T) {
	// Huffman table of all-zero lengths is an incomplete prefix code.
	in := make([]byte, 260)
	_, err := AppendHDecompressed(nil, in, 1)
	if err == nil {
		t.Error("huffman: expected error on invalid code lengths")
	}
}

func TestXpressOOB(t *testing.T) {
	// A match flag with only one byte of the two-byte match word.
	_, err := AppendDecompressed(nil, []byte{0x00, 0x00, 0x00, 0x80, 0x01})
	if !errors.Is(err, errTruncated) {
		t.Errorf("plain: expected truncated stream, got %v", err)
	}

	// A match farther back than the output produced so far.
	_, err = AppendDecompressed(nil, []byte{0x00, 0x00, 0x00, 0x40, 0x41, 0x50, 0x00})
	if !errors.Is(err, errCorrupt) {
		t.Errorf("plain: expected out-of-range offset, got %v", err)
	}
}

func TestXpressAppendMode(t *testing.T) {
	// The append-API keeps the caller's prefix and continues decoding.
	for _, v := range xpressVectors {
		if v.huff {
			continue
		}
		prefix := []byte("PRE")
		out, err := AppendDecompressed(prefix, v.data)
		if err != nil {
			t.Fatal(err)
		}
		if string(out[:len(prefix)]) != "PRE" {
			t.Error("append: prefix lost")
		}
		if string(out[len(prefix):]) != string(v.expected) {
			t.Error("append: decoded body mismatch")
		}
		prefix = make([]byte, MaxSize)
		out, err = AppendDecompressed(prefix, v.data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out[:MaxSize], prefix) {
			t.Error("append: prefix lost")
		}
		if string(out[len(prefix):]) != string(v.expected) {
			t.Error("append: decoded body mismatch")
		}

		// On error the output is returned unmodified: the returned slice
		// must equal the original prefix, not the partially built prefix.
		prefix2 := []byte("PRE")
		out2, err := AppendDecompressed(prefix2, []byte{0x00, 0x00, 0x00, 0x40, 0x41, 0x07, 0x00})
		if !errors.Is(err, errTruncated) {
			t.Fatalf("expected truncation error, got %v", err)
		}
		if !bytes.Equal(out2, prefix2) {
			t.Errorf("append: error path modified the slice: %q", out2)
		}
	}
}

func TestXpressAppendModeHuff(t *testing.T) {
	// The append-API keeps the caller's prefix and continues decoding.
	for _, v := range xpressVectors {
		if !v.huff {
			continue
		}
		prefix := []byte("PRE")
		out, err := AppendHDecompressed(prefix, v.data, len(v.expected))
		if err != nil {
			t.Fatal(err)
		}
		if string(out[:len(prefix)]) != "PRE" {
			t.Error("append: prefix lost")
		}
		if string(out[len(prefix):]) != string(v.expected) {
			t.Error("append: decoded body mismatch")
		}
		prefix = make([]byte, MaxSize)
		out, err = AppendHDecompressed(prefix, v.data, len(v.expected))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out[:MaxSize], prefix) {
			t.Error("append: prefix lost")
		}
		if string(out[len(prefix):]) != string(v.expected) {
			t.Error("append: decoded body mismatch")
		}
		// On error the output is returned unmodified: the returned slice
		// must equal the original prefix, not the partially built prefix.
		prefix2 := []byte("PRE")
		out2, err := AppendHDecompressed(prefix2, []byte{0x00, 0x00, 0x00, 0x40, 0x41, 0x07, 0x00}, 7)
		if !errors.Is(err, errTruncated) {
			t.Fatalf("expected truncation error, got %v", err)
		}
		if !bytes.Equal(out2, prefix2) {
			t.Errorf("append: error path modified the slice: %q", out2)
		}
	}
}
