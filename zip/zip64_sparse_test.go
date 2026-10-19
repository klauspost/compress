// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zip

import (
	"cmp"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
)

// A sparseFile represents an archive as a sequence of non-zero byte spans
// (the LFH headers, the Central Directory, the EOCD records, and any
// non-zero compressed bodies) plus a total length. Bytes outside any span
// are implicitly zero. This is the storage format used for goldens under
// testdata/zip64/ (suffix .zsparse), copied from the standard library.
//
// On-disk layout (all little-endian):
//
//	uint64 size
//	uint32 numSpans
//	for each span:
//		uint64 offset
//		uint32 dataLen
//		dataLen bytes
//
// Spans are sorted by offset and non-overlapping.
type sparseFile struct {
	Size  int64
	Spans []sparseSpan
}

type sparseSpan struct {
	Offset int64
	Data   []byte
}

// ReadAt implements [io.ReaderAt] by serving the underlying spans and
// synthesizing zero bytes for any gap inside [0, Size).
func (f *sparseFile) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("sparseFile: negative offset")
	}
	if off >= f.Size {
		return 0, io.EOF
	}
	end := min(off+int64(len(p)), f.Size)
	n := int(end - off)
	clear(p[:n])
	for _, s := range f.Spans {
		sEnd := s.Offset + int64(len(s.Data))
		if sEnd <= off || s.Offset >= end {
			continue
		}
		from := max(s.Offset, off)
		to := min(sEnd, end)
		copy(p[from-off:to-off], s.Data[from-s.Offset:to-s.Offset])
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// readSparse parses the on-disk format from r.
func readSparse(r io.Reader) (*sparseFile, error) {
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	f := &sparseFile{
		Size: int64(binary.LittleEndian.Uint64(hdr[:8])),
	}
	n := binary.LittleEndian.Uint32(hdr[8:12])
	if n > 1<<20 {
		return nil, fmt.Errorf("sparseFile: implausible span count %d", n)
	}
	f.Spans = make([]sparseSpan, n)
	for i := range f.Spans {
		var b [12]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		f.Spans[i].Offset = int64(binary.LittleEndian.Uint64(b[:8]))
		sz := binary.LittleEndian.Uint32(b[8:12])
		f.Spans[i].Data = make([]byte, sz)
		if _, err := io.ReadFull(r, f.Spans[i].Data); err != nil {
			return nil, err
		}
	}
	if !slices.IsSortedFunc(f.Spans, func(a, b sparseSpan) int {
		return cmp.Compare(a.Offset, b.Offset)
	}) {
		return nil, errors.New("sparseFile: spans not sorted")
	}
	return f, nil
}

// readSparseFile reads a sparse file from path. The file is expected to be
// gzip-compressed; the outer gzip wrap shrinks goldens that contain non-zero
// compressed bodies (e.g., the deflate-zeros entries) by 100x because
// deflate-of-zeros is highly repetitive. Small Store goldens benefit too:
// gzip's header overhead is ~30 bytes, well under the bytes saved on a 4 KB
// sparse representation.
func readSparseFile(path string) (*sparseFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return readSparse(zr)
}
