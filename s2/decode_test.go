// Copyright (c) 2019 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
)

func TestDecodeRegression(t *testing.T) {
	data, err := os.ReadFile("testdata/dec-block-regressions.zip")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range zr.File {
		if !strings.HasSuffix(t.Name(), "") {
			continue
		}
		t.Run(tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			in, err := io.ReadAll(r)
			if err != nil {
				t.Error(err)
			}
			got, err := Decode(nil, in)
			t.Log("Received:", len(got), err)
		})
	}
}

func TestDecoderMaxBlockSize(t *testing.T) {
	data, err := os.ReadFile("testdata/enc_regressions.zip")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	sizes := []int{4 << 10, 10 << 10, 1 << 20, 4 << 20}
	test := func(t *testing.T, data []byte) {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%d", size), func(t *testing.T) {
				var buf bytes.Buffer
				dec := NewReader(nil, ReaderMaxBlockSize(size), ReaderAllocBlock(size/2))
				enc := NewWriter(&buf, WriterBlockSize(size), WriterPadding(16<<10), WriterPaddingSrc(zeroReader{}))

				// Test writer.
				n, err := enc.Write(data)
				if err != nil {
					t.Error(err)
					return
				}
				if n != len(data) {
					t.Error(fmt.Errorf("Write: Short write, want %d, got %d", len(data), n))
					return
				}
				err = enc.Close()
				if err != nil {
					t.Error(err)
					return
				}
				// Calling close twice should not affect anything.
				err = enc.Close()
				if err != nil {
					t.Error(err)
					return
				}

				dec.Reset(&buf)
				got, err := io.ReadAll(dec)
				if err != nil {
					t.Error(err)
					return
				}
				if !bytes.Equal(data, got) {
					t.Error("block (reset) decoder mismatch")
					return
				}

				// Test Reset on both and use ReadFrom instead.
				buf.Reset()
				enc.Reset(&buf)
				n2, err := enc.ReadFrom(bytes.NewBuffer(data))
				if err != nil {
					t.Error(err)
					return
				}
				if n2 != int64(len(data)) {
					t.Error(fmt.Errorf("ReadFrom: Short read, want %d, got %d", len(data), n2))
					return
				}
				// Encode twice...
				n2, err = enc.ReadFrom(bytes.NewBuffer(data))
				if err != nil {
					t.Error(err)
					return
				}
				if n2 != int64(len(data)) {
					t.Error(fmt.Errorf("ReadFrom: Short read, want %d, got %d", len(data), n2))
					return
				}

				err = enc.Close()
				if err != nil {
					t.Error(err)
					return
				}
				if enc.pad > 0 && buf.Len()%enc.pad != 0 {
					t.Error(fmt.Errorf("wanted size to be mutiple of %d, got size %d with remainder %d", enc.pad, buf.Len(), buf.Len()%enc.pad))
					return
				}
				encoded := buf.Bytes()
				dec.Reset(&buf)
				// Skip first...
				dec.Skip(int64(len(data)))
				got, err = io.ReadAll(dec)
				if err != nil {
					t.Error(err)
					return
				}
				if !bytes.Equal(data, got) {
					t.Error("frame (reset) decoder mismatch")
					return
				}
				// Re-add data, Read concurrent.
				buf.Write(encoded)
				dec.Reset(&buf)
				var doubleB bytes.Buffer
				nb, err := dec.DecodeConcurrent(&doubleB, runtime.GOMAXPROCS(0))
				if err != nil {
					t.Error(err)
					return
				}
				if nb != int64(len(data)*2) {
					t.Errorf("want %d, got %d, err: %v", len(data)*2, nb, err)
					return
				}
				got = doubleB.Bytes()[:len(data)]
				if !bytes.Equal(data, got) {
					t.Error("frame (DecodeConcurrent) decoder mismatch")
					return
				}
				got = doubleB.Bytes()[len(data):]
				if !bytes.Equal(data, got) {
					t.Error("frame (DecodeConcurrent) decoder mismatch")
					return
				}
			})
		}
	}
	for _, tt := range zr.File {
		if !strings.HasSuffix(t.Name(), "") {
			continue
		}
		t.Run(tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			b, err := io.ReadAll(r)
			if err != nil {
				t.Error(err)
				return
			}
			test(t, b[:len(b):len(b)])
		})
	}
}

