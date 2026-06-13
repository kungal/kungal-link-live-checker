package pan123

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// newChecker returns a checker plus the test server base URL (https). The
// checker derives the API host from the share URL, so the share URL must point
// at the test server.
func newChecker(t *testing.T, h http.Handler) (*Checker, string) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return New(Options{Client: srv.Client(), Logger: quietLogger()}), srv.URL
}

func serveResp(code int, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/b/api/share/get" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"code":%d,"message":%q,"data":null}`, code, message)
	}
}

func TestCodeMapping(t *testing.T) {
	cases := []struct {
		name       string
		code       int
		message    string
		wantStatus checker.Status
		wantReason string
	}{
		{"ok -> alive", 0, "ok", checker.StatusAlive, checker.ReasonShareOK},
		{"5103 不存在 -> dead", 5103, "此分享不存在", checker.StatusDead, checker.ReasonShareNotFound},
		{"5103 提取码错误 -> alive (locked)", 5103, "提取码错误", checker.StatusAlive, checker.ReasonShareOK},
		{"5103 unknown message -> unknown", 5103, "服务器开小差", checker.StatusUnknown, checker.ReasonUnparseable},
		{"other code -> unknown", 9999, "???", checker.StatusUnknown, checker.ReasonUnparseable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, base := newChecker(t, serveResp(tc.code, tc.message))
			got := c.Check(context.Background(), mustURL(t, base+"/s/DXyA-IJRX"), "")
			if got.Status != tc.wantStatus || got.Reason != tc.wantReason {
				t.Fatalf("code %d/%q: got %+v, want {%s %s}", tc.code, tc.message, got, tc.wantStatus, tc.wantReason)
			}
		})
	}
}

// IRON LAW: the only path to Dead is 5103 with an explicit "不存在" message.
func TestNeverDeadUnlessNotFound(t *testing.T) {
	type tc struct {
		code int
		msg  string
	}
	for _, x := range []tc{{0, "ok"}, {5103, "提取码错误"}, {5103, "别的"}, {9999, "x"}, {1, "y"}, {5103, ""}} {
		c, base := newChecker(t, serveResp(x.code, x.msg))
		got := c.Check(context.Background(), mustURL(t, base+"/s/abc"), "")
		if got.Status == checker.StatusDead {
			t.Fatalf("code %d msg %q yielded Dead; only 5103+不存在 is dead", x.code, x.msg)
		}
	}
}

func TestPasscodeForwarded(t *testing.T) {
	var gotPwd string
	c, base := newChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPwd = r.URL.Query().Get("SharePwd")
		fmt.Fprint(w, `{"code":0,"message":"ok"}`)
	}))
	// passcode in URL ?pwd= should be forwarded as SharePwd
	c.Check(context.Background(), mustURL(t, base+"/s/abc?pwd=love"), "")
	if gotPwd != "love" {
		t.Fatalf("SharePwd = %q, want love", gotPwd)
	}
}

func TestMatchesAndExtractKey(t *testing.T) {
	c := New(Options{Logger: quietLogger()})
	for _, h := range []string{
		"https://www.123pan.com/s/abc",
		"https://123912.com/s/abc",
		"https://www.123684.com/s/abc",
		"https://1851621325.share.123pan.cn/123pan/abc",
	} {
		if !c.Matches(mustURL(t, h)) {
			t.Fatalf("should match %q", h)
		}
	}
	if c.Matches(mustURL(t, "https://pan.quark.cn/s/abc")) {
		t.Fatal("should not match quark")
	}
	if got := extractKey(mustURL(t, "https://www.123pan.com/s/yXg6Vv-enZ7v")); got != "yXg6Vv-enZ7v" {
		t.Fatalf("extractKey /s/ = %q", got)
	}
	if got := extractKey(mustURL(t, "https://x.share.123pan.cn/123pan/OsyJjv-QCUlh")); got != "OsyJjv-QCUlh" {
		t.Fatalf("extractKey /123pan/ = %q", got)
	}
}
