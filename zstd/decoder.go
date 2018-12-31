package zstd

import (
	"errors"
	"fmt"
	"io"
	"runtime"
)

type Decoder struct {
	concurrent int
	lowMem     bool

	frame *dFrame

	// Unreferenced decoders, ready for use
	decoders chan *dBlock

	// Blocks ready to be written to output.
	output chan decodeOutput

	// Streams ready to be decoded.
	stream chan decodeStream

	// Current read position
	current decodeOutput
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
	d.output = make(chan decodeOutput, d.concurrent)
	go d.startDecoders()
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
			fmt.Println("current error", d.current.err)
		}
	}
	if len(d.current.b) > 0 {
		// Only return error at end of block
		return n, nil
	}
	fmt.Println("returning", n, d.current.err)
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
drainDecoders:
	for {
		select {
		case o := <-d.output:
			d.decoders <- o.d
		default:
			break drainDecoders
		}
	}
	d.current = decodeOutput{}
	fmt.Println("Sending stream")
	d.stream <- decodeStream{
		r: r,
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
	d.current = <-d.output
	fmt.Println("got", len(d.current.b), "bytes, error:", d.current.err)
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
	if d.frame != nil {
		d.frame.Close()
		d.frame = nil
	}
	for dec := range d.decoders {
		dec.Close()
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
func (d *Decoder) startDecoders() {
	fmt.Println("startDecoders")
	if d.frame == nil {
		d.frame = newDFrame()
	}
	frame := d.frame
	frame.concurrent = d.concurrent
	frame.lowMem = d.lowMem

	// Create decoders
	d.decoders = make(chan *dBlock, d.concurrent)
	for i := 0; i < d.concurrent; i++ {
		d.decoders <- newDBlock(d.lowMem)
	}
	fmt.Println("decoders ready")
	for {
		in, ok := <-d.stream
		fmt.Println("got stream")
		if !ok {
			d.stream = nil
			close(d.output)
			return
		}
		for {
			err := frame.reset(in.r)
			if err != nil {
				d.output <- decodeOutput{
					err: err,
				}
				continue
			}
			fmt.Println("started frame")
			go frame.startDecoder(d.output)
		decodeFrame:
			for dec := range d.decoders {
				// TODO: Racing on shutdown on frame.
				fmt.Println("starting decoder")
				err := frame.next(dec)
				fmt.Println("decoder resturned", err)
				switch err {
				case io.EOF:
					// End of current block, no error
					break decodeFrame
				case nil:
					continue
				default:
					d.output <- decodeOutput{err: err}
				}
			}
			if _, err := in.r.Read([]byte{}); err == io.EOF {
				fmt.Println("Stream ended", err)
				// No more data
				break
			}
		}
	}
}
