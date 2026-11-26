// Copyright 2026+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

// Package pool holds a bounded pool of reusable items.
package pool

import (
	"context"
	"sync"
)

// Bounded holds a fixed set of items, such as the encoders of one
// Encoder. Get waits while all of them are in use.
//
// It uses no channel: a channel made in a testing/synctest bubble cannot
// be used outside it, and an Encoder or Decoder may outlive that bubble.
// Get waits on a Mutex and GetContext on a Cond, so a GetContext must be
// woken from its own bubble, and fake time does not advance while a Get
// in a bubble waits. Get and GetContext wait in one queue, first come
// first served.
type Bounded[T any] struct {
	mu      sync.Mutex
	cond    sync.Cond // wakes GetContext waiters
	free    []T       // empty while anyone waits
	waiters []*waiter[T]
	spare   []*waiter[T] // waiters to reuse, ready locked
	closed  bool
}

// waiter is one waiting Get or GetContext. Put or Close fill in v and ok
// and set done. A Get waits on ready, which is locked while it waits,
// and locks it again on waking. A GetContext waits on the pool's cond.
type waiter[T any] struct {
	ready sync.Mutex
	v     T
	ok    bool
	done  bool
	ctx   bool // a GetContext
}

// afterFunc is context.AfterFunc, replaced in tests.
var afterFunc = context.AfterFunc

// NewBounded returns an empty pool with room for capacity items.
func NewBounded[T any](capacity int) *Bounded[T] {
	p := &Bounded[T]{free: make([]T, 0, capacity)}
	p.cond.L = &p.mu
	return p
}

// Get takes a free item, waiting until one is put back.
// It returns false when the pool is closed.
func (p *Bounded[T]) Get() (v T, ok bool) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return v, false
	}
	if len(p.free) > 0 {
		v, ok = p.pop()
		p.mu.Unlock()
		return v, ok
	}
	w := p.wait(false)
	p.mu.Unlock()

	w.ready.Lock()
	p.mu.Lock()
	v, ok = p.release(w)
	p.mu.Unlock()
	return v, ok
}

// GetContext is Get that also stops waiting when ctx is done.
// It returns false when ctx is done or the pool is closed.
func (p *Bounded[T]) GetContext(ctx context.Context) (v T, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || ctx.Err() != nil {
		return v, false
	}
	if len(p.free) > 0 {
		return p.pop()
	}
	w := p.wait(true)
	// The callback may run after GetContext returns, as stop does not
	// wait for it. Then it must not wake a later waiter, which may be
	// in another bubble.
	returned := false
	stop := afterFunc(ctx, func() {
		p.mu.Lock()
		if !returned {
			p.cond.Broadcast()
		}
		p.mu.Unlock()
	})
	defer stop()
	for !w.done && ctx.Err() == nil {
		p.cond.Wait()
	}
	returned = true
	if !w.done {
		p.remove(w)
	}
	v, ok = p.release(w)
	if ok && ctx.Err() != nil {
		// Pass on an item given just before the cancel, unless closed.
		if !p.closed {
			p.give(v)
		}
		var zero T
		return zero, false
	}
	return v, ok
}

// wait queues a waiter, reusing a spare one. p.mu must be held.
func (p *Bounded[T]) wait(ctx bool) *waiter[T] {
	var w *waiter[T]
	if n := len(p.spare); n > 0 {
		w = p.spare[n-1]
		p.spare[n-1] = nil
		p.spare = p.spare[:n-1]
	} else {
		w = &waiter[T]{}
		w.ready.Lock()
	}
	w.ctx = ctx
	p.waiters = append(p.waiters, w)
	return w
}

// remove takes w out of the queue. p.mu must be held.
func (p *Bounded[T]) remove(w *waiter[T]) {
	for i, x := range p.waiters {
		if x == w {
			n := copy(p.waiters[i:], p.waiters[i+1:])
			p.waiters[i+n] = nil
			p.waiters = p.waiters[:i+n]
			return
		}
	}
}

// release returns what w was given and keeps w for reuse.
// p.mu must be held.
func (p *Bounded[T]) release(w *waiter[T]) (v T, ok bool) {
	v, ok = w.v, w.ok
	var zero T
	w.v, w.ok, w.done = zero, false, false
	p.spare = append(p.spare, w)
	return v, ok
}

// pop takes the last free item. p.mu must be held.
func (p *Bounded[T]) pop() (v T, ok bool) {
	n := len(p.free)
	if n == 0 {
		return v, false
	}
	v = p.free[n-1]
	var zero T
	p.free[n-1] = zero
	p.free = p.free[:n-1]
	return v, true
}

// Put gives an item back to the call that has waited longest, or else
// keeps it free. After Close it drops the item.
func (p *Bounded[T]) Put(v T) {
	p.mu.Lock()
	if !p.closed {
		p.give(v)
	}
	p.mu.Unlock()
}

// give hands v to the first waiter, or keeps it free. p.mu must be held.
func (p *Bounded[T]) give(v T) {
	if len(p.waiters) == 0 {
		p.free = append(p.free, v)
		return
	}
	w := p.waiters[0]
	n := copy(p.waiters, p.waiters[1:])
	p.waiters[n] = nil
	p.waiters = p.waiters[:n]
	w.v, w.ok = v, true
	p.wake(w)
}

// wake marks w done and wakes it. p.mu must be held.
func (p *Bounded[T]) wake(w *waiter[T]) {
	w.done = true
	if w.ctx {
		p.cond.Broadcast()
	} else {
		w.ready.Unlock()
	}
}

// Len returns the number of free items.
func (p *Bounded[T]) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.free)
}

// Waiting returns the number of calls of Get and GetContext that wait.
func (p *Bounded[T]) Waiting() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.waiters)
}

// Close marks the pool closed, wakes every waiting Get and GetContext,
// and returns the free items, which the pool no longer holds.
func (p *Bounded[T]) Close() []T {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeLocked()
}

// closeLocked is Close. p.mu must be held.
func (p *Bounded[T]) closeLocked() []T {
	free := p.free
	for _, w := range p.waiters {
		w.ok = false
		p.wake(w)
	}
	p.free = nil
	p.waiters = nil
	p.closed = true
	return free
}
