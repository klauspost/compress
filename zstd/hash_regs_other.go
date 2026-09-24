// Copyright 2026+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

//go:build !arm64

package zstd

// hashPrimesInRegs reports whether encoders should keep hash primes in
// registers: amd64 builds a prime in one instruction, and holding them
// across the encode loops spills. See betterHashPrimes.
const hashPrimesInRegs = false
