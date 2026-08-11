package checker

import (
	"context"
	"net/url"
	"time"
)

type Status string

const (
	StatusAlive   Status = "alive"
	StatusDead    Status = "dead"
	StatusUnknown Status = "unknown"
)

const (
	ReasonShareOK = "share_ok"

	ReasonShareNotFound = "share_not_found"
	ReasonShareExpired  = "share_expired"
	ReasonShareBlocked  = "share_blocked"

	ReasonPasscodeRequired = "passcode_required"
	ReasonRateLimited      = "rate_limited"
	ReasonCaptchaRequired  = "captcha_required"
	ReasonLoginRequired    = "login_required"
	ReasonTimeout          = "timeout"
	ReasonNetworkError     = "network_error"
	ReasonUnsupported      = "unsupported_provider"
	ReasonUnparseable      = "unparseable_response"
)

type Verdict struct {
	Status       Status
	Reason       string
	ProviderCode string
}

func Alive(reason, code string) Verdict   { return Verdict{StatusAlive, reason, code} }
func Dead(reason, code string) Verdict    { return Verdict{StatusDead, reason, code} }
func Unknown(reason, code string) Verdict { return Verdict{StatusUnknown, reason, code} }

type Checker interface {
	Name() string
	Matches(u *url.URL) bool
	Check(ctx context.Context, u *url.URL, passcode string) Verdict
}

type Result struct {
	Provider     string    `json:"provider"`
	Status       Status    `json:"status"`
	Reason       string    `json:"reason"`
	ProviderCode string    `json:"providerCode,omitempty"`
	CheckedAt    time.Time `json:"checkedAt"`
	Cached       bool      `json:"cached"`
}
