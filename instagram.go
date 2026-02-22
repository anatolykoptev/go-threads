package threads

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	// kkinstagram is an embed-proxy service that resolves Instagram video CDN URLs.
	// GET request returns 302 redirect to the actual video file.
	kkInstagramBase = "https://kkinstagram.com"

	// mediaTypeVideo is Instagram's media_type for video posts.
	mediaTypeVideo = 2
)

// GetInstagramPost fetches a post from instagram.com by shortcode.
// Works with /p/{code}/ and /reel/{code}/ URLs.
//
// Strategy (ordered by reliability):
//  1. kkinstagram.com proxy — redirect to CDN video URL (no auth needed)
//  2. Instagram embed page — parse GraphQL data from /p/{code}/embed/
//  3. Direct SSR scraping — requires session cookies in Config
func (c *Client) GetInstagramPost(ctx context.Context, shortcode string) (*Thread, error) {
	// Method 1: kkinstagram.com proxy (most reliable, no auth)
	thread, err := c.getInstagramViaProxy(ctx, shortcode)
	if err == nil && thread != nil && hasVideo(thread) {
		return thread, nil
	}
	if err != nil {
		slog.Debug("instagram: proxy method failed", slog.String("shortcode", shortcode), slog.String("error", err.Error()))
	}

	// Method 2: embed page GraphQL data (no auth, parses JS data)
	thread, err = c.getInstagramViaEmbed(ctx, shortcode)
	if err == nil && thread != nil && len(thread.Items) > 0 {
		return thread, nil
	}
	if err != nil {
		slog.Debug("instagram: embed method failed", slog.String("shortcode", shortcode), slog.String("error", err.Error()))
	}

	// Method 3: direct page SSR (requires session cookies)
	thread, err = c.getInstagramViaSSR(ctx, shortcode)
	if err == nil && thread != nil && len(thread.Items) > 0 {
		return thread, nil
	}
	if err != nil {
		slog.Debug("instagram: SSR method failed", slog.String("shortcode", shortcode), slog.String("error", err.Error()))
	}

	return nil, fmt.Errorf("GetInstagramPost: all methods failed for %s", shortcode)
}

// hasVideo checks if thread contains at least one post with video.
func hasVideo(t *Thread) bool {
	for _, p := range t.Items {
		if len(p.Videos) > 0 {
			return true
		}
	}
	return false
}

// --- Method 1: kkinstagram.com proxy ---

// getInstagramViaProxy uses kkinstagram.com to resolve the video CDN URL.
// The service returns a 302 redirect to the actual .mp4 file.
func (c *Client) getInstagramViaProxy(ctx context.Context, shortcode string) (*Thread, error) {
	proxyURL := kkInstagramBase + "/reel/" + shortcode + "/"

	// Use a client that does NOT follow redirects — we want the Location header.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "TelegramBot (like TwitterBot)")

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse // stop on first redirect
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy request: %w", err)
	}
	defer resp.Body.Close()

	// kkinstagram returns 302 with Location pointing to CDN
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently ||
		resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect {
		location := resp.Header.Get("Location")
		if location != "" && strings.Contains(location, "cdninstagram.com") {
			post := Post{
				Code:      shortcode,
				MediaType: mediaTypeVideo,
				Videos:    []MediaVersion{{URL: location}},
			}
			return &Thread{Items: []Post{post}}, nil
		}
	}

	// Some proxy services return 200 with og:video in HTML body
	if resp.StatusCode == http.StatusOK {
		// Check if we got redirected to the CDN directly (curl -L behavior)
		finalURL := resp.Request.URL.String()
		if strings.Contains(finalURL, "cdninstagram.com") && strings.Contains(finalURL, ".mp4") {
			post := Post{
				Code:      shortcode,
				MediaType: mediaTypeVideo,
				Videos:    []MediaVersion{{URL: finalURL}},
			}
			return &Thread{Items: []Post{post}}, nil
		}
	}

	return nil, fmt.Errorf("proxy: no redirect to CDN (status %d)", resp.StatusCode)
}

// --- Method 2: Instagram embed page ---

// embedGraphVideoRe extracts the GraphVideo JSON blob from embed page HTML.
// Instagram embeds post data as escaped JSON inside a <script> tag.
var embedGraphVideoRe = regexp.MustCompile(
	`"__typename"\s*:\s*"GraphVideo"\s*,\s*"id"\s*:\s*"(\d+)"\s*,\s*"shortcode"\s*:\s*"([^"]+)"`,
)

// embedVideoURLRe extracts video_url from the embed's JSON data.
var embedVideoURLRe = regexp.MustCompile(`"video_url"\s*:\s*"([^"]+)"`)

