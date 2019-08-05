// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flate

import "math/bits"

func reverseBits(number uint16, bitLength byte) uint16 {
	return bits.Reverse16(number << ((16 - bitLength) & 15))
}
