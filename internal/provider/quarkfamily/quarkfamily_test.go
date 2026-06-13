package quarkfamily

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newChecker spins an httptest server that always replies with the given HTTP
// status + body, and returns a Checker pointed at it.
func newChecker(t *testing.T, blockedAsDead bool, status int, body string) *Checker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return New(Config{
		Name:          "quark",
		Hosts:         []string{"pan.quark.cn"},
		TokenURL:      srv.URL,
		BlockedAsDead: blockedAsDead,
		Client:        srv.Client(),
		Logger:        quietLogger(),
	})
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestCheckCodeMapping(t *testing.T) {
	const shareURL = "https://pan.quark.cn/s/eb34b875e97f"
	cases := []struct {
		name          string
		blockedAsDead bool
		httpStatus    int
		body          string
		wantStatus    checker.Status
		wantReason    string
		wantCode      string
	}{
		{
			name:       "code 0 with stoken -> alive",
			httpStatus: 200,
			body:       `{"status":200,"code":0,"message":"ok","data":{"stoken":"abc"}}`,
			wantStatus: checker.StatusAlive, wantReason: checker.ReasonShareOK, wantCode: "0",
		},
		{
			name:       "passcode required (HTTP 404) -> unknown",
			httpStatus: 404,
			body:       `{"status":404,"code":41008,"message":"需要提取码"}`,
			wantStatus: checker.StatusUnknown, wantReason: checker.ReasonPasscodeRequired, wantCode: "41008",
		},
		{
			name:          "blocked 41031, BlockedAsDead=true -> dead",
			blockedAsDead: true,
			httpStatus:    403,
			body:          `{"status":403,"code":41031,"message":"分享者用户封禁链接查看受限"}`,
			wantStatus:    checker.StatusDead, wantReason: checker.ReasonShareBlocked, wantCode: "41031",
		},
		{
			name:          "blocked 41031, BlockedAsDead=false -> unknown",
			blockedAsDead: false,
			httpStatus:    403,
			body:          `{"status":403,"code":41031,"message":"受限"}`,
			wantStatus:    checker.StatusUnknown, wantReason: checker.ReasonShareBlocked, wantCode: "41031",
		},
		{
			name:       "41004 文件不存在 -> dead/share_not_found",
			httpStatus: 404,
			body:       `{"status":404,"code":41004,"message":"文件不存在"}`,
			wantStatus: checker.StatusDead, wantReason: checker.ReasonShareNotFound, wantCode: "41004",
		},
		{
			name:       "41006 分享不存在 -> dead/share_not_found",
			httpStatus: 404,
			body:       `{"status":404,"code":41006,"message":"分享不存在"}`,
			wantStatus: checker.StatusDead, wantReason: checker.ReasonShareNotFound, wantCode: "41006",
		},
		{
			name:       "41011 分享地址已失效 -> dead/share_expired",
			httpStatus: 404,
			body:       `{"status":404,"code":41011,"message":"分享地址已失效"}`,
			wantStatus: checker.StatusDead, wantReason: checker.ReasonShareExpired, wantCode: "41011",
		},
		{
			name:       "41012 好友已取消了分享 -> dead/share_not_found",
			httpStatus: 404,
			body:       `{"status":404,"code":41012,"message":"好友已取消了分享"}`,
			wantStatus: checker.StatusDead, wantReason: checker.ReasonShareNotFound, wantCode: "41012",
		},
		{
			name:          "41010 violation, BlockedAsDead=true -> dead/share_blocked",
			blockedAsDead: true,
			httpStatus:    404,
			body:          `{"status":404,"code":41010,"message":"文件涉及违规内容"}`,
			wantStatus:    checker.StatusDead, wantReason: checker.ReasonShareBlocked, wantCode: "41010",
		},
		{
			name:          "41010 violation, BlockedAsDead=false -> unknown/share_blocked",
			blockedAsDead: false,
			httpStatus:    404,
			body:          `{"status":404,"code":41010,"message":"文件涉及违规内容"}`,
			wantStatus:    checker.StatusUnknown, wantReason: checker.ReasonShareBlocked, wantCode: "41010",
		},
		{
			// API drift guard: a never-before-seen code must stay unknown.
			name:       "unrecognized code -> unknown, never dead",
			httpStatus: 200,
			body:       `{"status":200,"code":99999,"message":"???"}`,
			wantStatus: checker.StatusUnknown, wantReason: checker.ReasonUnparseable, wantCode: "99999",
		},
		{
			name:       "html / non-json body -> unknown unparseable",
			httpStatus: 200,
			body:       `<!doctype html><html>anti-bot</html>`,
			wantStatus: checker.StatusUnknown, wantReason: checker.ReasonUnparseable, wantCode: "",
		},
		{
			name:       "code 0 without stoken -> unknown (no false alive)",
			httpStatus: 200,
			body:       `{"status":200,"code":0,"message":"ok"}`,
			wantStatus: checker.StatusUnknown, wantReason: checker.ReasonUnparseable, wantCode: "0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newChecker(t, tc.blockedAsDead, tc.httpStatus, tc.body)
			got := c.Check(context.Background(), mustURL(t, shareURL), "")
			if got.Status != tc.wantStatus || got.Reason != tc.wantReason || got.ProviderCode != tc.wantCode {
				t.Fatalf("got %+v, want {Status:%s Reason:%s Code:%s}", got, tc.wantStatus, tc.wantReason, tc.wantCode)
			}
		})
	}
}

