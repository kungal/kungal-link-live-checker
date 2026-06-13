package service

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
	"github.com/KunMoe/kungal-link-live-checker/internal/ratelimit"
)

type fakeChecker struct {
	host    string
	verdict checker.Verdict
	calls   int
}

func (f *fakeChecker) Name() string            { return "fake" }
func (f *fakeChecker) Matches(u *url.URL) bool { return strings.EqualFold(u.Hostname(), f.host) }
func (f *fakeChecker) Check(_ context.Context, _ *url.URL, _ string) checker.Verdict {
	f.calls++
	return f.verdict
}

func newService(fc *fakeChecker, opts Options) *Service {
	reg := checker.NewRegistry(fc)
	lim := ratelimit.NewRegistry(0, 1) // disabled
	return New(reg, lim, opts, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestUnsupportedProvider(t *testing.T) {
	fc := &fakeChecker{host: "pan.quark.cn", verdict: checker.Alive(checker.ReasonShareOK, "0")}
	svc := newService(fc, Options{CheckTimeout: time.Second})

	res := svc.Check(context.Background(), "https://mega.nz/whatever", "")
	if res.Provider != "unknown" || res.Status != checker.StatusUnknown || res.Reason != checker.ReasonUnsupported {
		t.Fatalf("got %+v, want unknown/unsupported_provider", res)
	}
	if fc.calls != 0 {
		t.Fatal("non-matching provider should not be invoked")
	}
}

func TestGarbageURL(t *testing.T) {
	fc := &fakeChecker{host: "pan.quark.cn"}
	svc := newService(fc, Options{CheckTimeout: time.Second})
	res := svc.Check(context.Background(), "not a url", "")
	if res.Status != checker.StatusUnknown || res.Reason != checker.ReasonUnsupported {
		t.Fatalf("got %+v, want unknown/unsupported_provider", res)
	}
}

func TestAliveIsCached(t *testing.T) {
	fc := &fakeChecker{host: "pan.quark.cn", verdict: checker.Alive(checker.ReasonShareOK, "0")}
	svc := newService(fc, Options{CheckTimeout: time.Second, TTLAlive: time.Hour})

	first := svc.Check(context.Background(), "https://pan.quark.cn/s/abc", "")
	if first.Cached || fc.calls != 1 {
		t.Fatalf("first call: cached=%v calls=%d", first.Cached, fc.calls)
	}
	second := svc.Check(context.Background(), "https://pan.quark.cn/s/abc", "")
	if !second.Cached || fc.calls != 1 {
		t.Fatalf("second call should be served from cache: cached=%v calls=%d", second.Cached, fc.calls)
	}
}

func TestUnknownIsNotCachedWhenTTLZero(t *testing.T) {
	fc := &fakeChecker{host: "pan.quark.cn", verdict: checker.Unknown(checker.ReasonTimeout, "")}
	svc := newService(fc, Options{CheckTimeout: time.Second, TTLUnknown: 0})

	svc.Check(context.Background(), "https://pan.quark.cn/s/abc", "")
	svc.Check(context.Background(), "https://pan.quark.cn/s/abc", "")
	if fc.calls != 2 {
		t.Fatalf("unknown with ttl 0 must not cache; calls=%d want 2", fc.calls)
	}
}

func TestCacheKeyDistinguishesPasscodePresence(t *testing.T) {
	u, _ := url.Parse("https://pan.quark.cn/s/abc")
	withPC, _ := url.Parse("https://pan.quark.cn/s/abc?pwd=x")
	if cacheKey(u, "") == cacheKey(u, "code") {
		t.Fatal("passcode presence should change the cache key")
	}
	if cacheKey(u, "") == cacheKey(withPC, "") {
		t.Fatal("URL ?pwd= should count as passcode-present")
	}
	if cacheKey(u, "") != cacheKey(mustParse("https://pan.quark.cn/s/abc/"), "") {
		t.Fatal("trailing slash should normalize to the same key")
	}
}

// Regression: caiyun-style URLs carry the share id in the query or fragment,
// not the path. Distinct shares must not collide on one cache key.
func TestCacheKeyDistinguishesByQueryAndFragment(t *testing.T) {
	q1 := mustParse("https://caiyun.139.com/m/i?0a5CfleicjAWV")
	q2 := mustParse("https://caiyun.139.com/m/i?0a5Cg5dWZPEYs")
	if cacheKey(q1, "") == cacheKey(q2, "") {
		t.Fatal("different query share ids must yield different keys")
	}
	f1 := mustParse("https://caiyun.139.com/front/#/detail?linkID=AAA")
	f2 := mustParse("https://caiyun.139.com/front/#/detail?linkID=BBB")
	if cacheKey(f1, "") == cacheKey(f2, "") {
		t.Fatal("different fragment share ids must yield different keys")
	}
}

func mustParse(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}
