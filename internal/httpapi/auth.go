package httpapi

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
)

// authenticator enforces s2s auth: a Bearer token must match one of the
// configured API keys. Fails closed — with no keys configured, every /v1
// request is rejected (the service is never anonymously open).
type authenticator struct {
	keys [][]byte
	log  *slog.Logger
}

func newAuthenticator(keys []string, log *slog.Logger) *authenticator {
	a := &authenticator{log: log}
	for _, k := range keys {
		if k != "" {
			a.keys = append(a.keys, []byte(k))
		}
	}
	if len(a.keys) == 0 {
		log.Warn("no API keys configured (LLC_API_KEYS empty): all /v1 requests will be rejected")
	}
	return a
}

func (a *authenticator) valid(token string) bool {
	tb := []byte(token)
	ok := false
	for _, k := range a.keys {
		if subtle.ConstantTimeCompare(tb, k) == 1 {
			ok = true
		}
	}
	return ok
}

func (a *authenticator) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok || !a.valid(token) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(h[len(prefix):]), true
}
