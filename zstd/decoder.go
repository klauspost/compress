package zstd

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

type Decoder struct {
	o decoderOptions

	// Unreferenced decoders, ready for use.
	decoders chan *blockDec

	// Streams ready to be decoded.
	stream chan decodeStream

	// Current read position used for Reader functionality.
	current decoderState

	// Custom dictionaries
	dicts map[uint32]struct{}

	// decode output for DecodeAll
	decOut chan chan decodeOutput

	// streamWg is the waitgroup for all streams
	streamWg sync.WaitGroup
}

// decoderState is used for maintaining state when the decoder
// is used for streaming.
type decoderState struct {
	decodeOutput
	// output in order
	output chan decodeOutput

	// cancel remaining output.
	cancel chan struct{}

	flushed bool
}

var (
	// Check the interfaces we want to support.
	_ = io.WriterTo(&Decoder{})
	_ = io.Reader(&Decoder{})

	ErrDecoderClosed = errors.New("decoder used after Close")
)

// NewReader creates a new decoder.
// A nil Reader can be provided in which case Reset can be used to start a decode.
//
// A Decoder can be used in two modes:
//
// 1) As a stream, or
// 2) For stateless decoding using DecodeAll or DecodeBuffer.
//
// Only a single stream can be decoded concurrently, but the same decoder
// can run multiple concurrent stateless decodes. It is even possible to
// use stateless decodes while a stream is being decoded.
// For best speed it is recommended to keep track of
//
// The Reset function can be used to initiate a new stream, which is will considerably
// reduce the allocations normally caused by NewReader.
func NewReader(r io.Reader, opts ...DOption) (*Decoder, error) {
	d := Decoder{
		stream: make(chan decodeStream, 1),
	}
	d.o.setDefault()
	for _, o := range opts {
		err := o(&d.o)
		if err != nil {
			return nil, err
		}
	}
	d.decOut = make(chan chan decodeOutput, d.o.concurrent)
	d.current.output = make(chan decodeOutput, d.o.concurrent)
	d.current.flushed = true

	// Create decoders
	d.decoders = make(chan *blockDec, d.o.concurrent)
	for i := 0; i < d.o.concurrent; i++ {
		d.decoders <- newBlockDec(d.o.lowMem)
	}

	// Start stream decoders.
	//for i := 0; i < d.o.concurrent; i++ {
	go d.startStreamDecoder()
	//}
	if r == nil {
		return &d, nil
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
			//println("current error", d.current.err)
		}
	}
	if len(d.current.b) > 0 {
		// Only return error at end of block
		return n, nil
	}
	//println("returning", n, d.current.err)
	return n, d.current.err
}

// Reset will reset the decoder the supplied stream after the current has finished processing.
// Note that this functionality cannot be used after Close has been called.
func (d *Decoder) Reset(r io.Reader) error {
	if d.current.err == ErrDecoderClosed {
		return d.current.err
	}
	if r == nil {
		return errors.New("nil Reader sent as input")
	}
	if d.current.d != nil {
		println("adding current decoder", d.current.d)
		d.decoders <- d.current.d
	}
	d.drainOutput()

	// Remove current block.
	d.current.decodeOutput = decodeOutput{}
	d.current.err = nil
	d.current.cancel = make(chan struct{})
	d.current.flushed = false
	d.current.d = nil

	//println("Sending stream")
	d.stream <- decodeStream{
		r:      r,
		output: d.current.output,
		cancel: d.current.cancel,
	}
	return nil
}

// drainOutput will drain the output until errEndOfStream is sent.
func (d *Decoder) drainOutput() {
	if d.current.cancel != nil {
		println("cancelling current")
		close(d.current.cancel)
		d.current.cancel = nil
	}
	if d.current.output == nil || d.current.flushed {
		println("current already flushed")
		return
	}
	for {
		select {
		case v := <-d.current.output:
			if v.d != nil {
				println("got decoder", v.d)
				d.decoders <- v.d
			}
			if v.err == errEndOfStream {
				println("current flushed")
				d.current.flushed = true
				return
			}
		}
	}
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
	return d.DecodeBuffer(bytes.NewBuffer(input), dst)
}