func TestEncodeSize(t *testing.T) {
	for _, in := range []string{"sofia.gob", "ranks.gob", "mint.gob", "enwik9.gob", "github.gob", "nyc.gob", "consensus.gob"} {
		in := in
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			f, err := os.Open(in)
			if err != nil {
				t.Skip(err)
			}
			defer f.Close()
			dec := gob.NewDecoder(bufio.NewReaderSize(f, 1<<20))
			var s2s s2sizer
			var ng ngSizer
			var ngBrute ngSizer
			for {
				var e entry
				if err := dec.Decode(&e); err != nil {
					if err != io.EOF {
						t.Fatal(err)
					}
					break
				}
				s2s.add(e)
				ng.add(e)
				ngBrute.addBrute(e)
			}
			t.Log(strings.ToUpper(strings.TrimSuffix(in, ".gob")) + ":")
			t.Log("S2 Size:", s2s.sz)
			t.Log("NG Size:", ng.sz, "- Brute Force:", ngBrute.sz)
			delta := s2s.sz - ng.sz
			deltaBrute := s2s.sz - ngBrute.sz
			t.Logf("Gain: %d bytes (%0.2f%%; brute: %0.2f%%; %0.2f%% ex lits)", delta, float64(100*delta)/float64(s2s.sz), float64(100*deltaBrute)/float64(s2s.sz), float64(100*delta)/float64(s2s.sz-s2s.lits))
		})
	}
}

type s2sizer struct {
	lastoffset uint
	sz         int64
	lits       int64
}

func (s *s2sizer) add(e entry) {
	if e.LitLen > 0 {
		s.sz += literalExtraSize(int64(e.LitLen)) + int64(e.LitLen)
		s.lits += int64(e.LitLen)
	}
	if e.MatchLen > 0 {
		if e.Offset == s.lastoffset {
			s.sz += int64(emitRepeatSize(int(e.Offset), int(e.MatchLen)))
		} else {
			s.sz += int64(emitCopySize(int(e.Offset), int(e.MatchLen)))
			s.lastoffset = e.Offset
		}
	}
}

type ngSizer struct {
	// this will assume lastoffset is 1
	lastoffset uint
	sz         int64
	lits       int64
}

const shortOffsetBits = 9
const shortOffset = 1 << shortOffsetBits
const mediumOffset = shortOffset + (1 << 17)
const maxOffset = mediumOffset + (1 << 22)
const copy3MinMatchLen = 4

const litCopyMaxOffset = 1 << 16

func (s *ngSizer) add(e entry) {
	if e.MatchLen == 0 {
		s.emitLits(e.LitLen)
		return
	}

	// Always at least just as good as the alternatives.
	delta := int(e.Offset) - int(s.lastoffset)
	if delta >= math.MinInt8 && delta <= math.MaxInt8 {
		s.emitLits(e.LitLen)
		s.emitRepeat(e.Offset, e.MatchLen)
		return
	}
	bigOffset := e.Offset > shortOffset
	canRepeat := delta >= math.MinInt16 && delta <= math.MaxInt16 && bigOffset

	// If no literals, we don't have to consider the combination
	if e.LitLen == 0 {
		if canRepeat {
			s.emitRepeat(e.Offset, e.MatchLen)
			return
		}
		s.emitCopy(e)
		return
	}

	// Add combined if possible
	if bigOffset && e.Offset <= litCopyMaxOffset && (e.MatchLen <= 11 || e.LitLen < 7) && e.LitLen < 2<<20 {
		s.emitLitCopy(e)
		return
	}

	// Emit lits separately
	s.emitLits(e.LitLen)

	// Repeat if it makes sense.
	if canRepeat {
		s.emitRepeat(e.Offset, e.MatchLen)
		return
	}
	s.emitCopy(e)
}

