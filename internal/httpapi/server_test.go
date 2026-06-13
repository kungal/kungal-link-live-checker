package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
)

type stubService struct {
	res   checker.Result
	calls int
}

func (s *stubService) Check(_ context.Context, _, _ string) checker.Result {
	s.calls++
	return s.res
}

func newTestServer(stub CheckService) http.Handler {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(stub, []string{"secret"}, 50, log).Handler()
}

func TestHealthNeedsNoAuth(t *testing.T) {
	h := newTestServer(&stubService{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
}

func TestCheckRequiresAuth(t *testing.T) {
	stub := &stubService{}
	h := newTestServer(stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"https://pan.quark.cn/s/abc"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-auth status = %d, want 401", rec.Code)
	}
	if stub.calls != 0 {
		t.Fatal("service must not be called when unauthorized")
	}
}

func TestCheckHappyPath(t *testing.T) {
	stub := &stubService{res: checker.Result{
		Provider: "quark", Status: checker.StatusDead, Reason: checker.ReasonShareBlocked, ProviderCode: "41031",
	}}
	h := newTestServer(stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"https://pan.quark.cn/s/abc"}`))
	req.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got checker.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Provider != "quark" || got.Status != checker.StatusDead || got.Reason != checker.ReasonShareBlocked {
		t.Fatalf("got %+v", got)
	}
}

func TestCheckRejectsEmptyURL(t *testing.T) {
	h := newTestServer(&stubService{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"  "}`))
	req.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestBatchRespectsMax(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewServer(&stubService{res: checker.Result{Status: checker.StatusUnknown}}, []string{"secret"}, 2, log).Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/check/batch",
		strings.NewReader(`{"items":[{"url":"a"},{"url":"b"},{"url":"c"}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("over-limit batch status = %d, want 400", rec.Code)
	}
}

func TestNoKeysConfiguredRejectsAll(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewServer(&stubService{}, nil, 50, log).Handler() // no keys -> fail closed
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"x"}`))
	req.Header.Set("Authorization", "Bearer anything")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 when no keys configured", rec.Code)
	}
}
