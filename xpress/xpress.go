/*
Decompression support for the XPRESS (MS-XCA) compression algorithm.

Reference:
https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-xca/
(2.1 XPRESS Algorithm Details, 2.2 LZ77+Huffman Algorithm Details)

The LZ77+Huffman variant is used for WIM images and for NTFS files
compressed with the Windows Overlay Filter (Compact OS / system
compression) on Windows 8 and later. The plain LZ77 variant is the
COMPRESSION_FORMAT_XPRESS format of the Windows compression API
(RtlCompressBuffer), used e.g. for hibernation files.

Plain LZ77:
The stream is a sequence of 32-bit flag groups, each flag tested from
bit 31 down. A clear bit is a literal byte. A set bit is a match: an
LE16 word with (offset-1) in the top 13 bits and (length-3) in the low
3 bits. A length of 7 selects the shared-nibble form: the low nibble of
the next stream byte supplies the real length for this match, and the
high nibble of that byte supplies the length of the next consecutive
match that also uses this form. A nibble of 15 selects a raw length: a
byte >= 22, or if that byte is 255, an LE16, or if that is 0, an LE32.
The final flag group is padded with set bits; a match flag with no
input left is the end-of-data marker.

LZ77+Huffman:
The first 256 bytes hold 512 4-bit code lengths (even symbol in the low
nibble, odd in the high). Canonical codes are assigned in (length,
symbol) order, most-significant bit first. The bit stream follows as
LE16 words, MSB first, read through a 32-bit register that is refilled
while fewer than 15 bits remain. Symbols 0..255 are literals. Symbol
256 is end-of-data; mid-stream it decodes as a match of length 3 at
distance 1 (Microsoft behavior). Symbols 257..511 are matches: the low
nibble is (length-3), a value of 15 selecting a raw extension byte
(length = byte + 18, or if the byte is 255, an LE16/LE32 value plus 3),
and the high nibble is the position of the highest set bit of the
offset, whose remaining low bits follow in the stream. The uncompressed
size must be known in advance because end-of-data is only recognized
once the output is complete.

https://github.com/sleuthkit/sleuthkit (tsk/fs/xpress.c)
*/

package xpress

import (
	"encoding/binary"
	"errors"
)

// MaxSize is the maximum size a single stream may decompress to. Both
// variants enforce it so a crafted stream cannot request an excessive
// allocation or produce unbounded output.
const MaxSize = 32 << 20 // 32 MiB

var (
	errInvalidCode = errors.New("xpress: invalid Huffman code lengths")
	errCorrupt     = errors.New("xpress: corrupt stream")
	errTruncated   = errors.New("xpress: truncated stream")
	errTooLarge    = errors.New("xpress: decompressed data exceeds MaxSize")
)

const (
	// Depth of the canonical decode table (maximum code length is 15).
	xpressTableBits = 15

	// Size of the canonical decode table.
	xpressTableSize = 1 << xpressTableBits // 32768 entries

	// Number of Huffman symbols: 256 literals + 256 match symbols.
	xpressNumSymbols = 512
)

// AppendDecompressed appends the decompressed form of an XPRESS plain
// LZ77 stream to out and returns the extended slice. The stream is
// self-terminating, so no output size is required. On error, out is
// returned unmodified.
func AppendDecompressed(out, in []byte) ([]byte, error) {
	base := len(out)

	// Offset of the shared-nibble byte, or -1 when none is pending.
	// The pending half survives literals: it is consumed only by the
	// next match that uses the shared-nibble form.
	pending_len := -1

	flags := uint32(0)
	flags_left := 0
	i := 0

	for {
		if flags_left == 0 {
			if len(in)-i == 0 {
				return out, nil
			}
			if len(in)-i < 4 {
				return out[:base], errTruncated
			}
			flags = binary.LittleEndian.Uint32(in[i:])
			i += 4
			flags_left = 32
		}
		flags_left--
		if flags&(uint32(1)<<flags_left) == 0 {
			if i >= len(in) {
				return out[:base], errTruncated
			}
			out = append(out, in[i])
			i++
			if len(out)-base > MaxSize {
				return out[:base], errTooLarge
			}
			continue
		}
		// A set flag with no input left is the end-of-data marker.
		if i >= len(in) {
			return out, nil
		}
		if len(in)-i < 2 {
			return out[:base], errTruncated
		}
		mb := binary.LittleEndian.Uint16(in[i:])
		i += 2
		moff := int(mb>>3) + 1
		mlen := int(mb&7) + 3

		if mb&7 == 7 {
			var nib int
			if pending_len == -1 {
				if i >= len(in) {
					return out[:base], errTruncated
				}
				nib = int(in[i] & 0x0F)
				pending_len = i
				i++
			} else {
				nib = int(in[pending_len] >> 4)
				pending_len = -1
			}
			if nib == 15 {
				v := int64(0)
				if i >= len(in) {
					return out[:base], errTruncated
				}
				v = int64(in[i])
				i++
				if v == 255 {
					if len(in)-i < 2 {
						return out[:base], errTruncated
					}
					v = int64(binary.LittleEndian.Uint16(in[i:]))
					i += 2
					if v == 0 {
						if len(in)-i < 4 {
							return out[:base], errTruncated
						}
						v = int64(binary.LittleEndian.Uint32(in[i:]))
						i += 4
					}
					if v < 15+7 {
						return out[:base], errCorrupt
					}
					if v > MaxSize {
						return out[:base], errTooLarge
					}
					mlen = int(v) + 3
				} else {
					mlen = int(v) + 25
				}
			} else {
				mlen = nib + 10
			}
		}

		if mlen > MaxSize-(len(out)-base) {
			return out[:base], errTooLarge
		}
		// Matches may only reference output produced by this call, never
		// the caller-provided prefix.
		if moff > len(out)-base {
			return out[:base], errCorrupt
		}
		for j := 0; j < mlen; j++ {
			out = append(out, out[len(out)-moff])
		}
	}
}

