// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import (
	"fmt"
	"math"
	rdebug "runtime/debug"
	"sync"
)

// maxRingSize caps the input ring of parallel jobs.
// With very large jobs, fewer of them run at once.
const maxRingSize = min(2<<30, math.MaxInt>>2)

type encJob struct {
	prefix []byte        // history before input, with input in its capacity
	input  []byte        // job's own input data, in the ring
	last   bool          // last block of last job gets last=true
	output []byte        // compressed blocks (filled by worker)
	err    error         // encoding error
	done   chan struct{} // closed when complete
}

// jobState keeps the input of parallel jobs in a ring: an overlap area, then
// slots of jobSize. Writes fill the ring, workers encode it where it is, and
// a job's prefix is the input before it, so only a wrap copies the prefix,
// into the overlap area. A stream starts in an output buffer instead, and
// moves to the ring when that fills.
type jobState struct {
	jobSize     int
	overlapSize int
	maxSlots    int

	ring     []byte
	first    bool      // ring is the output buffer the stream started in
	ringPool sync.Pool // *[]byte rings, kept between streams
	slotSeq  []int     // jobs to flush before a slot can be written again
	slot     int       // slot being filled
	hist     int       // start of the input usable as history
	start    int       // start of the input not dispatched yet
	end      int       // end of the input
	limit    int       // end of the slot

	jobSeq int // next job sequence number

	jobCh    chan *encJob // dispatch to workers
	resultCh chan *encJob // ordered results to flusher

	workerWg  sync.WaitGroup
	flusherWg sync.WaitGroup

	mu         sync.Mutex
	flushedSeq int // last flushed sequence number
	cond       *sync.Cond

	flusherErr error
	started    bool

	outputPool sync.Pool // *[]byte buffers for compressed output
}

func (e *Encoder) startJobWorkers() {
	js := &e.state.jobs
	n := e.o.concurrent
	js.jobCh = make(chan *encJob, n)
	js.resultCh = make(chan *encJob, n)
	js.flushedSeq = 0
	js.cond = sync.NewCond(&js.mu)

	// Workers borrow encoders from the shared e.encoders pool per-job.
	// Ensure the pool is initialized before any worker tries to borrow.
	e.init.Do(e.initialize)

	for range n {
		js.workerWg.Add(1)
		go e.jobWorker()
	}
	js.flusherWg.Add(1)
	go e.jobFlusher()
	js.started = true
}

func (e *Encoder) jobWorker() {
	js := &e.state.jobs
	defer js.workerWg.Done()
	for job := range js.jobCh {
		enc, _ := e.encoders.Get()
		e.compressJob(enc, job)
		e.encoders.Put(enc)
		close(job.done)
	}
}

func (e *Encoder) compressJob(enc encoder, job *encJob) {
	defer func() {
		if r := recover(); r != nil {
			job.err = fmt.Errorf("panic in parallel job: %v", r)
			rdebug.PrintStack()
		}
	}()
	// Pooled encoders must not keep the ring.
	defer enc.inPlace(nil)

	if len(job.prefix) > 0 {
		enc.ResetPrefix(job.prefix)
	} else {
		enc.Reset(nil, false)
		enc.inPlace(job.prefix)
	}

	data := job.input
	if len(data) == 0 && job.last {
		blk := enc.Block()
		blk.reset(nil)
		blk.last = true
		blk.encodeRaw(nil)
		job.output = append(job.output, blk.output...)
		return
	}

	blk := enc.Block()
	for len(data) > 0 {
		todo := data
		if len(todo) > e.o.blockSize {
			todo = todo[:e.o.blockSize]
		}
		data = data[len(todo):]

		blk.pushOffsets()
		enc.Encode(blk, todo)
		blk.last = len(data) == 0 && job.last

		err := blk.encode(todo, e.o.noEntropy, !e.o.allLitEntropy)
		if err != nil {
			job.err = err
			return
		}
		if len(job.output)+len(blk.output) > cap(job.output) && !e.o.lowMem {
			// Grow once, to the most a job can take: blocks are at most
			// 3 bytes larger than their input.
			js := &e.state.jobs
			n := js.jobSize + 3*((js.jobSize+e.o.blockSize-1)/e.o.blockSize)
			job.output = append(make([]byte, 0, n), job.output...)
		}
		job.output = append(job.output, blk.output...)
		blk.reset(nil)
	}
}

