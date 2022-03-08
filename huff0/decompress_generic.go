//go:build !amd64 || appengine || !gc || noasm
// +build !amd64 appengine !gc noasm

// This file contains a generic implementation of Decoder.Decompress4X.
package huff0

import (
	"errors"
	"fmt"
	"io"
)

// Decompress4X will decompress a 4X encoded stream.
// The length of the supplied input must match the end of a block exactly.
// The *capacity* of the dst slice must match the destination size of
// the uncompressed data exactly.
func (d *Decoder) Decompress4X(dst, src []byte) ([]byte, error) {
	if len(d.dt.single) == 0 {
		return nil, errors.New("no table loaded")
	}
	if len(src) < 6+(4*1) {
		return nil, errors.New("input too small")
	}
	if use8BitTables && d.actualTableLog <= 8 {
		return d.decompress4X8bit(dst, src)
	}

	var br [4]bitReaderShifted
	// Decode "jump table"
	start := 6
	for i := 0; i < 3; i++ {
		length := int(src[i*2]) | (int(src[i*2+1]) << 8)
		if start+length >= len(src) {
			return nil, errors.New("truncated input (or invalid offset)")
		}
		err := br[i].init(src[start : start+length])
		if err != nil {
			return nil, err
		}
		start += length
	}
	err := br[3].init(src[start:])
	if err != nil {
		return nil, err
	}

	// destination, offset to match first output
	dstSize := cap(dst)
	dst = dst[:dstSize]
	out := dst
	dstEvery := (dstSize + 3) / 4

	const tlSize = 1 << tableLogMax
	const tlMask = tlSize - 1
	single := d.dt.single[:tlSize]

	// Use temp table to avoid bound checks/append penalty.
	buf := d.buffer()
	var off uint8
	var decoded int

	// Decode 2 values from each decoder/loop.
	const bufoff = 256
	for {
		if br[0].off < 4 || br[1].off < 4 || br[2].off < 4 || br[3].off < 4 {
			break
		}

		{
			const stream = 0
			const stream2 = 1
			br[stream].fillFast()
			br[stream2].fillFast()

			val := br[stream].peekBitsFast(d.actualTableLog)
			val2 := br[stream2].peekBitsFast(d.actualTableLog)
			v := single[val&tlMask]
			v2 := single[val2&tlMask]
			br[stream].advance(uint8(v.entry))
			br[stream2].advance(uint8(v2.entry))
			buf[stream][off] = uint8(v.entry >> 8)
			buf[stream2][off] = uint8(v2.entry >> 8)

			val = br[stream].peekBitsFast(d.actualTableLog)
			val2 = br[stream2].peekBitsFast(d.actualTableLog)
			v = single[val&tlMask]
			v2 = single[val2&tlMask]
			br[stream].advance(uint8(v.entry))
			br[stream2].advance(uint8(v2.entry))
			buf[stream][off+1] = uint8(v.entry >> 8)
			buf[stream2][off+1] = uint8(v2.entry >> 8)
		}

		{
			const stream = 2
			const stream2 = 3
			br[stream].fillFast()
			br[stream2].fillFast()

			val := br[stream].peekBitsFast(d.actualTableLog)
			val2 := br[stream2].peekBitsFast(d.actualTableLog)
			v := single[val&tlMask]
			v2 := single[val2&tlMask]
			br[stream].advance(uint8(v.entry))
			br[stream2].advance(uint8(v2.entry))
			buf[stream][off] = uint8(v.entry >> 8)
			buf[stream2][off] = uint8(v2.entry >> 8)

			val = br[stream].peekBitsFast(d.actualTableLog)
			val2 = br[stream2].peekBitsFast(d.actualTableLog)
			v = single[val&tlMask]
			v2 = single[val2&tlMask]
			br[stream].advance(uint8(v.entry))
			br[stream2].advance(uint8(v2.entry))
			buf[stream][off+1] = uint8(v.entry >> 8)
			buf[stream2][off+1] = uint8(v2.entry >> 8)
		}

		off += 2

		if off == 0 {
			if bufoff > dstEvery {
				d.bufs.Put(buf)
				return nil, errors.New("corruption detected: stream overrun 1")
			}
			copy(out, buf[0][:])
			copy(out[dstEvery:], buf[1][:])
			copy(out[dstEvery*2:], buf[2][:])
			copy(out[dstEvery*3:], buf[3][:])
			out = out[bufoff:]
			decoded += bufoff * 4
			// There must at least be 3 buffers left.
			if len(out) < dstEvery*3 {
				d.bufs.Put(buf)
				return nil, errors.New("corruption detected: stream overrun 2")
			}
		}
	}
	if off > 0 {
		ioff := int(off)
		if len(out) < dstEvery*3+ioff {
			d.bufs.Put(buf)
			return nil, errors.New("corruption detected: stream overrun 3")
		}
		copy(out, buf[0][:off])
		copy(out[dstEvery:], buf[1][:off])
		copy(out[dstEvery*2:], buf[2][:off])
		copy(out[dstEvery*3:], buf[3][:off])
		decoded += int(off) * 4
		out = out[off:]
	}

	// Decode remaining.
	remainBytes := dstEvery - (decoded / 4)
	for i := range br {
		offset := dstEvery * i
		endsAt := offset + remainBytes
		if endsAt > len(out) {
			endsAt = len(out)
		}
		br := &br[i]
		bitsLeft := br.remaining()
		for bitsLeft > 0 {
			br.fill()
			if offset >= endsAt {
				d.bufs.Put(buf)
				return nil, errors.New("corruption detected: stream overrun 4")
			}

			// Read value and increment offset.
			val := br.peekBitsFast(d.actualTableLog)
			v := single[val&tlMask].entry
			nBits := uint8(v)
			br.advance(nBits)
			bitsLeft -= uint(nBits)
			out[offset] = uint8(v >> 8)
			offset++
		}
		if offset != endsAt {
			d.bufs.Put(buf)
			return nil, fmt.Errorf("corruption detected: short output block %d, end %d != %d", i, offset, endsAt)
		}
		decoded += offset - dstEvery*i
		err = br.close()
		if err != nil {
			return nil, err
		}
	}
	d.bufs.Put(buf)
	if dstSize != decoded {
		return nil, errors.New("corruption detected: short output block")
	}
	return dst, nil
}