// TestNeverDeadOnAnyUnknownCode is a guard: across a wide sweep of codes, only
// the verified gone/blocked codes may produce Dead. Anything else (including
// codes the docs once merely *suspected*, like 41027) must stay non-dead.
func TestNeverDeadOnAnyUnknownCode(t *testing.T) {
	deadAllowed := map[int]bool{41004: true, 41006: true, 41011: true, 41012: true, 41010: true, 41031: true}
	for _, code := range []int{-1, 1, 105, 116, 41027, 41008, 41099, 50000, 99999} {
		body := `{"code":` + strconv.Itoa(code) + `,"message":"x"}`
		c := newChecker(t, true, 200, body)
		got := c.Check(context.Background(), mustURL(t, "https://pan.quark.cn/s/abc123"), "")
		if got.Status == checker.StatusDead && !deadAllowed[code] {
			t.Fatalf("code %d produced Dead but is not a verified dead code", code)
		}
	}
}

func TestPasscodeFromURLQuery(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"code":0,"data":{"stoken":"ok"}}`)
	}))
	t.Cleanup(srv.Close)
	c := New(Config{Name: "quark", Hosts: []string{"pan.quark.cn"}, TokenURL: srv.URL, Client: srv.Client(), Logger: quietLogger()})

	// passcode arg empty, but URL carries ?pwd=wdwV
	c.Check(context.Background(), mustURL(t, "https://pan.quark.cn/s/abc?pwd=wdwV"), "")
	if want := `"passcode":"wdwV"`; !strings.Contains(gotBody, want) {
		t.Fatalf("request body %q does not carry %q", gotBody, want)
	}
	if want := `"pwd_id":"abc"`; !strings.Contains(gotBody, want) {
		t.Fatalf("request body %q does not carry %q", gotBody, want)
	}
}

func TestMatchesAndUnparseableURL(t *testing.T) {
	c := New(Config{Name: "quark", Hosts: []string{"pan.quark.cn"}, TokenURL: "http://unused", Logger: quietLogger()})
	if !c.Matches(mustURL(t, "https://pan.quark.cn/s/abc")) {
		t.Fatal("should match pan.quark.cn")
	}
	if c.Matches(mustURL(t, "https://pan.baidu.com/s/abc")) {
		t.Fatal("should not match baidu")
	}
	// No /s/<id> segment -> can't parse share id -> unknown, no network call.
	got := c.Check(context.Background(), mustURL(t, "https://pan.quark.cn/list/all"), "")
	if got.Status != checker.StatusUnknown || got.Reason != checker.ReasonUnparseable {
		t.Fatalf("got %+v, want unknown/unparseable", got)
	}
}
