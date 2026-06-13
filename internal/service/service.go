// Package service orchestrates a link check: provider dispatch, caching, and
// per-provider rate limiting around the conservative Checker verdict. It carries
// no downstream business logic (no failure counts / notifications / thresholds).
package service

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/KunMoe/kungal-link-live-checker/internal/cache"
	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
	"github.com/KunMoe/kungal-link-live-checker/internal/ratelimit"
)

// Options configures cache TTLs and the per-check timeout.
type Options struct {
	CheckTimeout time.Duration
	TTLAlive     time.Duration
	TTLDead      time.Duration
	TTLUnknown   time.Duration
}

// cached is the cache value; mirrors a Result minus the Cached flag.
type cached struct {
	provider     string
	status       checker.Status
	reason       string
	providerCode string
	checkedAt    time.Time
}

// Service resolves a URL to a conservative Result.
type Service struct {
	registry *checker.Registry
	cache    *cache.Cache[cached]
	limiters *ratelimit.Registry
	opts     Options
	log      *slog.Logger

	// Clock is overridable in tests; defaults to time.Now.
	Clock func() time.Time
}

// New wires a Service. It owns an internal cache.
func New(reg *checker.Registry, lim *ratelimit.Registry, opts Options, log *slog.Logger) *Service {
	return &Service{
		registry: reg,
		cache:    cache.New[cached](),
		limiters: lim,
		opts:     opts,
		log:      log,
		Clock:    time.Now,
	}
}

// RunJanitor evicts expired cache entries until ctx is done.
func (s *Service) RunJanitor(ctx context.Context, interval time.Duration) {
	s.cache.Janitor(ctx, interval)
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

// Check resolves rawURL (with an optional passcode) to a conservative Result.
func (s *Service) Check(ctx context.Context, rawURL, passcode string) checker.Result {
	start := s.now()

	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return s.result("unknown", checker.Unknown(checker.ReasonUnsupported, ""))
	}

	ck := s.registry.Match(u)
	if ck == nil {
		return s.result("unknown", checker.Unknown(checker.ReasonUnsupported, ""))
	}

	key := cacheKey(u, passcode)
	if hit, ok := s.cache.Get(key); ok {
		s.log.Info("check", "provider", hit.provider, "status", hit.status,
			"reason", hit.reason, "code", hit.providerCode, "cached", true)
		return checker.Result{
			Provider: hit.provider, Status: hit.status, Reason: hit.reason,
			ProviderCode: hit.providerCode, CheckedAt: hit.checkedAt, Cached: true,
		}
	}

	cctx, cancel := context.WithTimeout(ctx, s.opts.CheckTimeout)
	defer cancel()

	if err := s.limiters.For(ck.Name()).Wait(cctx); err != nil {
		// The per-check deadline cut off the wait before a token was free.
		return s.result(ck.Name(), checker.Unknown(checker.ReasonTimeout, ""))
	}

	verdict := ck.Check(cctx, u, passcode)
	res := s.result(ck.Name(), verdict)

	if ttl := s.ttlFor(verdict.Status); ttl > 0 {
		s.cache.Set(key, cached{
			provider: res.Provider, status: res.Status, reason: res.Reason,
			providerCode: res.ProviderCode, checkedAt: res.CheckedAt,
		}, ttl)
	}

	s.log.Info("check", "provider", res.Provider, "status", res.Status,
		"reason", res.Reason, "code", res.ProviderCode, "cached", false,
		"dur_ms", s.now().Sub(start).Milliseconds())
	return res
}

func (s *Service) result(provider string, v checker.Verdict) checker.Result {
	return checker.Result{
		Provider: provider, Status: v.Status, Reason: v.Reason,
		ProviderCode: v.ProviderCode, CheckedAt: s.now(), Cached: false,
	}
}

func (s *Service) ttlFor(st checker.Status) time.Duration {
	switch st {
	case checker.StatusAlive:
		return s.opts.TTLAlive
	case checker.StatusDead:
		return s.opts.TTLDead
	default:
		return s.opts.TTLUnknown
	}
}

// cacheKey is the normalized URL plus whether a passcode was supplied
// (REQUIREMENTS §5). Unknown verdicts are short-/un-cached, so a wrong-passcode
// miss never poisons a later correct-passcode lookup.
func cacheKey(u *url.URL, passcode string) string {
	host := strings.ToLower(u.Hostname())
	path := strings.TrimRight(u.Path, "/")
	hasPC := passcode != "" || u.Query().Get("pwd") != ""
	pc := "0"
	if hasPC {
		pc = "1"
	}
	return host + path + "|pc=" + pc
}
