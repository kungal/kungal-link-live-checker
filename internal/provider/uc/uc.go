package uc

import (
	"log/slog"
	"net/http"

	"github.com/KunMoe/kungal-link-live-checker/internal/provider/quarkfamily"
)

type Options struct {
	TokenURL      string
	Client        *http.Client
	Logger        *slog.Logger
	BlockedAsDead bool
}

// UC rejects the token call without an Origin header; Quark does not need one.
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
