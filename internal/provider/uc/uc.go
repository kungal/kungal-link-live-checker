// Package uc is the UC netdisk (drive.uc.cn) checker. UC is homologous to Quark
// and shares the same clouddrive token API and code mapping; only the host
// (pc-api.uc.cn) and query params (pr=UCBrowser) differ. Verified against the
// live API on 2026-06-13 (docs/PROVIDERS.md). UC also requires an Origin header.
package uc

import (
	"log/slog"
	"net/http"

	"github.com/KunMoe/kungal-link-live-checker/internal/provider/quarkfamily"
)

// Options tunes the UC checker.
type Options struct {
	TokenURL      string // unverified default lives in config; override when confirmed
	Client        *http.Client
	Logger        *slog.Logger
	BlockedAsDead bool
}

// New builds the (experimental) UC checker on the shared Quark-family engine.
func New(opts Options) *quarkfamily.Checker {
	return quarkfamily.New(quarkfamily.Config{
		Name:          "uc",
		Hosts:         []string{"drive.uc.cn"},
		TokenURL:      opts.TokenURL,
		Referer:       "https://drive.uc.cn/",
		Origin:        "https://drive.uc.cn",
		BlockedAsDead: opts.BlockedAsDead,
		Client:        opts.Client,
		Logger:        opts.Logger,
	})
}
