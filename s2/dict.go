// Copyright (c) 2022+ Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"encoding/binary"
	"sync"
)

type Dict struct {
	dict   []byte
	repeat int // Repeat as index of dict

	fast, better, best sync.Once
	fastTable          *[1 << 14]uint32

	betterTableShort *[1 << 14]uint32
	betterTableLong  *[1 << 17]uint32

	bestTableShort *[1 << 19]uint32
	bestTableLong  *[1 << 16]uint32
}

func NewDict(dict []byte) *Dict {
	var d Dict
	// Repeat is the first value of the dict
	r, n := binary.Uvarint(dict)
	dict = dict[n:]
	d.dict = dict
	if len(dict) == 0 {
		return nil
	}
	d.repeat = int(r)
	if d.repeat >= len(dict) {
		d.repeat = 0
	}
	return &d
}

func (d *Dict) initFast() {
	d.fast.Do(func() {
		const (
			tableBits    = 14
			maxTableSize = 1 << tableBits
			debug        = false
		)

		var table [maxTableSize]uint32

		for i := 0; i < len(d.dict)-8; i += 3 {
			x0 := load64(d.dict, i)
			h0 := hash6(x0, tableBits)
			h1 := hash6(x0>>8, tableBits)
			h2 := hash6(x0>>16, tableBits)
			table[h0] = uint32(i)
			table[h1] = uint32(i + 1)
			table[h2] = uint32(i + 2)
		}
		d.fastTable = &table
	})
}
