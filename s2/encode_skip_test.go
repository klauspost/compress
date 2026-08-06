//go:build amd64 && !appengine && !noasm && gc

package s2

import (
	"bytes"
	"math/rand"
	"testing"
)

// mixedRunsForSkip builds total bytes of alternating incompressible and
// compressible runs of runLen each.
//
// Long runs are the point. The adaptive skip only grows past the clamp after a
// long stretch with no matches, so shapes with short noise runs -- and uniform
// shapes of either kind -- cannot distinguish a clamped encoder from an
// unclamped one. On uniform text the two agree either way.
func mixedRunsForSkip(rng *rand.Rand, total, runLen int) []byte {
	words := []string{"lorem", "ipsum", "dolor", "sit", "amet", "consectetur"}
	out := make([]byte, 0, total+2*runLen)
	for len(out) < total {
		noise := make([]byte, runLen)
		rng.Read(noise)
		out = append(out, noise...)
		txt := make([]byte, 0, runLen+16)
		for len(txt) < runLen {
			txt = append(txt, words[rng.Intn(len(words))]...)
			txt = append(txt, ' ')
		}
		out = append(out, txt[:runLen]...)
	}
	return out[:total]
}

// TestBetterGoSkipMatchesAsm requires encodeBlockBetterGo to produce exactly
// what the assembly produces on blocks above 64KB.
//
// Both clamp the adaptive scan skip at 100. Before the Go side did, it emitted
// 0.10%-0.25% more bytes on these shapes, because after a long matchless
// stretch its unclamped skip strode past the start of the next compressible
// region and lost the matches there.
//
// Byte equality is a deliberately strong assertion: it is what the clamp buys,
// and it is currently true, so anything that breaks it is a divergence worth
// looking at rather than noise. Only the >64KB path is covered --
// encodeBlockBetterGo64K is a different configuration (skipLog 6 rather than 7)
// and does not clamp.
func TestBetterGoSkipMatchesAsm(t *testing.T) {
	for _, runLen := range []int{16 << 10, 64 << 10, 256 << 10, 1 << 20} {
		src := mixedRunsForSkip(rand.New(rand.NewSource(1)), 4<<20, runLen)
		if len(src) <= 64<<10 {
			t.Fatalf("block must exceed 64KB to reach encodeBlockBetterGo")
		}
		asm := make([]byte, MaxEncodedLen(len(src)))
		goOut := make([]byte, MaxEncodedLen(len(src)))
		nAsm := encodeBlockBetter(asm, src)
		nGo := encodeBlockBetterGo(goOut, src)
		if nGo != nAsm || !bytes.Equal(goOut[:nGo], asm[:nAsm]) {
			t.Errorf("noise runs of %d bytes: Go emitted %d bytes, assembly %d (%+.3f%%) -- is the skip clamp still in encodeBlockBetterGo?",
				runLen, nGo, nAsm, 100*float64(nGo-nAsm)/float64(nAsm))
		}
	}
}
