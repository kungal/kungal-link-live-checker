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

type Options struct {
	CheckTimeout time.Duration
	TTLAlive     time.Duration
	TTLDead      time.Duration
	TTLUnknown   time.Duration
}

type cached struct {
	provider     string
	status       checker.Status
	reason       string
	providerCode string
	checkedAt    time.Time
}

type Service struct {
	registry *checker.Registry
	cache    *cache.Cache[cached]
	limiters *ratelimit.Registry
	opts     Options
	log      *slog.Logger

	Clock func() time.Time
}

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

func (s *Service) RunJanitor(ctx context.Context, interval time.Duration) {
	s.cache.Janitor(ctx, interval)
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

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

// Not every provider puts the share id in the path. caiyun uses the bare query
// (/m/i?<id>) and the SPA fragment (front/#/detail?linkID=<id>), so a key built
// from host+path alone collapses every caiyun share onto one entry and serves
// one share's verdict for another.
func cacheKey(u *url.URL, passcode string) string {
	host := strings.ToLower(u.Hostname())
	path := strings.TrimRight(u.Path, "/")

	q := u.Query()
	q.Del("pwd")
	ident := q.Encode()

	hasPC := passcode != "" || u.Query().Get("pwd") != ""
	pc := "0"
	if hasPC {
		pc = "1"
	}
	return host + path + "?" + ident + "#" + u.Fragment + "|pc=" + pc
}
