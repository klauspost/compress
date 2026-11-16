// Copyright 2026+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

package zstd

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"testing/synctest"
)

// An Encoder or Decoder made inside a testing/synctest bubble must stay
// usable after the bubble ends. Programs keep them for reuse, often in
// a sync.Pool or a package variable, so the next user may be another
// bubble or no bubble at all. A channel inside them would belong to
// the bubble that made it, and using it from elsewhere stops the
// program: "fatal error: receive on synctest channel from outside
// bubble".

func synctestInput() []byte {
	return bytes.Repeat([]byte("synctest bubbles and zstd "), 1000)
}

func TestSynctestDecoderOutlivesBubble(t *testing.T) {
	in := synctestInput()
	enc, err := NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	comp := enc.EncodeAll(in, nil)
	enc.Close()

	var dec *Decoder
	synctest.Test(t, func(t *testing.T) {
		var err error
		dec, err = NewReader(nil, WithDecoderConcurrency(2))
		if err != nil {
			t.Fatal(err)
		}
		got, err := dec.DecodeAll(comp, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, in) {
			t.Fatal("decoded output differs inside the bubble")
		}
	})
	defer dec.Close()

	got, err := dec.DecodeAll(comp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, in) {
		t.Fatal("decoded output differs outside the bubble")
	}
	synctest.Test(t, func(t *testing.T) {
		got, err := dec.DecodeAll(comp, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, in) {
			t.Fatal("decoded output differs in a second bubble")
		}
	})
}

func TestSynctestEncoderOutlivesBubble(t *testing.T) {
	in := synctestInput()
	var enc *Encoder
	var compIn []byte
	synctest.Test(t, func(t *testing.T) {
		var err error
		enc, err = NewWriter(nil, WithEncoderConcurrency(2))
		if err != nil {
			t.Fatal(err)
		}
		compIn = enc.EncodeAll(in, nil)
	})
	defer enc.Close()

	compOut := enc.EncodeAll(in, nil)
	if !bytes.Equal(compIn, compOut) {
		t.Fatal("EncodeAll gave other output outside the bubble")
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	got, err := dec.DecodeAll(compOut, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, in) {
		t.Fatal("round trip differs")
	}
}

// With concurrency 1, concurrent DecodeAll calls wait for the one block
// decoder. That wait must be one synctest sees as durably blocked.
func TestSynctestDecodeAllWaits(t *testing.T) {
	in := synctestInput()
	enc, err := NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	comp := enc.EncodeAll(in, nil)
	enc.Close()

	synctest.Test(t, func(t *testing.T) {
		dec, err := NewReader(nil, WithDecoderConcurrency(1))
		if err != nil {
			t.Fatal(err)
		}
		defer dec.Close()
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				got, err := dec.DecodeAll(comp, nil)
				if err != nil {
					t.Error(err)
					return
				}
				if !bytes.Equal(got, in) {
					t.Error("decoded output differs")
				}
			})
		}
		wg.Wait()
	})
}

// A stream decoder with several goroutines runs inside a bubble and is
// closed there.
func TestSynctestStreamDecoder(t *testing.T) {
	in := synctestInput()
	enc, err := NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	comp := enc.EncodeAll(in, nil)
	enc.Close()

	synctest.Test(t, func(t *testing.T) {
		dec, err := NewReader(bytes.NewReader(comp), WithDecoderConcurrency(4))
		if err != nil {
			t.Fatal(err)
		}
		defer dec.Close()
		got, err := io.ReadAll(dec)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, in) {
			t.Fatal("decoded output differs")
		}
	})
}

func TestBoundedPoolGetContextCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newBoundedPool[int](1)
		ctx, cancel := context.WithCancel(context.Background())
		var ok bool
		done := make(chan struct{})
		go func() {
			_, ok = p.getContext(ctx)
			close(done)
		}()
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("getContext returned from an empty pool")
		default:
		}
		cancel()
		<-done
		if ok {
			t.Fatal("getContext returned an item after cancel")
		}
		p.put(1)
		if v, ok := p.getContext(context.Background()); !ok || v != 1 {
			t.Fatalf("getContext = %d, %v; want 1, true", v, ok)
		}
	})
}

func TestBoundedPoolClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newBoundedPool[int](2)
		p.put(1)
		p.put(2)
		if v, ok := p.get(); !ok || v != 2 {
			t.Fatalf("get = %d, %v; want 2, true", v, ok)
		}
		var ok bool
		done := make(chan struct{})
		go func() {
			_, _ = p.get()
			_, ok = p.get()
			close(done)
		}()
		synctest.Wait()
		free := p.close()
		<-done
		if ok {
			t.Fatal("get returned an item from a closed pool")
		}
		if len(free) != 0 {
			t.Fatalf("close returned %d items; want 0", len(free))
		}
	})
}
