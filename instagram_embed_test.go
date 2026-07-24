package threads

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Fixtures ---

// embedContextJSONVideo builds a realistic embed HTML page containing a
// double-encoded contextJSON blob for a video reel, matching the format IG
// serves on /reel/<code>/embed/ as of July 2026. The contextJSON value is a
// JSON string that itself contains escaped JSON — two layers of encoding.
func embedContextJSONVideo(shortcode, videoURL string) string {
	inner := map[string]any{
		"context": map[string]any{
			"type":              "GraphVideo",
			"shortcode":         shortcode,
			"copyright_blocked": false,
		},
		"gql_data": map[string]any{
			"shortcode_media": map[string]any{
				"__typename":  "GraphVideo",
				"id":          "3947485265850968601",
				"shortcode":   shortcode,
				"is_video":    true,
				"video_url":   videoURL,
				"display_url": "https://scontent-sjc3-1.cdninstagram.com/v/t51.82787-15/753990204_n.jpg",
				"dimensions":  map[string]int{"width": 1080, "height": 1920},
				"edge_media_to_caption": map[string]any{
					"edges": []map[string]any{
						{"node": map[string]any{"text": "Test caption for reel"}},
					},
				},
				"owner": map[string]any{
					"id":       "123456",
					"username": "testuser",
				},
			},
		},
	}
	innerJSON, _ := json.Marshal(inner)
	// Double-encode: marshal the inner JSON as a JSON string value. This
	// produces "{\"context\":{...}}" with \" and \/ escapes — exactly what
	// the real embed page contains.
	encoded, _ := json.Marshal(string(innerJSON))
	return `<html><head></head><body><script nonce="abc">requireLazy([],function(){s.handle({"define":[["BootloaderConfig",[],{}]]});});</script>` +
		`<script nonce="abc">{"contextJSON":` + string(encoded) + `}</script>` +
		`</body></html>`
}

// embedContextJSONImage builds an embed HTML page with a contextJSON blob for
// an image post (is_video=false, no video_url, display_url populated).
func embedContextJSONImage(shortcode, displayURL string) string {
	inner := map[string]any{
		"context": map[string]any{
			"type":      "GraphImage",
			"shortcode": shortcode,
		},
		"gql_data": map[string]any{
			"shortcode_media": map[string]any{
				"__typename":  "GraphImage",
				"id":          "9988776655",
				"shortcode":   shortcode,
				"is_video":    false,
				"display_url": displayURL,
				"dimensions":  map[string]int{"width": 1080, "height": 1080},
				"edge_media_to_caption": map[string]any{
					"edges": []map[string]any{
						{"node": map[string]any{"text": "Image post caption"}},
					},
				},
				"owner": map[string]any{
					"id":       "123456",
					"username": "imguser",
				},
			},
		},
	}
	innerJSON, _ := json.Marshal(inner)
	encoded, _ := json.Marshal(string(innerJSON))
	return `<html><body><script>{"contextJSON":` + string(encoded) + `}</script></body></html>`
}

// embedContextJSONNull is an embed page where contextJSON is null (the /p/
// embed path returns this for reels — no data).
const embedContextJSONNull = `<html><body><script>{"ProfileEmbed":false,"contextJSON":null}</script></body></html>`

// embedNoContextJSON is a page with no contextJSON at all (e.g. a login wall).
const embedNoContextJSON = `<html><body>nothing useful here</body></html>`

// --- Tests ---