// Decompress4X will decompress a 4X encoded stream.
// The length of the supplied input must match the end of a block exactly.
// The *capacity* of the dst slice must match the destination size of
// the uncompressed data exactly.
func (d *Decoder) decompress4X8bit(dst, src []byte) ([]byte, error) {
	if d.actualTableLog == 8 {
		return d.decompress4X8bitExactly(dst, src)
	}

	var br [4]bitReaderBytes
	start := 6
	for i := 0; i < 3; i++ {
		length := int(src[i*2]) | (int(src[i*2+1]) << 8)
		if start+length >= len(src) {
			return nil, errors.New("truncated input (or invalid offset)")
		}
		err := br[i].init(src[start : start+length])
		if err != nil {
			return nil, err
		}
		start += length
	}
	err := br[3].init(src[start:])
	if err != nil {
		return nil, err
	}

	// destination, offset to match first output
	dstSize := cap(dst)
	dst = dst[:dstSize]
	out := dst
	dstEvery := (dstSize + 3) / 4

	shift := (56 + (8 - d.actualTableLog)) & 63

	const tlSize = 1 << 8
	single := d.dt.single[:tlSize]

	// Use temp table to avoid bound checks/append penalty.
	buf := d.buffer()
	var off uint8
	var decoded int

	// Decode 4 values from each decoder/loop.
	const bufoff = 256
	for {
		if br[0].off < 4 || br[1].off < 4 || br[2].off < 4 || br[3].off < 4 {
			break
		}

		{
			// Interleave 2 decodes.
			const stream = 0
			const stream2 = 1
			br1 := &br[stream]
			br2 := &br[stream2]
			br1.fillFast()
			br2.fillFast()

			v := single[uint8(br1.value>>shift)].entry
			v2 := single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off] = uint8(v >> 8)
			buf[stream2][off] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+1] = uint8(v >> 8)
			buf[stream2][off+1] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+2] = uint8(v >> 8)
			buf[stream2][off+2] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+3] = uint8(v >> 8)
			buf[stream2][off+3] = uint8(v2 >> 8)
		}

		{
			const stream = 2
			const stream2 = 3
			br1 := &br[stream]
			br2 := &br[stream2]
			br1.fillFast()
			br2.fillFast()

			v := single[uint8(br1.value>>shift)].entry
			v2 := single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off] = uint8(v >> 8)
			buf[stream2][off] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+1] = uint8(v >> 8)
			buf[stream2][off+1] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+2] = uint8(v >> 8)
			buf[stream2][off+2] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+3] = uint8(v >> 8)
			buf[stream2][off+3] = uint8(v2 >> 8)
		}

		off += 4

		if off == 0 {
			if bufoff > dstEvery {
				d.bufs.Put(buf)
				return nil, errors.New("corruption detected: stream overrun 1")
			}
			copy(out, buf[0][:])
			copy(out[dstEvery:], buf[1][:])
			copy(out[dstEvery*2:], buf[2][:])
			copy(out[dstEvery*3:], buf[3][:])
			out = out[bufoff:]
			decoded += bufoff * 4
			// There must at least be 3 buffers left.
			if len(out) < dstEvery*3 {
				d.bufs.Put(buf)
				return nil, errors.New("corruption detected: stream overrun 2")
			}
		}
	}
	if off > 0 {
		ioff := int(off)
		if len(out) < dstEvery*3+ioff {
			d.bufs.Put(buf)
			return nil, errors.New("corruption detected: stream overrun 3")
		}
		copy(out, buf[0][:off])
		copy(out[dstEvery:], buf[1][:off])
		copy(out[dstEvery*2:], buf[2][:off])
		copy(out[dstEvery*3:], buf[3][:off])
		decoded += int(off) * 4
		out = out[off:]
	}

	// Decode remaining.
	// Decode remaining.
	remainBytes := dstEvery - (decoded / 4)
	for i := range br {
		offset := dstEvery * i
		endsAt := offset + remainBytes
		if endsAt > len(out) {
			endsAt = len(out)
		}
		br := &br[i]
		bitsLeft := br.remaining()
		for bitsLeft > 0 {
			if br.finished() {
				d.bufs.Put(buf)
				return nil, io.ErrUnexpectedEOF
			}
			if br.bitsRead >= 56 {
				if br.off >= 4 {
					v := br.in[br.off-4:]
					v = v[:4]
					low := (uint32(v[0])) | (uint32(v[1]) << 8) | (uint32(v[2]) << 16) | (uint32(v[3]) << 24)
					br.value |= uint64(low) << (br.bitsRead - 32)
					br.bitsRead -= 32
					br.off -= 4
				} else {
					for br.off > 0 {
						br.value |= uint64(br.in[br.off-1]) << (br.bitsRead - 8)
						br.bitsRead -= 8
						br.off--
					}
				}
			}
			// end inline...
			if offset >= endsAt {
				d.bufs.Put(buf)
				return nil, errors.New("corruption detected: stream overrun 4")
			}

			// Read value and increment offset.
			v := single[uint8(br.value>>shift)].entry
			nBits := uint8(v)
			br.advance(nBits)
			bitsLeft -= uint(nBits)
			out[offset] = uint8(v >> 8)
			offset++
		}
		if offset != endsAt {
			d.bufs.Put(buf)
			return nil, fmt.Errorf("corruption detected: short output block %d, end %d != %d", i, offset, endsAt)
		}
		decoded += offset - dstEvery*i
		err = br.close()
		if err != nil {
			d.bufs.Put(buf)
			return nil, err
		}
	}
	d.bufs.Put(buf)
	if dstSize != decoded {
		return nil, errors.New("corruption detected: short output block")
	}
	return dst, nil
}

