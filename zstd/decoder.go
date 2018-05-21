package zstd

import (
	"io"
)

type Decoder struct {
	in    io.Reader
	frame *dFrame
}

func NewDecoder(r io.Reader, opts ...interface{}) (*Decoder, error) {
	d := Decoder{}
	d.Reset(r)
	return &d, nil
}

func (*Decoder) Read(p []byte) (n int, err error) {
	panic("implement me")
}

func (*Decoder) Close() error {
	panic("implement me")
}

func (d *Decoder) Reset(r io.Reader) (err error) {
	d.in = r
	d.frame, err = newDFrame(r)
	return err
}