func (js *jobState) getOutputBuf(size int) []byte {
	if v := js.outputPool.Get(); v != nil {
		bp := v.(*[]byte)
		b := *bp
		if cap(b) >= size {
			return b[:0]
		}
	}
	return make([]byte, 0, size)
}

func (js *jobState) putOutputBuf(b []byte) {
	if cap(b) > 0 {
		b = b[:0]
		js.outputPool.Put(&b)
	}
}

func (e *Encoder) jobFlusher() {
	js := &e.state.jobs
	defer js.flusherWg.Done()
	for job := range js.resultCh {
		<-job.done
		if job.err != nil {
			js.mu.Lock()
			js.flusherErr = job.err
			js.cond.Broadcast()
			js.mu.Unlock()
			for range js.resultCh {
			}
			return
		}
		// Hashing here keeps it off the writer's goroutine.
		// The input stays in the ring until flushedSeq passes the job.
		if e.o.crc {
			_, _ = e.state.encoder.CRC().Write(job.input)
		}
		if len(job.output) > 0 {
			_, err := e.state.w.Write(job.output)
			if err != nil {
				js.mu.Lock()
				js.flusherErr = err
				js.cond.Broadcast()
				js.mu.Unlock()
				for range js.resultCh {
				}
				return
			}
			e.state.nWritten += int64(len(job.output))
		}
		js.putOutputBuf(job.output)
		job.prefix, job.input, job.output = nil, nil, nil

		js.mu.Lock()
		js.flushedSeq++
		js.cond.Broadcast()
		js.mu.Unlock()
	}
}

// shutdownJobWorkers stops the workers and the flusher, and releases the ring.
func (e *Encoder) shutdownJobWorkers() {
	js := &e.state.jobs
	if js.started {
		close(js.jobCh)
		js.workerWg.Wait()
		close(js.resultCh)
		js.flusherWg.Wait()
		js.started = false
	}
	js.releaseRing()
}

// waitAllJobs blocks until all dispatched jobs have been flushed.
func (e *Encoder) waitAllJobs() {
	js := &e.state.jobs
	if !js.started {
		return
	}
	js.mu.Lock()
	for js.flushedSeq < js.jobSeq && js.flusherErr == nil {
		js.cond.Wait()
	}
	js.mu.Unlock()
}

// resetRing sets the ring layout for the options and starts a stream:
// the next slot is the first, with no history.
func (js *jobState) resetRing(o *encoderOptions) {
	js.jobSize, js.overlapSize = o.jobSize(), o.overlapSize()
	// A slot is held by its own job and by the prefixes of the next
	// ceil(overlap/jobSize) jobs, and one slot is being filled.
	slots := o.concurrent + 1 + (js.overlapSize+js.jobSize-1)/js.jobSize
	js.maxSlots = min(slots, (maxRingSize-js.overlapSize)/js.jobSize)
	js.slot = -1
	js.hist, js.start, js.end, js.limit = 0, 0, 0, 0
}

// releaseRing keeps the input buffer for the next stream, in the pool it came
// from. The pools let an idle encoder drop it.
func (js *jobState) releaseRing() {
	if js.first {
		js.putOutputBuf(js.ring)
	} else if js.ring != nil {
		r := js.ring
		js.ringPool.Put(&r)
	}
	js.ring, js.slotSeq, js.first = nil, nil, false
}

// newRing takes a ring from the pool, or allocates one.
func (js *jobState) newRing() {
	n := js.overlapSize + js.maxSlots*js.jobSize
	if v, ok := js.ringPool.Get().(*[]byte); ok && len(*v) == n {
		js.ring = *v
	} else {
		js.ring = make([]byte, n)
	}
	js.slotSeq = make([]int, js.maxSlots)
}

// slotOf returns the slot of ring position i. The overlap area is part of the first.
func (js *jobState) slotOf(i int) int {
	return max(i-js.overlapSize, 0) / js.jobSize
}

