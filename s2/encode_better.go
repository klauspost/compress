// Copyright 2016 The Snappy-Go Authors. All rights reserved.
// Copyright (c) 2019 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

// hash4 returns the hash of the lowest 4 bytes of u to fit in a hash table with h bits.
// Preferably h should be a constant and should always be <32.
func hash4(u uint64, h uint8) uint32 {
	const prime4bytes = 2654435761
	return (uint32(u) * prime4bytes) >> ((32 - h) & 31)
}

// hash8 returns the hash of u to fit in a hash table with h bits.
// Preferably h should be a constant and should always be <64.
func hash8(u uint64, h uint8) uint32 {
	const prime8bytes = 0xcf1bbcdcb7a56463
	return uint32((u * prime8bytes) >> ((64 - h) & 63))
}
