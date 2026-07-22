package threads

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBuildFetchScript_WebEndpointAndHeaders(t *testing.T) {
	script, err := buildFetchScript(
		"https://www.instagram.com/api/v1/web/likes/123/like/",
		http.MethodPost,
		"",
		igWebAppID,
		"",
		"",
	)
	if err != nil {
		t.Fatalf("buildFetchScript: %v", err)
	}

	want := []string{
		`"https://www.instagram.com/api/v1/web/likes/123/like/"`,
		`credentials:"include"`,
		`redirect:"manual"`,
		`"x-csrftoken":csrf`,
		`"x-ig-app-id":"936619743392459"`,
		`"x-requested-with":"XMLHttpRequest"`,
		`["content-type"] = "application/x-www-form-urlencoded"`,
	}
	for _, w := range want {
		if !strings.Contains(script, w) {
			t.Errorf("script missing %q", w)
		}
	}
}

func TestBuildFetchScript_GraphQLHeaders(t *testing.T) {
	script, err := buildFetchScript(
		"https://www.threads.net/api/graphql",
		http.MethodPost,
		"doc_id=123&variables=%7B%7D",
		igAppID,
		"AVqDh-2lkJ8",
		"BarcelonaProfileRootQuery",
	)
	if err != nil {
		t.Fatalf("buildFetchScript: %v", err)
	}

	want := []string{
		`"https://www.threads.net/api/graphql"`,
		`const lsd = "AVqDh-2lkJ8";`,
		`opts.headers["x-fb-lsd"] = lsd;`,
		`opts.headers["x-fb-friendly-name"] = "BarcelonaProfileRootQuery";`,
		`"x-ig-app-id":"238260118697367"`,
	}
	for _, w := range want {
		if !strings.Contains(script, w) {
			t.Errorf("script missing %q", w)
		}
	}
}

