// Copyright 2026+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

package pool

import (
	"context"
	"runtime"
	"testing"
	"testing/synctest"
)

// waitForWaiters returns once n calls of Get or GetContext wait on p.
func waitForWaiters[T any](p *Bounded[T], n int) {
	for p.Waiting() < n {
		runtime.Gosched()
	}
}

func TestBoundedGetContextCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := NewBounded[int](1)
		ctx, cancel := context.WithCancel(context.Background())
		var ok bool
		done := make(chan struct{})
		go func() {
			_, ok = p.GetContext(ctx)
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
		p.Put(1)
		if v, ok := p.GetContext(context.Background()); !ok || v != 1 {
			t.Fatalf("getContext = %d, %v; want 1, true", v, ok)
		}
	})
}

// Calls of Get that wait are served in the order they started.
func TestBoundedGetFirstComeFirstServed(t *testing.T) {
	p := NewBounded[int](1)
	const waiters = 3
	got := make([]chan int, waiters)
	for i := range got {
		got[i] = make(chan int, 1)
		go func() {
			v, _ := p.Get()
			got[i] <- v
		}()
		waitForWaiters(p, i+1)
	}
	for i := range waiters {
		p.Put(10 + i)
	}
	for i := range got {
		if v := <-got[i]; v != 10+i {
			t.Errorf("waiter %d got %d; want %d", i, v, 10+i)
		}
	}
	if n := p.Len(); n != 0 {
		t.Fatalf("len = %d; want 0", n)
	}
}

// close wakes every Get and GetContext that waits, and they return false.
func TestBoundedClose(t *testing.T) {
	p := NewBounded[int](2)
	p.Put(1)
	p.Put(2)
	if v, ok := p.Get(); !ok || v != 2 {
		t.Fatalf("get = %d, %v; want 2, true", v, ok)
	}
	if v, ok := p.Get(); !ok || v != 1 {
		t.Fatalf("get = %d, %v; want 1, true", v, ok)
	}
	getOK := make(chan bool, 1)
	go func() {
		_, ok := p.Get()
		getOK <- ok
	}()
	waitForWaiters(p, 1)
	ctxOK := make(chan bool, 1)
	go func() {
		_, ok := p.GetContext(context.Background())
		ctxOK <- ok
	}()
	waitForWaiters(p, 2)
	free := p.Close()
	if <-getOK {
		t.Fatal("get returned an item from a closed pool")
	}
	if <-ctxOK {
		t.Fatal("getContext returned an item from a closed pool")
	}
	if len(free) != 0 {
		t.Fatalf("close returned %d items; want 0", len(free))
	}
}

// An item given back after close is dropped: neither Get nor GetContext
// hands it out.
func TestBoundedPutAfterClose(t *testing.T) {
	p := NewBounded[int](1)
	p.Put(1)
	v, ok := p.Get()
	if !ok || v != 1 {
		t.Fatalf("get = %d, %v; want 1, true", v, ok)
	}
	p.Close()
	p.Put(v)
	if v, ok := p.Get(); ok {
		t.Fatalf("get after close returned %d", v)
	}
	if v, ok := p.GetContext(context.Background()); ok {
		t.Fatalf("getContext after close returned %d", v)
	}
	if n := p.Len(); n != 0 {
		t.Fatalf("len after close = %d; want 0", n)
	}
}

// A GetContext's cancel callback can run after it returned, since stop
// does not wait for it. It must not wake a later waiter, here in a bubble
// while the callback runs outside it: that wake-up is a fatal error.
func TestBoundedGetContextLateCallback(t *testing.T) {
	var late func()
	afterFunc = func(ctx context.Context, f func()) func() bool {
		if late == nil {
			late = f
		}
		// As when the callback has started: stop returns false.
		return func() bool { return false }
	}
	defer func() { afterFunc = context.AfterFunc }()

	p := NewBounded[int](1)
	got := make(chan int)
	go func() {
		v, _ := p.GetContext(context.Background())
		got <- v
	}()
	for {
		p.mu.Lock()
		registered := late != nil
		p.mu.Unlock()
		if registered {
			break
		}
		runtime.Gosched()
	}
	p.Put(1)
	if v := <-got; v != 1 {
		t.Fatalf("GetContext = %d; want 1", v)
	}

	fire := make(chan struct{})
	fired := make(chan struct{})
	go func() {
		<-fire
		late()
		close(fired)
	}()
	synctest.Test(t, func(t *testing.T) {
		done := make(chan int)
		go func() {
			v, _ := p.GetContext(context.Background())
			done <- v
		}()
		synctest.Wait()
		close(fire)
		<-fired
		p.Put(2)
		if v := <-done; v != 2 {
			t.Fatalf("GetContext = %d; want 2", v)
		}
	})
}

