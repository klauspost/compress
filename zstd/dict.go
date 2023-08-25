package zstd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/klauspost/compress/huff0"
)

type dict struct {
	id uint32

	litEnc              *huff0.Scratch
	llDec, ofDec, mlDec sequenceDec
	offsets             [3]int
	content             []byte
}

const dictMagic = "\x37\xa4\x30\xec"

// Maximum dictionary size for the reference implementation (1.5.3) is 2 GiB.
const dictMaxLength = 1 << 31

// ID returns the dictionary id or 0 if d is nil.
func (d *dict) ID() uint32 {
	if d == nil {
		return 0
	}
	return d.id
}

// ContentSize returns the dictionary content size or 0 if d is nil.
func (d *dict) ContentSize() int {
	if d == nil {
		return 0
	}
	return len(d.content)
}

// Content returns the dictionary content.
func (d *dict) Content() []byte {
	if d == nil {
		return nil
	}
	return d.content
}

// Offsets returns the initial offsets.
func (d *dict) Offsets() [3]int {
	if d == nil {
		return [3]int{}
	}
	return d.offsets
}

// LitEncoder returns the literal encoder.
func (d *dict) LitEncoder() *huff0.Scratch {
	if d == nil {
		return nil
	}
	return d.litEnc
}

// Load a dictionary as described in
// https://github.com/facebook/zstd/blob/master/doc/zstd_compression_format.md#dictionary-format
func loadDict(b []byte) (*dict, error) {
	// Check static field size.
	if len(b) <= 8+(3*4) {
		return nil, io.ErrUnexpectedEOF
	}
	d := dict{
		llDec: sequenceDec{fse: &fseDecoder{}},
		ofDec: sequenceDec{fse: &fseDecoder{}},
		mlDec: sequenceDec{fse: &fseDecoder{}},
	}
	if string(b[:4]) != dictMagic {
		return nil, ErrMagicMismatch
	}
	d.id = binary.LittleEndian.Uint32(b[4:8])
	if d.id == 0 {
		return nil, errors.New("dictionaries cannot have ID 0")
	}

	// Read literal table
	var err error
	d.litEnc, b, err = huff0.ReadTable(b[8:], nil)
	if err != nil {
		return nil, fmt.Errorf("loading literal table: %w", err)
	}
	d.litEnc.Reuse = huff0.ReusePolicyMust

	br := byteReader{
		b:   b,
		off: 0,
	}
	readDec := func(i tableIndex, dec *fseDecoder) error {
		if err := dec.readNCount(&br, uint16(maxTableSymbol[i])); err != nil {
			return err
		}
		if br.overread() {
			return io.ErrUnexpectedEOF
		}
		err = dec.transform(symbolTableX[i])
		if err != nil {
			println("Transform table error:", err)
			return err
		}
		if debugDecoder || debugEncoder {
			println("Read table ok", "symbolLen:", dec.symbolLen)
		}
		// Set decoders as predefined so they aren't reused.
		dec.preDefined = true
		return nil
	}

	if err := readDec(tableOffsets, d.ofDec.fse); err != nil {
		return nil, err
	}
	if err := readDec(tableMatchLengths, d.mlDec.fse); err != nil {
		return nil, err
	}
	if err := readDec(tableLiteralLengths, d.llDec.fse); err != nil {
		return nil, err
	}
	if br.remain() < 12 {
		return nil, io.ErrUnexpectedEOF
	}

	d.offsets[0] = int(br.Uint32())
	br.advance(4)
	d.offsets[1] = int(br.Uint32())
	br.advance(4)
	d.offsets[2] = int(br.Uint32())
	br.advance(4)
	if d.offsets[0] <= 0 || d.offsets[1] <= 0 || d.offsets[2] <= 0 {
		return nil, errors.New("invalid offset in dictionary")
	}
	d.content = make([]byte, br.remain())
	copy(d.content, br.unread())
	if d.offsets[0] > len(d.content) || d.offsets[1] > len(d.content) || d.offsets[2] > len(d.content) {
		return nil, fmt.Errorf("initial offset bigger than dictionary content size %d, offsets: %v", len(d.content), d.offsets)
	}

	return &d, nil
}

// InspectDictionary loads a zstd dictionary and provides functions to inspect the content.
func InspectDictionary(b []byte) (interface {
	ID() uint32
	ContentSize() int
	Content() []byte
	Offsets() [3]int
	LitEncoder() *huff0.Scratch
}, error) {
	initPredefined()
	d, err := loadDict(b)
	return d, err
}

type BuildDictOptions struct {
	// Dictionary ID.
	ID uint32

	// Content to use to create dictionary tables.
	Contents [][]byte

	// History to use for all blocks.
	History []byte

	// Offsets to use.
	Offsets [3]int
}

