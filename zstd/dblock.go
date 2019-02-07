package zstd

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/klauspost/compress/huff0"
)

type BlockType uint8

//go:generate stringer -type=BlockType,LiteralsBlockType,seqCompMode

const (
	BlockTypeRaw BlockType = iota
	BlockTypeRLE
	BlockTypeCompressed
	BlockTypeReserved

	// maxCompressedBlockSize is the biggest allowed compressed block size (128KB)
	maxCompressedBlockSize = 128 << 10

	// Maximum possible block size (all Raw+Uncompressed).
	maxBlockSize = (1 << 21) - 1
)

const (
	LiteralsBlockRaw LiteralsBlockType = iota
	LiteralsBlockRLE
	LiteralsBlockCompressed
	LiteralsBlockTreeless
)

type LiteralsBlockType uint8

var (
	// ErrReservedBlockType is returned when a reserved block type is found.
	// Typically this indicates wrong or corrupted input.
	ErrReservedBlockType = errors.New("invalid input: reserved block type encountered")

	// ErrReservedBlockType is returned when a reserved block type is found.
	// Typically this indicates wrong or corrupted input.
	ErrCompressedSizeTooBig = errors.New("invalid input: compressed size too big")

	huffDecoderPool = sync.Pool{New: func() interface{} {
		return &huff0.Scratch{}
	}}

	fseDecoderPool = sync.Pool{New: func() interface{} {
		return &fseDecoder{}
	}}
)

type dBlock struct {
	data       []byte
	dst        []byte
	literalBuf []byte
	WindowSize uint64
	Type       BlockType
	RLESize    uint32
	Last       bool
	// Use less memory
	lowMem      bool
	history     chan *history
	input       chan struct{}
	result      chan decodeOutput
	sequenceBuf []seq
}

func (b *dBlock) String() string {
	if b == nil {
		return "<nil>"
	}
	return fmt.Sprintf("Steam Size: %d, Type: %v, Last: %t, Window: %d", len(b.data), b.Type, b.Last, b.WindowSize)
}

func newDBlock(lowMem bool) *dBlock {
	b := dBlock{
		lowMem:  lowMem,
		result:  make(chan decodeOutput, 1),
		input:   make(chan struct{}, 1),
		history: make(chan *history, 1),
	}
	go b.startDecoder()
	return &b
}

// reset will reset the block.
// Input must be a start of a block and will be at the end of the block when returned.
func (b *dBlock) reset(br io.Reader, windowSize uint64) error {
	b.WindowSize = windowSize
	var tmp [4]byte
	_, err := io.ReadFull(br, tmp[:3])
	if err != nil {
		if debug {
			fmt.Println("Reading block header:", err)
		}
		return err
	}
	bh := uint32(tmp[0]) | (uint32(tmp[1]) << 8) | (uint32(tmp[2]) << 16)
	b.Last = bh&1 != 0
	b.Type = BlockType((bh >> 1) & 3)
	// find size.
	cSize := int(bh >> 3)
	switch b.Type {
	case BlockTypeReserved:
		return ErrReservedBlockType
	case BlockTypeRLE:
		b.RLESize = uint32(cSize)
		cSize = 1
	case BlockTypeCompressed:
		if debug {
			fmt.Println("Data size on stream:", cSize)
		}
		b.RLESize = 0
		if cSize > maxCompressedBlockSize || uint64(cSize) > b.WindowSize {
			if debug {
				fmt.Printf("compressed block too big: %+v\n", b)
			}
			return ErrCompressedSizeTooBig
		}
	default:
		b.RLESize = 0
	}

	// Read block data.
	if cap(b.data) < cSize {
		if b.lowMem {
			b.data = make([]byte, 0, maxBlockSize)
		} else {
			b.data = make([]byte, 0, cSize)
		}
	}
	b.data = b.data[:cSize]
	// Read all.
	_, err = io.ReadFull(br, b.data)
	if err != nil {
		if debug {
			fmt.Println("Reading block:", err)
		}
		return err
	}
	// Start decoding
	b.input <- struct{}{}
	return nil
}

// Close will release resources.
// Closed dBlock cannot be reset.
func (b *dBlock) Close() {
	close(b.input)
	close(b.history)
	close(b.result)
}

