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

// stubOpts scripts the go-wowa stub.
type stubOpts struct {
	fetchBody    string // body of in-page fetches; default "{}"
	redirectOnce bool   // first in-page fetch (no navigate) answers "redirected"
	failAnon     string // "", "http" (HTTP 500) or "action" (ok status, failed action) for -anon sessions; the error echoes the raw proxy URL
}

// proxyModeStub is a go-wowa stub that records every interact request. It
// answers each script with a well-formed, parser-agnostic payload; callers
// inspect the routing tuple, so downstream parse errors are expected.
func proxyModeStub(t *testing.T, o stubOpts) (*httptest.Server, func() []recordedCall) {
	t.Helper()
	if o.fetchBody == "" {
		o.fetchBody = "{}"
	}
	var mu sync.Mutex
	var calls []recordedCall
	redirected := false
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
		firstRedirect := o.redirectOnce && !redirected && !hasNavigate && strings.Contains(req.Actions[len(req.Actions)-1].Script, "fetch(")
		if firstRedirect {
			redirected = true
		}
		mu.Unlock()

		echo := "create tab: context key proxy:" + fakeProxyURL
		if o.failAnon != "" && strings.HasSuffix(req.Session, wowaAnonSuffix) {
			if o.failAnon == "http" {
				http.Error(w, echo, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(wowaInteractResponse{URL: req.URL, Status: "ok",
				Actions: []wowaActionResult{{Action: "navigate", Ok: false, Error: echo}}})
			return
		}

		last := req.Actions[len(req.Actions)-1]
		var data json.RawMessage
		switch {
		case last.Script == "document.cookie":
			data = json.RawMessage(`"ds_user_id=1; csrftoken=c"`)
		case last.Script == "document.documentElement.outerHTML":
			data = json.RawMessage(`"<html>\"user_id\":\"1\"</html>"`)
		case strings.Contains(last.Script, "DTSGInitialData"):
			data = json.RawMessage(`{"lsd":"L","csrf":"c","fbDtsg":"d"}`)
		case strings.Contains(last.Script, "fetch("):
			if firstRedirect {
				data = json.RawMessage(`{"redirected":true,"status":302}`)
			} else {
				b, _ := json.Marshal(fetchResult{Status: 200, Body: o.fetchBody})
				data = b
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

// kindOf classifies a captured request against the two hand-written routing
// tuples. A = logged-in profile: mode default, no proxy field, the session.
// N = proxied anonymous: mode proxy, the proxy, "<session>-anon". "?" = neither.
func kindOf(rc recordedCall) string {
	switch {
	case rc.Mode == "default" && rc.Proxy == nil && rc.Session == "threads-cdp":
		return "A"
	case rc.Mode == "proxy" && rc.Proxy != nil && *rc.Proxy == fakeProxyURL && rc.Session == "threads-cdp-anon":
		return "N"
	}
	return "?"
}

// kinds returns the run-length-collapsed kind sequence, e.g. "N,A,N".
func kinds(calls []recordedCall) string {
	var out []string
	for _, rc := range calls {
		k := kindOf(rc)
		if len(out) == 0 || out[len(out)-1] != k {
			out = append(out, k)
		}
	}
	return strings.Join(out, ",")
}

// flowCase is one public go-threads entry point and the kind sequence of its
// go-wowa requests when a proxy is configured. Expected sequences are
// hand-written: A = logged-in default profile, N = proxied anonymous.
type flowCase struct {
	name      string
	want      string
	fetchBody string
	call      func(c *Client, ctx context.Context)
}

func flowCases() []flowCase {
	return []flowCase{
		// Logged-in only: the Meta login lives in go-browser's default profile.
		{"GetInstagramUser", "A", "", func(c *Client, ctx context.Context) { _, _ = c.GetInstagramUser(ctx, "u") }},
		{"GetUserFollowers", "A", "", func(c *Client, ctx context.Context) { _, _ = c.GetUserFollowers(ctx, "1", 5) }},
		{"GetUserFollowing", "A", "", func(c *Client, ctx context.Context) { _, _ = c.GetUserFollowing(ctx, "1", 5) }},
		{"SearchUserPrivate", "A", "", func(c *Client, ctx context.Context) { _, _ = c.SearchUserPrivate(ctx, "q") }},
		{"GetThreadByID", "A", "", func(c *Client, ctx context.Context) { _, _, _ = c.GetThreadByID(ctx, "1") }},
		{"LikeThread", "A", "", func(c *Client, ctx context.Context) { _ = c.LikeThread(ctx, "1") }},
		{"Follow", "A", "", func(c *Client, ctx context.Context) { _ = c.Follow(ctx, "1") }},
		{"SearchUsers", "A", "", func(c *Client, ctx context.Context) { _, _ = c.SearchUsers(ctx, "q", 5) }},
		{"SearchPosts", "A", "", func(c *Client, ctx context.Context) { _, _ = c.SearchPosts(ctx, "q", SearchPostsOpts{}) }},
		// GraphQL returns no data logged out (live-checked 2026-10-07): authed.
		{"GetUserByID", "A", "", func(c *Client, ctx context.Context) { _, _ = c.GetUserByID(ctx, "1") }},
		{"GetThreadLikers", "A", "", func(c *Client, ctx context.Context) { _, _ = c.GetThreadLikers(ctx, "1", 5) }},
		// Anonymous page fetches.
		{"GetUser", "N", "", func(c *Client, ctx context.Context) { _, _ = c.GetUser(ctx, "zuck") }},
		{"GetThread", "N", "", func(c *Client, ctx context.Context) { _, _, _ = c.GetThread(ctx, "zuck", "ABC") }},
		// Mixed: anonymous profile-page resolve, then logged-in LSD + GraphQL.
		{"GetUserThreads", "N,A", "", func(c *Client, ctx context.Context) { _, _ = c.GetUserThreads(ctx, "zuck", 5) }},
		{"GetUserReplies", "N,A", "", func(c *Client, ctx context.Context) { _, _ = c.GetUserReplies(ctx, "zuck", 5) }},
		// Mixed fallthrough: CDP tier fails (logged in), embed (anonymous),
		// SSR tier (logged in); the kkinstagram proxy tier does not touch go-wowa.
		{"GetInstagramPost", "A,N,A", "<html>challenge</html>", func(c *Client, ctx context.Context) { _, _ = c.GetInstagramPost(ctx, "ABC123DEF") }},
	}
}

// TestWowaFlowRouting asserts the routing tuple (mode, proxy, session) of
// EVERY go-wowa request each entry point produces, with and without a proxy.
func TestWowaFlowRouting(t *testing.T) {
	for _, withProxy := range []bool{true, false} {
		name, proxy := "no proxy", ""
		if withProxy {
			name, proxy = "proxy configured", fakeProxyURL
		}
		for _, fc := range flowCases() {
			t.Run(name+"/"+fc.name, func(t *testing.T) {
				withZeroDelays(t)
				var logs bytes.Buffer
				prev := slog.Default()
				slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
				t.Cleanup(func() { slog.SetDefault(prev) })
				kk := httptest.NewServer(http.NotFoundHandler())
				defer kk.Close()
				origKK := kkInstagramBase
				kkInstagramBase = kk.URL
				t.Cleanup(func() { kkInstagramBase = origKK })

				ts, recorded := proxyModeStub(t, stubOpts{fetchBody: fc.fetchBody})
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
				want := fc.want
				if !withProxy {
					want = "A" // no proxy: every flow is identical to today
				}
				if g := kinds(got); g != want {
					t.Errorf("request kinds = %q, want %q (calls: %+v)", g, want, got)
				}
				if !withProxy {
					for i, rc := range got {
						if rc.Proxy != nil {
							t.Errorf("call %d carries a proxy field with no proxy configured", i)
						}
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
	ts, recorded := proxyModeStub(t, stubOpts{})
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

// TestWowaFlowRouting_RetryLegKeepsLoggedInTuple: when the in-page fetch is
// redirected, the navigate+evaluate retry must keep the logged-in tuple.
func TestWowaFlowRouting_RetryLegKeepsLoggedInTuple(t *testing.T) {
	ts, recorded := proxyModeStub(t, stubOpts{redirectOnce: true})
	c, err := NewClient(Config{WowaURL: ts.URL, Session: fakeSession, Proxy: fakeProxyURL})
	if err != nil {
		t.Fatal(err)
	}
	c.limiter = nil
	_, _ = c.GetInstagramUser(context.Background(), "u")
	got := recorded()
	// cookie check, redirected fetch, navigate+evaluate retry.
	if len(got) != 3 {
		t.Fatalf("calls = %d, want 3: %+v", len(got), got)
	}
	if strings.Join(got[2].Actions, ",") != "navigate,evaluate" {
		t.Fatalf("third call actions = %v, want the navigate+evaluate retry leg", got[2].Actions)
	}
	if k := kinds(got); k != "A" {
		t.Errorf("kinds = %q, want A (retry leg left the logged-in profile): %+v", k, got)
	}
}

// TestWowaErrorsDoNotLeakProxyCredentials: go-browser v0.20.9 echoes the raw
// context key (it embeds the proxy URL) in create-tab errors. Neither the
// errors go-threads returns nor its logs may carry the credentials.
func TestWowaErrorsDoNotLeakProxyCredentials(t *testing.T) {
	for _, mode := range []string{"http", "action"} {
		t.Run(mode, func(t *testing.T) {
			withZeroDelays(t)
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			ts, recorded := proxyModeStub(t, stubOpts{failAnon: mode})
			c, err := NewClient(Config{WowaURL: ts.URL, Session: fakeSession, Proxy: fakeProxyURL})
			if err != nil {
				t.Fatal(err)
			}
			c.limiter = nil
			_, _, gerr := c.GetThread(context.Background(), "zuck", "ABC")
			if gerr == nil {
				t.Fatal("expected an error from the failing anon stub")
			}
			if k := kinds(recorded()); k != "N" {
				t.Fatalf("anon error path did not run as anon: kinds=%q", k)
			}
			out := gerr.Error() + "\n" + logs.String()
			if !strings.Contains(gerr.Error(), "context key") {
				t.Fatalf("stub error text did not reach the returned error (vacuous test): %v", gerr)
			}
			for _, secret := range []string{"USERTOK", "S3CRETPW", "proxy.test"} {
				if strings.Contains(out, secret) {
					t.Errorf("error or log output leaks %q: %s", secret, out)
				}
			}
		})
	}
}