// A Get that waits reuses its waiter, so it does not allocate.
func TestBoundedGetWaitAllocs(t *testing.T) {
	p := NewBounded[int](1)
	kick := make(chan struct{})
	defer close(kick)
	go func() {
		for range kick {
			waitForWaiters(p, 1)
			p.Put(1)
		}
	}()
	allocs := testing.AllocsPerRun(100, func() {
		kick <- struct{}{}
		if _, ok := p.Get(); !ok {
			t.Fatal("Get failed")
		}
	})
	if allocs != 0 {
		t.Errorf("a waiting Get allocates %v times; want 0", allocs)
	}
}

// A Get that reuses the waiter of an earlier Get and is woken by Close
// returns false, not the earlier hand-off's true.
func TestBoundedCloseReusedWaiter(t *testing.T) {
	p := NewBounded[int](1)
	got := make(chan bool)
	get := func() {
		_, ok := p.Get()
		got <- ok
	}
	go get()
	waitForWaiters(p, 1)
	p.Put(1)
	if !<-got {
		t.Fatal("Get after Put returned false")
	}
	go get()
	waitForWaiters(p, 1)
	p.Close()
	if <-got {
		t.Fatal("Get woken by Close returned true")
	}
}

// Get and GetContext wait in one queue, so calls of Get that keep
// arriving cannot starve a GetContext.
func TestBoundedGetContextInQueue(t *testing.T) {
	p := NewBounded[int](1)
	get := func(f func() (int, bool)) chan int {
		c := make(chan int, 1)
		go func() {
			v, _ := f()
			c <- v
		}()
		return c
	}
	a := get(p.Get)
	waitForWaiters(p, 1)
	c := get(func() (int, bool) { return p.GetContext(context.Background()) })
	waitForWaiters(p, 2)
	b := get(p.Get)
	waitForWaiters(p, 3)
	for i := range 3 {
		p.Put(10 + i)
	}
	for i, ch := range []chan int{a, c, b} {
		if v := <-ch; v != 10+i {
			t.Errorf("waiter %d got %d; want %d", i, v, 10+i)
		}
	}
}

// A GetContext given an item just as its ctx is cancelled returns false
// and passes the item on.
func TestBoundedGetContextCancelPassesItemOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := NewBounded[int](1)
		ctx, cancel := context.WithCancel(context.Background())
		var ok bool
		done := make(chan struct{})
		go func() {
			_, ok = p.GetContext(ctx)
			close(done)
		}()
		synctest.Wait()
		p.mu.Lock()
		cancel()
		p.give(1)
		p.mu.Unlock()
		<-done
		if ok {
			t.Fatal("GetContext returned an item after cancel")
		}
		if v, ok := p.Get(); !ok || v != 1 {
			t.Fatalf("get = %d, %v; want 1, true", v, ok)
		}
	})
}

// A GetContext given an item, then closed and cancelled before it wakes,
// drops the item, so the closed pool holds none.
func TestBoundedGetContextCancelAfterClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := NewBounded[int](1)
		ctx, cancel := context.WithCancel(context.Background())
		var ok bool
		done := make(chan struct{})
		go func() {
			_, ok = p.GetContext(ctx)
			close(done)
		}()
		synctest.Wait()
		p.mu.Lock()
		p.give(1)
		free := p.closeLocked()
		cancel()
		p.mu.Unlock()
		<-done
		if ok {
			t.Fatal("GetContext returned an item after cancel")
		}
		if len(free) != 0 {
			t.Fatalf("close returned %v; want none", free)
		}
		if n := p.Len(); n != 0 {
			t.Fatalf("len = %d; want 0", n)
		}
	})
}
