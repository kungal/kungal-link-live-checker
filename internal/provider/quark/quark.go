// Package quark is the Quark netdisk (pan.quark.cn) checker. Its status API and
// code mapping are verified against production (docs/PROVIDERS.md, 2026-06-13).
package quark

import (
	"log/slog"
	"net/http"

	"github.com/KunMoe/kungal-link-live-checker/internal/provider/quarkfamily"
)

// Options tunes the Quark checker.
type Options struct {
	Client        *http.Client
	Logger        *slog.Logger
	BlockedAsDead bool
}

// New builds the verified Quark checker.
func New(opts Options) *quarkfamily.Checker {
	return quarkfamily.New(quarkfamily.Config{
		Name:          "quark",
		Hosts:         []string{"pan.quark.cn"},
		TokenURL:      "https://drive-pc.quark.cn/1/clouddrive/share/sharepage/token?pr=ucpro&fr=pc",
		Referer:       "https://pan.quark.cn/",
		BlockedAsDead: opts.BlockedAsDead,
		Client:        opts.Client,
		Logger:        opts.Logger,
	})
}
