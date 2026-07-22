package threads

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	stealth "github.com/anatolykoptev/go-stealth"
)

// cdpTestServer spins up a fake go-wowa that records the last interact request
// and returns a scripted JSON response.
func cdpTestServer(t *testing.T, wantPath string, body string) (*httptest.Server, *string, *string) {
	t.Helper()
	var gotURL, gotScript string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chrome/interact" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req wowaInteractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Actions) == 0 {
			http.Error(w, "no actions", http.StatusBadRequest)
			return
		}
		gotURL = req.URL
		gotScript = req.Actions[len(req.Actions)-1].Script

		resp := wowaInteractResponse{
			URL:    req.URL,
			Status: "ok",
			Actions: []wowaActionResult{
				{Action: "evaluate", Ok: true, Data: json.RawMessage(body)},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	return ts, &gotURL, &gotScript
}

func TestDoGraphQL_CDP_ScriptTargetsThreadsGraphQL(t *testing.T) {
	mockBody := `{"status":200,"body":"{\"data\":{\"likers\":{\"users\":[]}}}"}`
	ts, gotURL, gotScript := cdpTestServer(t, "/api/v1/chrome/interact", mockBody)
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "threads-cdp"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// Warm LSD cache so ensureLSD does not issue a separate page fetch.
	c.lsd = "AVqDh-2lkJ8"
	c.lsdAt = time.Now()
	c.fbDtsg = "ABCD"

	_, err = c.GetThreadLikers(context.Background(), "123", 5)
	if err != nil {
		t.Fatalf("GetThreadLikers: %v", err)
	}

	if !strings.HasPrefix(*gotURL, threadsBaseURL+"/") {
		t.Errorf("page URL origin = %q, want %s/", *gotURL, threadsBaseURL)
	}
	wantParts := []string{
		threadsBaseURL + "/api/graphql",
		`"x-ig-app-id":"238260118697367"`,
		`"x-fb-friendly-name"`,
		`"BarcelonaMediaLikersQuery"`,
		`"x-asbd-id"`,
		`"129477"`,
		`"x-fb-lsd"`,
		`credentials:"include"`,
		`redirect:"manual"`,
		`doc_id`,
		`variables`,
	}
	for _, w := range wantParts {
		if !strings.Contains(*gotScript, w) {
			t.Errorf("script missing %q\nscript:\n%s", w, *gotScript)
		}
	}
	if strings.Contains(*gotScript, igBaseURL) || strings.Contains(*gotScript, "i.instagram.com") {
		t.Errorf("script targeted mobile Instagram host: %q", *gotScript)
	}
}

func TestGetInstagramPost_CDP_TargetsWebEndpoint(t *testing.T) {
	mockBody := `{"status":200,"body":"{\"items\":[{\"pk\":\"111222333\",\"code\":\"ABC123DEF\",\"user\":{\"pk\":\"1\",\"username\":\"test\",\"full_name\":\"Test\"},\"caption\":{\"text\":\"hi\"},\"taken_at\":1700000000,\"like_count\":5,\"media_type\":1}]}"}`
	ts, gotURL, gotScript := cdpTestServer(t, "/api/v1/chrome/interact", mockBody)
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "ig-cdp"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	thread, err := c.GetInstagramPost(context.Background(), "ABC123DEF")
	if err != nil {
		t.Fatalf("GetInstagramPost: %v", err)
	}
	if thread == nil || len(thread.Items) == 0 {
		t.Fatal("expected post")
	}
	if thread.Items[0].Code != "ABC123DEF" {
		t.Errorf("post code = %q, want ABC123DEF", thread.Items[0].Code)
	}

	if !strings.HasPrefix(*gotURL, igWebBaseURL+"/") {
		t.Errorf("page URL origin = %q, want %s/", *gotURL, igWebBaseURL)
	}
	if !strings.Contains(*gotScript, "/p/ABC123DEF/") {
		t.Errorf("script missing /p/ABC123DEF/: %s", *gotScript)
	}
	if !strings.Contains(*gotScript, "__a=1") || !strings.Contains(*gotScript, "__d=dis") {
		t.Errorf("script missing __a=1 / __d=dis: %s", *gotScript)
	}
	if strings.Contains(*gotScript, igBaseURL) || strings.Contains(*gotScript, "i.instagram.com") {
		t.Errorf("script targeted mobile Instagram host: %q", *gotScript)
	}
}

func TestGetInstagramUser_CDP_TargetsWebProfileInfo(t *testing.T) {
	mockBody := `{"status":200,"body":"{\"data\":{\"user\":{\"pk\":\"25025320\",\"username\":\"instagram\",\"full_name\":\"Instagram\",\"biography\":\"hello\",\"profile_pic_url\":\"https://example.com/pic.jpg\",\"is_verified\":true,\"text_post_app_is_private\":false,\"follower_count\":100,\"following_count\":50}}}"}`
	ts, gotURL, gotScript := cdpTestServer(t, "/api/v1/chrome/interact", mockBody)
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "ig-cdp"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	user, err := c.GetInstagramUser(context.Background(), "instagram")
	if err != nil {
		t.Fatalf("GetInstagramUser: %v", err)
	}
	if user.Username != "instagram" {
		t.Errorf("username = %q, want instagram", user.Username)
	}

	if !strings.HasPrefix(*gotURL, igWebBaseURL+"/") {
		t.Errorf("page URL origin = %q, want %s/", *gotURL, igWebBaseURL)
	}
	if !strings.Contains(*gotScript, "/api/v1/users/web_profile_info/") {
		t.Errorf("script missing web_profile_info endpoint: %s", *gotScript)
	}
	if !strings.Contains(*gotScript, "username=instagram") {
		t.Errorf("script missing username param: %s", *gotScript)
	}
	if strings.Contains(*gotScript, igBaseURL) || strings.Contains(*gotScript, "i.instagram.com") {
		t.Errorf("script targeted mobile Instagram host: %q", *gotScript)
	}
}

func TestPrivateReads_CDP_TargetInstagramWebHost(t *testing.T) {
	tests := []struct {
		name    string
		call    func(*Client, context.Context) error
		wantSub string
	}{
		{
			name:    "GetUserFollowers",
			call:    func(c *Client, ctx context.Context) error { _, err := c.GetUserFollowers(ctx, "123", 5); return err },
			wantSub: "/api/v1/friendships/123/followers/",
		},
		{
			name:    "GetUserFollowing",
			call:    func(c *Client, ctx context.Context) error { _, err := c.GetUserFollowing(ctx, "123", 5); return err },
			wantSub: "/api/v1/friendships/123/following/",
		},
		{
			name:    "SearchUserPrivate",
			call:    func(c *Client, ctx context.Context) error { _, err := c.SearchUserPrivate(ctx, "threads"); return err },
			wantSub: "/api/v1/users/search/",
		},
		{
			name:    "GetThreadByID",
			call:    func(c *Client, ctx context.Context) error { _, _, err := c.GetThreadByID(ctx, "999"); return err },
			wantSub: "/api/v1/text_feed/999/replies/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockBody := `{"status":200,"body":"{\"users\":[],\"containing_thread\":{\"thread_items\":[]},\"reply_threads\":[]}"}`
			ts, gotURL, gotScript := cdpTestServer(t, "/api/v1/chrome/interact", mockBody)
			defer ts.Close()

			cfg := Config{WowaURL: ts.URL, Session: "ig-cdp"}
			c, err := NewClient(cfg)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			// We do not care about parsing the dummy response; only the request shape.
			_ = tt.call(c, context.Background())

			if !strings.HasPrefix(*gotURL, igWebBaseURL+"/") {
				t.Errorf("page URL origin = %q, want %s/", *gotURL, igWebBaseURL)
			}
			if !strings.Contains(*gotScript, tt.wantSub) {
				t.Errorf("script missing %q: %s", tt.wantSub, *gotScript)
			}
			if strings.Contains(*gotScript, igBaseURL) || strings.Contains(*gotScript, "i.instagram.com") {
				t.Errorf("script targeted mobile Instagram host: %q", *gotScript)
			}
		})
	}
}

// withZeroDelays replaces stealth delay defaults with near-zero values for the
// duration of the test and restores them on cleanup.
func withZeroDelays(t *testing.T) {
	t.Helper()
	oldJitter := stealth.DefaultJitter
	oldBackoff := stealth.DefaultBackoff
	t.Cleanup(func() {
		stealth.DefaultJitter = oldJitter
		stealth.DefaultBackoff = oldBackoff
	})
	stealth.DefaultJitter = stealth.Jitter{Min: 0, Max: 1}
	stealth.DefaultBackoff = stealth.BackoffConfig{InitialWait: 1, MaxWait: 1, Multiplier: 1}
}

func TestDoGraphQL_CDP_RedirectIsLoginRedirect(t *testing.T) {
	redirectBody := `{"redirected":true,"status":302}`
	ts, _, _ := cdpTestServer(t, "/api/v1/chrome/interact", redirectBody)
	defer ts.Close()
	withZeroDelays(t)

	var successMetrics []string
	cfg := Config{
		WowaURL: ts.URL,
		Session: "threads-cdp",
		MetricsHook: func(endpoint string, success bool) {
			if success {
				successMetrics = append(successMetrics, endpoint)
			}
		},
	}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.lsd = "AVqDh-2lkJ8"
	c.lsdAt = time.Now()

	_, err = c.doGraphQL(context.Background(), "Test", docIDGetThreadLikers, "BarcelonaMediaLikersQuery", map[string]any{"mediaID": "123"})
	if err == nil {
		t.Fatal("expected an error for a redirect")
	}
	if !IsLoginRedirect(err) {
		t.Fatalf("expected IsLoginRedirect, got %v", err)
	}
	if len(successMetrics) != 0 {
		t.Fatalf("success metric recorded on redirect: %v", successMetrics)
	}
}

func TestFetchPage_CDP_RedirectIsLoginRedirect(t *testing.T) {
	redirectBody := `{"redirected":true,"status":302}`
	ts, _, _ := cdpTestServer(t, "/api/v1/chrome/interact", redirectBody)
	defer ts.Close()
	withZeroDelays(t)

	var successMetrics []string
	cfg := Config{
		WowaURL: ts.URL,
		Session: "threads-cdp",
		MetricsHook: func(endpoint string, success bool) {
			if success {
				successMetrics = append(successMetrics, endpoint)
			}
		},
	}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.fetchPage(context.Background(), "Test", threadsBaseURL+"/@zuck/post/ABC123")
	if err == nil {
		t.Fatal("expected an error for a redirect")
	}
	if !IsLoginRedirect(err) {
		t.Fatalf("expected IsLoginRedirect, got %v", err)
	}
	if len(successMetrics) != 0 {
		t.Fatalf("success metric recorded on redirect: %v", successMetrics)
	}
}

// fakeHTTPDoer is a go-stealth backend that always returns 404, forcing the
// go-stealth leg to fail fast without network.
type fakeHTTPDoer struct{}

func (fakeHTTPDoer) Do(req *stealth.Request) (*stealth.Response, error) {
	return &stealth.Response{
		StatusCode: 404,
		Body:       []byte("not found"),
		Headers:    map[string]string{},
	}, nil
}
func (fakeHTTPDoer) SetProxy(url string) error                 { return nil }
func (fakeHTTPDoer) GetCookieValue(rawURL, name string) string { return "" }

func fakeBackendFactory(stealth.BackendConfig) (stealth.HTTPDoer, error) {
	return fakeHTTPDoer{}, nil
}

func TestReadMethods_WowaURLEmpty_KeepStealthPath(t *testing.T) {
	// These methods should not touch go-wowa when WowaURL is empty.
	// We wire the stealth client to a fake 404 backend so the network leg
	// fails fast and we can prove the error did not come from go-wowa.
	methods := []struct {
		name string
		call func(*Client, context.Context) error
	}{
		{name: "GetUser", call: func(c *Client, ctx context.Context) error { _, err := c.GetUser(ctx, "zuck"); return err }},
		{name: "GetUserThreads", call: func(c *Client, ctx context.Context) error { _, err := c.GetUserThreads(ctx, "zuck", 3); return err }},
		{name: "GetUserWithThreads", call: func(c *Client, ctx context.Context) error {
			_, _, err := c.GetUserWithThreads(ctx, "zuck", 3)
			return err
		}},
		{name: "GetUserReplies", call: func(c *Client, ctx context.Context) error { _, err := c.GetUserReplies(ctx, "zuck", 3); return err }},
		{name: "GetThread", call: func(c *Client, ctx context.Context) error {
			_, _, err := c.GetThread(ctx, "zuck", "ABC123")
			return err
		}},
		{name: "GetThreadLikers", call: func(c *Client, ctx context.Context) error { _, err := c.GetThreadLikers(ctx, "123", 5); return err }},
		{name: "GetUserFollowers", call: func(c *Client, ctx context.Context) error { _, err := c.GetUserFollowers(ctx, "123", 5); return err }},
		{name: "GetUserFollowing", call: func(c *Client, ctx context.Context) error { _, err := c.GetUserFollowing(ctx, "123", 5); return err }},
		{name: "SearchUserPrivate", call: func(c *Client, ctx context.Context) error { _, err := c.SearchUserPrivate(ctx, "threads"); return err }},
		{name: "GetThreadByID", call: func(c *Client, ctx context.Context) error { _, _, err := c.GetThreadByID(ctx, "999"); return err }},
		{name: "SearchUsers", call: func(c *Client, ctx context.Context) error { _, err := c.SearchUsers(ctx, "threads", 5); return err }},
	}

	for _, tt := range methods {
		t.Run(tt.name, func(t *testing.T) {
			bc, err := stealth.NewClient(
				stealth.WithBackend(fakeBackendFactory),
				stealth.WithHeaderOrder(threadsHeaderOrder),
			)
			if err != nil {
				t.Fatalf("stealth.NewClient: %v", err)
			}
			c := &Client{bc: bc, cfg: Config{Timeout: 1}}
			if c.wowa != nil {
				t.Fatal("expected no wowa transport when WowaURL is empty")
			}
			// Warm the LSD cache so doGraphQL does not try to fetch a page first.
			c.lsd = "cached-lsd"
			c.lsdAt = time.Now()

			err = tt.call(c, context.Background())
			if err == nil {
				t.Fatal("expected an error from the go-stealth path")
			}
			if strings.Contains(err.Error(), "go-wowa") {
				t.Fatalf("method routed through go-wowa with WowaURL empty: %v", err)
			}
		})
	}
}
