// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flate

import (
	"math/rand"
	"slices"
	"testing"
)

// checkCode verifies that codes is a complete prefix code for the
// symbols with non-zero frequency, that no code is longer than maxBits,
// and returns the total number of bits needed to encode freq with it.
func checkCode(t *testing.T, freq []uint16, codes []hcode, maxBits int) (total int) {
	t.Helper()
	var kraft uint64 // scaled by 1<<maxBitsLimit
	var used []int
	for i, f := range freq {
		c := codes[i]
		if f == 0 {
			if !c.zero() {
				t.Fatalf("symbol %d has frequency 0 but code of length %d", i, c.len())
			}
			continue
		}
		n := int(c.len())
		if n == 0 || n > maxBits {
			t.Fatalf("symbol %d: code length %d, want 1..%d", i, n, maxBits)
		}
		if c.code64()>>n != 0 {
			t.Fatalf("symbol %d: code %#x does not fit in %d bits", i, c.code64(), n)
		}
		kraft += 1 << (maxBitsLimit - n)
		total += n * int(f)
		used = append(used, i)
	}
	// Two or fewer symbols are all assigned length 1 and may leave the
	// code incomplete; otherwise the code must be complete.
	if len(used) > 2 && kraft != 1<<maxBitsLimit {
		t.Fatalf("code is not complete: kraft sum %d/%d", kraft, 1<<maxBitsLimit)
	} else if kraft > 1<<maxBitsLimit {
		t.Fatalf("code is not a prefix code: kraft sum %d/%d", kraft, 1<<maxBitsLimit)
	}

	// The Kraft sum only constrains the code lengths. Also check the
	// code words themselves: no code may equal or be a prefix of another.
	// Codes are stored bit-reversed, so a prefix is a suffix in the
	// stored form: the shorter code must match the low bits of the longer.
	for a, i := range used {
		ci := codes[i]
		for _, j := range used[a+1:] {
			cj := codes[j]
			n := min(ci.len(), cj.len())
			mask := uint64(1)<<n - 1
			if ci.code64()&mask == cj.code64()&mask {
				t.Fatalf("symbol %d (code %0*b) and symbol %d (code %0*b) overlap",
					i, ci.len(), ci.code64(), j, cj.len(), cj.code64())
			}
		}
	}
	return total
}

func countNonZero(freq []uint16) (n int) {
	for _, f := range freq {
		if f != 0 {
			n++
		}
	}
	return n
}

// TestHuffmanGenerateOptimal checks that the fast unrestricted tree
// construction produces codes of the same total length as the
// length-limited construction whenever it is used.
func TestHuffmanGenerateOptimal(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	treeUsed := 0
	for iter := range 2000 {
		size := []int{literalCount, offsetCodeCount, codegenCodeCount}[iter%3]
		maxBits := int32(15)
		if size == codegenCodeCount {
			maxBits = 7
		}
		freq := make([]uint16, size)
		// Vary the number of used symbols and the skew of the distribution.
		used := 3 + rng.Intn(size-2)
		for range used {
			i := rng.Intn(size)
			switch rng.Intn(3) {
			case 0:
				freq[i] += uint16(1 + rng.Intn(10))
			case 1:
				freq[i] += uint16(1 + rng.Intn(1000))
			default:
				freq[i] += uint16(1 + rng.Intn(60000))
			}
		}
		if countNonZero(freq) < 3 {
			continue
		}

		// The sorted symbol list, as generate builds it.
		h := newHuffmanEncoder(size)
		list := h.freqcache[:len(freq)+1]
		count := 0
		for i, f := range freq {
			if f != 0 {
				list[count] = literalNode{uint16(i), f}
				count++
			}
		}
		list = list[:count]
		slices.SortFunc(list, func(a, b literalNode) int {
			return (int(a.freq)<<10 + int(a.literal)) - (int(b.freq)<<10 + int(b.literal))
		})

		// Unrestricted tree construction. Only compare when it is within
		// the limit, which is when generate uses it.
		treeCount, treeMax := h.treeBitCounts(list)
		if treeMax > int(maxBits) {
			continue
		}
		treeUsed++
		treeCount = slices.Clone(treeCount)

		// Reference: the length-limited construction.
		ref := newHuffmanEncoder(size)
		refCount := slices.Clone(ref.bitCounts(list, maxBits))

		// assignEncodingAndSize reorders list, so give each call a copy.
		tree := newHuffmanEncoder(size)
		tree.assignEncodingAndSize(treeCount, slices.Clone(list), tree.codes)
		gotBits := checkCode(t, freq, tree.codes, int(maxBits))
		ref.assignEncodingAndSize(refCount, slices.Clone(list), ref.codes)
		wantBits := checkCode(t, freq, ref.codes, int(maxBits))

		if gotBits != wantBits {
			t.Fatalf("iteration %d: tree construction needs %d bits, length-limited construction needs %d", iter, gotBits, wantBits)
		}

		// generate must produce the same code as the tree construction.
		h.generate(freq, maxBits)
		if !slices.Equal(h.codes, tree.codes) {
			t.Fatalf("iteration %d: generate produced a different code than treeBitCounts", iter)
		}
	}
	if treeUsed == 0 {
		t.Fatal("tree construction was never within the limit; nothing was tested")
	}
}

// TestHuffmanGenerateLimit checks that frequency distributions whose
// unrestricted Huffman tree exceeds the maximum code length still
// produce a valid length-limited code.
func TestHuffmanGenerateLimit(t *testing.T) {
	for _, maxBits := range []int32{7, 15} {
		// Fibonacci frequencies produce a maximally unbalanced tree,
		// one level deeper per symbol.
		for used := 3; used <= 25; used++ {
			freq := make([]uint16, literalCount)
			a, b := 1, 1
			for i := range used {
				freq[i*7] = uint16(a)
				a, b = b, min(a+b, 65535)
			}
			h := newHuffmanEncoder(literalCount)
			h.generate(freq, maxBits)
			checkCode(t, freq, h.codes, int(maxBits))
		}
	}
}