// embedCaptionRe extracts caption text.
var embedCaptionRe = regexp.MustCompile(`"edge_media_to_caption"\s*:\s*\{"edges"\s*:\s*\[\s*\{"node"\s*:\s*\{"text"\s*:\s*"([^"]*)"`)

// getInstagramViaEmbed fetches the /p/{code}/embed/ page and parses GraphQL data.
// The embed page returns 200 even without auth and contains post metadata in JS.
func (c *Client) getInstagramViaEmbed(ctx context.Context, shortcode string) (*Thread, error) {
	embedURL := igWebBaseURL + "/p/" + shortcode + "/embed/"
	html, err := c.fetchPage(ctx, "GetInstagramEmbed", embedURL)
	if err != nil {
		return nil, fmt.Errorf("fetch embed: %w", err)
	}

	s := string(html)

	// Check if it's a video post
	if !embedGraphVideoRe.MatchString(s) {
		// Not a video — try to extract image data instead
		return parseInstagramEmbedImage(html, shortcode)
	}

	// Extract video URL from the escaped JSON data
	videoURL := extractEscapedField(s, "video_url")
	if videoURL == "" {
		// Try unescaped variant
		if m := embedVideoURLRe.FindStringSubmatch(s); len(m) > 1 {
			videoURL = m[1]
		}
	}

	if videoURL == "" {
		return nil, fmt.Errorf("embed: GraphVideo found but no video_url")
	}

	// Unescape URL (embed page escapes slashes)
	videoURL = strings.ReplaceAll(videoURL, `\/`, `/`)
	videoURL = strings.ReplaceAll(videoURL, "&amp;", "&")

	// Extract caption
	caption := extractEscapedField(s, "text")
	if caption == "" {
		if m := embedCaptionRe.FindStringSubmatch(s); len(m) > 1 {
			caption = unescapeJSON(m[1])
		}
	}

	post := Post{
		Code:      shortcode,
		Text:      caption,
		MediaType: mediaTypeVideo,
		Videos:    []MediaVersion{{URL: videoURL}},
	}

	// Extract media ID
	if m := embedGraphVideoRe.FindStringSubmatch(s); len(m) > 1 {
		post.ID = m[1]
	}

	return &Thread{Items: []Post{post}}, nil
}

// parseInstagramEmbedImage extracts image post data from embed page.
func parseInstagramEmbedImage(html []byte, shortcode string) (*Thread, error) {
	// Look for data-media-id attribute (present in embed for all post types)
	mediaIDRe := regexp.MustCompile(`data-media-id="(\d+)"`)
	m := mediaIDRe.FindSubmatch(html)
	if len(m) < 2 {
		return nil, fmt.Errorf("embed: no media data found")
	}

	// Extract display_url for images
	displayURL := extractEscapedFieldBytes(html, "display_url")
	if displayURL != "" {
		displayURL = strings.ReplaceAll(displayURL, `\/`, `/`)
		displayURL = strings.ReplaceAll(displayURL, "&amp;", "&")
	}

	post := Post{
		ID:        string(m[1]),
		Code:      shortcode,
		MediaType: 1, // image
	}
	if displayURL != "" {
		post.Images = []MediaVersion{{URL: displayURL}}
	}

	return &Thread{Items: []Post{post}}, nil
}

