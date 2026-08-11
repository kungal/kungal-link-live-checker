package quarkfamily

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
	"strconv"
	"strings"

	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
)

type Config struct {
	Name          string
	Hosts         []string
	TokenURL      string
	DetailURL     string
	Referer       string
	Origin        string
	UserAgent     string
	BlockedAsDead bool
	Client        *http.Client
	Logger        *slog.Logger
}

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

type Checker struct {
	cfg Config
}

func New(cfg Config) *Checker {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUserAgent
	}
	return &Checker{cfg: cfg}
}

func (c *Checker) Name() string { return c.cfg.Name }

func (c *Checker) Matches(u *url.URL) bool {
	return slices.Contains(c.cfg.Hosts, strings.ToLower(u.Hostname()))
}

var pwdIDRe = regexp.MustCompile(`/s/([0-9A-Za-z]+)`)

func extractPwdID(u *url.URL) string {
	m := pwdIDRe.FindStringSubmatch(u.Path)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

type tokenResp struct {
	Status  int    `json:"status"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Stoken string `json:"stoken"`
	} `json:"data"`
}

// Each code here was probed against a real share known to be gone. Codes that
// docs or community posts merely claim to mean "deleted" (41027 was one) do not
// belong here until measured.
var goneDeadCodes = map[int]string{
	41004: checker.ReasonShareNotFound,
	41006: checker.ReasonShareNotFound,
	41011: checker.ReasonShareExpired,
	41012: checker.ReasonShareNotFound,
}

var blockedCodes = map[int]bool{
	41010: true,
	41031: true,
}

func (c *Checker) Check(ctx context.Context, u *url.URL, passcode string) checker.Verdict {
	pwdID := extractPwdID(u)
	if pwdID == "" {
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	if passcode == "" {
		passcode = u.Query().Get("pwd")
	}

	reqBody, _ := json.Marshal(map[string]string{"pwd_id": pwdID, "passcode": passcode})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, bytes.NewReader(reqBody))
	if err != nil {
		return checker.Unknown(checker.ReasonNetworkError, "")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	if c.cfg.Referer != "" {
		req.Header.Set("Referer", c.cfg.Referer)
	}
	if c.cfg.Origin != "" {
		req.Header.Set("Origin", c.cfg.Origin)
	}

	resp, err := c.cfg.Client.Do(req)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return checker.Unknown(checker.ReasonTimeout, "")
		}
		return checker.Unknown(checker.ReasonNetworkError, "")
	}
	defer resp.Body.Close()

	// The verdict is in the JSON body, not the HTTP status: 41008 arrives as 404
	// and 41031 as 403, so branching on resp.StatusCode reads a live share as
	// gone. Parse the body regardless of status.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return checker.Unknown(checker.ReasonNetworkError, "")
	}

	var tr tokenResp
	if err := json.Unmarshal(raw, &tr); err != nil {
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	v := c.mapCode(tr)
	// A share taken down for violation still answers the token call with code 0
	// and a valid stoken: the token step only proves the passcode was accepted,
	// not that anything is left to download. pan.quark.cn/s/2fce2f1d6d91 was
	// reported alive here while the browser showed "该分享已失效，不可访问".
	if v.Status == checker.StatusAlive && c.cfg.DetailURL != "" {
		return c.verifyDetail(ctx, pwdID, tr.Data.Stoken)
	}
	return v
}

type detailResp struct {
	Code int `json:"code"`
	Data struct {
		Share struct {
			PartialViolation bool `json:"partial_violation"`
		} `json:"share"`
	} `json:"data"`
}

// PartialViolation is deliberately not mapped to Dead. Measured against real
// shares, it covers both "every file is gone" and "some files are gone, the
// rest still list and download", and nothing in this response separates the
// two. Unknown keeps the report gate honest without ever killing a live link.
func (c *Checker) verifyDetail(ctx context.Context, pwdID, stoken string) checker.Verdict {
	q := url.Values{
		"pwd_id":       {pwdID},
		"stoken":       {stoken},
		"pdir_fid":     {"0"},
		"_page":        {"1"},
		"_size":        {"1"},
		"_fetch_share": {"1"},
	}
	sep := "&"
	if !strings.Contains(c.cfg.DetailURL, "?") {
		sep = "?"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.DetailURL+sep+q.Encode(), nil)
	if err != nil {
		return checker.Unknown(checker.ReasonNetworkError, "")
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	if c.cfg.Referer != "" {
		req.Header.Set("Referer", c.cfg.Referer)
	}
	if c.cfg.Origin != "" {
		req.Header.Set("Origin", c.cfg.Origin)
	}

	resp, err := c.cfg.Client.Do(req)
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
	var dr detailResp
	if err := json.Unmarshal(raw, &dr); err != nil {
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	if dr.Code != 0 {
		c.cfg.Logger.Warn("share detail returned a non-zero code; treating as unknown",
			"provider", c.cfg.Name, "code", dr.Code)
		return checker.Unknown(checker.ReasonUnparseable, strconv.Itoa(dr.Code))
	}
	if dr.Data.Share.PartialViolation {
		return checker.Unknown(checker.ReasonShareBlocked, "0/partial_violation")
	}
	return checker.Alive(checker.ReasonShareOK, "0")
}

func (c *Checker) mapCode(tr tokenResp) checker.Verdict {
	code := strconv.Itoa(tr.Code)
	switch tr.Code {
	case 0:
		// A real OK always carries an stoken. Code 0 without one is some other
		// envelope, so trusting the code alone reports a false alive.
		if strings.TrimSpace(tr.Data.Stoken) == "" {
			c.cfg.Logger.Warn("code 0 without stoken; treating as unknown", "provider", c.cfg.Name)
			return checker.Unknown(checker.ReasonUnparseable, code)
		}
		return checker.Alive(checker.ReasonShareOK, code)
	case 41008:
		return checker.Unknown(checker.ReasonPasscodeRequired, code)
	}

	if reason, ok := goneDeadCodes[tr.Code]; ok {
		return checker.Dead(reason, code)
	}
	if blockedCodes[tr.Code] {
		if c.cfg.BlockedAsDead {
			return checker.Dead(checker.ReasonShareBlocked, code)
		}
		return checker.Unknown(checker.ReasonShareBlocked, code)
	}

	c.cfg.Logger.Warn("unrecognized provider code; treating as unknown (possible API drift)",
		"provider", c.cfg.Name, "code", tr.Code, "message", tr.Message)
	return checker.Unknown(checker.ReasonUnparseable, code)
}
