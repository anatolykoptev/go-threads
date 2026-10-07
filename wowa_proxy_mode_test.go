package threads

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const (
	fakeProxyURL = "http://USERTOK:S3CRETPW@proxy.test:8080"
	fakeSession  = "threads-cdp"
)

// recordedCall is one captured go-wowa interact request.
type recordedCall struct {
	Mode    string
	Session string
	Proxy   *string
	Actions []string // action types, in order
}

// proxyModeStub is a go-wowa stub that records every interact request and
// answers each scripted leg. A fetch evaluate WITHOUT a navigate action is
// answered "redirected" so the in-page-fetch retry leg (navigate+evaluate) runs.
func proxyModeStub(t *testing.T) (*httptest.Server, func() []recordedCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []recordedCall
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req wowaInteractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rc := recordedCall{Mode: req.Mode, Session: req.Session, Proxy: req.Proxy}
		hasNavigate := false
		for _, a := range req.Actions {
			rc.Actions = append(rc.Actions, a.Type)
			if a.Type == "navigate" {
				hasNavigate = true
			}
		}
		mu.Lock()
		calls = append(calls, rc)
		mu.Unlock()

		last := req.Actions[len(req.Actions)-1]
		var data json.RawMessage
		switch {
		case last.Script == "document.cookie":
			data = json.RawMessage(`"ds_user_id=1; csrftoken=c"`)
		case last.Script == "document.documentElement.outerHTML":
			data = json.RawMessage(`"<html>ok</html>"`)
		case strings.Contains(last.Script, "DTSGInitialData"):
			data = json.RawMessage(`{"lsd":"L","csrf":"c","fbDtsg":"d"}`)
		case strings.Contains(last.Script, "fetch("):
			if hasNavigate {
				data = json.RawMessage(`{"status":200,"body":"{}"}`)
			} else {
				data = json.RawMessage(`{"redirected":true,"status":302}`)
			}
		default:
			http.Error(w, "unexpected script", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(wowaInteractResponse{
			URL: req.URL, Status: "ok",
			Actions: []wowaActionResult{{Action: last.Type, Ok: true, Data: data}},
		})
	}))
	t.Cleanup(ts.Close)
	return ts, func() []recordedCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedCall(nil), calls...)
	}
}

// TestWowaInteract_ProxyModeAcrossThreadsSequence drives every go-threads path
// that reaches go-wowa (CDP REST fetch with retry leg, GraphQL fetch with retry
// leg, LSD token, page fetch) and asserts the routing tuple on EACH request.
// go-browser ignores a proxy for mode "default", so every proxied call must be
// mode "proxy"; with no proxy every call must stay mode "default" and carry no
// proxy field (mode "proxy" + empty proxy would be a fresh unproxied context).
func TestWowaInteract_ProxyModeAcrossThreadsSequence(t *testing.T) {
	// Hand-written expectation: 3 (cookie, fetch, retry) + 2 (fetch, retry)
	// + 1 (LSD) + 1 (page) = 7 calls, all on the one named session.
	wantActions := [][]string{
		{"evaluate"}, {"evaluate"}, {"navigate", "evaluate"},
		{"evaluate"}, {"navigate", "evaluate"},
		{"evaluate"},
		{"navigate", "evaluate"},
	}
	cases := []struct {
		name      string
		proxy     string
		wantMode  string
		wantProxy string // "" = field absent
	}{
		{"proxy configured", fakeProxyURL, "proxy", fakeProxyURL},
		{"no proxy", "", "default", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			ts, recorded := proxyModeStub(t)
			c, err := NewClient(Config{WowaURL: ts.URL, Session: fakeSession, Proxy: tc.proxy})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			c.limiter = nil // stub traffic must not be paced by the real per-domain limiter
			ctx := context.Background()

			if _, err := c.doCDP(ctx, "GetUserFollowers", http.MethodGet, "/api/v1/friendships/123/followers/", url.Values{}); err != nil {
				t.Fatalf("doCDP: %v", err)
			}
			if _, status, err := c.doGraphQLCDP(ctx, "Q", "q=1", "L", "Fn"); err != nil || status != 200 {
				t.Fatalf("doGraphQLCDP status=%d err=%v", status, err)
			}
			if _, _, _, err := c.fetchLSDTokenCDP(ctx); err != nil {
				t.Fatalf("fetchLSDTokenCDP: %v", err)
			}
			if _, _, err := c.fetchPageCDP(ctx, "https://www.threads.com/@x"); err != nil {
				t.Fatalf("fetchPageCDP: %v", err)
			}

			got := recorded()
			if len(got) != len(wantActions) {
				t.Fatalf("interact calls = %d, want %d: %+v", len(got), len(wantActions), got)
			}
			for i, rc := range got {
				if rc.Mode != tc.wantMode {
					t.Errorf("call %d mode = %q, want %q", i, rc.Mode, tc.wantMode)
				}
				if rc.Session != "threads-cdp" {
					t.Errorf("call %d session = %q, want threads-cdp", i, rc.Session)
				}
				switch {
				case tc.wantProxy == "" && rc.Proxy != nil:
					t.Errorf("call %d proxy = %q, want absent", i, *rc.Proxy)
				case tc.wantProxy != "" && (rc.Proxy == nil || *rc.Proxy != tc.wantProxy):
					t.Errorf("call %d proxy mismatch (nil=%v)", i, rc.Proxy == nil)
				}
				if strings.Join(rc.Actions, ",") != strings.Join(wantActions[i], ",") {
					t.Errorf("call %d actions = %v, want %v", i, rc.Actions, wantActions[i])
				}
			}
			for _, secret := range []string{"USERTOK", "S3CRETPW"} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("log output leaks proxy credential %q", secret)
				}
			}
		})
	}
}
