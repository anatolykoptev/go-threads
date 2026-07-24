package threads

import (
	"os"
	"strings"
	"testing"
)

// TestGetInstagramViaEmbed_RealReelHTML is the REAL-HTML gate for the embed
// parser. It loads a real embed page captured live from
// https://www.instagram.com/reel/DbISO9DIqoZ/embed/ (testdata/embed_reel_real.html)
// and runs the FULL embed parse path (parseInstagramEmbed → parseEmbedContextJSON
// + the extractEscapedField fallback exactly as getInstagramViaEmbed calls them).
//
// Unlike the synthetic fixtures in instagram_embed_test.go (which build a
// gql_data.shortcode_media blob in-process), this proves the parser handles the
// REAL escaping Instagram serves: the contextJSON value is a JSON string whose
// inner JSON itself contains \/ and \\\/ escape layers, and the video_url is
// nested under gql_data.shortcode_media.
//
// If this test fails, the embed fallback tier does NOT work on real reels and
// downloads silently fall through to SSR/proxy (which 403/404 for reels).
func TestGetInstagramViaEmbed_RealReelHTML(t *testing.T) {
	html, err := os.ReadFile("testdata/embed_reel_real.html")
	if err != nil {
		t.Fatalf("read fixture: %v — run the capture step from the task brief", err)
	}

	// Run the full embed parse path — exactly what getInstagramViaEmbed calls
	// after fetching the page.
	thread := parseInstagramEmbed(html, "DbISO9DIqoZ")
	if thread == nil {
		t.Fatal("parseInstagramEmbed returned nil for real reel embed HTML — " +
			"the embed fallback tier cannot extract video from real reels")
	}
	if len(thread.Items) == 0 {
		t.Fatal("expected at least one post from real reel embed")
	}
	post := thread.Items[0]

	if post.MediaType != mediaTypeVideo {
		t.Errorf("MediaType = %d, want %d (video)", post.MediaType, mediaTypeVideo)
	}
	if len(post.Videos) == 0 {
		t.Fatal("expected at least one video version from real reel embed")
	}

	url := post.Videos[0].URL
	if url == "" {
		t.Fatal("video URL is empty")
	}
	if !strings.HasPrefix(url, "https://") {
		t.Errorf("video URL = %q, want https:// prefix", url)
	}
	// Real Instagram video CDN hosts. The mp4 may be under a video CDN host
	// (scontent-*.cdninstagram.com) and the path contains .mp4.
	if !strings.Contains(url, "cdninstagram.com") {
		t.Errorf("video URL = %q, want a cdninstagram.com host", url)
	}
	if !strings.Contains(url, ".mp4") {
		t.Errorf("video URL = %q, want a .mp4 segment", url)
	}
	t.Logf("real reel extracted mp4 URL: %s", url)

	// Dimensions must flow through — the downstream vaelor fix needs them.
	// The real blob carries dimensions width=1284 height=2283.
	if post.Videos[0].Width != 1284 {
		t.Errorf("video Width = %d, want 1284 (from real blob dimensions.width)", post.Videos[0].Width)
	}
	if post.Videos[0].Height != 2283 {
		t.Errorf("video Height = %d, want 2283 (from real blob dimensions.height)", post.Videos[0].Height)
	}

	// Caption should be present (the real reel has a Cyrillic caption).
	if post.Text == "" {
		t.Error("expected non-empty caption from real reel embed")
	} else {
		t.Logf("real reel caption: %q", post.Text)
	}
}