// DecodeBuffer allows stateless decoding of a blob of bytes.
// Output will be appended to dst, so if the destination size is known
// you can pre-allocate the destination slice to avoid allocations.
// DecodeAll can be used concurrently. If you plan to, do not use the low memory option.
// The Decoder concurrency limits will be respected.
func (d *Decoder) DecodeBuffer(input *bytes.Buffer, dst []byte) ([]byte, error) {
	var output chan decodeOutput
	select {
	case output = <-d.decOut:
	default:
		output = make(chan decodeOutput, d.o.concurrent)
	}
	if cap(dst) == 0 {
		// Allocate a reasonable buffer if nothing was provided.
		dst = make([]byte, 0, input.Len())
	}
	d.stream <- decodeStream{
		r:      input,
		output: output,
	}
	dst = dst[:0]
	for {
		o := <-output
		dst = append(dst, o.b...)
		if o.d != nil {
			d.decoders <- o.d
		}
		if o.err != nil {
			if o.err == io.EOF {
				o.err = nil
			}
			// Drain stream
			for v := range output {
				if v.err == errEndOfStream {
					break
				}
			}
			select {
			case d.decOut <- output:
			default:
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
	//println("got", len(d.current.b), "bytes, error:", d.current.err)
}

// Close will release all resources.
// It is NOT possible to reuse the decoder after this.
func (d *Decoder) Close() {
	if d.current.err == ErrDecoderClosed {
		return
	}
	d.drainOutput()
	if d.stream != nil {
		close(d.stream)
		d.streamWg.Wait()
		d.stream = nil
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
}

type decodeOutput struct {
	d   *blockDec
	b   []byte
	err error
}

type decodeStream struct {
	r io.Reader

	// Blocks ready to be written to output.
	output chan decodeOutput

	// cancel reading from the input
	cancel chan struct{}
}

// errEndOfStream indicates that everything from the stream was read.
var errEndOfStream = errors.New("end-of-stream")

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
	//println("creating stream decoder")
	d.streamWg.Add(1)
	defer d.streamWg.Done()
	frame := newFrameDec(d.o)
	done := frame.frameDone

	for {
		stream, ok := <-d.stream
		if !ok {
			return
		}
	decodeStream:
		for {
			err := frame.reset(stream.r)
			println("Frame decoder returned", err)
			if err != nil {
				stream.output <- decodeOutput{
					err: err,
				}
				break
			}
			println("starting frame decoder")

			// This goroutine will forward history between frames.
			go frame.startDecoder(stream)
		decodeFrame:
			// Go through all blocks of the frame.
			for {
				select {
				case dec := <-d.decoders:
					select {
					case <-stream.cancel:
						frame.sendEOF(dec)
						<-done
						break decodeStream
					default:
					}
					err := frame.next(dec)
					switch err {
					case io.EOF:
						// End of current frame, no error
						println("EOF on next block")
						// stream.output <- decodeOutput{err: err, d: nil}
						break decodeFrame
					case nil:
						continue
					default:
						println("block decoder returned", err)
						//stream.output <- decodeOutput{err: err, d: dec}
						// dec := <-d.decoders
						//frame.sendEOF(dec)
						<-done
						break decodeStream
					}
				case <-stream.cancel:
					<-done
					break decodeStream
				}
			}
			println("waiting for done")
			// All blocks have started decoding, check if there are more frames.
			br := <-done
			println("done waiting...")
			if b, err := br.Peek(4); err == io.EOF || !bytes.Equal(frameMagic, b) {
				println("No data left on stream, or not frame magic")

				// No more data.
				stream.output <- decodeOutput{
					err: io.EOF,
				}
				break
			}
			stream.r = br
		}
		println("Sending EOS")
		stream.output <- decodeOutput{err: errEndOfStream}
	}
}
