package pan123

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

type Options struct {
	Client    *http.Client
	UserAgent string
	Logger    *slog.Logger
}

type Checker struct {
	client    *http.Client
	userAgent string
	logger    *slog.Logger
}

func New(opts Options) *Checker {
	c := &Checker{client: opts.Client, userAgent: opts.UserAgent, logger: opts.Logger}
	if c.client == nil {
		c.client = http.DefaultClient
	}
	if c.userAgent == "" {
		c.userAgent = defaultUserAgent
	}
	if c.logger == nil {
		c.logger = slog.Default()
	}
	return c
}

func (c *Checker) Name() string { return "pan123" }

var (
	hostRe = regexp.MustCompile(`(?i)(^|\.)123(pan|684|865|912)\.(com|cn)$`)
	keyRe  = regexp.MustCompile(`/(?:s|123pan)/([A-Za-z0-9_-]+)`)
)

func (c *Checker) Matches(u *url.URL) bool {
	return hostRe.MatchString(strings.ToLower(u.Hostname()))
}

func extractKey(u *url.URL) string {
	m := keyRe.FindStringSubmatch(u.Path)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

type response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Checker) Check(ctx context.Context, u *url.URL, passcode string) checker.Verdict {
	key := extractKey(u)
	if key == "" {
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	if passcode == "" {
		passcode = u.Query().Get("pwd")
	}

	q := url.Values{
		"shareKey":       {key},
		"SharePwd":       {passcode},
		"limit":          {"1"},
		"next":           {"0"},
		"orderBy":        {"file_id"},
		"orderDirection": {"desc"},
		"parentFileId":   {"0"},
		"Page":           {"1"},
	}
	// Each mirror domain is its own deployment, so the API must be called on the
	// same host as the share link; a fixed 123pan.com base returns "not found"
	// for perfectly live 123912/123684 shares.
	api := "https://" + u.Host + "/b/api/share/get?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return checker.Unknown(checker.ReasonNetworkError, "")
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("platform", "web")
	req.Header.Set("app-version", "3")
	req.Header.Set("Referer", "https://"+u.Host+"/")
	req.Header.Set("Origin", "https://"+u.Host)

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
	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	return c.mapCode(r)
}

func (c *Checker) mapCode(r response) checker.Verdict {
	code := strconv.Itoa(r.Code)
	switch r.Code {
	case 0:
		return checker.Alive(checker.ReasonShareOK, code)
	case 5103:
		// 5103 is two different answers: "此分享不存在" and "提取码错误". Only the
		// message separates them, so mapping the code straight to dead kills
		// every passcode-locked share that is still perfectly alive.
		switch {
		case strings.Contains(r.Message, "不存在"):
			return checker.Dead(checker.ReasonShareNotFound, code)
		case strings.Contains(r.Message, "提取码"):
			return checker.Alive(checker.ReasonShareOK, code)
		default:
			c.logger.Warn("unrecognized 123pan 5103 message; treating as unknown",
				"message", r.Message)
			return checker.Unknown(checker.ReasonUnparseable, code)
		}
	default:
		c.logger.Warn("unrecognized 123pan code; treating as unknown (possible API drift)",
			"code", r.Code, "message", r.Message)
		return checker.Unknown(checker.ReasonUnparseable, code)
	}
}