// TestParseEmbedContextJSON_VideoReel proves the modern contextJSON parser
// extracts a clean video_url from the double-encoded blob.
//
// RED before fix: the old embedVideoURLRe regex ("video_url":"([^"]+)") matches
// 0 occurrences on the escaped form \"video_url\":\"https:\\\/\\\/...\" — the
// parser returns "no video_url" and the embed tier fails.
// GREEN after fix: parseEmbedContextJSON double-decodes the contextJSON and
// returns a Thread with Videos[0].URL == the clean mp4 URL.
func TestParseEmbedContextJSON_VideoReel(t *testing.T) {
	wantURL := "https://scontent-sjc6-1.cdninstagram.com/o1/v/t2/f2/m86/AQNp8XykN1d_CkfHNxojkh0VtSr1aSOLup2dkuGnLP13vYl4mAuBryXqART3yIoKc-_fU5ilgH9RTftqFI07ybQPlxj99FNkHZqorYc.mp4?_nc_cat=100&oe=6A65A20D"
	html := embedContextJSONVideo("DbISO9DIqoZ", wantURL)

	thread := parseInstagramEmbed([]byte(html), "DbISO9DIqoZ")
	if thread == nil {
		t.Fatal("parseInstagramEmbed returned nil for contextJSON video fixture")
	}
	if len(thread.Items) == 0 {
		t.Fatal("expected at least one post")
	}
	post := thread.Items[0]
	if post.MediaType != mediaTypeVideo {
		t.Errorf("MediaType = %d, want %d (video)", post.MediaType, mediaTypeVideo)
	}
	if len(post.Videos) == 0 {
		t.Fatal("expected at least one video version")
	}
	if post.Videos[0].URL != wantURL {
		t.Errorf("video URL = %q, want %q", post.Videos[0].URL, wantURL)
	}
	if post.Videos[0].Width != 1080 || post.Videos[0].Height != 1920 {
		t.Errorf("dimensions = %dx%d, want 1080x1920", post.Videos[0].Width, post.Videos[0].Height)
	}
	if post.ID != "3947485265850968601" {
		t.Errorf("ID = %q, want 3947485265850968601", post.ID)
	}
	if post.Text != "Test caption for reel" {
		t.Errorf("Text = %q, want %q", post.Text, "Test caption for reel")
	}
	if post.Author.Username != "testuser" {
		t.Errorf("Author.Username = %q, want testuser", post.Author.Username)
	}
}

// TestParseEmbedContextJSON_ImagePost proves the parser handles image posts
// (is_video=false, no video_url, display_url populated).
func TestParseEmbedContextJSON_ImagePost(t *testing.T) {
	wantURL := "https://scontent-sjc3-1.cdninstagram.com/v/t51.2885-19/392819267_n.jpg"
	html := embedContextJSONImage("CgRsVjljOx2", wantURL)

	thread := parseInstagramEmbed([]byte(html), "CgRsVjljOx2")
	if thread == nil {
		t.Fatal("parseInstagramEmbed returned nil for contextJSON image fixture")
	}
	post := thread.Items[0]
	if post.MediaType != 1 {
		t.Errorf("MediaType = %d, want 1 (image)", post.MediaType)
	}
	if len(post.Images) == 0 {
		t.Fatal("expected at least one image version")
	}
	if post.Images[0].URL != wantURL {
		t.Errorf("image URL = %q, want %q", post.Images[0].URL, wantURL)
	}
	if post.Text != "Image post caption" {
		t.Errorf("Text = %q, want %q", post.Text, "Image post caption")
	}
}

// TestParseEmbedContextJSON_Null returns nil (the /p/ embed path for reels).
func TestParseEmbedContextJSON_Null(t *testing.T) {
	thread := parseInstagramEmbed([]byte(embedContextJSONNull), "DbISO9DIqoZ")
	if thread != nil {
		t.Errorf("expected nil for contextJSON:null, got thread with %d items", len(thread.Items))
	}
}

// TestParseEmbedContextJSON_VideoNoURL returns nil when is_video=true but
// video_url is empty (some reels load the URL via JS, not in contextJSON).
// The parser must bail so fallback paths can try, not return a video with
// an empty URL.
func TestParseEmbedContextJSON_VideoNoURL(t *testing.T) {
	inner := map[string]any{
		"context": map[string]any{"type": "GraphVideo", "shortcode": "C4fzxe1P5JN"},
		"gql_data": map[string]any{
			"shortcode_media": map[string]any{
				"__typename":     "GraphVideo",
				"id":             "3323602750754755149",
				"shortcode":      "C4fzxe1P5JN",
				"is_video":       true,
				"video_url":      "",
				"display_url":    "https://scontent.cdninstagram.com/cover.jpg",
				"video_duration": 54.402,
				"dimensions":     map[string]int{"width": 720, "height": 1280},
			},
		},
	}
	innerJSON, _ := json.Marshal(inner)
	encoded, _ := json.Marshal(string(innerJSON))
	html := `<html><body><script>{"contextJSON":` + string(encoded) + `}</script></body></html>`

	thread := parseInstagramEmbed([]byte(html), "C4fzxe1P5JN")
	if thread != nil {
		t.Errorf("expected nil for is_video=true with empty video_url, got thread with %d items", len(thread.Items))
	}
}

// TestParseEmbed_NoData returns nil for a page with no contextJSON and no
// legacy-format data.
func TestParseEmbed_NoData(t *testing.T) {
	thread := parseInstagramEmbed([]byte(embedNoContextJSON), "DbISO9DIqoZ")
	if thread != nil {
		t.Errorf("expected nil for no-data page, got thread with %d items", len(thread.Items))
	}
}

