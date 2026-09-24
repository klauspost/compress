// Copyright 2026+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

package zstd

// hashPrimesInRegs reports whether encoders should keep hash primes in
// registers: arm64 has registers to spare, and builds a 64-bit prime with
// up to 4 instructions per use otherwise. See betterHashPrimes.
const hashPrimesInRegs = true
