package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Limiter struct {
	clock func() time.Time

	mu           sync.Mutex
	tokens       float64
	capacity     float64
	refillPerSec float64
	last         time.Time
}

// rps <= 0 disables limiting.
func NewLimiter(rps float64, burst int) *Limiter {
	capacity := float64(burst)
	if capacity < 1 {
		capacity = 1
	}
	l := &Limiter{
		clock:        time.Now,
		tokens:       capacity,
		capacity:     capacity,
		refillPerSec: rps,
	}
	l.last = l.clock()
	return l
}

func (l *Limiter) refillLocked(now time.Time) {
	if l.refillPerSec <= 0 {
		l.tokens = l.capacity
		l.last = now
		return
	}
	elapsed := now.Sub(l.last).Seconds()
	if elapsed <= 0 {
		return
	}
	l.tokens += elapsed * l.refillPerSec
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}
	l.last = now
}

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
		if l.refillPerSec > 0 {
			wait = time.Duration((1 - l.tokens) / l.refillPerSec * float64(time.Second))
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

type Registry struct {
	mu       sync.Mutex
	limiters map[string]*Limiter
	rps      float64
	burst    int
}

func NewRegistry(rps float64, burst int) *Registry {
	return &Registry{limiters: make(map[string]*Limiter), rps: rps, burst: burst}
}

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
