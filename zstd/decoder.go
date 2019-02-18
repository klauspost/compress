package zstd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"runtime"

	"github.com/klauspost/compress/huff0"
)

type Decoder struct {
	concurrent int
	lowMem     bool

	// Unreferenced decoders, ready for use
	decoders chan *dBlock

	// Streams ready to be decoded.
	stream chan decodeStream

	// Current read position
	current decoderState

	// Custom dictionaries
	dicts map[uint32]struct{}
}

type decoderState struct {
	decodeOutput
	// finished
	output chan decodeOutput
}

type history struct {
	b             []byte
	writePos      int
	huffTree      *huff0.Scratch
	recentOffsets [3]int
	decoders      sequenceDecoders
}

var (
	// Check the interfaces we want to support.
	_ = io.WriterTo(&Decoder{})
	_ = io.ReadCloser(&Decoder{})

	ErrDecoderClosed = errors.New("decoder used after Close")
)

func NewDecoder(r io.Reader, opts ...interface{}) (*Decoder, error) {
	d := Decoder{
		concurrent: runtime.GOMAXPROCS(0),
		stream:     make(chan decodeStream, 1),
	}

	d.current.output = make(chan decodeOutput, d.concurrent)
	//fmt.Println("startDecoders")

	// Create decoders
	d.decoders = make(chan *dBlock, d.concurrent)
	for i := 0; i < d.concurrent; i++ {
		d.decoders <- newDBlock(d.lowMem)
	}

	// Start stream decoders.
	nStreamDecs := d.concurrent
	if d.lowMem {
		nStreamDecs = 1
	}
	for i := 0; i < nStreamDecs; i++ {
		go d.startStreamDecoder()
	}
	return &d, d.Reset(r)
}

func (d *Decoder) Read(p []byte) (int, error) {
	var n int
	for {
		if len(d.current.b) > 0 {
			filled := copy(p, d.current.b)
			p = p[filled:]
			d.current.b = d.current.b[filled:]
			n += filled
		}
		if len(p) == 0 {
			break
		}
		if len(d.current.b) == 0 {
			// We have an error and no more data
			if d.current.err != nil {
				break
			}
			d.nextBlock()
			//fmt.Println("current error", d.current.err)
		}
	}
	if len(d.current.b) > 0 {
		// Only return error at end of block
		return n, nil
	}
	//fmt.Println("returning", n, d.current.err)
	return n, d.current.err
}

// Reset will reset the decoder the supplied stream after the current has finished processing.
// Note that this functionality cannot be used after Close has been called.
func (d *Decoder) Reset(r io.Reader) error {
	if d.current.err == ErrDecoderClosed {
		return d.current.err
	}
	if d.current.d != nil {
		d.decoders <- d.current.d
	}
	// Remove current block.
	d.current.decodeOutput = decodeOutput{}

	// Drain output
drainOutput:
	for {
		select {
		case <-d.current.output:
		default:
			break drainOutput
		}
	}
	//fmt.Println("Sending stream")
	d.stream <- decodeStream{
		r:      r,
		output: d.current.output,
	}
	return nil
}

func (d *Decoder) WriteTo(w io.Writer) (int64, error) {
	var n int64
	for d.current.err == nil {
		if len(d.current.b) > 0 {
			n2, err2 := w.Write(d.current.b)
			n += int64(n2)
			if err2 != nil && d.current.err == nil {
				d.current.err = err2
				break
			}
		}
		d.nextBlock()
	}
	err := d.current.err
	if err == io.EOF {
		err = nil
	}
	return n, err
}

// DecodeAll allows stateless decoding of a blob of bytes.
// Output will be appended to dst, so if the destination size is known
// you can pre-allocate the destination slice to avoid allocations.
// DecodeAll can be used concurrently. If you plan to, do not use the low memory option.
// The Decoder concurrency limits will be respected.
func (d *Decoder) DecodeAll(input, dst []byte) ([]byte, error) {
	output := make(chan decodeOutput, d.concurrent)
	// TODO: Store this to avoid allocation
	d.stream <- decodeStream{
		r:      bytes.NewBuffer(input),
		output: output,
	}
	if cap(dst) == 0 {
		// Allocate a reasonable buffer if nothing was provided.
		dst = make([]byte, 0, len(input))
	}
	for {
		o := <-output
		dst = append(dst, o.b...)
		d.decoders <- o.d
		if o.err != nil {
			if o.err == io.EOF {
				o.err = nil
			}
			return dst, o.err
		}
	}
}

// nextBlock returns the next block.
// If an error occurs d.err will be set.
func (d *Decoder) nextBlock() {
	if d.current.d != nil {
		d.decoders <- d.current.d
		d.current.d = nil
	}
	if d.current.err != nil {
		// Keep error state.
		return
	}
	d.current.decodeOutput = <-d.current.output
	//fmt.Println("got", len(d.current.b), "bytes, error:", d.current.err)
}

// Close will release all resources.
// It is NOT possible to reuse the decoder after this.
func (d *Decoder) Close() error {
	if d.current.err == ErrDecoderClosed {
		return d.current.err
	}

	if d.stream != nil {
		close(d.stream)
	}
	if d.decoders != nil {
		close(d.decoders)
		for dec := range d.decoders {
			dec.Close()
		}
		d.decoders = nil
	}
	if d.current.d != nil {
		d.current.d.Close()
		d.current.d = nil
	}
	d.current.err = ErrDecoderClosed
	return nil
}

type decodeOutput struct {
	d   *dBlock
	b   []byte
	err error
}

type decodeStream struct {
	r io.Reader

	// Blocks ready to be written to output.
	output chan decodeOutput
}

// Create Decoder:
// Spawn n block decoders. These accept tasks to decode a block.
// Create goroutine that handles stream processing, this will send history to decoders as they are available.
// Decoders update the history as they decode.
// When a block is returned:
// 		a) history is sent to the next decoder,
// 		b) content written to CRC.
// 		c) return data to WRITER.
// 		d) wait for next block to return data.
// Once WRITTEN, the decoders reused by the writer frame decoder for re-use.
func (d *Decoder) startStreamDecoder() {
	//fmt.Println("creating stream decoder")
	frame := newDFrame()
	frame.concurrent = d.concurrent
	frame.lowMem = d.lowMem

	for {
		in, ok := <-d.stream
		//fmt.Println("got stream")
		if !ok {
			d.stream = nil
			return
		}
		for {
			err := frame.reset(in.r)
			if err != nil {
				if err == io.EOF {
					break
				}
				fmt.Println("Frame decoder returned", err)
				in.output <- decodeOutput{
					err: err,
				}
				break
			}
			//fmt.Println("started frame")
			go frame.startDecoder(in.output)
		decodeFrame:
			for dec := range d.decoders {
				// TODO: Racing on shutdown on frame.
				//fmt.Println("starting decoder")
				err := frame.next(dec)
				//fmt.Println("decoder returned", err)
				switch err {
				case io.EOF:
					// End of current block, no error
					break decodeFrame
				case nil:
					continue
				default:
					in.output <- decodeOutput{err: err}
				}
			}
			if _, err := in.r.Read([]byte{}); err == io.EOF {
				//fmt.Println("Stream ended", err)
				// No more data
				break
			}
		}
	}
}