// Decompress4X will decompress a 4X encoded stream.
// The length of the supplied input must match the end of a block exactly.
// The *capacity* of the dst slice must match the destination size of
// the uncompressed data exactly.
func (d *Decoder) decompress4X8bitExactly(dst, src []byte) ([]byte, error) {
	var br [4]bitReaderBytes
	start := 6
	for i := 0; i < 3; i++ {
		length := int(src[i*2]) | (int(src[i*2+1]) << 8)
		if start+length >= len(src) {
			return nil, errors.New("truncated input (or invalid offset)")
		}
		err := br[i].init(src[start : start+length])
		if err != nil {
			return nil, err
		}
		start += length
	}
	err := br[3].init(src[start:])
	if err != nil {
		return nil, err
	}

	// destination, offset to match first output
	dstSize := cap(dst)
	dst = dst[:dstSize]
	out := dst
	dstEvery := (dstSize + 3) / 4

	const shift = 56
	const tlSize = 1 << 8
	const tlMask = tlSize - 1
	single := d.dt.single[:tlSize]

	// Use temp table to avoid bound checks/append penalty.
	buf := d.buffer()
	var off uint8
	var decoded int

	// Decode 4 values from each decoder/loop.
	const bufoff = 256
	for {
		if br[0].off < 4 || br[1].off < 4 || br[2].off < 4 || br[3].off < 4 {
			break
		}

		{
			// Interleave 2 decodes.
			const stream = 0
			const stream2 = 1
			br1 := &br[stream]
			br2 := &br[stream2]
			br1.fillFast()
			br2.fillFast()

			v := single[uint8(br1.value>>shift)].entry
			v2 := single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off] = uint8(v >> 8)
			buf[stream2][off] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+1] = uint8(v >> 8)
			buf[stream2][off+1] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+2] = uint8(v >> 8)
			buf[stream2][off+2] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+3] = uint8(v >> 8)
			buf[stream2][off+3] = uint8(v2 >> 8)
		}

		{
			const stream = 2
			const stream2 = 3
			br1 := &br[stream]
			br2 := &br[stream2]
			br1.fillFast()
			br2.fillFast()

			v := single[uint8(br1.value>>shift)].entry
			v2 := single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off] = uint8(v >> 8)
			buf[stream2][off] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+1] = uint8(v >> 8)
			buf[stream2][off+1] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+2] = uint8(v >> 8)
			buf[stream2][off+2] = uint8(v2 >> 8)

			v = single[uint8(br1.value>>shift)].entry
			v2 = single[uint8(br2.value>>shift)].entry
			br1.bitsRead += uint8(v)
			br1.value <<= v & 63
			br2.bitsRead += uint8(v2)
			br2.value <<= v2 & 63
			buf[stream][off+3] = uint8(v >> 8)
			buf[stream2][off+3] = uint8(v2 >> 8)
		}

		off += 4

		if off == 0 {
			if bufoff > dstEvery {
				d.bufs.Put(buf)
				return nil, errors.New("corruption detected: stream overrun 1")
			}
			copy(out, buf[0][:])
			copy(out[dstEvery:], buf[1][:])
			copy(out[dstEvery*2:], buf[2][:])
			copy(out[dstEvery*3:], buf[3][:])
			out = out[bufoff:]
			decoded += bufoff * 4
			// There must at least be 3 buffers left.
			if len(out) < dstEvery*3 {
				d.bufs.Put(buf)
				return nil, errors.New("corruption detected: stream overrun 2")
			}
		}
	}
	if off > 0 {
		ioff := int(off)
		if len(out) < dstEvery*3+ioff {
			return nil, errors.New("corruption detected: stream overrun 3")
		}
		copy(out, buf[0][:off])
		copy(out[dstEvery:], buf[1][:off])
		copy(out[dstEvery*2:], buf[2][:off])
		copy(out[dstEvery*3:], buf[3][:off])
		decoded += int(off) * 4
		out = out[off:]
	}

	// Decode remaining.
	remainBytes := dstEvery - (decoded / 4)
	for i := range br {
		offset := dstEvery * i
		endsAt := offset + remainBytes
		if endsAt > len(out) {
			endsAt = len(out)
		}
		br := &br[i]
		bitsLeft := br.remaining()
		for bitsLeft > 0 {
			if br.finished() {
				d.bufs.Put(buf)
				return nil, io.ErrUnexpectedEOF
			}
			if br.bitsRead >= 56 {
				if br.off >= 4 {
					v := br.in[br.off-4:]
					v = v[:4]
					low := (uint32(v[0])) | (uint32(v[1]) << 8) | (uint32(v[2]) << 16) | (uint32(v[3]) << 24)
					br.value |= uint64(low) << (br.bitsRead - 32)
					br.bitsRead -= 32
					br.off -= 4
				} else {
					for br.off > 0 {
						br.value |= uint64(br.in[br.off-1]) << (br.bitsRead - 8)
						br.bitsRead -= 8
						br.off--
					}
				}
			}
			// end inline...
			if offset >= endsAt {
				d.bufs.Put(buf)
				return nil, errors.New("corruption detected: stream overrun 4")
			}

			// Read value and increment offset.
			v := single[br.peekByteFast()].entry
			nBits := uint8(v)
			br.advance(nBits)
			bitsLeft -= uint(nBits)
			out[offset] = uint8(v >> 8)
			offset++
		}
		if offset != endsAt {
			d.bufs.Put(buf)
			return nil, fmt.Errorf("corruption detected: short output block %d, end %d != %d", i, offset, endsAt)
		}

		decoded += offset - dstEvery*i
		err = br.close()
		if err != nil {
			d.bufs.Put(buf)
			return nil, err
		}
	}
	d.bufs.Put(buf)
	if dstSize != decoded {
		return nil, errors.New("corruption detected: short output block")
	}
	return dst, nil
}
