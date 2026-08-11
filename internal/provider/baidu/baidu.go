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

type Options struct {
	Client    *http.Client
	APIBase   string
	UserAgent string
	Logger    *slog.Logger
}

type Checker struct {
	client    *http.Client
	apiBase   string
	userAgent string
	logger    *slog.Logger
}

// The shared client is rewrapped with a private cookie jar: without a BAIDUID
// cookie, shorturlinfo 302s to an anti-bot page instead of returning JSON.
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

var shortRe = regexp.MustCompile(`/s/(1[A-Za-z0-9_-]+)`)

func extractShort(u *url.URL) string {
	m := shortRe.FindStringSubmatch(u.Path)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

type shortURLInfo struct {
	// A pointer, because an anti-bot envelope carries no errno at all and a
	// plain int would decode that as 0, which this checker reads as alive.
	Errno   *int   `json:"errno"`
	ShowMsg string `json:"show_msg"`
}

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
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	// errno -9 was first read as "locked, therefore alive". It is not: a deleted
	// passcode share and a live passcode share both return -9, and one dead link
	// was observed flipping between -9 and -21 across probes. Shipping -9 as
	// alive passed dead links through the report gate as false reports.
	if info.Errno != nil && *info.Errno == -9 {
		return c.pageVerdict(ctx, short, passcode)
	}
	return c.mapErrno(info)
}

// The share page is server-rendered, so its two sentinels survive where the API
// is ambiguous. Neither sentinel present stays unknown.
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
		return checker.Alive(checker.ReasonShareOK, "-9/page")
	default:
		return checker.Unknown(checker.ReasonPasscodeRequired, "-9")
	}
}

func (c *Checker) mapErrno(info shortURLInfo) checker.Verdict {
	if info.Errno == nil {
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	errno := *info.Errno
	code := strconv.Itoa(errno)
	switch errno {
	case 0:
		return checker.Alive(checker.ReasonShareOK, code)
	case -9:
		return checker.Unknown(checker.ReasonPasscodeRequired, code)
	case -21:
		return checker.Dead(checker.ReasonShareNotFound, code)
	default:
		c.logger.Warn("unrecognized baidu errno; treating as unknown (possible anti-crawl or API drift)",
			"errno", errno, "show_msg", info.ShowMsg)
		return checker.Unknown(checker.ReasonUnparseable, code)
	}
}

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
