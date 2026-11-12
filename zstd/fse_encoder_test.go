package zstd

import (
	"math/rand"
	"testing"
)

func TestFSEMinNewSize(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := range 20000 {
		var enc fseEncoder
		h := enc.Histogram()
		symbols := 2 + rng.Intn(maxMatchLengthSymbol)
		nonZero := 2 + rng.Intn(symbols-1)
		n := nonZero + rng.Intn([]int{8, 64, 1024, 8192}[i%4])
		var maxCount, total int
		for range n {
			s := rng.Intn(symbols)
			if rng.Intn(4) == 0 {
				// Skew towards few symbols.
				s %= nonZero
			}
			if s%(symbols/nonZero+1) != 0 {
				s -= s % (symbols/nonZero + 1)
			}
			h[s]++
			maxCount = max(maxCount, int(h[s]))
			total++
		}
		h[symbols-1]++
		total++
		maxCount = max(maxCount, int(h[symbols-1]))
		if maxCount == total {
			continue
		}
		enc.HistogramFinished(uint8(symbols-1), maxCount)
		hist := enc.count[:enc.symbolLen]
		lb := minNewSize(hist, total)
		if err := enc.normalizeCount(total); err != nil {
			t.Fatal(err)
		}
		hdr, err := enc.writeCount(nil)
		if err != nil {
			t.Fatal(err)
		}
		size := enc.approxSize(hist) + uint32(len(hdr))*8 + uint32(enc.actualTableLog)
		if lb > size {
			t.Fatalf("lower bound %d > size %d (approx %d, header %d bytes), hist %v", lb, size, enc.approxSize(hist), len(hdr), hist)
		}
	}
}