// addBrute will try all possible combinations.
// Checks if we are missing obvious criteria.
func (s *ngSizer) addBrute(e entry) {
	if e.MatchLen == 0 {
		s.emitLits(e.LitLen)
		return
	}
	minSz := math.MaxInt
	try := func(n int) {
		if n < minSz {
			minSz = n
		}
	}
	defer func() {
		s.sz += int64(minSz)
		if e.MatchLen > 0 {
			s.lastoffset = e.Offset
		}
	}()

	//Always at least just as good as the alternatives.
	delta := int(e.Offset) - int(s.lastoffset)
	canRepeat := delta >= math.MinInt16 && delta <= math.MaxInt16
	if e.LitLen == 0 {
		if canRepeat {
			try(s.emitRepeatS(s.lastoffset, e.Offset, e.MatchLen))
		}
		try(s.emitCopyS(e))
		return
	}

	if canRepeat {
		try(s.emitLitsS(e.LitLen) + s.emitRepeatS(s.lastoffset, e.Offset, e.MatchLen))
	}

	// Emit lits separately
	try(s.emitCopyS(e) + s.emitLitsS(e.LitLen))

	if e.Offset <= litCopyMaxOffset && e.LitLen < 2<<20 {
		got := s.emitLitCopyS(e)
		if false && e.MatchLen > 11 && e.LitLen > 7 && got < minSz {
			fmt.Printf("better (%d<%d) with: %+v\n", got, minSz, e)
		}
		if got < minSz {
			minSz = got
		}
	}
}

const base0 = 60
const base1 = base0 + (1 << 8)
const base2 = base1 + (1 << 16)
const base3 = base2 + (1 << 24)

func (s *ngSizer) emitLits(n uint) {
	s.sz += int64(n)
	s.lits += int64(n)
	for n > 0 {
		n2 := n
		if n2 > base3 {
			n2 = base3
		}
		s.addValue(n2)
		n -= n2
	}
}

func (s *ngSizer) emitRepeat(offset, length uint) {
	delta := int(offset) - int(s.lastoffset)
	s.lastoffset = offset
	switch {
	case delta == 0:
		v := (length - 1) << 2
		s.addValue(v)
	case delta == 1:
		v := (length - 2) << 2
		v |= 1
		s.addValue(v)
	case delta >= math.MinInt8 && delta <= math.MaxInt8:
		v := (length - 3) << 2
		v |= 2
		s.addValue(v)
		s.sz += 1
	case delta >= math.MinInt16 && delta <= math.MaxInt16:
		v := (length - 4) << 2
		v |= 3
		s.addValue(v)
		s.sz += 2
	default:
		panic(delta)
	}
}

func (s *ngSizer) emitLitCopy(e entry) {
	v := e.MatchLen - 4
	if v >= 8 {
		v = 7
	}
	remain := e.MatchLen - 4 - v
	v |= (e.LitLen - 1) << 3
	if e.Offset > litCopyMaxOffset {
		panic(e.Offset)
	}
	s.lastoffset = e.Offset
	s.addValue(v) // 23989940
	s.sz += 2 + int64(e.LitLen)
	s.lits += int64(e.LitLen)
	if remain > 0 {
		s.emitRepeat(e.Offset, remain)
	}
}

