package cache

import (
	"context"
	"sync"
	"time"
)

type Cache[V any] struct {
	Clock func() time.Time

	mu    sync.Mutex
	items map[string]entry[V]
}

type entry[V any] struct {
	val V
	exp time.Time
}

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

func (c *Cache[V]) Set(key string, val V, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = entry[V]{val: val, exp: c.now().Add(ttl)}
}

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
