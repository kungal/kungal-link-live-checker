// Package checker defines the conservative three-state link verdict and the
// provider Checker contract. See docs/REQUIREMENTS.md §3 and CLAUDE.md for the
// iron law: only return Dead when a netdisk API *explicitly* says the share is
// gone; everything uncertain is Unknown, never Dead.
package checker

import (
	"context"
	"net/url"
	"time"
)

// Status is the conservative three-state verdict.
type Status string

const (
	StatusAlive   Status = "alive"
	StatusDead    Status = "dead"
	StatusUnknown Status = "unknown"
)

// Reason enumerates machine-readable causes. Keep in sync with
// docs/REQUIREMENTS.md §3.2.
const (
	// alive
	ReasonShareOK = "share_ok"

	// dead — only ever set on an explicit, verified upstream signal
	ReasonShareNotFound = "share_not_found"
	ReasonShareExpired  = "share_expired"
	ReasonShareBlocked  = "share_blocked"

	// unknown — the safe default for anything uncertain
	ReasonPasscodeRequired = "passcode_required"
	ReasonRateLimited      = "rate_limited"
	ReasonCaptchaRequired  = "captcha_required"
	ReasonLoginRequired    = "login_required"
	ReasonTimeout          = "timeout"
	ReasonNetworkError     = "network_error"
	ReasonUnsupported      = "unsupported_provider"
	ReasonUnparseable      = "unparseable_response"
)

// Verdict is a Checker's raw judgment, without service-level metadata.
type Verdict struct {
	Status       Status
	Reason       string
	ProviderCode string // upstream raw code/flag, for audit and drift detection
}

// Alive, Dead and Unknown are convenience constructors for a Verdict.
func Alive(reason, code string) Verdict   { return Verdict{StatusAlive, reason, code} }
func Dead(reason, code string) Verdict    { return Verdict{StatusDead, reason, code} }
func Unknown(reason, code string) Verdict { return Verdict{StatusUnknown, reason, code} }

// Checker is one netdisk provider's share-status probe. Implementations MUST
// honor the iron law: never return StatusDead unless the upstream API
// explicitly reports the share is gone / removed / cancelled / blocked.
type Checker interface {
	// Name is the stable provider identifier, e.g. "quark".
	Name() string
	// Matches reports whether this checker handles the given share URL.
	Matches(u *url.URL) bool
	// Check probes the share. passcode may be empty.
	Check(ctx context.Context, u *url.URL, passcode string) Verdict
}

// Result is the service-level outcome returned over the API (REQUIREMENTS §3).
type Result struct {
	Provider     string    `json:"provider"`
	Status       Status    `json:"status"`
	Reason       string    `json:"reason"`
	ProviderCode string    `json:"providerCode,omitempty"`
	CheckedAt    time.Time `json:"checkedAt"`
	Cached       bool      `json:"cached"`
}
