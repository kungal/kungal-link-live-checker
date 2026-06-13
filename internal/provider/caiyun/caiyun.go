// Package caiyun is the China Mobile Cloud (和彩云, caiyun.139.com) checker. It
// calls the getOutLinkInfoV6 share API, which is reachable in plaintext without
// login, and maps its code to the conservative three-state. Verified against
// real shares from the kungal forum DB on 2026-06-13 (docs/PROVIDERS.md).
package caiyun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
)

const (
	defaultAPIURL    = "https://share-kd-njs.yun.139.com/yun-share/richlifeApp/devapp/IOutLink/getOutLinkInfoV6"
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

// Options tunes the caiyun checker.
type Options struct {
	Client    *http.Client
	APIURL    string // override for tests
	UserAgent string
	Logger    *slog.Logger
}

// Checker probes caiyun shares via getOutLinkInfoV6.
type Checker struct {
	client    *http.Client
	apiURL    string
	userAgent string
	logger    *slog.Logger
}

// New builds the caiyun checker.
func New(opts Options) *Checker {
	c := &Checker{
		client:    opts.Client,
		apiURL:    opts.APIURL,
		userAgent: opts.UserAgent,
		logger:    opts.Logger,
	}
	if c.client == nil {
		c.client = http.DefaultClient
	}
	if c.apiURL == "" {
		c.apiURL = defaultAPIURL
	}
	if c.userAgent == "" {
		c.userAgent = defaultUserAgent
	}
	if c.logger == nil {
		c.logger = slog.Default()
	}
	return c
}

func (c *Checker) Name() string { return "caiyun" }

var caiyunHosts = []string{"caiyun.139.com", "yun.139.com"}

func (c *Checker) Matches(u *url.URL) bool {
	return slices.Contains(caiyunHosts, strings.ToLower(u.Hostname()))
}

var linkIDRe = regexp.MustCompile(`linkID=([A-Za-z0-9_-]+)`)

// extractLinkID handles the two share-URL shapes:
//
//	caiyun.139.com/m/i?<linkID>              (bare query)
//	caiyun.139.com/front/#/detail?linkID=<linkID>  (in the SPA fragment)
func extractLinkID(u *url.URL) string {
	if v := u.Query().Get("linkID"); v != "" {
		return v
	}
	if rq := u.RawQuery; rq != "" && !strings.Contains(rq, "=") {
		if i := strings.IndexByte(rq, '&'); i >= 0 {
			rq = rq[:i]
		}
		return rq
	}
	if m := linkIDRe.FindStringSubmatch(u.Fragment); len(m) >= 2 {
		return m[1]
	}
	return ""
}

type response struct {
	Code string `json:"code"`
	Desc string `json:"desc"`
}

// Check probes the share. caiyun's plaintext API does not take the passcode in
// a field we have verified, so passcode is unused; a passcode-locked but extant
// share already resolves to alive via code 9188.
func (c *Checker) Check(ctx context.Context, u *url.URL, _ string) checker.Verdict {
	linkID := extractLinkID(u)
	if linkID == "" {
		return checker.Unknown(checker.ReasonUnparseable, "")
	}

	reqBody, _ := json.Marshal(map[string]any{
		"getOutLinkInfoReq": map[string]any{"linkID": linkID, "pCaID": "root"},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return checker.Unknown(checker.ReasonNetworkError, "")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Referer", "https://yun.139.com/")

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
	switch r.Code {
	case "0":
		return checker.Alive(checker.ReasonShareOK, r.Code)
	case "9188":
		// 提取码非法 — the share exists but is passcode-locked. Per project
		// decision an existing-but-locked share is alive (the link is not dead).
		return checker.Alive(checker.ReasonShareOK, r.Code)
	case "200000727":
		// 外链不存在 / 外链被分享者取消
		return checker.Dead(checker.ReasonShareNotFound, r.Code)
	default:
		c.logger.Warn("unrecognized caiyun code; treating as unknown (possible API drift)",
			"code", r.Code, "desc", r.Desc)
		return checker.Unknown(checker.ReasonUnparseable, r.Code)
	}
}