func (s *ngSizer) emitCopy(e entry) {
	v := uint(1)
	s.lastoffset = e.Offset
	if e.Offset <= shortOffset {
		v |= (e.Offset & 256) >> 7
		v |= (e.MatchLen - 4) << 2
		s.addValue(v)
		s.sz += 1
		return
	}
	v = 0
	if e.Offset <= mediumOffset {
		offset := e.Offset - shortOffset
		v |= ((offset) >> 16) << 1 // 1 bit, remaining 16 are saved
		v |= (e.MatchLen - 4) << 3
		s.addValue(v)
		s.sz += 2
		return
	}
	if e.Offset <= maxOffset {
		if e.MatchLen < copy3MinMatchLen {
			s.emitLits(e.MatchLen)
			return
		}
		// offset := e.Offset - mediumOffset
		v |= 1 << 1
		// Lowest 2 length bits stored in output, store remaining in value.
		v |= (e.MatchLen - copy3MinMatchLen) & (math.MaxUint - 3)
		s.addValue(v)
		s.sz += 3
		return
	}
	panic(e)
}

func (s *ngSizer) addValue(v uint) {
	switch {
	case v <= base0:
		s.sz++
	case v <= base1:
		s.sz += 2
	case v <= base2:
		s.sz += 3
	case v <= base3:
		s.sz += 4
	default:
		panic(v)
	}
}

func (s *ngSizer) addValueS(v uint) int {
	switch {
	case v <= base0:
		return 1
	case v <= base1:
		return 2
	case v <= base2:
		return 3
	case v <= base3:
		return 4
	default:
		panic(v)
	}
}

func (s *ngSizer) emitLitsS(n uint) int {
	sz := int(n)
	for n > 0 {
		n2 := n
		if n2 > base3 {
			n2 = base3
		}
		sz += s.addValueS(n2)
		n -= n2
	}
	return sz
}

func (s *ngSizer) emitRepeatS(lastoffset, offset, length uint) int {
	delta := int(offset) - int(lastoffset)
	s.lastoffset = offset
	switch {
	case delta == 0:
		v := (length - 1) << 2
		return s.addValueS(v)
	case delta == 1:
		v := (length - 2) << 2
		v |= 1
		return s.addValueS(v)
	case delta >= math.MinInt8 && delta <= math.MaxInt8:
		v := (length - 3) << 2
		v |= 2
		return s.addValueS(v) + 1
	case delta >= math.MinInt16 && delta <= math.MaxInt16:
		v := (length - 4) << 2
		v |= 3
		return s.addValueS(v) + 2
	default:
		panic(delta)
	}
}

func (s *ngSizer) emitLitCopyS(e entry) int {
	v := e.MatchLen - 4
	if v >= 8 {
		v = 7
	}
	remain := e.MatchLen - 4 - v
	v |= (e.LitLen - 1) << 3
	if e.Offset > litCopyMaxOffset {
		panic(e.Offset)
	}
	sz := s.addValueS(v) // 23989940
	sz += 2 + int(e.LitLen)
	if remain > 0 {
		return sz + s.emitRepeatS(e.Offset, e.Offset, remain)
	}
	return sz
}

func (s *ngSizer) emitCopyS(e entry) int {
	v := uint(1)
	// 1 bit indicating short
	if e.Offset <= shortOffset {
		// 1 bit from offset
		v |= (e.Offset & 256) >> 7
		// remaining from matchlength
		v |= (e.MatchLen - 4) << 2
		return s.addValueS(v) + 1
	}
	v = 0
	if e.Offset <= mediumOffset {
		// 2 bits from offset
		offset := e.Offset - shortOffset
		// bit 1 is 0 (short)
		v |= ((offset) >> 16) << 2 // 1 bit, remaining 16 are saved
		// remaining from matchlength
		v |= (e.MatchLen - 4) << 3
		return s.addValueS(v) + 2
	}
	if e.Offset <= maxOffset {
		if e.MatchLen < copy3MinMatchLen {
			return s.emitLitsS(e.MatchLen)
		}
		//offset := e.Offset - mediumOffset
		v |= 1 << 1
		// Lowest 2 length bits stored in output, store remaining in value.
		v |= (e.MatchLen - copy3MinMatchLen) & (math.MaxUint - 3)
		return s.addValueS(v) + 3
	}
	panic(e)
}