// decode will prepare decoding the block when it receives the history.
func (b *dBlock) startDecoder() {
	for {
		_, ok := <-b.input
		if !ok {
			return
		}
		fmt.Println("dBlock: Got block input")
		switch b.Type {
		case BlockTypeRLE:
			if cap(b.dst) < int(b.RLESize) {
				if b.lowMem {
					b.dst = make([]byte, b.RLESize)
				} else {
					b.dst = make([]byte, maxBlockSize)
				}
			}
			o := decodeOutput{
				d:   b,
				b:   b.dst[:b.RLESize],
				err: nil,
			}
			v := b.data[0]
			for i := range o.b {
				o.b[i] = v
			}
			<-b.history
			// TODO: add rle to hist.
			// TODO: We should check if result is closed.
			b.result <- o
		case BlockTypeRaw:
			o := decodeOutput{
				d:   b,
				b:   b.data,
				err: nil,
			}
			<-b.history
			// TODO: add block to history.
			b.result <- o
		case BlockTypeCompressed:
			// Read literal section
			err := b.decodeCompressed()
			o := decodeOutput{
				d:   b,
				b:   b.dst,
				err: err,
			}
			b.result <- o
		default:
			panic("Invalid block type")
		}
		fmt.Println("dBlock: Finished block")
	}
}

var ErrBlockTooSmall = errors.New("block too small")

