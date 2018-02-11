package splitter

type fixedWriter struct {
	buffer  []byte
	out     chan<- []byte
	off     int
	maxSize int
	sb      sendBack
}

func newFixedWriter(maxSize uint, out chan<- []byte) *fixedWriter {
	return &fixedWriter{
		buffer:  make([]byte, maxSize),
		out:     out,
		off:     0,
		maxSize: int(maxSize),
	}
}

// Write blocks of similar size.
func (f *fixedWriter) write(b []byte) (n int, err error) {
	written := 0
	for len(b) > 0 {
		n := copy(f.buffer[f.off:], b)
		b = b[n:]
		f.off += n
		written += n
		// Filled the buffer? Send it off!
		if f.off == f.maxSize {
			f.split()
		}
	}
	return written, nil
}

// Split content, so a new block begins with next write
func (f *fixedWriter) split() {
	if f.off == 0 {
		return
	}
	out := f.sb.getBuffer(f.off)
	copy(out, f.buffer[:f.off])
	f.out <- out
	f.off = 0
}
