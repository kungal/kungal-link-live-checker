package baidu

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// serveErrno returns a handler that warms a BAIDUID cookie on "/" and replies
// to shorturlinfo with the given errno.
func serveErrno(errno int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "BAIDUID", Value: "test"})
			_, _ = io.WriteString(w, "ok")
		case "/api/shorturlinfo":
			fmt.Fprintf(w, `{"errno":%d,"shareid":12345,"uk":678,"show_msg":""}`, errno)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func newChecker(t *testing.T, h http.Handler) *Checker {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(Options{APIBase: srv.URL, Client: srv.Client(), Logger: quietLogger()})
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestErrnoMapping(t *testing.T) {
	const shareURL = "https://pan.baidu.com/s/1BeVBwj47Pm4Vcnggbx85Kw"
	cases := []struct {
		name       string
		errno      int
		wantStatus checker.Status
		wantReason string
	}{
		{"public ok -> alive", 0, checker.StatusAlive, checker.ReasonShareOK},
		{"passcode-protected -9 -> unknown (NOT alive: -9 confirms nothing)", -9, checker.StatusUnknown, checker.ReasonPasscodeRequired},
		{"deleted -21 -> dead", -21, checker.StatusDead, checker.ReasonShareNotFound},
		{"malformed 140 -> unknown", 140, checker.StatusUnknown, checker.ReasonUnparseable},
		{"unseen errno -> unknown", -55, checker.StatusUnknown, checker.ReasonUnparseable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newChecker(t, serveErrno(tc.errno))
			got := c.Check(context.Background(), mustURL(t, shareURL), "")
			if got.Status != tc.wantStatus || got.Reason != tc.wantReason || got.ProviderCode != strconv.Itoa(tc.errno) {
				t.Fatalf("errno %d: got %+v, want {%s %s %d}", tc.errno, got, tc.wantStatus, tc.wantReason, tc.errno)
			}
		})
	}
}

// IRON LAW: only the verified -21 may yield Dead.
func TestNeverDeadExceptMinus21(t *testing.T) {
	for _, errno := range []int{0, -9, -1, -7, -8, 2, 140, -55, -12, 105, 116} {
		c := newChecker(t, serveErrno(errno))
		got := c.Check(context.Background(), mustURL(t, "https://pan.baidu.com/s/1abcDEF"), "")
		if got.Status == checker.StatusDead {
			t.Fatalf("errno %d yielded Dead but only -21 is a verified dead signal", errno)
		}
	}
}

func TestNonJSONBodyIsUnknown(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.SetCookie(w, &http.Cookie{Name: "BAIDUID", Value: "t"})
			return
		}
		_, _ = io.WriteString(w, "<!doctype html><title>安全验证</title>")
	})
	c := newChecker(t, h)
	got := c.Check(context.Background(), mustURL(t, "https://pan.baidu.com/s/1abcDEF"), "")
	if got.Status != checker.StatusUnknown || got.Reason != checker.ReasonUnparseable {
		t.Fatalf("got %+v, want unknown/unparseable", got)
	}
}

// A JSON envelope with no errno field must NOT default to alive (errno 0).
func TestMissingErrnoIsUnknown(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "BAIDUID", Value: "t"})
		default:
			_, _ = io.WriteString(w, `{"shareid":123,"uk":456}`) // no errno
		}
	})
	c := newChecker(t, h)
	got := c.Check(context.Background(), mustURL(t, "https://pan.baidu.com/s/1abcDEF"), "")
	if got.Status != checker.StatusUnknown {
		t.Fatalf("missing errno: got %+v, want unknown (never alive)", got)
	}
}

func TestMatchesAndExtract(t *testing.T) {
	c := New(Options{Logger: quietLogger()})
	if !c.Matches(mustURL(t, "https://pan.baidu.com/s/1abc?pwd=xxxx")) {
		t.Fatal("should match pan.baidu.com")
	}
	if c.Matches(mustURL(t, "https://pan.quark.cn/s/abc")) {
		t.Fatal("should not match quark")
	}
	got := c.Check(context.Background(), mustURL(t, "https://pan.baidu.com/disk/home"), "")
	if got.Status != checker.StatusUnknown || got.Reason != checker.ReasonUnparseable {
		t.Fatalf("no /s/ short-url: got %+v, want unknown/unparseable", got)
	}
}

func TestCookieWarmup(t *testing.T) {
	var sawCookieOnAPI bool
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "BAIDUID", Value: "warm"})
		case "/api/shorturlinfo":
			if ck, err := r.Cookie("BAIDUID"); err == nil && ck.Value == "warm" {
				sawCookieOnAPI = true
			}
			_, _ = io.WriteString(w, `{"errno":-21}`)
		}
	})
	c := newChecker(t, h)
	c.Check(context.Background(), mustURL(t, "https://pan.baidu.com/s/1abcDEF"), "")
	if !sawCookieOnAPI {
		t.Fatal("shorturlinfo request should carry the warmed-up BAIDUID cookie")
	}
}
