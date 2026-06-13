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
	UserAgent     string   // optional; a browser-like default is used if empty
	BlockedAsDead bool     // map the "blocked" code (41031) to dead vs unknown
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

// suspectedDeadCodes are codes the community associates with removed shares but
// which we have NOT verified against a known-dead share. Per the iron law they
// stay Unknown until confirmed and promoted in PROVIDERS.md; we only log them
// loudly so operators can capture the real "not found" code (PROVIDERS.md).
var suspectedDeadCodes = map[int]bool{41006: true, 41027: true}

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
		return checker.Unknown(checker.ReasonPasscodeRequired, code)
	case 41031:
		if c.cfg.BlockedAsDead {
			return checker.Dead(checker.ReasonShareBlocked, code)
		}
		return checker.Unknown(checker.ReasonShareBlocked, code)
	default:
		if suspectedDeadCodes[tr.Code] {
			c.cfg.Logger.Warn("unconfirmed suspected-dead code; treating as unknown (confirm in PROVIDERS.md)",
				"provider", c.cfg.Name, "code", tr.Code, "message", tr.Message)
		} else {
			c.cfg.Logger.Warn("unrecognized provider code; treating as unknown (possible API drift)",
				"provider", c.cfg.Name, "code", tr.Code, "message", tr.Message)
		}
		return checker.Unknown(checker.ReasonUnparseable, code)
	}
}
