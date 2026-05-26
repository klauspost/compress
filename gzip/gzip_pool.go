package gzip

import (
	"io"
	"sync"
)

// WriterPool is a pool of reusable gzip.Writers.
// Writers in the pool are reused via Reset, avoiding allocation
// of large internal data structures like hash tables.
type WriterPool struct {
	pool  sync.Pool
	level int
}

// NewWriterPool creates a new WriterPool for the given compression level.
func NewWriterPool(level int) *WriterPool {
	return &WriterPool{level: level}
}

// Get returns a Writer from the pool, or creates a new one if the pool is empty.
// The Writer is reset to write to w.
func (p *WriterPool) Get(w io.Writer) *Writer {
	v := p.pool.Get()
	if v == nil {
		z, _ := NewWriterLevel(w, p.level)
		return z
	}
	z := v.(*Writer)
	z.Reset(w)
	return z
}

// Put returns a Writer to the pool. The Writer is closed before being returned.
// The Writer should not be used after Put is called.
func (p *WriterPool) Put(z *Writer) {
	z.Close()
	p.pool.Put(z)
}
