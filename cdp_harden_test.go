package threads

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// --- P0: challenge detector tests ---

func TestDetectChallenge(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string // non-empty = challenge detected
	}{
		{"checkpoint_required", `{"message":"checkpoint_required","status":"fail"}`, "checkpoint_required"},
		{"login_required", `{"message":"login_required","status":"fail"}`, "login_required"},
		{"challenge_required", `{"message":"challenge_required","status":"fail"}`, "challenge_required"},
		{"feedback_required", `{"message":"feedback_required","status":"fail"}`, "feedback_required"},
		{"require_login flag", `{"require_login":true}`, "require_login"},
		{"spam flag", `{"spam":true}`, "spam"},
		{"generic fail no items", `{"status":"fail","message":"user not found"}`, "error envelope"},
		{"valid status ok", `{"status":"ok"}`, ""},
		{"valid items array", `{"items":[{"pk":"123"}]}`, ""},
		{"valid users array", `{"users":[]}`, ""},
		{"valid data wrapper", `{"data":{"user":{"pk":"1"}}}`, ""},
		{"not JSON", `<html>not json</html>`, ""},
		{"empty", ``, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectChallenge(tt.body)
			if tt.want == "" && got != "" {
				t.Errorf("detectChallenge(%q) = %q, want empty", tt.body, got)
			}
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Errorf("detectChallenge(%q) = %q, want to contain %q", tt.body, got, tt.want)
			}
		})
	}
}

// TestDoCDP_Challenge_CheckpointRequired proves that a 200 with a small JSON
// challenge body (~718 bytes in prod) is classified as ErrChallenge, NOT
// silently returned as a stub success.
//
// RED before P0: doCDP returns the challenge body as a success (200, not HTML,
// not non-200) → parseInstagramPost gets a stub → silent partial-data
// degradation. IsChallenge(err) is false (no errChallenge class existed).
// GREEN after P0: doCDP detects the challenge envelope → returns ErrChallenge.
func TestDoCDP_Challenge_CheckpointRequired(t *testing.T) {
	challengeBody := `{"status":200,"body":"{\"message\":\"checkpoint_required\",\"status\":\"fail\"}"}`
	ts, _, _ := cdpTestServer(t, "/api/v1/chrome/interact", challengeBody)
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "ig-cdp"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.doPrivateGET(context.Background(), "GetInstagramPost", "/api/v1/media/123/info/", nil)
	if err == nil {
		t.Fatal("expected ErrChallenge, got nil")
	}
	if !IsChallenge(err) {
		t.Fatalf("expected IsChallenge, got %v", err)
	}
}

// TestDoCDP_Challenge_LoginRequired covers the login_required signal variant.
func TestDoCDP_Challenge_LoginRequired(t *testing.T) {
	challengeBody := `{"status":200,"body":"{\"message\":\"login_required\",\"status\":\"fail\"}"}`
	ts, _, _ := cdpTestServer(t, "/api/v1/chrome/interact", challengeBody)
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "ig-cdp"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.doPrivateGET(context.Background(), "GetInstagramPost", "/api/v1/media/123/info/", nil)
	if err == nil {
		t.Fatal("expected ErrChallenge, got nil")
	}
	if !IsChallenge(err) {
		t.Fatalf("expected IsChallenge, got %v", err)
	}
}

// TestDoCDP_Challenge_ValidSmallBodyNotFlagged proves that a legit tiny
// response (like/unlike ack {"status":"ok"}) is NOT false-positive'd as a
// challenge.
func TestDoCDP_Challenge_ValidSmallBodyNotFlagged(t *testing.T) {
	validBody := `{"status":200,"body":"{\"status\":\"ok\"}"}`
	ts, _, _ := cdpTestServer(t, "/api/v1/chrome/interact", validBody)
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "ig-cdp"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	body, err := c.doPrivateAPI(context.Background(), "LikeThread", "/api/v1/media/456_789/like/", url.Values{"media_id": []string{"456"}})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if string(body) != `{"status":"ok"}` {
		t.Errorf("body = %q, want {\"status\":\"ok\"}", string(body))
	}
}

