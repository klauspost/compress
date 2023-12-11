// Copyright (c) 2019 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

func TestEncodeHuge(t *testing.T) {
	if true {
		t.Skip("Takes too much memory")
	}
	test := func(t *testing.T, data []byte) {
		comp := Encode(make([]byte, MaxEncodedLen(len(data))), data)
		decoded, err := Decode(nil, comp)
		if err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(data, decoded) {
			t.Error("block decoder mismatch")
			return
		}
		if mel := MaxEncodedLen(len(data)); len(comp) > mel {
			t.Error(fmt.Errorf("MaxEncodedLen Exceed: input: %d, mel: %d, got %d", len(data), mel, len(comp)))
			return
		}
		comp = EncodeBetter(make([]byte, MaxEncodedLen(len(data))), data)
		decoded, err = Decode(nil, comp)
		if err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(data, decoded) {
			t.Error("block decoder mismatch")
			return
		}
		if mel := MaxEncodedLen(len(data)); len(comp) > mel {
			t.Error(fmt.Errorf("MaxEncodedLen Exceed: input: %d, mel: %d, got %d", len(data), mel, len(comp)))
			return
		}

		comp = EncodeBest(make([]byte, MaxEncodedLen(len(data))), data)
		decoded, err = Decode(nil, comp)
		if err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(data, decoded) {
			t.Error("block decoder mismatch")
			return
		}
		if mel := MaxEncodedLen(len(data)); len(comp) > mel {
			t.Error(fmt.Errorf("MaxEncodedLen Exceed: input: %d, mel: %d, got %d", len(data), mel, len(comp)))
			return
		}
	}
	test(t, make([]byte, math.MaxInt32))
	if math.MaxInt > math.MaxInt32 {
		x := int64(math.MaxInt32 + math.MaxUint16)
		test(t, make([]byte, x))
	}
	test(t, make([]byte, MaxBlockSize))
}

func TestSizes(t *testing.T) {
	var src [2]byte
	src[0] = 123
	src[1] = 57
	s := 2

	want := int(uint32(src[s-2])&0xe0<<3 | uint32(src[s-1]))
	//got := bits.RotateLeft16(binary.LittleEndian.Uint16(src[:]), 16-5) & 2047
	got := binary.LittleEndian.Uint16(src[:])
	t.Logf("w:%012b G:%016b", want, got)
	for i := 4; i < 100; i++ {
		if i == 99 {
			i = (1 << 24) - 1
		}
		t.Logf("%d: short:%d medium: %d long: %d repeat: %d", i, emitCopySize(10, i), emitCopySize(4000, i), emitCopySize(70000, i), emitRepeatSize(0, i))
	}
}
