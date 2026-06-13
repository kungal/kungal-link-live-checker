// Package ratelimit provides a minimal token-bucket limiter, one per provider,
// so the service never floods a netdisk and gets the egress IP blocked
// (docs/REQUIREMENTS.md §7). Stdlib-only, no external deps.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter is a token bucket refilled at a steady rate.
type Limiter struct {
	clock func() time.Time

	mu       sync.Mutex
	tokens   float64
	capacity float64
	refill   float64 // tokens per second
	last     time.Time
}

// NewLimiter builds a limiter allowing rps tokens/second with the given burst
// capacity. rps <= 0 disables limiting (every request allowed immediately).
func NewLimiter(rps float64, burst int) *Limiter {
	capacity := float64(burst)
	if capacity < 1 {
		capacity = 1
	}
	l := &Limiter{
		clock:    time.Now,
		tokens:   capacity,
		capacity: capacity,
		refill:   rps,
	}
	l.last = l.clock()
	return l
}

func (l *Limiter) refillLocked(now time.Time) {
	if l.refill <= 0 { // disabled: always full
		l.tokens = l.capacity
		l.last = now
		return
	}
	elapsed := now.Sub(l.last).Seconds()
	if elapsed <= 0 {
		return
	}
	l.tokens += elapsed * l.refill
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}
	l.last = now
}

// Allow consumes a token without blocking, reporting whether one was available.
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refillLocked(l.clock())
	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

// Wait blocks until a token is available or ctx is done.
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		l.refillLocked(l.clock())
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		var wait time.Duration
		if l.refill > 0 {
			wait = time.Duration((1 - l.tokens) / l.refill * float64(time.Second))
		}
		l.mu.Unlock()
		if wait <= 0 {
			wait = time.Millisecond
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// Registry hands out one Limiter per provider name, created lazily.
type Registry struct {
	mu       sync.Mutex
	limiters map[string]*Limiter
	rps      float64
	burst    int
}

// NewRegistry returns a registry whose limiters share the given rps/burst.
func NewRegistry(rps float64, burst int) *Registry {
	return &Registry{limiters: make(map[string]*Limiter), rps: rps, burst: burst}
}

// For returns the limiter for a provider, creating it on first use.
func (r *Registry) For(name string) *Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.limiters[name]
	if !ok {
		l = NewLimiter(r.rps, r.burst)
		r.limiters[name] = l
	}
	return l
}