// TestParseEmbed_LegacyFormatStillWorks proves the old unescaped-JSON embed
// format (used by existing tests and possibly older embed pages) still parses
// via the fallback regex path.
func TestParseEmbed_LegacyFormatStillWorks(t *testing.T) {
	html := `<html><body><script>{"shortcode_media":{"__typename":"GraphVideo","id":"123456","shortcode":"ABC123DEF","video_url":"https://cdninstagram.com/video.mp4"}}</script></body></html>`
	thread := parseInstagramEmbed([]byte(html), "ABC123DEF")
	if thread == nil {
		t.Fatal("expected thread from legacy embed format")
	}
	if len(thread.Items[0].Videos) == 0 {
		t.Fatal("expected video from legacy embed format")
	}
	if thread.Items[0].Videos[0].URL != "https://cdninstagram.com/video.mp4" {
		t.Errorf("video URL = %q, want https://cdninstagram.com/video.mp4", thread.Items[0].Videos[0].URL)
	}
}

// TestParseEmbed_OGVideoFallback proves the og:video meta tag is used as a
// last resort when neither contextJSON nor legacy data is present.
func TestParseEmbed_OGVideoFallback(t *testing.T) {
	html := `<html><head><meta property="og:video" content="https://cdninstagram.com/ogvideo.mp4"></head><body></body></html>`
	thread := parseInstagramEmbed([]byte(html), "ABC123DEF")
	if thread == nil {
		t.Fatal("expected thread from og:video fallback")
	}
	if len(thread.Items[0].Videos) == 0 {
		t.Fatal("expected video from og:video")
	}
	if thread.Items[0].Videos[0].URL != "https://cdninstagram.com/ogvideo.mp4" {
		t.Errorf("video URL = %q, want https://cdninstagram.com/ogvideo.mp4", thread.Items[0].Videos[0].URL)
	}
}

// --- Ladder order test ---

// embedLadderServer is a fake go-wowa server for ladder-order tests. The CDP
// API returns a challenge body (fails), and the page fetch returns a
// contextJSON embed page (succeeds). This mirrors the cdpFallbackServer
// pattern but with a modern contextJSON embed fixture.
func embedLadderServer(t *testing.T, apiBody, pageHTML string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chrome/interact" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req wowaInteractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		isPageFetch := false
		for _, a := range req.Actions {
			if a.Type == "navigate" {
				isPageFetch = true
				break
			}
		}
		var data json.RawMessage
		if isPageFetch {
			encoded, _ := json.Marshal(pageHTML)
			data = encoded
		} else if len(req.Actions) > 0 && strings.TrimSpace(req.Actions[len(req.Actions)-1].Script) == "document.cookie" {
			b, _ := json.Marshal("ds_user_id=123; csrftoken=csrf; mid=mid")
			data = json.RawMessage(b)
		} else {
			data = json.RawMessage(apiBody)
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
}

// TestGetInstagramPost_CDPFails_EmbedSucceeds_SourceMethod proves that when
// CDP fails, the embed tier (now FIRST in the fallback chain) succeeds and
// SourceMethod is set to "embed".
//
// RED before fix: the old code tried proxy first (404 for reels), then embed
// with the broken /p/ path + broken regex → all tiers fail. SourceMethod was
// not set.
// GREEN after fix: embed is tried first with /reel/ path + contextJSON parser
// → returns a valid thread with SourceMethod="embed".
func TestGetInstagramPost_CDPFails_EmbedSucceeds_SourceMethod(t *testing.T) {
	withZeroDelays(t)

	apiBody := `{"status":200,"body":"<html>challenge/login wall</html>"}`
	embedHTML := embedContextJSONVideo("ABC123DEF", "https://scontent.cdninstagram.com/video.mp4")

	ts := embedLadderServer(t, apiBody, embedHTML)
	defer ts.Close()

	cfg := Config{WowaURL: ts.URL, Session: "ig-cdp"}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	thread, err := c.GetInstagramPost(context.Background(), "ABC123DEF")
	if err != nil {
		t.Fatalf("GetInstagramPost: expected embed fallback to succeed, got: %v", err)
	}
	if thread == nil || len(thread.Items) == 0 {
		t.Fatal("expected a thread from embed fallback")
	}
	if len(thread.Items[0].Videos) == 0 {
		t.Fatal("expected video post from embed fallback")
	}
	if thread.SourceMethod != "embed" {
		t.Errorf("SourceMethod = %q, want \"embed\"", thread.SourceMethod)
	}
}