// extractEscapedField finds a JSON field value in potentially escaped JSON.
// Handles both "field":"value" and "field":"value" (escaped quotes).
func extractEscapedField(s, field string) string {
	// Try escaped JSON first (\\\"field\\\":\\\"value\\\")
	escapedRe := regexp.MustCompile(`\\"` + field + `\\"\s*:\s*\\"([^\\]*(?:\\.[^\\]*)*)\\"`)
	if m := escapedRe.FindStringSubmatch(s); len(m) > 1 {
		return unescapeJSON(m[1])
	}
	// Try normal JSON ("field":"value")
	normalRe := regexp.MustCompile(`"` + field + `"\s*:\s*"([^"]*)"`)
	if m := normalRe.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractEscapedFieldBytes(html []byte, field string) string {
	return extractEscapedField(string(html), field)
}

// unescapeJSON unescapes common JSON escape sequences.
func unescapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\/`, `/`)
	s = strings.ReplaceAll(s, `\\n`, "\n")
	s = strings.ReplaceAll(s, `\\t`, "\t")
	s = strings.ReplaceAll(s, `\\"`, `"`)
	s = strings.ReplaceAll(s, `\\\\`, `\`)
	return s
}

// --- Method 3: Direct SSR (requires session cookies) ---

// getInstagramViaSSR fetches the post page directly and parses SSR data.
// This only works when session cookies are provided in Config.
func (c *Client) getInstagramViaSSR(ctx context.Context, shortcode string) (*Thread, error) {
	postURL := igWebBaseURL + "/p/" + shortcode + "/"
	html, err := c.fetchPage(ctx, "GetInstagramPost", postURL)
	if err != nil {
		return nil, fmt.Errorf("fetch page: %w", err)
	}

	// Try Threads-compatible SSR parser
	if thread, _, ssrErr := parseThreadFromSSR(html); ssrErr == nil && thread != nil && len(thread.Items) > 0 {
		return thread, nil
	}

	// Try Instagram-specific SSR structures
	if thread, err := parseInstagramSSR(html); err == nil && thread != nil && len(thread.Items) > 0 {
		return thread, nil
	}

	// Try ld+json
	if thread, err := parseInstagramLDJSON(html); err == nil && thread != nil && len(thread.Items) > 0 {
		return thread, nil
	}

	// Try og:video meta
	if thread, err := parseInstagramOGMeta(html); err == nil && thread != nil && len(thread.Items) > 0 {
		return thread, nil
	}

	return nil, fmt.Errorf("SSR: no data extracted")
}

// --- SSR sub-parsers (used by method 3) ---

func parseInstagramSSR(html []byte) (*Thread, error) {
	for _, block := range extractSSRBlocks(html) {
		var probeShortcode struct {
			ShortcodeMedia *rawPost `json:"shortcode_media"`
		}
		if json.Unmarshal(block, &probeShortcode) == nil && probeShortcode.ShortcodeMedia != nil {
			post := convertPost(*probeShortcode.ShortcodeMedia)
			return &Thread{Items: []Post{post}}, nil
		}

		var probeXDT struct {
			XDTMedia *struct {
				Items []rawPost `json:"items"`
			} `json:"xdt_api__v1__media__shortcode__web_info"`
		}
		if json.Unmarshal(block, &probeXDT) == nil && probeXDT.XDTMedia != nil && len(probeXDT.XDTMedia.Items) > 0 {
			post := convertPost(probeXDT.XDTMedia.Items[0])
			return &Thread{Items: []Post{post}}, nil
		}

		var probeMedia struct {
			Items []rawPost `json:"items"`
		}
		if json.Unmarshal(block, &probeMedia) == nil && len(probeMedia.Items) > 0 {
			rp := probeMedia.Items[0]
			if rp.Code != "" || len(rp.VideoVersions) > 0 {
				post := convertPost(rp)
				return &Thread{Items: []Post{post}}, nil
			}
		}
	}
	return nil, fmt.Errorf("instagram SSR: no matching block found")
}

func parseInstagramLDJSON(html []byte) (*Thread, error) {
	ldJSONRe := regexp.MustCompile(`<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>`)
	matches := ldJSONRe.FindAllSubmatch(html, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}

		var obj struct {
			Type       string `json:"@type"`
			ContentURL string `json:"contentUrl"`
			Desc       string `json:"description"`
			Upload     string `json:"uploadDate"`
		}
		if json.Unmarshal(m[1], &obj) != nil || obj.Type != "VideoObject" || obj.ContentURL == "" {
			continue
		}

		post := Post{
			Text:      obj.Desc,
			MediaType: mediaTypeVideo,
			Videos:    []MediaVersion{{URL: obj.ContentURL}},
		}
		if obj.Upload != "" {
			if t, err := time.Parse(time.RFC3339, obj.Upload); err == nil {
				post.CreatedAt = t
			}
		}
		return &Thread{Items: []Post{post}}, nil
	}
	return nil, fmt.Errorf("ld+json: no VideoObject found")
}

func parseInstagramOGMeta(html []byte) (*Thread, error) {
	s := string(html)

	ogVideoRe := regexp.MustCompile(`<meta\s+(?:property="og:video"\s+content="([^"]+)"|content="([^"]+)"\s+property="og:video")`)
	m := ogVideoRe.FindStringSubmatch(s)
	videoURL := ""
	if len(m) > 1 {
		videoURL = m[1]
		if videoURL == "" {
			videoURL = m[2]
		}
	}
	if videoURL == "" {
		return nil, fmt.Errorf("og:video meta tag not found")
	}

	videoURL = strings.ReplaceAll(videoURL, "&amp;", "&")

	post := Post{
		MediaType: mediaTypeVideo,
		Videos:    []MediaVersion{{URL: videoURL}},
	}
	return &Thread{Items: []Post{post}}, nil
}
