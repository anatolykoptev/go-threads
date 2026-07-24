package threads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
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
// When Config.WowaURL is set the request is routed through the browser CDP
// transport as an in-page fetch to www.instagram.com (preferred). If the CDP
// method fails (transient challenge, empty response, rate-limit) the legacy
// proxy -> embed -> SSR fallback chain is attempted. If WowaURL is unset only
// the legacy chain is used.
func (c *Client) GetInstagramPost(ctx context.Context, shortcode string) (*Thread, error) {
	var cdpErr error
	if c.wowa != nil {
		thread, err := c.getInstagramViaCDP(ctx, shortcode)
		if err == nil && thread != nil && len(thread.Items) > 0 {
			thread.SourceMethod = "cdp"
			return thread, nil
		}
		cdpErr = err
		slog.Warn("instagram: CDP method failed, falling back to public chain",
			slog.String("shortcode", shortcode),
			slog.Any("error", err))
	}

	// Method 1: embed page (public, no auth — the working fallback tier).
	// Tries /reel/<code>/embed/ first (reels), then /p/<code>/embed/ (posts).
	thread, err := c.getInstagramViaEmbed(ctx, shortcode)
	if err == nil && thread != nil && len(thread.Items) > 0 {
		thread.SourceMethod = "embed"
		slog.Info("instagram: embed fallback succeeded (degraded — no engagement metrics)",
			slog.String("shortcode", shortcode))
		return thread, nil
	}
	embedErr := err
	if err != nil {
		slog.Debug("instagram: embed method failed", slog.String("shortcode", shortcode), slog.String("error", err.Error()))
	}

	// Method 2: direct page SSR (requires session cookies).
	thread, err = c.getInstagramViaSSR(ctx, shortcode)
	if err == nil && thread != nil && len(thread.Items) > 0 {
		thread.SourceMethod = "ssr"
		return thread, nil
	}
	ssrErr := err
	if err != nil {
		slog.Debug("instagram: SSR method failed", slog.String("shortcode", shortcode), slog.String("error", err.Error()))
	}

	// Method 3 (LAST RESORT): kkinstagram.com proxy — often 404/403 for reels,
	// kept last in case the service revives. Returns video URL only.
	thread, err = c.getInstagramViaProxy(ctx, shortcode)
	if err == nil && thread != nil && hasVideo(thread) {
		thread.SourceMethod = "proxy"
		return thread, nil
	}
	proxyErr := err
	if err != nil {
		slog.Debug("instagram: proxy method failed", slog.String("shortcode", shortcode), slog.String("error", err.Error()))
	}

	// Surface every method's error so prod can diagnose the failure. Wrap the
	// CDP error (the preferred method) with %w so callers can errors.Is/As it.
	if cdpErr != nil {
		return nil, fmt.Errorf("GetInstagramPost: all methods failed for %s (embed: %v; ssr: %v; proxy: %v): %w",
			shortcode, embedErr, ssrErr, proxyErr, cdpErr)
	}
	return nil, fmt.Errorf("GetInstagramPost: all methods failed for %s (embed: %v; ssr: %v; proxy: %v)",
		shortcode, embedErr, ssrErr, proxyErr)
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

// GetInstagramUser fetches an Instagram user profile by username using the
// authenticated web API. Requires Config.WowaURL (CDP transport).
func (c *Client) GetInstagramUser(ctx context.Context, username string) (*ThreadsUser, error) {
	if c.wowa == nil {
		return nil, fmt.Errorf("GetInstagramUser: CDP transport required (set WowaURL in Config)")
	}

	params := url.Values{}
	params.Set("username", username)
	body, err := c.doCDP(ctx, "GetInstagramUser", http.MethodGet, "/api/v1/users/web_profile_info/", params)
	if err != nil {
		return nil, fmt.Errorf("GetInstagramUser: %w", err)
	}

	user, err := parseUser(body)
	if err != nil {
		return nil, fmt.Errorf("GetInstagramUser: %w", err)
	}
	return user, nil
}

// getInstagramViaCDP decodes the shortcode to a media id and fetches the
// post JSON via the web /api/v1/media/<id>/info/ endpoint in-page from
// www.instagram.com.
func (c *Client) getInstagramViaCDP(ctx context.Context, shortcode string) (*Thread, error) {
	mediaID, err := mediaIDFromShortcode(shortcode)
	if err != nil {
		return nil, fmt.Errorf("decode shortcode %q: %w", shortcode, err)
	}

	path := fmt.Sprintf("/api/v1/media/%s/info/", mediaID)
	body, err := c.doCDP(ctx, "GetInstagramPost", http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	post, err := parseInstagramPost(body)
	if err != nil {
		return nil, err
	}
	return &Thread{Items: []Post{*post}}, nil
}

const igShortcodeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// mediaIDFromShortcode decodes an Instagram shortcode to its numeric media id.
func mediaIDFromShortcode(shortcode string) (string, error) {
	n := big.NewInt(0)
	for _, r := range shortcode {
		idx := strings.IndexRune(igShortcodeAlphabet, r)
		if idx < 0 {
			return "", fmt.Errorf("invalid character %q in shortcode", r)
		}
		n.Mul(n, big.NewInt(64))
		n.Add(n, big.NewInt(int64(idx)))
	}
	return n.String(), nil
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

// --- Method 1: Instagram embed page (public, no auth) ---

// embedGraphVideoRe extracts the GraphVideo JSON blob from embed page HTML.
// Instagram embeds post data as escaped JSON inside a <script> tag.
var embedGraphVideoRe = regexp.MustCompile(
	`"__typename"\s*:\s*"GraphVideo"\s*,\s*"id"\s*:\s*"(\d+)"\s*,\s*"shortcode"\s*:\s*"([^"]+)"`,
)

// embedVideoURLRe extracts video_url from the embed's JSON data (unescaped form).
var embedVideoURLRe = regexp.MustCompile(`"video_url"\s*:\s*"([^"]+)"`)

// embedCaptionRe extracts caption text.
var embedCaptionRe = regexp.MustCompile(`"edge_media_to_caption"\s*:\s*\{"edges"\s*:\s*\[\s*\{"node"\s*:\s*\{"text"\s*:\s*"([^"]*)"`)

// embedMediaShape is the subset of the contextJSON gql_data.shortcode_media
// structure we extract from the embed page.
type embedMediaShape struct {
	Typename   string `json:"__typename"`
	ID         string `json:"id"`
	Shortcode  string `json:"shortcode"`
	IsVideo    bool   `json:"is_video"`
	VideoURL   string `json:"video_url"`
	DisplayURL string `json:"display_url"`
	Dimensions struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"dimensions"`
	EdgeMediaToCaption struct {
		Edges []struct {
			Node struct {
				Text string `json:"text"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"edge_media_to_caption"`
	Owner struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"owner"`
}

// getInstagramViaEmbed fetches the embed page and parses the contextJSON blob.
// For reels it tries /reel/<code>/embed/ first (the /p/ path returns a JS shell
// with no data for reels); for posts it falls back to /p/<code>/embed/.
// The embed page returns 200 without auth and contains post metadata as a
// double-encoded JSON string inside a "contextJSON" field.
func (c *Client) getInstagramViaEmbed(ctx context.Context, shortcode string) (*Thread, error) {
	// Try /reel/ embed first — this is the path that works for reels. The /p/
	// embed returns a JS-only shell with contextJSON:null for reels.
	for _, suffix := range []string{"/reel/", "/p/"} {
		embedURL := igWebBaseURL + suffix + shortcode + "/embed/"
		html, err := c.fetchPage(ctx, "GetInstagramEmbed", embedURL)
		if err != nil {
			slog.Debug("instagram: embed fetch failed",
				slog.String("shortcode", shortcode),
				slog.String("path", suffix),
				slog.String("error", err.Error()))
			continue
		}
		if thread := parseInstagramEmbed(html, shortcode); thread != nil {
			return thread, nil
		}
	}
	return nil, fmt.Errorf("embed: no media data extracted for %s", shortcode)
}

// parseInstagramEmbed parses embed page HTML and returns a Thread. It tries
// the modern contextJSON double-encoded blob first, then falls back to the
// legacy raw-regex parser, then to og:video meta. Returns nil if nothing
// parseable is found.
func parseInstagramEmbed(html []byte, shortcode string) *Thread {
	// Path 1: modern contextJSON (double-encoded JSON string).
	if thread := parseEmbedContextJSON(html, shortcode); thread != nil {
		return thread
	}

	s := string(html)

	// Path 2: legacy raw-regex parser (unescaped JSON in a <script> tag —
	// the format older embed pages and our test fixtures use).
	if embedGraphVideoRe.MatchString(s) {
		videoURL := extractEscapedField(s, "video_url")
		if videoURL == "" {
			if m := embedVideoURLRe.FindStringSubmatch(s); len(m) > 1 {
				videoURL = m[1]
			}
		}
		if videoURL != "" {
			videoURL = strings.ReplaceAll(videoURL, `\/`, `/`)
			videoURL = strings.ReplaceAll(videoURL, "&amp;", "&")

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
			if m := embedGraphVideoRe.FindStringSubmatch(s); len(m) > 1 {
				post.ID = m[1]
			}
			return &Thread{Items: []Post{post}}
		}
	}

	// Path 2b: legacy image embed (data-media-id + display_url).
	if thread := parseInstagramEmbedImage(html, shortcode); thread != nil {
		return thread
	}

	// Path 3: og:video meta tag (last resort).
	if thread, err := parseInstagramOGMeta(html); err == nil && thread != nil {
		if thread.Items[0].Code == "" {
			thread.Items[0].Code = shortcode
		}
		return thread
	}

	return nil
}

// parseEmbedContextJSON extracts the double-encoded contextJSON blob from the
// embed HTML and unmarshals the gql_data.shortcode_media object. Returns nil
// if the blob is absent, null, or doesn't contain usable media data.
func parseEmbedContextJSON(html []byte, shortcode string) *Thread {
	// Locate "contextJSON":"..." — the value is a JSON string (double-encoded).
	idx := bytes.Index(html, []byte(`"contextJSON"`))
	if idx < 0 {
		return nil
	}
	// Find the opening quote of the string value after the colon.
	rest := html[idx+len(`"contextJSON"`):]
	colon := bytes.IndexByte(rest, ':')
	if colon < 0 {
		return nil
	}
	rest = rest[colon+1:]
	// Skip whitespace.
	rest = bytes.TrimLeft(rest, " \t")
	if len(rest) == 0 {
		return nil
	}
	// Check for null.
	if rest[0] == 'n' && bytes.HasPrefix(rest, []byte("null")) {
		return nil
	}
	// Must be a string starting with ".
	if rest[0] != '"' {
		return nil
	}

	// Extract the raw JSON string value (between the opening and closing
	// unescaped quote). We need to handle \\ and \" escape sequences.
	rawStr, ok := extractJSONStringValue(rest)
	if !ok {
		return nil
	}

	// rawStr is a valid JSON quoted string (e.g. "{\"context\":...}").
	// Use json.Unmarshal to decode it — it handles all JSON escape sequences
	// including \/ (forward slash), \uXXXX, \" — which strconv.Unquote does
	// NOT support (Go string literals don't allow \/).
	var innerJSON string
	if err := json.Unmarshal([]byte(rawStr), &innerJSON); err != nil {
		return nil
	}

	// innerJSON is now the unescaped JSON object: {"context":{...},"gql_data":{"shortcode_media":{...}}}
	var wrapper struct {
		Context struct {
			Type      string `json:"type"`
			Shortcode string `json:"shortcode"`
		} `json:"context"`
		GQLData struct {
			ShortcodeMedia embedMediaShape `json:"shortcode_media"`
		} `json:"gql_data"`
	}
	if json.Unmarshal([]byte(innerJSON), &wrapper) != nil {
		return nil
	}
	media := wrapper.GQLData.ShortcodeMedia
	if media.ID == "" && media.VideoURL == "" && media.DisplayURL == "" {
		return nil
	}

	// If the post is a video but the embed didn't include video_url (some
	// reels load it via JS only), we can't return a downloadable URL — bail
	// so the fallback paths (SSR, og:video, proxy) can try.
	if media.IsVideo && media.VideoURL == "" {
		return nil
	}

	post := Post{
		ID:   media.ID,
		Code: shortcode,
	}
	if media.Shortcode != "" {
		post.Code = media.Shortcode
	}

	// Caption.
	if len(media.EdgeMediaToCaption.Edges) > 0 {
		post.Text = media.EdgeMediaToCaption.Edges[0].Node.Text
	}

	if media.VideoURL != "" {
		post.MediaType = mediaTypeVideo
		videoURL := strings.ReplaceAll(media.VideoURL, "&amp;", "&")
		v := MediaVersion{URL: videoURL, Width: media.Dimensions.Width, Height: media.Dimensions.Height}
		post.Videos = []MediaVersion{v}
	} else {
		post.MediaType = 1 // image
		displayURL := strings.ReplaceAll(media.DisplayURL, "&amp;", "&")
		post.Images = []MediaVersion{{URL: displayURL, Width: media.Dimensions.Width, Height: media.Dimensions.Height}}
	}

	if media.Owner.Username != "" {
		post.Author = ThreadsUser{
			ID:       media.Owner.ID,
			Username: media.Owner.Username,
		}
	}

	return &Thread{Items: []Post{post}}
}

// extractJSONStringValue extracts a JSON string value starting at s[0]=='"',
// returning the full quoted string (including surrounding quotes). It handles
// \\ and \" escape sequences to find the matching closing quote.
func extractJSONStringValue(s []byte) (string, bool) {
	if len(s) < 2 || s[0] != '"' {
		return "", false
	}
	var b strings.Builder
	b.WriteByte('"')
	i := 1
	for i < len(s) {
		c := s[i]
		if c == '\\' {
			// Copy the escape sequence verbatim (backslash + next char).
			if i+1 >= len(s) {
				return "", false
			}
			b.WriteByte(c)
			b.WriteByte(s[i+1])
			i += 2
			continue
		}
		if c == '"' {
			b.WriteByte('"')
			return b.String(), true
		}
		b.WriteByte(c)
		i++
	}
	return "", false
}

// parseInstagramEmbedImage extracts image post data from embed page (legacy
// data-media-id path). Returns nil if no data-media-id is found.
func parseInstagramEmbedImage(html []byte, shortcode string) *Thread {
	mediaIDRe := regexp.MustCompile(`data-media-id="(\d+)"`)
	m := mediaIDRe.FindSubmatch(html)
	if len(m) < 2 {
		return nil
	}

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

	return &Thread{Items: []Post{post}}
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
