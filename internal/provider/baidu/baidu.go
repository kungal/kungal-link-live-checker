// Package baidu is the Baidu netdisk (pan.baidu.com) checker. Baidu 302-redirects
// cookieless requests, so the checker first warms up a BAIDUID cookie, then calls
// the public shorturlinfo JSON API (no login required) and maps its errno to the
// conservative three-state. Verified against real shares from the kungal forum DB
// on 2026-06-13 (docs/PROVIDERS.md).
package baidu

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
)

const (
	defaultAPIBase   = "https://pan.baidu.com"
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

// Options tunes the Baidu checker.
type Options struct {
	Client    *http.Client // shared client; its Transport/Timeout are reused
	APIBase   string       // override for tests; defaults to https://pan.baidu.com
	UserAgent string
	Logger    *slog.Logger
}

// Checker probes Baidu shares via shorturlinfo. It owns a cookie jar (Baidu
// needs a BAIDUID cookie) so it uses a dedicated client wrapping the shared
// transport.
type Checker struct {
	client    *http.Client
	apiBase   string
	userAgent string
	logger    *slog.Logger
}

// New builds the Baidu checker.
func New(opts Options) *Checker {
	base := opts.Client
	if base == nil {
		base = http.DefaultClient
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Transport: base.Transport, Timeout: base.Timeout, Jar: jar}

	c := &Checker{
		client:    client,
		apiBase:   opts.APIBase,
		userAgent: opts.UserAgent,
		logger:    opts.Logger,
	}
	if c.apiBase == "" {
		c.apiBase = defaultAPIBase
	}
	if c.userAgent == "" {
		c.userAgent = defaultUserAgent
	}
	if c.logger == nil {
		c.logger = slog.Default()
	}
	return c
}

func (c *Checker) Name() string { return "baidu" }

func (c *Checker) Matches(u *url.URL) bool {
	return strings.EqualFold(u.Hostname(), "pan.baidu.com")
}

// shortRe captures the share short-url, e.g. /s/1AbC… → 1AbC…
var shortRe = regexp.MustCompile(`/s/(1[A-Za-z0-9_-]+)`)

func extractShort(u *url.URL) string {
	m := shortRe.FindStringSubmatch(u.Path)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

type shortURLInfo struct {
	Errno   int    `json:"errno"`
	ShowMsg string `json:"show_msg"`
}

// Check probes the share. A passcode is not needed to tell alive from dead:
// shorturlinfo distinguishes "exists (errno 0/-9)" from "gone (errno -21)"
// without it.
func (c *Checker) Check(ctx context.Context, u *url.URL, _ string) checker.Verdict {
	short := extractShort(u)
	if short == "" {
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	c.ensureCookie(ctx)

	api := c.apiBase + "/api/shorturlinfo?app_id=250528&web=1&channel=chunlei&clienttype=0&shorturl=" + url.QueryEscape(short)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return checker.Unknown(checker.ReasonNetworkError, "")
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Referer", c.apiBase+"/s/"+short)

	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return checker.Unknown(checker.ReasonTimeout, "")
		}
		return checker.Unknown(checker.ReasonNetworkError, "")
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return checker.Unknown(checker.ReasonNetworkError, "")
	}
	var info shortURLInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		// Anti-bot HTML / redirect page instead of JSON — never Dead.
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	return c.mapErrno(info)
}

func (c *Checker) mapErrno(info shortURLInfo) checker.Verdict {
	code := strconv.Itoa(info.Errno)
	switch info.Errno {
	case 0, -9:
		// 0 = public share OK; -9 = exists but passcode-protected. Either way
		// the share link is live, so a "dead" report should be rejected.
		return checker.Alive(checker.ReasonShareOK, code)
	case -21:
		// Verified: the share page renders "页面不存在" — deleted/cancelled/expired.
		return checker.Dead(checker.ReasonShareNotFound, code)
	default:
		// errno 140 (malformed link) and anything else: not a verified dead
		// signal — stay Unknown and log so anti-crawl/API drift is visible.
		c.logger.Warn("unrecognized baidu errno; treating as unknown (possible anti-crawl or API drift)",
			"errno", info.Errno, "show_msg", info.ShowMsg)
		return checker.Unknown(checker.ReasonUnparseable, code)
	}
}

// ensureCookie warms up a BAIDUID cookie if the jar lacks one. Baidu sets it on
// any homepage hit; without it shorturlinfo 302s to an anti-bot page.
func (c *Checker) ensureCookie(ctx context.Context) {
	base, err := url.Parse(c.apiBase)
	if err != nil {
		return
	}
	for _, ck := range c.client.Jar.Cookies(base) {
		if ck.Name == "BAIDUID" {
			return
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+"/", nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}
