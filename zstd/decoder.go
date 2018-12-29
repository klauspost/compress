package zstd

import (
	"io"
)

type Decoder struct {
	in    io.Reader
	frame *dFrame
}

var (
	// Check the interfaces we want to support.
	_ = io.WriterTo(&Decoder{})
	_ = io.ReadCloser(&Decoder{})
)

func NewDecoder(r io.Reader, opts ...interface{}) (*Decoder, error) {
	d := Decoder{}
	err := d.Reset(r)
	return &d, err
}

func (*Decoder) Read(p []byte) (n int, err error) {
	panic("implement me")
}

func (d *Decoder) WriteTo(w io.Writer) (n int64, err error) {
	for err == nil {
		for {
			b, err := d.frame.next()
			if err != nil {
				return n, err
			}
			if b.Last {
				break
			}
		}
		if _, err := d.frame.br.Read([]byte{}); err == io.EOF {
			break
		}
		err = d.frame.reset(d.frame.br)
	}
	return n, err
}

func (*Decoder) Close() error {
	panic("implement me")
}

func (d *Decoder) Reset(r io.Reader) (err error) {
	d.in = r
	d.frame, err = newDFrame(r)
	return err
}
