package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestAllowBurstThenRefill(t *testing.T) {
	l := NewLimiter(1, 2) // 1 token/s, burst 2
	now := time.Unix(0, 0)
	l.clock = func() time.Time { return now }
	l.last = now
	l.tokens = 2

	if !l.Allow() {
		t.Fatal("first burst token should be allowed")
	}
	if !l.Allow() {
		t.Fatal("second burst token should be allowed")
	}
	if l.Allow() {
		t.Fatal("third immediate request should be denied")
	}

	now = now.Add(time.Second) // one token refilled
	if !l.Allow() {
		t.Fatal("after 1s a token should be available")
	}
}

func TestDisabledLimiterAlwaysAllows(t *testing.T) {
	l := NewLimiter(0, 1) // rps <= 0 disables limiting
	for i := range 100 {
		if !l.Allow() {
			t.Fatalf("disabled limiter denied at i=%d", i)
		}
	}
}

func TestWaitRespectsContext(t *testing.T) {
	l := NewLimiter(0.001, 1) // very slow refill
	l.tokens = 0              // force a wait

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done
	if err := l.Wait(ctx); err == nil {
		t.Fatal("Wait should return the context error when ctx is done")
	}
}

func TestRegistryReusesPerName(t *testing.T) {
	r := NewRegistry(5, 5)
	a1, a2 := r.For("quark"), r.For("quark")
	if a1 != a2 {
		t.Fatal("same provider name should return the same limiter")
	}
	if r.For("uc") == a1 {
		t.Fatal("different provider names should get distinct limiters")
	}
}
