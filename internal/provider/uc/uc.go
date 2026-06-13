// Package uc is the UC netdisk (drive.uc.cn) checker. UC is homologous to Quark
// but its exact API host/params are NOT yet verified (docs/PROVIDERS.md): until
// confirmed it returns Unknown for anything it cannot positively read — never
// Dead. Enable via LLC_UC_ENABLED once the real endpoint is confirmed and set
// LLC_UC_TOKEN_URL accordingly.
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
		BlockedAsDead: opts.BlockedAsDead,
		Client:        opts.Client,
		Logger:        opts.Logger,
	})
}