// TestGetInstagramPost_CDP_ChallengeFallsBackToLegacy proves that when the CDP
// transport returns a 200 JSON challenge body (not HTML), the P0 detector
// classifies it as ErrChallenge and GetInstagramPost falls through to the
// legacy embed fallback chain.
//
// RED before P0: the 200 JSON challenge body passes as success →
// parseInstagramPost gets a stub → GetInstagramPost returns an empty/partial
// thread instead of falling back.
// GREEN after P0: ErrChallenge → falls back to proxy → embed → returns video.
func TestGetInstagramPost_CDP_ChallengeFallsBackToLegacy(t *testing.T) {
	withZeroDelays(t)

	apiBody := `{"status":200,"body":"{\"message\":\"checkpoint_required\",\"status\":\"fail\"}"}`

	embedHTML := `<html><body><script>{"shortcode_media":{"__typename":"GraphVideo","id":"123456","shortcode":"ABC123DEF","video_url":"https://cdninstagram.com/video.mp4"}}</script></body></html>`

	ts := cdpFallbackServer(t, apiBody, embedHTML)
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "ig-cdp"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	thread, err := c.GetInstagramPost(context.Background(), "ABC123DEF")
	if err != nil {
		t.Fatalf("GetInstagramPost: expected legacy fallback to succeed after challenge, got: %v", err)
	}
	if thread == nil || len(thread.Items) == 0 {
		t.Fatal("expected a thread from legacy fallback")
	}
	if len(thread.Items[0].Videos) == 0 {
		t.Error("expected video post from embed fallback")
	}
}

// --- P1-guard: session cookie check tests ---