func BuildDict(o BuildDictOptions) ([]byte, error) {
	initPredefined()
	hist := o.History
	contents := o.Contents
	const debug = false
	if int64(len(hist)) > dictMaxLength {
		return nil, fmt.Errorf("dictionary of size %d > %d", len(hist), int64(dictMaxLength))
	}
	if len(hist) < 8 {
		return nil, fmt.Errorf("dictionary of size %d < %d", len(hist), 8)
	}
	if len(contents) == 0 {
		return nil, errors.New("no content provided")
	}
	d := dict{
		id:      o.ID,
		litEnc:  nil,
		llDec:   sequenceDec{},
		ofDec:   sequenceDec{},
		mlDec:   sequenceDec{},
		offsets: o.Offsets,
		content: hist,
	}
	block := blockEnc{lowMem: false}
	block.init()
	enc := &bestFastEncoder{fastBase: fastBase{maxMatchOff: int32(maxMatchLen), bufferReset: math.MaxInt32 - int32(maxMatchLen*2), lowMem: false}}
	var (
		remain [256]int
		ll     [256]int
		ml     [256]int
		of     [256]int
	)
	addValues := func(dst *[256]int, src []byte) {
		for _, v := range src {
			dst[v]++
		}
	}
	addHist := func(dst *[256]int, src *[256]uint32) {
		for i, v := range src {
			dst[i] += int(v)
		}
	}
	seqs := 0
	nUsed := 0
	litTotal := 0
	newOffsets := make(map[uint32]int, 1000)
	for _, b := range contents {
		block.reset(nil)
		if len(b) < 8 {
			continue
		}
		nUsed++
		enc.Reset(&d, true)
		enc.Encode(&block, b)
		addValues(&remain, block.literals)
		litTotal += len(block.literals)
		seqs += len(block.sequences)
		block.genCodes()
		addHist(&ll, block.coders.llEnc.Histogram())
		addHist(&ml, block.coders.mlEnc.Histogram())
		addHist(&of, block.coders.ofEnc.Histogram())
		for i, seq := range block.sequences {
			if i > 3 {
				break
			}
			offset := seq.offset
			if offset == 0 {
				continue
			}
			if offset > 3 {
				newOffsets[offset-3]++
			} else {
				newOffsets[uint32(o.Offsets[offset-1])]++
			}
		}
	}
	// Find most used offsets.
	var sortedOffsets []uint32
	for k := range newOffsets {
		sortedOffsets = append(sortedOffsets, k)
	}
	sort.Slice(sortedOffsets, func(i, j int) bool {
		a, b := sortedOffsets[i], sortedOffsets[j]
		if a == b {
			// Prefer the longer offset
			return sortedOffsets[i] > sortedOffsets[j]
		}
		return newOffsets[sortedOffsets[i]] > newOffsets[sortedOffsets[j]]
	})
	if len(sortedOffsets) > 3 {
		if debug {
			fmt.Print("Offsets:")
			for i, v := range sortedOffsets {
				if i > 20 {
					break
				}
				fmt.Printf("[%d: %d],", v, newOffsets[v])
			}
			fmt.Println("")
		}

		sortedOffsets = sortedOffsets[:3]
	}
	for i, v := range sortedOffsets {
		o.Offsets[i] = int(v)
	}
	if debug {
		fmt.Println("New repeat offsets", o.Offsets)
	}

	if nUsed == 0 || seqs == 0 {
		return nil, fmt.Errorf("%d blocks, %d sequences found", nUsed, seqs)
	}
	if debug {
		fmt.Println("Sequences:", seqs, "Blocks:", nUsed, "Literals:", litTotal)
	}
	if seqs/nUsed < 512 {
		// Use 512 as minimum.
		nUsed = seqs / 512
	}
	copyHist := func(dst *fseEncoder, src *[256]int) ([]byte, error) {
		hist := dst.Histogram()
		var maxSym uint8
		var maxCount int
		var fakeLength int
		for i, v := range src {
			if v > 0 {
				v = v / nUsed
				if v == 0 {
					v = 1
				}
			}
			if v > maxCount {
				maxCount = v
			}
			if v != 0 {
				maxSym = uint8(i)
			}
			fakeLength += v
			hist[i] = uint32(v)
		}
		dst.HistogramFinished(maxSym, maxCount)
		dst.reUsed = false
		dst.useRLE = false
		err := dst.normalizeCount(fakeLength)
		if err != nil {
			return nil, err
		}
		if debug {
			fmt.Println("RAW:", dst.count[:maxSym+1], "NORM:", dst.norm[:maxSym+1], "LEN:", fakeLength)
		}
		return dst.writeCount(nil)
	}
	if debug {
		fmt.Print("Literal lengths: ")
	}
	llTable, err := copyHist(block.coders.llEnc, &ll)
	if err != nil {
		return nil, err
	}
	if debug {
		fmt.Print("Match lengths: ")
	}
	mlTable, err := copyHist(block.coders.mlEnc, &ml)
	if err != nil {
		return nil, err
	}
	if debug {
		fmt.Print("Offsets: ")
	}
	ofTable, err := copyHist(block.coders.ofEnc, &of)
	if err != nil {
		return nil, err
	}

	// Literal table
	avgSize := litTotal
	if avgSize > huff0.BlockSizeMax/2 {
		avgSize = huff0.BlockSizeMax / 2
	}
	huffBuff := make([]byte, 0, avgSize)
	// Target size
	div := litTotal / avgSize
	if div < 1 {
		div = 1
	}
	if debug {
		fmt.Println("Huffman weights:")
	}
	for i, n := range remain[:] {
		if n > 0 {
			n = n / div
			// Allow all entries to be represented.
			if n == 0 {
				n = 1
			}
			huffBuff = append(huffBuff, bytes.Repeat([]byte{byte(i)}, n)...)
			if debug {
				fmt.Printf("[%d: %d], ", i, n)
			}
		}
	}
	if remain[255]/div == 0 {
		huffBuff = append(huffBuff, 255)
	}
	scratch := &huff0.Scratch{TableLog: 11}
	for tries := 0; tries < 255; tries++ {
		scratch = &huff0.Scratch{TableLog: 11}
		_, _, err = huff0.Compress1X(huffBuff, scratch)
		if err == nil {
			break
		}
		if debug {
			fmt.Printf("Try %d: Huffman error: %v\n", tries+1, err)
		}
		huffBuff = huffBuff[:0]
		if tries == 250 {
			if debug {
				fmt.Println("Huffman: Bailing out with predefined table")
			}

			// Bail out.... Just generate something
			huffBuff = append(huffBuff, bytes.Repeat([]byte{255}, 10000)...)
			for i := 0; i < 128; i++ {
				huffBuff = append(huffBuff, byte(i))
			}
			continue
		}
		if errors.Is(err, huff0.ErrIncompressible) {
			// Try truncating least common.
			for i, n := range remain[:] {
				if n > 0 {
					n = n / (div * (i + 1))
					if n > 0 {
						huffBuff = append(huffBuff, bytes.Repeat([]byte{byte(i)}, n)...)
					}
				}
				if i == 255 && (len(huffBuff) == 0 || huffBuff[len(huffBuff)-1] != 255) {
					huffBuff = append(huffBuff, 255)
				}
			}
		}
		if errors.Is(err, huff0.ErrUseRLE) {
			for i, n := range remain[:] {
				n = n / (div * (i + 1))
				// Allow all entries to be represented.
				if n == 0 {
					n = 1
				}
				huffBuff = append(huffBuff, bytes.Repeat([]byte{byte(i)}, n)...)
			}
		}
	}

	var out bytes.Buffer
	out.Write([]byte(dictMagic))
	out.Write(binary.LittleEndian.AppendUint32(nil, o.ID))
	out.Write(scratch.OutTable)
	if debug {
		fmt.Println("huff table:", len(scratch.OutTable), "bytes")
		fmt.Println("of table:", len(ofTable), "bytes")
		fmt.Println("ml table:", len(mlTable), "bytes")
		fmt.Println("ll table:", len(llTable), "bytes")
	}
	out.Write(ofTable)
	out.Write(mlTable)
	out.Write(llTable)
	out.Write(binary.LittleEndian.AppendUint32(nil, uint32(o.Offsets[0])))
	out.Write(binary.LittleEndian.AppendUint32(nil, uint32(o.Offsets[1])))
	out.Write(binary.LittleEndian.AppendUint32(nil, uint32(o.Offsets[2])))
	out.Write(hist)
	if debug {
		_, err := loadDict(out.Bytes())
		if err != nil {
			panic(err)
		}
		i, err := InspectDictionary(out.Bytes())
		if err != nil {
			panic(err)
		}
		fmt.Println("ID:", i.ID())
		fmt.Println("Content size:", i.ContentSize())
		fmt.Println("Encoder:", i.LitEncoder() != nil)
		fmt.Println("Offsets:", i.Offsets())
		enc, err := NewWriter(nil, WithEncoderDict(out.Bytes()))
		if err != nil {
			panic(err)
		}
		defer enc.Close()
		var dst []byte
		var totalSize int
		for _, b := range contents {
			dst = enc.EncodeAll(b, dst[:0])
			totalSize += len(dst)
		}
		fmt.Println("Compressed size:", totalSize)
	}
	return out.Bytes(), nil
}