// decodeCompressed ...
func (b *dBlock) decodeCompressed() error {
	b.dst = b.dst[:0]
	in := b.data
	// There must be at least one byte for Literals_Block_Type and one for Sequences_Section_Header
	if len(in) < 2 {
		return ErrBlockTooSmall
	}
	litType := LiteralsBlockType(in[0] & 3)
	var litRegenSize int
	var litCompSize int
	sizeFormat := (in[0] >> 2) & 3
	var fourStreams bool
	fmt.Println("Literals type:", litType, "sizeFormat:", sizeFormat)
	switch litType {
	case LiteralsBlockRaw, LiteralsBlockRLE:
		switch sizeFormat {
		case 0, 2:
			// Regenerated_Size uses 5 bits (0-31). Literals_Section_Header uses 1 byte.
			litRegenSize = int(in[0] >> 3)
			in = in[1:]
		case 1:
			// Regenerated_Size uses 12 bits (0-4095). Literals_Section_Header uses 2 bytes.
			litRegenSize = int(in[0]>>4) + (int(in[1]) << 4)
			in = in[2:]
		case 3:
			//  Regenerated_Size uses 20 bits (0-1048575). Literals_Section_Header uses 3 bytes.
			if len(in) < 3 {
				fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, len(in))
				return ErrBlockTooSmall
			}
			litRegenSize = int(in[0]>>4) + (int(in[1]) << 4) + (int(in[2]) << 12)
			in = in[3:]
		}
	case LiteralsBlockCompressed, LiteralsBlockTreeless:
		switch sizeFormat {
		case 0, 1:
			// Both Regenerated_Size and Compressed_Size use 10 bits (0-1023).
			if len(in) < 3 {
				fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, len(in))
				return ErrBlockTooSmall
			}
			n := uint64(in[0]>>4) + (uint64(in[1]) << 4) + (uint64(in[2]) << 12)
			litRegenSize = int(n & 1023)
			litCompSize = int(n >> 10)
			fourStreams = sizeFormat == 1
			in = in[3:]
		case 2:
			fourStreams = true
			if len(in) < 4 {
				fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, len(in))
				return ErrBlockTooSmall
			}
			n := uint64(in[0]>>4) + (uint64(in[1]) << 4) + (uint64(in[2]) << 12) + (uint64(in[3]) << 20)
			litRegenSize = int(n & 16383)
			litCompSize = int(n >> 14)
			in = in[4:]
		case 3:
			fourStreams = true
			if len(in) < 5 {
				fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, len(in))
				return ErrBlockTooSmall
			}
			n := uint64(in[0]>>4) + (uint64(in[1]) << 4) + (uint64(in[2]) << 12) + (uint64(in[3]) << 20) + (uint64(in[4]) << 28)
			litRegenSize = int(n & 262143)
			litCompSize = int(n >> 18)
			in = in[5:]
		}
	}
	var literals []byte
	var huff *huff0.Scratch
	switch litType {
	case LiteralsBlockRaw:
		if len(in) < litRegenSize {
			fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, "remain:", len(in), "want:", litRegenSize)
			return ErrBlockTooSmall
		}
		literals = in[:litRegenSize]
		in = in[litRegenSize:]
		fmt.Printf("Found %d uncompressed literals\n", litRegenSize)
	case LiteralsBlockRLE:
		if len(in) < 1 {
			fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, "remain:", len(in), "want:", 1)
			return ErrBlockTooSmall
		}
		if cap(b.literalBuf) < litRegenSize {
			if b.lowMem {
				b.literalBuf = make([]byte, litRegenSize)
			} else {
				b.literalBuf = make([]byte, litRegenSize, 1<<18)
			}
		}
		literals = b.literalBuf[:litRegenSize]
		v := in[0]
		for i := range literals {
			literals[i] = v
		}
		in = in[1:]
		fmt.Printf("Found %d RLE compressed literals\n", litRegenSize)
	case LiteralsBlockTreeless:
		if len(in) < litCompSize {
			fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, "remain:", len(in), "want:", litCompSize)
			return ErrBlockTooSmall
		}
		// Store compressed literals, so we defer decoding until we get history.
		literals = in[:litCompSize]
		in = in[litCompSize:]
		fmt.Printf("Found %d compressed literals\n", litCompSize)
	case LiteralsBlockCompressed:
		if len(in) < litCompSize {
			fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, "remain:", len(in), "want:", litCompSize)
			return ErrBlockTooSmall
		}
		literals = in[:litCompSize]
		in = in[litCompSize:]

		huff = huffDecoderPool.Get().(*huff0.Scratch)
		var err error
		huff, literals, err = huff0.ReadTable(literals, huff)
		if err != nil {
			return err
		}
		// Ensure we have space to store it.
		if cap(b.literalBuf) < litRegenSize {
			if b.lowMem {
				b.literalBuf = make([]byte, 0, litRegenSize)
			} else {
				b.literalBuf = make([]byte, 0, 1<<18)
			}
		}
		// Use our out buffer.
		huff.Out = b.literalBuf[:0]
		if fourStreams {
			literals, err = huff.Decompress4X(literals, litRegenSize)
		} else {
			literals, err = huff.Decompress1X(literals)
		}
		if err != nil {
			return err
		}
		if len(literals) != litRegenSize {
			return fmt.Errorf("literal output size mismatch want %d, got %d", litRegenSize, len(literals))
		}
		fmt.Printf("Decompressed %d literals into %d bytes\n", litCompSize, litRegenSize)
	}

	// Decode Sequences
	// https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md#sequences-section
	if len(in) < 1 {
		return ErrBlockTooSmall
	}
	seqHeader := in[0]
	nSeqs := 0
	switch {
	case seqHeader == 0:
		in = in[1:]
	case seqHeader < 128:
		nSeqs = int(seqHeader)
		in = in[1:]
	case seqHeader < 255:
		if len(in) < 2 {
			return ErrBlockTooSmall
		}
		nSeqs = int(seqHeader)<<8 | int(in[1])
		in = in[2:]
	case seqHeader == 255:
		if len(in) < 3 {
			return ErrBlockTooSmall
		}
		nSeqs = 0x7f00 + int(in[1]) + (int(in[2]) << 8)
		in = in[3:]
	}

	// Allocate sequences
	if cap(b.sequenceBuf) < nSeqs {
		if b.lowMem {
			b.sequenceBuf = make([]seq, nSeqs)
		} else {
			// Allocate max
			b.sequenceBuf = make([]seq, nSeqs, 0x7f00+0xffff)
		}
	} else {
		// Reuse buffer
		b.sequenceBuf = b.sequenceBuf[:nSeqs]
	}
	var seqs = &sequenceDecoders{}
	if nSeqs > 0 {
		if len(in) < 1 {
			return ErrBlockTooSmall
		}
		br := byteReader{b: in, off: 0}
		compMode := br.Uint8()
		br.advance(1)
		for i := uint(0); i < 3; i++ {
			mode := seqCompMode((compMode >> (6 - i*2)) & 3)
			fmt.Println("Sequence", i, "Comp mode", mode)
			var seq *sequenceDecoder
			switch i {
			case 0:
				seq = &seqs.litLengths
			case 1:
				seq = &seqs.offsets
			case 2:
				seq = &seqs.matchLengths
			}
			switch mode {
			case compModePredefined:
				seq.fse = &fsePredef[i]
			case compModeRLE:
				if br.remain() < 1 {
					return ErrBlockTooSmall
				}
				v := br.Uint8()
				br.advance(1)
				dec := fseDecoderPool.Get().(*fseDecoder)
				symb, err := decSymbolValue(v, symbolTableX[i])
				if err != nil {
					fmt.Println("RLE Transform table error:", err)
					return err
				}
				dec.setRLE(symb)
				seq.fse = dec
				fmt.Println("RLE set to ", *symb)
			case compModeFSE:
				dec := fseDecoderPool.Get().(*fseDecoder)
				err := dec.readNCount(&br)
				if err != nil {
					fmt.Println("Read table error:", err)
					return err
				}
				fmt.Println("Read table ok")
				err = dec.transform(symbolTableX[i])
				if err != nil {
					fmt.Println("Transform table error:", err)
					return err
				}
				seq.fse = dec
			case compModeRepeat:
				seq.repeat = true
			}
		}
		in = br.unread()
	}

	// Wait for history.
	// All time spent after this is critical since it is strictly sequential.
	hist := <-b.history

	// Decode treeless literal block.
	if litType == LiteralsBlockTreeless {
		if hist.huffTree == nil {
			return errors.New("literal block was treeless, but no history was defined")
		}
		// Ensure we have space to store it.
		if cap(b.literalBuf) < litRegenSize {
			if b.lowMem {
				b.literalBuf = make([]byte, 0, litRegenSize)
			} else {
				b.literalBuf = make([]byte, 0, 1<<18)
			}
		}
		var err error
		// Use our out buffer.
		huff = hist.huffTree
		huff.Out = b.literalBuf[:0]
		if fourStreams {
			literals, err = huff.Decompress4X(literals, litRegenSize)
		} else {
			literals, err = huff.Decompress1X(literals)
		}
		if err != nil {
			return err
		}
		if len(literals) != litRegenSize {
			return fmt.Errorf("literal output size mismatch want %d, got %d", litRegenSize, len(literals))
		}
	} else {
		if hist.huffTree != nil {
			huffDecoderPool.Put(hist.huffTree)
			hist.huffTree = nil
		}
	}
	if huff != nil {
		huff.Out = nil
	}
	hist.huffTree = huff
	fmt.Println("Final literals:", len(literals))

	if nSeqs == 0 {
		// Decompressed content is defined entirely as Literals Section content.
		b.dst = append(b.dst, literals...)
		return nil
	}

	seqs, err := seqs.mergeHistory(&hist.decoders)
	if err != nil {
		return err
	}

	br := &bitReader{}
	if err := br.init(in); err != nil {
		return err
	}

	if err := seqs.initialize(br); err != nil {
		return err
	}
	for i := 0; i < nSeqs; i++ {
		if br.finished() {
			return io.ErrUnexpectedEOF
		}
		litLen, matchOff, matchLen := seqs.next(br)
		fmt.Printf("Seq %d: Litlen: %d, matchOff: %d, matchLen: %d\n", i, litLen, matchOff, matchLen)
	}
	if !br.finished() {
		return fmt.Errorf("%d extra bits on block, should be 0", br.remain())
	}

	fmt.Println("History merged ok")
	return br.close()
}