// TestDoCDP_NoSessionID_ReturnsLoginRequired proves that when the pinned tab
// has no ds_user_id cookie (logged out), doCDP fails fast with ErrLoginRequired
// BEFORE issuing the media fetch.
//
// RED before P1: no cookie check → doCDP proceeds to the media fetch →
// callCount=1 (media fetch only), err is nil or a parse error, not
// ErrLoginRequired.
// GREEN after P1: cookie check fires first → ErrLoginRequired, media fetch
// never called (callCount=1, cookie check only).
//
// NOTE: the guard checks ds_user_id (NOT sessionid) because Instagram's
// sessionid cookie is HttpOnly and therefore invisible to document.cookie /
// JS. ds_user_id is the readable (non-HttpOnly) auth indicator that Instagram
// sets to the logged-in account's user id — present iff authenticated.
func TestDoCDP_NoSessionID_ReturnsLoginRequired(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req wowaInteractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var data json.RawMessage
		if len(req.Actions) > 0 && strings.TrimSpace(req.Actions[len(req.Actions)-1].Script) == "document.cookie" {
			// Return cookies WITHOUT ds_user_id — simulates a logged-out tab.
			// (sessionid is HttpOnly and never appears in document.cookie
			// regardless of auth state, so its absence is not a signal.)
			b, _ := json.Marshal("csrftoken=csrf; mid=mid")
			data = b
		} else {
			// Media fetch — should NOT be called if the guard works.
			data = json.RawMessage(`{"status":200,"body":"{\"items\":[{\"pk\":\"1\"}]}"}`)
		}

		resp := wowaInteractResponse{
			URL:    req.URL,
			Status: "ok",
			Actions: []wowaActionResult{
				{Action: "evaluate", Ok: true, Data: data},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "ig-cdp"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.doPrivateGET(context.Background(), "GetInstagramPost", "/api/v1/media/123/info/", nil)
	if err == nil {
		t.Fatal("expected ErrLoginRequired, got nil")
	}
	if !IsLoginRequired(err) {
		t.Fatalf("expected IsLoginRequired, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 interact call (cookie check only, no media fetch), got %d", callCount)
	}
}

// TestDoCDP_WithSessionID_ProceedsToFetch proves that when the pinned tab HAS
// a ds_user_id cookie (the readable auth indicator), doCDP proceeds to the
// media fetch normally.
//
// NOTE: the cookie string deliberately OMITS sessionid because sessionid is
// HttpOnly and never visible to document.cookie. Against the old (buggy)
// sessionid= check this test goes RED (guard returns ErrLoginRequired even
// though the tab is authenticated) — proving the bug. After the fix
// (ds_user_id= check) it goes GREEN.
func TestDoCDP_WithSessionID_ProceedsToFetch(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req wowaInteractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var data json.RawMessage
		if len(req.Actions) > 0 && strings.TrimSpace(req.Actions[len(req.Actions)-1].Script) == "document.cookie" {
			// ds_user_id present (authed), sessionid ABSENT (HttpOnly — never
			// visible to document.cookie). This mirrors the LIVE state on a
			// logged-in cloakbrowser profile.
			b, _ := json.Marshal("ds_user_id=123; csrftoken=csrf; mid=mid")
			data = b
		} else {
			data = json.RawMessage(`{"status":200,"body":"{\"items\":[{\"pk\":\"111222333\",\"code\":\"ABC123DEF\",\"user\":{\"pk\":\"1\",\"username\":\"test\",\"full_name\":\"Test\"},\"caption\":{\"text\":\"hi\"},\"taken_at\":1700000000,\"like_count\":5,\"media_type\":1}]}"}`)
		}

		resp := wowaInteractResponse{
			URL:    req.URL,
			Status: "ok",
			Actions: []wowaActionResult{
				{Action: "evaluate", Ok: true, Data: data},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "ig-cdp"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	body, err := c.doPrivateGET(context.Background(), "GetInstagramPost", "/api/v1/media/123/info/", nil)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !strings.Contains(string(body), "items") {
		t.Errorf("expected media payload, got %q", string(body))
	}
	if callCount != 2 {
		t.Errorf("expected 2 interact calls (cookie check + media fetch), got %d", callCount)
	}
}

// --- P2: residential proxy plumbing tests ---

// TestDoCDP_ProxySet_SentInInteractRequest proves that when Config.Proxy is
// set, the interact request carries the proxy URL.
//
// RED before P2: wowaInteractRequest had no Proxy field → req.Proxy is always
// nil.
// GREEN after P2: Proxy field threaded through → req.Proxy matches Config.Proxy.
func TestDoCDP_ProxySet_SentInInteractRequest(t *testing.T) {
	var gotProxy *string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req wowaInteractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotProxy = req.Proxy

		resp := wowaInteractResponse{
			URL:    req.URL,
			Status: "ok",
			Actions: []wowaActionResult{
				{Action: "evaluate", Ok: true, Data: cdpInteractData(req, `{"status":200,"body":"{\"items\":[]}"}`)},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := Config{
		WowaURL: ts.URL,
		Session: "ig-cdp",
		Proxy:   "http://user:pass@p.webshare.io:10050",
	}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, _ = c.doPrivateGET(context.Background(), "GetInstagramPost", "/api/v1/media/123/info/", nil)
	if gotProxy == nil {
		t.Fatal("expected proxy to be set in interact request, got nil")
	}
	if *gotProxy != "http://user:pass@p.webshare.io:10050" {
		t.Errorf("proxy = %q, want http://user:pass@p.webshare.io:10050", *gotProxy)
	}
}

// TestDoCDP_ProxyEmpty_NoProxyField proves that when Config.Proxy is empty,
// the interact request has no proxy field (no regression).
func TestDoCDP_ProxyEmpty_NoProxyField(t *testing.T) {
	var gotProxy *string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req wowaInteractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotProxy = req.Proxy

		resp := wowaInteractResponse{
			URL:    req.URL,
			Status: "ok",
			Actions: []wowaActionResult{
				{Action: "evaluate", Ok: true, Data: cdpInteractData(req, `{"status":200,"body":"{\"items\":[]}"}`)},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "ig-cdp"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, _ = c.doPrivateGET(context.Background(), "GetInstagramPost", "/api/v1/media/123/info/", nil)
	if gotProxy != nil {
		t.Errorf("expected no proxy field, got %q", *gotProxy)
	}
}
