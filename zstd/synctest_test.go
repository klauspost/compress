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

// With concurrency 1, DecodeAll waits while the one block decoder is in
// use. That wait must be one synctest sees as durably blocked.
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

		// Hold the only block decoder, so every DecodeAll must wait.
		block, ok := dec.decoders.get()
		if !ok {
			t.Fatal("no block decoder")
		}
		const calls = 4
		var wg sync.WaitGroup
		var mu sync.Mutex
		done := 0
		for range calls {
			wg.Go(func() {
				got, err := dec.DecodeAll(comp, nil)
				if err != nil {
					t.Error(err)
				} else if !bytes.Equal(got, in) {
					t.Error("decoded output differs")
				}
				mu.Lock()
				done++
				mu.Unlock()
			})
		}
		synctest.Wait()
		mu.Lock()
		if done != 0 {
			t.Fatalf("%d DecodeAll calls finished while the block decoder was held", done)
		}
		mu.Unlock()

		dec.decoders.put(block)
		wg.Wait()
		if done != calls {
			t.Fatalf("%d DecodeAll calls finished; want %d", done, calls)
		}
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

// An item given back after close is dropped: neither get nor getContext
// hands it out.
func TestBoundedPoolPutAfterClose(t *testing.T) {
	p := newBoundedPool[int](1)
	p.put(1)
	v, ok := p.get()
	if !ok || v != 1 {
		t.Fatalf("get = %d, %v; want 1, true", v, ok)
	}
	p.close()
	p.put(v)
	if v, ok := p.get(); ok {
		t.Fatalf("get after close returned %d", v)
	}
	if v, ok := p.getContext(context.Background()); ok {
		t.Fatalf("getContext after close returned %d", v)
	}
	if n := p.len(); n != 0 {
		t.Fatalf("len after close = %d; want 0", n)
	}
}
