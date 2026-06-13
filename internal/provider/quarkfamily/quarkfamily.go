// Package quarkfamily implements the shared share-status probe for Quark and
// the (homologous) UC netdisk. Both expose the same clouddrive token API; only
// the host and pr/fr query params differ (docs/PROVIDERS.md).
//
// Iron law (CLAUDE.md): a code maps to Dead only when it is a *verified*
// gone/blocked signal. Every unrecognized or merely community-rumored code
// stays Unknown until confirmed against a known-dead share.
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

// Config configures one provider instance built on the Quark-family API.
type Config struct {
	Name          string   // provider identifier, e.g. "quark"
	Hosts         []string // hostnames this checker matches (exact, lowercase)
	TokenURL      string   // full sharepage/token endpoint incl. pr/fr query
	Referer       string   // Referer header the API expects
	Origin        string   // optional Origin header (UC requires it)
	UserAgent     string   // optional; a browser-like default is used if empty
	BlockedAsDead bool     // map "blocked/violation" codes to dead vs unknown
	Client        *http.Client
	Logger        *slog.Logger
}

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// Checker probes a Quark-family share endpoint.
type Checker struct {
	cfg Config
}

// New builds a Checker, filling in sensible defaults.
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

// goneDeadCodes are VERIFIED "the share no longer exists" signals — probed
// against real (now-dead) shares from the kungal forum DB on 2026-06-13; see
// docs/PROVIDERS.md for the evidence. Mapping a code here is the only way a
// verdict becomes Dead for a definitively-gone share.
var goneDeadCodes = map[int]string{
	41004: checker.ReasonShareNotFound, // 文件不存在
	41006: checker.ReasonShareNotFound, // 分享不存在
	41011: checker.ReasonShareExpired,  // 分享地址已失效
	41012: checker.ReasonShareNotFound, // 好友已取消了分享
}

// blockedCodes are violation/ban takedowns: the share is inaccessible to the
// user, but this is a moderation action rather than a plain deletion. Dead by
// default, downgradable to Unknown via BlockedAsDead=false (REQUIREMENTS §3.3
// / §10 open question #1).
var blockedCodes = map[int]bool{
	41010: true, // 文件涉及违规内容
	41031: true, // 分享者用户封禁链接查看受限
}

// Check probes the share. passcode may be empty; if so we fall back to the
// URL's ?pwd= query.
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

	// The meaningful signal is in the JSON body's code, NOT the HTTP status
	// (41008 arrives as 404, 41031 as 403), so we parse the body regardless.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return checker.Unknown(checker.ReasonNetworkError, "")
	}

	var tr tokenResp
	if err := json.Unmarshal(raw, &tr); err != nil {
		// SPA shell, HTML anti-bot page, or a changed API — never Dead.
		return checker.Unknown(checker.ReasonUnparseable, "")
	}
	return c.mapCode(tr)
}

func (c *Checker) mapCode(tr tokenResp) checker.Verdict {
	code := strconv.Itoa(tr.Code)
	switch tr.Code {
	case 0:
		// A genuine OK always carries an stoken; code 0 without one is some
		// other JSON envelope we don't understand — stay Unknown, not Alive.
		if strings.TrimSpace(tr.Data.Stoken) == "" {
			c.cfg.Logger.Warn("code 0 without stoken; treating as unknown", "provider", c.cfg.Name)
			return checker.Unknown(checker.ReasonUnparseable, code)
		}
		return checker.Alive(checker.ReasonShareOK, code)
	case 41008:
		// Missing or wrong passcode — never dead; caller should retry with one.
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

	// Unrecognized code: possible API drift — stay Unknown, never Dead, and log
	// loudly so a spike surfaces the change.
	c.cfg.Logger.Warn("unrecognized provider code; treating as unknown (possible API drift)",
		"provider", c.cfg.Name, "code", tr.Code, "message", tr.Message)
	return checker.Unknown(checker.ReasonUnparseable, code)
}
