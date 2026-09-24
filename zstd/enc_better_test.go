// Copyright 2026+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

package zstd

import (
	"math/rand"
	"testing"
)

func TestBetterHashMatchesHashLen(t *testing.T) {
	inputs := []uint64{0, 1, 0xff, 0xffffffffff, 1 << 40, 1<<64 - 1, 0x0123456789abcdef}
	rng := rand.New(rand.NewSource(1))
	for range 1000 {
		inputs = append(inputs, rng.Uint64())
	}
	primeL, primeS := betterHashPrimes()
	tests := []struct {
		name string
		got  func(u uint64) uint32
		want func(u uint64) uint32
	}{
		{
			name: "long",
			got:  func(u uint64) uint32 { return betterHashL(u, primeL) },
			want: func(u uint64) uint32 { return hashLen(u, betterLongTableBits, betterLongLen) },
		},
		{
			name: "short",
			got:  func(u uint64) uint32 { return betterHashS(u, primeS) },
			want: func(u uint64) uint32 { return hashLen(u, betterShortTableBits, betterShortLen) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, u := range inputs {
				if got, want := tt.got(u), tt.want(u); got != want {
					t.Fatalf("u=%#x: got %#x, want %#x", u, got, want)
				}
			}
		})
	}
}
