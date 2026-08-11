package quark

import (
	"log/slog"
	"net/http"

	"github.com/KunMoe/kungal-link-live-checker/internal/provider/quarkfamily"
)

type Options struct {
	Client        *http.Client
	Logger        *slog.Logger
	BlockedAsDead bool
	VerifyDetail  bool
}

func New(opts Options) *quarkfamily.Checker {
	detailURL := ""
	if opts.VerifyDetail {
		detailURL = "https://drive-pc.quark.cn/1/clouddrive/share/sharepage/detail?pr=ucpro&fr=pc"
	}
	return quarkfamily.New(quarkfamily.Config{
		Name:          "quark",
		Hosts:         []string{"pan.quark.cn"},
		TokenURL:      "https://drive-pc.quark.cn/1/clouddrive/share/sharepage/token?pr=ucpro&fr=pc",
		DetailURL:     detailURL,
		Referer:       "https://pan.quark.cn/",
		BlockedAsDead: opts.BlockedAsDead,
		Client:        opts.Client,
		Logger:        opts.Logger,
	})
}