func TestDoCDP_WebEndpointAndHeaders(t *testing.T) {
	var gotScript string
	var gotSecret string
	var gotContentType bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chrome/interact" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		gotSecret = r.Header.Get("X-Internal-Secret")
		gotContentType = r.Header.Get("Content-Type") == "application/json"

		var req wowaInteractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Actions) == 0 {
			http.Error(w, "no actions", http.StatusBadRequest)
			return
		}
		gotScript = req.Actions[len(req.Actions)-1].Script

		resp := wowaInteractResponse{
			URL:    req.URL,
			Status: "ok",
			Actions: []wowaActionResult{
				{
					Action: "evaluate",
					Ok:     true,
					Data:   json.RawMessage(`{"status":200,"body":"{\"users\":[]}"}`),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "threads-spike", InternalSecret: "shh"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.wowa == nil {
		t.Fatal("expected wowa transport to be created")
	}

	body, err := c.doPrivateGET(context.Background(), "GetUserFollowers", "/api/v1/friendships/123/followers/", url.Values{"count": []string{"5"}})
	if err != nil {
		t.Fatalf("doPrivateGET: %v", err)
	}
	if string(body) != `{"users":[]}` {
		t.Errorf("body = %q, want {\"users\":[]}", string(body))
	}
	if gotSecret != "shh" {
		t.Errorf("X-Internal-Secret = %q, want shh", gotSecret)
	}
	if !gotContentType {
		t.Error("expected Content-Type application/json on go-wowa request")
	}

	wantParts := []string{
		"https://www.instagram.com/api/v1/friendships/123/followers/?count=5",
		`credentials:"include"`,
		`redirect:"manual"`,
	}
	for _, w := range wantParts {
		if !strings.Contains(gotScript, w) {
			t.Errorf("script missing %q", w)
		}
	}
}

func TestDoCDP_ClassifiesRedirectBeforeBody(t *testing.T) {
	call := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		var req wowaInteractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// First call is evaluate; second call should be navigate + evaluate.
		if call == 2 {
			if len(req.Actions) < 2 || req.Actions[0].Type != "navigate" || !strings.HasPrefix(req.Actions[0].URL, "https://www.instagram.com/") {
				t.Error("expected re-navigate to instagram.com before retry evaluate")
			}
		}

		resp := wowaInteractResponse{
			URL:    req.URL,
			Status: "ok",
			Actions: []wowaActionResult{
				{
					Action: "evaluate",
					Ok:     true,
					Data:   json.RawMessage(`{"redirected":true,"status":302}`),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "threads-spike"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.doPrivateGET(context.Background(), "GetUserFollowers", "/api/v1/friendships/123/followers/", nil)
	if err == nil {
		t.Fatal("expected redirect error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != 302 {
		t.Errorf("status = %d, want 302", apiErr.Status)
	}
	if apiErr.Class != errLoginRedirect {
		t.Errorf("class = %v, want errLoginRedirect", apiErr.Class)
	}
}

func TestDoCDP_RetryLegErrorNotSyntheticRedirect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req wowaInteractRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		if len(req.Actions) > 1 && req.Actions[0].Type == "navigate" {
			http.Error(w, "boom", http.StatusServiceUnavailable)
			return
		}

		resp := wowaInteractResponse{
			URL:    req.URL,
			Status: "ok",
			Actions: []wowaActionResult{
				{
					Action: "evaluate",
					Ok:     true,
					Data:   json.RawMessage(`{"redirected":true,"status":302}`),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "threads-spike"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.doPrivateGET(context.Background(), "GetUserFollowers", "/api/v1/friendships/123/followers/", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "302") {
		t.Fatalf("retry transport error misclassified as 302: %v", err)
	}
	if !strings.Contains(err.Error(), "go-wowa interact (retry)") {
		t.Errorf("expected wrapped retry error, got %v", err)
	}
}

func TestWowaURLEmpty_KeepsStealthPath(t *testing.T) {
	cfg := Config{}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.wowa != nil {
		t.Fatal("expected no wowa transport when WowaURL is empty")
	}

	_, err = c.doPrivateGET(context.Background(), "GetUserFollowers", "/api/v1/friendships/123/followers/", nil)
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("expected go-stealth 'not authenticated' error, got %v", err)
	}
}

func TestDoCDP_LikeTargetsWebEndpoint(t *testing.T) {
	var gotScript string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req wowaInteractRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Actions) > 0 {
			gotScript = req.Actions[len(req.Actions)-1].Script
		}
		resp := wowaInteractResponse{
			URL:    req.URL,
			Status: "ok",
			Actions: []wowaActionResult{
				{Action: "evaluate", Ok: true, Data: json.RawMessage(`{"status":200,"body":"{\"status\":\"ok\"}"}`)},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "threads-spike"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	body, err := c.doPrivateAPI(context.Background(), "LikeThread", "/api/v1/media/456_789/like/", url.Values{"media_id": []string{"456"}})
	if err != nil {
		t.Fatalf("doPrivateAPI: %v", err)
	}
	if string(body) != `{"status":"ok"}` {
		t.Errorf("body = %q", string(body))
	}
	if !strings.Contains(gotScript, "https://www.instagram.com/api/v1/web/likes/456/like/") {
		t.Errorf("script did not target web like endpoint: %s", gotScript)
	}
}

func TestDoCDP_PublishNotImplemented(t *testing.T) {
	cfg := Config{WowaURL: "http://example.invalid", Session: "threads-spike"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.doPrivateAPI(context.Background(), "PublishThread", pathPublishText, url.Values{"caption": []string{"hello"}})
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
	if !strings.Contains(err.Error(), "not implemented") && !strings.Contains(err.Error(), "TODO") {
		t.Errorf("expected not implemented/TODO error, got %v", err)
	}
}

// extractJSONString is a tiny helper used by some tests to fish string values
// out of the generated fetch script when needed.
func extractJSONString(t *testing.T, script, key string) string {
	t.Helper()
	start := strings.Index(script, `"`+key+`":"`)
	if start == -1 {
		t.Fatalf("key %q not found in script", key)
	}
	start += len(key) + 4
	end := start
	for ; end < len(script); end++ {
		if script[end] == '"' && script[end-1] != '\\' {
			break
		}
	}
	return script[start:end]
}

func TestParseFetchResult(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    fetchResult
		wantErr bool
	}{
		{
			name: "ok",
			data: `{"status":200,"body":"ok"}`,
			want: fetchResult{Status: 200, Body: "ok"},
		},
		{
			name: "redirected",
			data: `{"redirected":true,"status":302}`,
			want: fetchResult{Redirected: true, Status: 302},
		},
		{
			name:    "empty",
			data:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fr, err := parseFetchResult(json.RawMessage(tt.data))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseFetchResult err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if fr != tt.want {
				t.Errorf("fr = %+v, want %+v", fr, tt.want)
			}
		})
	}
}

// drainBody is used by tests to keep go vet happy when we don't care about response bodies.
func drainBody(r *http.Response) {
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
}

func TestWowaTransportInteract_RequestShape(t *testing.T) {
	var got wowaInteractRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		resp := wowaInteractResponse{
			URL:    got.URL,
			Status: "ok",
			Actions: []wowaActionResult{
				{Action: "evaluate", Ok: true, Data: json.RawMessage(`{"status":200,"body":"ok"}`)},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	w := newWowaTransport(ts.URL, "secret")
	_, err := w.interact(context.Background(), "threads-spike", "https://www.instagram.com/", []wowaAction{{Type: "evaluate", Script: "1"}})
	if err != nil {
		t.Fatalf("interact: %v", err)
	}
	if got.URL != "https://www.instagram.com/" {
		t.Errorf("url = %q", got.URL)
	}
	if got.Session != "threads-spike" {
		t.Errorf("session = %q", got.Session)
	}
	if got.Mode != "default" {
		t.Errorf("mode = %q", got.Mode)
	}
}
