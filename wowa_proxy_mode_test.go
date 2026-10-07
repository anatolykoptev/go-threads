package threads

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
// answers each script with a well-formed, parser-agnostic payload; callers only
// inspect the routing tuple, so downstream parse errors are expected and ignored.
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
		for _, a := range req.Actions {
			rc.Actions = append(rc.Actions, a.Type)
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
			data = json.RawMessage(`{"status":200,"body":"{}"}`)
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

// flowCase is one public go-threads entry point and the routing it must use.
// Expected values are hand-written, not derived from the code under test.
type flowCase struct {
	name   string
	authed bool
	call   func(c *Client, ctx context.Context)
}

func flowCases() []flowCase {
	return []flowCase{
		// Logged-in: the Meta login lives only in go-browser's default profile.
		{"GetInstagramUser", true, func(c *Client, ctx context.Context) { _, _ = c.GetInstagramUser(ctx, "u") }},
		{"GetUserFollowers", true, func(c *Client, ctx context.Context) { _, _ = c.GetUserFollowers(ctx, "1", 5) }},
		{"GetUserFollowing", true, func(c *Client, ctx context.Context) { _, _ = c.GetUserFollowing(ctx, "1", 5) }},
		{"SearchUserPrivate", true, func(c *Client, ctx context.Context) { _, _ = c.SearchUserPrivate(ctx, "q") }},
		{"GetThreadByID", true, func(c *Client, ctx context.Context) { _, _, _ = c.GetThreadByID(ctx, "1") }},
		{"LikeThread", true, func(c *Client, ctx context.Context) { _ = c.LikeThread(ctx, "1") }},
		{"Follow", true, func(c *Client, ctx context.Context) { _ = c.Follow(ctx, "1") }},
		{"SearchUsers", true, func(c *Client, ctx context.Context) { _, _ = c.SearchUsers(ctx, "q", 5) }},
		{"SearchPosts", true, func(c *Client, ctx context.Context) { _, _ = c.SearchPosts(ctx, "q", SearchPostsOpts{}) }},
		// Anonymous: no login needed.
		{"GetUser", false, func(c *Client, ctx context.Context) { _, _ = c.GetUser(ctx, "zuck") }},
		{"GetUserThreads", false, func(c *Client, ctx context.Context) { _, _ = c.GetUserThreads(ctx, "zuck", 5) }},
		{"GetThread", false, func(c *Client, ctx context.Context) { _, _, _ = c.GetThread(ctx, "zuck", "ABC") }},
		{"GetUserReplies", false, func(c *Client, ctx context.Context) { _, _ = c.GetUserReplies(ctx, "zuck", 5) }},
		{"GetThreadLikers", false, func(c *Client, ctx context.Context) { _, _ = c.GetThreadLikers(ctx, "1", 5) }},
	}
}

// TestWowaFlowRouting asserts the routing tuple (mode, proxy, session) on EVERY
// go-wowa request each entry point produces, with and without a proxy.
//
//	proxy configured: authed -> default / no proxy / "threads-cdp"
//	                  anon   -> proxy / the proxy / "threads-cdp-anon"
//	no proxy:         both   -> default / no proxy / "threads-cdp"
func TestWowaFlowRouting(t *testing.T) {
	for _, withProxy := range []bool{true, false} {
		name := "no proxy"
		proxy := ""
		if withProxy {
			name, proxy = "proxy configured", fakeProxyURL
		}
		for _, fc := range flowCases() {
			t.Run(name+"/"+fc.name, func(t *testing.T) {
				var logs bytes.Buffer
				prev := slog.Default()
				slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
				t.Cleanup(func() { slog.SetDefault(prev) })

				ts, recorded := proxyModeStub(t)
				c, err := NewClient(Config{WowaURL: ts.URL, Session: fakeSession, Proxy: proxy, Token: "IGT:2:t"})
				if err != nil {
					t.Fatalf("NewClient: %v", err)
				}
				c.limiter = nil // stub traffic must not be paced by the real per-domain limiter
				fc.call(c, context.Background())

				got := recorded()
				if len(got) == 0 {
					t.Fatal("no go-wowa request captured")
				}
				wantMode, wantSession, wantProxy := "default", "threads-cdp", ""
				if withProxy && !fc.authed {
					wantMode, wantSession, wantProxy = "proxy", "threads-cdp-anon", fakeProxyURL
				}
				for i, rc := range got {
					if rc.Mode != wantMode {
						t.Errorf("call %d mode = %q, want %q", i, rc.Mode, wantMode)
					}
					if rc.Session != wantSession {
						t.Errorf("call %d session = %q, want %q", i, rc.Session, wantSession)
					}
					switch {
					case wantProxy == "" && rc.Proxy != nil:
						t.Errorf("call %d carries a proxy field, want none", i)
					case wantProxy != "" && (rc.Proxy == nil || *rc.Proxy != wantProxy):
						t.Errorf("call %d proxy mismatch (nil=%v)", i, rc.Proxy == nil)
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
}

// TestWowaFlowRouting_PoolSuffix: with SessionPool the anon suffix goes after
// the pool index, so each pool tab keeps its own "-anon" twin.
func TestWowaFlowRouting_PoolSuffix(t *testing.T) {
	ts, recorded := proxyModeStub(t)
	c, err := NewClient(Config{WowaURL: ts.URL, Session: fakeSession, SessionPool: 2, Proxy: fakeProxyURL})
	if err != nil {
		t.Fatal(err)
	}
	c.limiter = nil
	_, _ = c.GetUser(context.Background(), "zuck")
	got := recorded()
	if len(got) == 0 {
		t.Fatal("no request captured")
	}
	for i, rc := range got {
		if rc.Session != "threads-cdp-0-anon" && rc.Session != "threads-cdp-1-anon" {
			t.Errorf("call %d session = %q, want threads-cdp-{0,1}-anon", i, rc.Session)
		}
	}
}
