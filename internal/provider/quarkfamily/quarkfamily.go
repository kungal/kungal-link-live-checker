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
	return c.mapCode(tr)
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
