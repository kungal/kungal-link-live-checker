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
	Errno   *int   `json:"errno"` // pointer: a missing errno must NOT default to 0/alive
	ShowMsg string `json:"show_msg"`
}

// Check probes the share via shorturlinfo, then resolves the ambiguous -9
// (passcode-protected) case with the share page. The passcode (arg or ?pwd=) is
// only used to build the page URL.
func (c *Checker) Check(ctx context.Context, u *url.URL, passcode string) checker.Verdict {
	short := extractShort(u)
	if short == "" {
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	if passcode == "" {
		passcode = u.Query().Get("pwd")
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
	// shorturlinfo's -9 (passcode-protected) is ambiguous: a DELETED passcode
	// share and a LIVE passcode share both return -9 (and Baidu even flips a dead
	// share between -9 and -21 over time). Resolve it with the share page, which
	// renders a server-side (non-SPA) sentinel either way. See docs/PROVIDERS.md.
	if info.Errno != nil && *info.Errno == -9 {
		return c.pageVerdict(ctx, short, passcode)
	}
	return c.mapErrno(info)
}

// pageVerdict resolves the ambiguous -9 case by reading the share page. Both
// sentinels are server-rendered and mutually exclusive (verified across dead and
// live passcode shares), so this yields neither false-dead nor false-alive; any
// other page stays unknown.
func (c *Checker) pageVerdict(ctx context.Context, short, passcode string) checker.Verdict {
	pageURL := c.apiBase + "/s/" + short
	if passcode != "" {
		pageURL += "?pwd=" + url.QueryEscape(passcode)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return checker.Unknown(checker.ReasonPasscodeRequired, "-9")
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return checker.Unknown(checker.ReasonTimeout, "-9")
		}
		return checker.Unknown(checker.ReasonNetworkError, "-9")
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return checker.Unknown(checker.ReasonPasscodeRequired, "-9")
	}
	body := string(raw)
	switch {
	case strings.Contains(body, "链接不存在"):
		return checker.Dead(checker.ReasonShareNotFound, "-9/page")
	case strings.Contains(body, "请输入提取码"):
		// The passcode-entry page renders => the share exists (locked). Per the
		// project decision, existing-but-locked is alive.
		return checker.Alive(checker.ReasonShareOK, "-9/page")
	default:
		return checker.Unknown(checker.ReasonPasscodeRequired, "-9")
	}
}

func (c *Checker) mapErrno(info shortURLInfo) checker.Verdict {
	if info.Errno == nil {
		// Valid JSON but no errno field — an anti-bot / changed-API envelope.
		// Never Alive on a missing code.
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	errno := *info.Errno
	code := strconv.Itoa(errno)
	switch errno {
	case 0:
		// Public share, no passcode — confirmed accessible.
		return checker.Alive(checker.ReasonShareOK, code)
	case -9:
		// Passcode-protected. shorturlinfo will NOT reveal the real state without
		// the passcode and returns -9 for dead-locked shares too (a known-dead
		// link was observed returning -9, then -21). So -9 confirms nothing —
		// Unknown, not Alive. Unlike caiyun/123pan, Baidu has no distinct
		// "not found" message at this layer to separate dead-locked from
		// alive-locked. See docs/PROVIDERS.md.
		return checker.Unknown(checker.ReasonPasscodeRequired, code)
	case -21:
		// Verified: the share page renders "百度网盘-链接不存在" — gone.
		return checker.Dead(checker.ReasonShareNotFound, code)
	default:
		// errno 140 (malformed link) and anything else: not a verified signal.
		c.logger.Warn("unrecognized baidu errno; treating as unknown (possible anti-crawl or API drift)",
			"errno", errno, "show_msg", info.ShowMsg)
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