// nextSlot makes room for more input. A stream starts in an output buffer of
// half a job, so a short stream needs no ring. When it fills, the history and
// the input not dispatched yet move to the ring. Only a Flush dispatches jobs
// from the buffer, and it waits for them, so the buffer is free then.
// In the ring, it moves to the next slot once the jobs using it are flushed,
// or back to the first when the first two are free, so that input the workers
// keep up with stays in cache.
func (e *Encoder) nextSlot() error {
	js := &e.state.jobs
	if js.ring == nil {
		n := js.jobSize / 2
		js.ring, js.first = js.getOutputBuf(n)[:n], true
		js.hist, js.start, js.end, js.limit = 0, 0, 0, n
		return nil
	}
	i := js.slot + 1
	if js.first || i == js.maxSlots {
		i = 0
	}
	js.mu.Lock()
	if i > 1 && js.flushedSeq >= js.slotSeq[0] && js.flushedSeq >= js.slotSeq[1] {
		i = 0
	}
	for !js.first && js.flushedSeq < js.slotSeq[i] && js.flusherErr == nil {
		js.cond.Wait()
	}
	err := js.flusherErr
	js.mu.Unlock()
	if err != nil {
		return err
	}
	if js.first {
		buf := js.ring
		js.newRing()
		h := min(js.start-js.hist, js.overlapSize)
		copy(js.ring[js.overlapSize-h:], buf[js.start-h:js.end])
		js.putOutputBuf(buf)
		js.first = false
		js.hist, js.end = js.overlapSize-h, js.overlapSize+js.end-js.start
		js.start = js.overlapSize
	} else if i == 0 {
		h := min(js.end-js.hist, js.overlapSize)
		copy(js.ring[js.overlapSize-h:js.overlapSize], js.ring[js.end-h:js.end])
		js.hist = js.overlapSize - h
		js.start, js.end = js.overlapSize, js.overlapSize
	}
	js.slot = i
	js.limit = js.overlapSize + (i+1)*js.jobSize
	return nil
}

func (e *Encoder) dispatchJob(final bool) error {
	s := &e.state
	js := &s.jobs

	js.mu.Lock()
	fErr := js.flusherErr
	js.mu.Unlock()
	if fErr != nil {
		return fErr
	}

	filling := js.ring[js.start:js.end]
	if !s.headerWritten {
		// Single-block optimization: fall through to encodeAll path.
		if final && len(filling) > 0 && len(filling) <= e.o.blockSize {
			s.current = e.encodeAll(s.encoder, filling, s.current[:0])
			var n2 int
			n2, s.err = s.w.Write(s.current)
			if s.err != nil {
				return s.err
			}
			s.nWritten += int64(n2)
			s.nInput += int64(len(filling))
			s.current = s.current[:0]
			js.start = js.end
			s.headerWritten = true
			s.fullFrameWritten = true
			s.eofWritten = true
			return nil
		}
		if final && len(filling) == 0 && !e.o.fullZero {
			s.headerWritten = true
			s.fullFrameWritten = true
			s.eofWritten = true
			return nil
		}

		var tmp [maxHeaderSize]byte
		fh := frameHeader{
			ContentSize:   uint64(s.frameContentSize),
			WindowSize:    uint32(s.encoder.WindowSize(s.frameContentSize)),
			SingleSegment: false,
			Checksum:      e.o.crc,
			DictID:        0,
		}
		dst := fh.appendTo(tmp[:0])
		var n2 int
		n2, s.err = s.w.Write(dst)
		if s.err != nil {
			return s.err
		}
		s.nWritten += int64(n2)
		s.headerWritten = true
	}

	if len(filling) == 0 && !final {
		return nil
	}

	if !js.started {
		e.startJobWorkers()
	}

	// Every job takes half a job, so pooled buffers have that size, or the
	// most a job can take once one has grown (see compressJob).
	outputEst := js.jobSize / 2
	if e.o.lowMem {
		outputEst = max(len(filling)/2, 512)
	}

	pstart := max(js.hist, js.start-js.overlapSize)
	job := &encJob{
		prefix: js.ring[pstart:js.start:js.end],
		input:  filling,
		last:   final,
		done:   make(chan struct{}),
		output: js.getOutputBuf(outputEst),
	}
	if js.end > pstart && !js.first {
		for i := js.slotOf(pstart); i <= js.slotOf(js.end-1); i++ {
			js.slotSeq[i] = js.jobSeq + 1
		}
	}
	js.start = js.end

	s.nInput += int64(len(job.input))
	js.jobSeq++

	if final {
		s.eofWritten = true
	}

	js.resultCh <- job
	js.jobCh <- job

	return nil
}
