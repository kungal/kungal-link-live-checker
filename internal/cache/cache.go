// Package cache is a tiny, concurrency-safe in-memory TTL cache. The link
// checker caches dead verdicts for a long time, alive for a medium time, and
// unknown briefly or not at all (docs/REQUIREMENTS.md §5).
package cache

import (
	"context"
	"sync"
	"time"
)

// Cache is a map with per-entry expiry.
type Cache[V any] struct {
	// Clock returns the current time; overridable in tests. Defaults to time.Now.
	Clock func() time.Time

	mu    sync.Mutex
	items map[string]entry[V]
}

type entry[V any] struct {
	val V
	exp time.Time
}

// New returns an empty cache.
func New[V any]() *Cache[V] {
	return &Cache[V]{
		Clock: time.Now,
		items: make(map[string]entry[V]),
	}
}

func (c *Cache[V]) now() time.Time {
	if c.Clock != nil {
		return c.Clock()
	}
	return time.Now()
}

// Get returns the live value for key, or ok=false when absent or expired.
func (c *Cache[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok || !c.now().Before(e.exp) {
		var zero V
		return zero, false
	}
	return e.val, true
}

// Set stores val under key for ttl. A ttl <= 0 is a no-op (do not cache).
func (c *Cache[V]) Set(key string, val V, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = entry[V]{val: val, exp: c.now().Add(ttl)}
}

// Janitor periodically evicts expired entries until ctx is done.
func (c *Cache[V]) Janitor(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.evictExpired()
		}
	}
}

func (c *Cache[V]) evictExpired() {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.items {
		if !now.Before(e.exp) {
			delete(c.items, k)
		}
	}
}
