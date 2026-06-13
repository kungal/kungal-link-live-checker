package cache

import (
	"testing"
	"time"
)

func TestGetSetAndExpiry(t *testing.T) {
	c := New[string]()
	now := time.Unix(1000, 0)
	c.Clock = func() time.Time { return now }

	c.Set("k", "v", time.Minute)
	if v, ok := c.Get("k"); !ok || v != "v" {
		t.Fatalf("got (%q,%v), want (v,true)", v, ok)
	}

	now = now.Add(2 * time.Minute) // past expiry
	if _, ok := c.Get("k"); ok {
		t.Fatal("entry should have expired")
	}
}

func TestNonPositiveTTLDoesNotStore(t *testing.T) {
	c := New[int]()
	c.Set("a", 1, 0)
	c.Set("b", 2, -time.Second)
	if _, ok := c.Get("a"); ok {
		t.Fatal("ttl 0 must not store")
	}
	if _, ok := c.Get("b"); ok {
		t.Fatal("negative ttl must not store")
	}
}

func TestEvictExpired(t *testing.T) {
	c := New[int]()
	now := time.Unix(0, 0)
	c.Clock = func() time.Time { return now }
	c.Set("keep", 1, time.Hour)
	c.Set("drop", 2, time.Minute)

	now = now.Add(2 * time.Minute)
	c.evictExpired()
	if _, ok := c.Get("drop"); ok {
		t.Fatal("expired entry should be evicted")
	}
	if _, ok := c.Get("keep"); !ok {
		t.Fatal("live entry should remain")
	}
}
