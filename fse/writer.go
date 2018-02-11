package fse

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/OneOfOne/xxhash"
	"github.com/klauspost/compress/splitter"
)

type WriterOption func(o *wOptions) error

var WriterWith = WriterOption(nil)

const (
	streamBlockSizeLimit = 10 << 20

	streamMagicNumber = 0x284E3319

	blockTypeCompressed = iota
	blockTypeRaw
	blockTypeRLE
	blockTypeCRC
	blockTypeEOS
)

func (w WriterOption) parent(o *wOptions) error {
	if w != nil {
		return w(o)
	}
	return nil
}
func (w WriterOption) MaxBlockSize(n uint) WriterOption {
	return func(o *wOptions) error {
		if err := w.parent(o); err != nil {
			return err
		}

		if n >= streamBlockSizeLimit {
			return fmt.Errorf("max block size must <= %d bytes", streamBlockSizeLimit)
		}
		o.maxBlockSize = n
		return nil
	}
}

func (w WriterOption) CRC(b bool) WriterOption {
	return func(o *wOptions) error {
		if err := w.parent(o); err != nil {
			return err
		}
		o.withCRC = b
		return nil
	}
}

func defaultWriterOptions() wOptions {
	return wOptions{
		maxBlockSize: 256 << 10,
		withCRC:      true,
	}
}

type wOptions struct {
	maxBlockSize uint
	withCRC      bool
}

type Writer struct {
	bw    *bufio.Writer
	o     wOptions
	err   error
	errMu sync.Mutex
	h     xxhash.XXHash64

	splitter  io.WriteCloser
	fragments chan []byte
	sendBack  func([]byte)
	wg        sync.WaitGroup
	scr       Scratch
}

func NewWriter(w io.Writer, opts ...WriterOption) (*Writer, error) {
	o := defaultWriterOptions()
	for _, opt := range opts {
		err := opt(&o)
		if err != nil {
			return nil, err
		}
	}
	wr := Writer{o: o}
	return &wr, wr.Reset(w)
}

func (w *Writer) writeHeader() {
	var b [4 + binary.MaxVarintLen32]byte
	// 4 bytes, magic number
	binary.LittleEndian.PutUint32(b[:4], streamMagicNumber)
	// maximum block size
	n := binary.PutUvarint(b[4:], uint64(w.o.maxBlockSize))
	w.write(b[:4+n])
}

func (w *Writer) setErr(err error) {
	w.errMu.Lock()
	if w.err != nil {
		w.err = err
	}
	w.errMu.Unlock()
}

func (w *Writer) getErr() error {
	w.errMu.Lock()
	err := w.err
	w.errMu.Unlock()
	return err
}

func (w *Writer) write(p []byte) {
	if w.getErr() != nil {
		return
	}
	n, err := w.bw.Write(p)
	w.setErr(err)
	if n != len(p) {
		w.setErr(io.ErrShortWrite)
	}
}

func (w *Writer) compressor() {
	defer w.wg.Done()
	var tmp [1 + 2*binary.MaxVarintLen32]byte
	for frag := range w.fragments {
		if len(frag) == 0 {
			continue
		}
		headerSize := 0
		b, err := Compress(frag, &w.scr)
		switch err {
		case nil:
			tmp[0] = blockTypeCompressed
			headerSize = 1 + binary.PutUvarint(tmp[1:], uint64(len(frag)))
			headerSize += binary.PutUvarint(tmp[headerSize:], uint64(len(b)))
			w.write(tmp[:headerSize])
			w.write(b)
		case ErrIncompressible:
			tmp[0] = blockTypeRaw
			headerSize = 1 + binary.PutUvarint(tmp[1:], uint64(len(frag)))
			w.write(tmp[:headerSize])
			w.write(frag)
		case ErrUseRLE:
			tmp[0] = blockTypeRLE
			tmp[1] = frag[0]
			headerSize = 2 + binary.PutUvarint(tmp[2:], uint64(len(frag)))
			w.write(tmp[:headerSize])
		default:
			w.setErr(err)
		}
		w.sendBack(frag)
	}
	if w.o.withCRC {
		buf := tmp[:1]
		buf[0] = blockTypeCRC
		buf = w.h.Sum(buf)
		buf = append(buf, blockTypeEOS)
		w.write(buf)
	} else {
		tmp[0] = blockTypeEOS
		w.write(tmp[:1])
	}
}

func (w *Writer) Write(p []byte) (n int, err error) {
	if w.o.withCRC {
		n, err := w.h.Write(p)
		w.setErr(err)
		if n != len(p) {
			w.setErr(io.ErrShortWrite)
		}
	}
	n, err = w.splitter.Write(p)
	if err != nil {
		w.setErr(err)
	}
	if n != len(p) {
		w.setErr(io.ErrShortWrite)
	}
	return n, w.getErr()
}

func (w *Writer) Close() error {
	w.setErr(w.splitter.Close())
	w.wg.Wait()
	w.setErr(w.bw.Flush())
	return w.err
}

func (w *Writer) Reset(wr io.Writer) error {
	if w.bw != nil {
		w.Close()
	}
	w.fragments = make(chan []byte, 1)
	var opt splitter.Option
	opt, w.sendBack = splitter.With.SendBack(2)
	s, err := splitter.New(w.fragments, splitter.With.Mode(splitter.ModeEntropy).MaxBlockSize(w.o.maxBlockSize), opt)
	if err != nil {
		return err
	}
	w.splitter = s
	if w.bw == nil {
		w.bw = bufio.NewWriter(wr)
	} else {
		w.bw.Reset(wr)
	}
	w.h.Reset()
	w.err = nil
	w.wg = sync.WaitGroup{}
	w.writeHeader()
	w.wg.Add(1)
	go w.compressor()
	return w.err
}
