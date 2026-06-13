package caiyun

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

func newChecker(t *testing.T, code, desc string) *Checker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"code":%q,"desc":%q,"success":true,"data":null}`, code, desc)
	}))
	t.Cleanup(srv.Close)
	return New(Options{APIURL: srv.URL, Client: srv.Client(), Logger: quietLogger()})
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestCodeMapping(t *testing.T) {
	const shareURL = "https://caiyun.139.com/m/i?0a5CfleicjAWV"
	cases := []struct {
		name       string
		code, desc string
		wantStatus checker.Status
		wantReason string
	}{
		{"ok -> alive", "0", "", checker.StatusAlive, checker.ReasonShareOK},
		{"passcode-locked 9188 -> alive", "9188", "提取码非法", checker.StatusAlive, checker.ReasonShareOK},
		{"gone 200000727 -> dead", "200000727", "外链不存在/外链被分享者取消", checker.StatusDead, checker.ReasonShareNotFound},
		{"unseen code -> unknown", "300000001", "啥玩意", checker.StatusUnknown, checker.ReasonUnparseable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newChecker(t, tc.code, tc.desc)
			got := c.Check(context.Background(), mustURL(t, shareURL), "")
			if got.Status != tc.wantStatus || got.Reason != tc.wantReason || got.ProviderCode != tc.code {
				t.Fatalf("code %s: got %+v, want {%s %s %s}", tc.code, got, tc.wantStatus, tc.wantReason, tc.code)
			}
		})
	}
}

// IRON LAW: only the verified 200000727 may yield Dead.
func TestNeverDeadExceptGone(t *testing.T) {
	for _, code := range []string{"0", "9188", "1", "9999", "200000001", "-1"} {
		c := newChecker(t, code, "x")
		got := c.Check(context.Background(), mustURL(t, "https://caiyun.139.com/m/i?abc"), "")
		if got.Status == checker.StatusDead {
			t.Fatalf("code %s yielded Dead but only 200000727 is verified dead", code)
		}
	}
}

func TestExtractLinkIDAndMatches(t *testing.T) {
	c := New(Options{Logger: quietLogger()})
	if !c.Matches(mustURL(t, "https://caiyun.139.com/m/i?0a5CfleicjAWV")) {
		t.Fatal("should match caiyun.139.com")
	}
	if c.Matches(mustURL(t, "https://pan.baidu.com/s/1abc")) {
		t.Fatal("should not match baidu")
	}
	cases := map[string]string{
		"https://caiyun.139.com/m/i?0a5CfleicjAWV":             "0a5CfleicjAWV",
		"http://caiyun.139.com/front/#/detail?linkID=0I5CJThV": "0I5CJThV",
	}
	for raw, want := range cases {
		if got := extractLinkID(mustURL(t, raw)); got != want {
			t.Fatalf("extractLinkID(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestNonJSONIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>blocked</html>")
	}))
	t.Cleanup(srv.Close)
	c := New(Options{APIURL: srv.URL, Client: srv.Client(), Logger: quietLogger()})
	got := c.Check(context.Background(), mustURL(t, "https://caiyun.139.com/m/i?abc"), "")
	if got.Status != checker.StatusUnknown || got.Reason != checker.ReasonUnparseable {
		t.Fatalf("got %+v, want unknown/unparseable", got)
	}
}
