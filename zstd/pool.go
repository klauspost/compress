// Copyright 2026+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

package zstd

import (
	"context"
	"sync"
)

// boundedPool holds the free encoders or block decoders of one
// Encoder or Decoder. get waits while all of them are in use.
//
// It uses a mutex and a sync.Cond, not a channel. A channel made inside
// a testing/synctest bubble belongs to that bubble, and any use of it
// from outside the bubble stops the program. An Encoder or Decoder is
// meant to be kept and reused, often in a sync.Pool or a package
// variable, so the one that was made in a test bubble must stay usable
// after the bubble ends.
type boundedPool[T any] struct {
	mu     sync.Mutex
	cond   sync.Cond
	free   []T
	closed bool
}

func newBoundedPool[T any](capacity int) *boundedPool[T] {
	p := &boundedPool[T]{free: make([]T, 0, capacity)}
	p.cond.L = &p.mu
	return p
}

// get takes a free item, waiting until one is put back.
// It returns false when the pool is closed.
func (p *boundedPool[T]) get() (v T, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.free) == 0 && !p.closed {
		p.cond.Wait()
	}
	return p.pop()
}

// getContext is get that also stops waiting when ctx is done.
// It returns false when ctx is done or the pool is closed.
func (p *boundedPool[T]) getContext(ctx context.Context) (v T, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.free) == 0 && !p.closed && ctx.Err() == nil {
		stop := context.AfterFunc(ctx, func() {
			p.mu.Lock()
			p.cond.Broadcast()
			p.mu.Unlock()
		})
		defer stop()
		for len(p.free) == 0 && !p.closed && ctx.Err() == nil {
			p.cond.Wait()
		}
	}
	if ctx.Err() != nil {
		return v, false
	}
	return p.pop()
}

// pop takes the last free item. p.mu must be held.
func (p *boundedPool[T]) pop() (v T, ok bool) {
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

// put gives an item back and wakes one waiting get.
func (p *boundedPool[T]) put(v T) {
	p.mu.Lock()
	p.free = append(p.free, v)
	p.mu.Unlock()
	p.cond.Signal()
}

// len returns the number of free items.
func (p *boundedPool[T]) len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.free)
}

// close marks the pool closed, wakes every waiting get and returns
// the free items, which the pool no longer holds.
func (p *boundedPool[T]) close() []T {
	p.mu.Lock()
	free := p.free
	p.free = nil
	p.closed = true
	p.mu.Unlock()
	p.cond.Broadcast()
	return free
}
