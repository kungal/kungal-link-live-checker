// Package httpapi exposes the conservative link checker over HTTP with s2s auth.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
)

// CheckService is the behavior the HTTP layer needs from the core service.
type CheckService interface {
	Check(ctx context.Context, url, passcode string) checker.Result
}

// Server adapts a CheckService to HTTP handlers.
type Server struct {
	svc      CheckService
	auth     *authenticator
	batchMax int
	log      *slog.Logger
}

// NewServer builds the HTTP server façade.
func NewServer(svc CheckService, apiKeys []string, batchMax int, log *slog.Logger) *Server {
	return &Server{
		svc:      svc,
		auth:     newAuthenticator(apiKeys, log),
		batchMax: batchMax,
		log:      log,
	}
}

// Handler returns the routed http.Handler. /healthz is unauthenticated; all
// /v1 endpoints require a valid Bearer key.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.Handle("POST /v1/check", s.auth.middleware(http.HandlerFunc(s.handleCheck)))
	mux.Handle("POST /v1/check/batch", s.auth.middleware(http.HandlerFunc(s.handleBatch)))
	return mux
}

type checkRequest struct {
	URL      string `json:"url"`
	Passcode string `json:"passcode"`
}

type batchRequest struct {
	Items []checkRequest `json:"items"`
}

type batchResponse struct {
	Results []checker.Result `json:"results"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	var req checkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
		return
	}
	writeJSON(w, http.StatusOK, s.svc.Check(r.Context(), req.URL, req.Passcode))
}

func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	switch {
	case len(req.Items) == 0:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "items is required"})
		return
	case len(req.Items) > s.batchMax:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("too many items: max %d", s.batchMax)})
		return
	}
	results := make([]checker.Result, len(req.Items))
	for i, it := range req.Items {
		results[i] = s.svc.Check(r.Context(), it.URL, it.Passcode)
	}
	writeJSON(w, http.StatusOK, batchResponse{Results: results})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