// AppendHDecompressed appends decompressed_size bytes of an XPRESS
// LZ77+Huffman stream to out and returns the extended slice. The
// uncompressed size must be known in advance. On error, out is returned
// unmodified.
func AppendHDecompressed(out, in []byte, decompressed_size int) ([]byte, error) {
	base := len(out)
	if decompressed_size < 0 || decompressed_size > MaxSize {
		return out, errTooLarge
	}

	if len(in) < 256 {
		return out, errTruncated
	}

	// 512 4-bit code lengths, even symbol in the low nibble.
	lens := make([]uint16, xpressNumSymbols)
	for l := range 256 {
		lens[l*2] = uint16(in[l] & 0x0F)
		lens[l*2+1] = uint16(in[l] >> 4)
	}
	i := 256

	// Decode table filled in canonical (length, symbol) order.
	table := make([]uint16, xpressTableSize)
	entry := 0
	for l := 1; l <= xpressTableBits; l++ {
		for s := range xpressNumSymbols {
			if lens[s] == uint16(l) {
				for k := 1 << (xpressTableBits - l); k > 0; k-- {
					if entry >= xpressTableSize {
						// Oversubscribed codes: the table cannot hold
						// this many entries.
						return out, errInvalidCode
					}
					table[entry] = uint16(s)
					entry++
				}
			}
		}
	}
	if entry != xpressTableSize {
		// The code lengths must form a complete prefix code.
		return out, errInvalidCode
	}

	// Preload two LE16 words, most-significant bit first.
	bits := uint32(0)
	nbits := 0
	for nbits < 32 {
		if len(in)-i < 2 {
			return out, errTruncated
		}
		bits |= uint32(binary.LittleEndian.Uint16(in[i:])) << (16 - nbits)
		i += 2
		nbits += 16
	}

	for len(out)-base < decompressed_size {
		for nbits < 15 {
			if len(in)-i < 2 {
				return out[:base], errTruncated
			}
			bits |= uint32(binary.LittleEndian.Uint16(in[i:])) << (16 - nbits)
			i += 2
			nbits += 16
		}
		sym := int(table[(bits>>17)&0x7FFF])
		clen := int(lens[sym])
		bits <<= clen
		nbits -= clen

		if sym < 256 {
			out = append(out, byte(sym))
			continue
		}

		if sym == 256 {
			// End of data; Microsoft decodes it as match(3, 1)
			// mid-stream.
			if len(out)-base == decompressed_size {
				break
			}
			if len(out)-base == 0 || decompressed_size-(len(out)-base) < 3 {
				return out[:base], errCorrupt
			}
			start := len(out) - 1
			for j := range 3 {
				out = append(out, out[start+j])
			}
			continue
		}

		hb := (sym - 256) / 16
		mlen := (sym - 256) % 16
		if mlen == 15 {
			v := uint32(0)
			if i >= len(in) {
				return out[:base], errTruncated
			}
			v = uint32(in[i])
			i++
			if v == 255 {
				if len(in)-i < 2 {
					return out[:base], errTruncated
				}
				v = uint32(binary.LittleEndian.Uint16(in[i:]))
				i += 2
				if v == 0 {
					if len(in)-i < 4 {
						return out[:base], errTruncated
					}
					v = binary.LittleEndian.Uint32(in[i:])
					i += 4
				}
				if v > MaxSize {
					return out[:base], errTooLarge
				}
				mlen = int(v) + 3
			} else {
				mlen = int(v) + 18
			}
		} else {
			mlen += 3
		}

		for nbits < hb {
			if len(in)-i < 2 {
				return out[:base], errTruncated
			}
			bits |= uint32(binary.LittleEndian.Uint16(in[i:])) << (16 - nbits)
			i += 2
			nbits += 16
		}
		moff := 0
		if hb > 0 {
			moff = int((bits >> (32 - hb)) & (uint32(1)<<hb - 1))
			bits <<= hb
			nbits -= hb
		}
		moff += 1 << hb

		if moff > len(out)-base {
			return out[:base], errCorrupt
		}
		if len(out)-base > MaxSize-mlen {
			return out[:base], errTooLarge
		}
		if mlen > decompressed_size-(len(out)-base) {
			return out[:base], errCorrupt
		}
		start := len(out) - moff
		for j := 0; j < mlen; j++ {
			out = append(out, out[start+j])
		}
	}
	return out, nil
}