type sequenceDecoder struct {
	// decoder keeps track of the current state and updates it from the bitstream.
	fse    *fseDecoder
	state  fseState
	repeat bool
}

func (s *sequenceDecoder) init(br *bitReader) error {
	if s.fse == nil {
		return errors.New("neither FSE nor RLE defined")
	}
	s.state.init(br, s.fse.actualTableLog, s.fse.dt[:1<<s.fse.actualTableLog])
	return nil
}

type sequenceDecoders struct {
	litLengths   sequenceDecoder
	offsets      sequenceDecoder
	matchLengths sequenceDecoder
	prevOffset   [3]uint32
}

func (s *sequenceDecoders) initialize(br *bitReader) error {
	if err := s.litLengths.init(br); err != nil {
		return errors.New("litLengths:" + err.Error())
	}
	if err := s.offsets.init(br); err != nil {
		return errors.New("litLengths:" + err.Error())
	}
	if err := s.matchLengths.init(br); err != nil {
		return errors.New("matchLengths:" + err.Error())
	}
	return nil
}

func (s *sequenceDecoders) next(br *bitReader) (ll, mo, ml uint32) {
	br.fill()
	// Max 8 bits
	ll, llB := s.litLengths.state.next(br)
	// Max 9 bits
	ml, mlB := s.matchLengths.state.next(br)
	// Max 8 bits
	mo, moB := s.offsets.state.next(br)
	br.fill()

	// extra bits are stored in reverse order.
	mo += br.getBits(moB)
	ml += br.getBits(mlB)
	ll += br.getBits(llB)
	return
}

// mergeHistory will merge history.
func (s *sequenceDecoders) mergeHistory(hist *sequenceDecoders) (*sequenceDecoders, error) {
	for i := uint(0); i < 3; i++ {
		var sNew, sHist *sequenceDecoder
		switch i {
		case 0:
			sNew = &s.litLengths
			sHist = &hist.litLengths
		case 1:
			sNew = &s.offsets
			sHist = &hist.offsets
		case 2:
			sNew = &s.matchLengths
			sHist = &hist.matchLengths
		}
		if sNew.repeat {
			if sHist.fse == nil {
				return nil, fmt.Errorf("sequence stream %d, repeat requested, but no history", i)
			}
			continue
		}
		if sNew.fse == nil {
			return nil, fmt.Errorf("sequence stream %d, no fse found", i)
		}
		if sHist.fse != nil {
			fseDecoderPool.Put(sHist.fse)
		}
		sHist.fse = sNew.fse
	}
	return hist, nil
}

type seq struct {
	literals    uint32
	matchLen    uint32
	matchOffset uint32
}

type seqCompMode uint8

const (
	compModePredefined seqCompMode = iota
	compModeRLE
	compModeFSE
	compModeRepeat
)
