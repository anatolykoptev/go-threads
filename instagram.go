package threads

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// GetInstagramPost fetches a post from instagram.com by shortcode.
// Works with /p/{code}/ and /reel/{code}/ URLs.
func (c *Client) GetInstagramPost(ctx context.Context, shortcode string) (*Thread, error) {
	postURL := igWebBaseURL + "/p/" + shortcode + "/"
	html, err := c.fetchPage(ctx, "GetInstagramPost", postURL)
	if err != nil {
		return nil, fmt.Errorf("GetInstagramPost: %w", err)
	}

	// Try Threads-compatible SSR parser first (both platforms share Meta backend)
	thread, _, ssrErr := parseThreadFromSSR(html)
	if ssrErr == nil && thread != nil && len(thread.Items) > 0 {
		return thread, nil
	}

	// Fallback: Instagram-specific SSR extraction
	thread, err = parseInstagramSSR(html)
	if err == nil && thread != nil && len(thread.Items) > 0 {
		return thread, nil
	}

	// Fallback: extract from ld+json structured data
	thread, err = parseInstagramLDJSON(html)
	if err == nil && thread != nil && len(thread.Items) > 0 {
		return thread, nil
	}

	// Last resort: extract video URL from og:video meta tag
	thread, err = parseInstagramOGMeta(html)
	if err == nil && thread != nil && len(thread.Items) > 0 {
		return thread, nil
	}

	return nil, fmt.Errorf("GetInstagramPost: could not extract post data for %s", shortcode)
}

// parseInstagramSSR tries Instagram-specific SSR block structures.
// Instagram may embed post data under keys like "xdt_api__v1__media__shortcode__web_info"
// or "shortcode_media".
func parseInstagramSSR(html []byte) (*Thread, error) {
	for _, block := range extractSSRBlocks(html) {
		// Try "shortcode_media" (Instagram GraphQL structure)
		var probeShortcode struct {
			ShortcodeMedia *rawPost `json:"shortcode_media"`
		}
		if json.Unmarshal(block, &probeShortcode) == nil && probeShortcode.ShortcodeMedia != nil {
			post := convertPost(*probeShortcode.ShortcodeMedia)
			return &Thread{Items: []Post{post}}, nil
		}

		// Try xdt_api structure (newer Instagram SSR)
		var probeXDT struct {
			XDTMedia *struct {
				Items []rawPost `json:"items"`
			} `json:"xdt_api__v1__media__shortcode__web_info"`
		}
		if json.Unmarshal(block, &probeXDT) == nil && probeXDT.XDTMedia != nil && len(probeXDT.XDTMedia.Items) > 0 {
			post := convertPost(probeXDT.XDTMedia.Items[0])
			return &Thread{Items: []Post{post}}, nil
		}

		// Try direct media item (some pages embed items at top level)
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

// ldJSONVideoObject represents a VideoObject from ld+json.
type ldJSONVideoObject struct {
	Type        string `json:"@type"`
	ContentURL  string `json:"contentUrl"`
	Description string `json:"description"`
	Name        string `json:"name"`
	UploadDate  string `json:"uploadDate"`
	Width       string `json:"width"`
	Height      string `json:"height"`
}

var ldJSONRe = regexp.MustCompile(`<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>`)

// parseInstagramLDJSON extracts video info from <script type="application/ld+json">.
func parseInstagramLDJSON(html []byte) (*Thread, error) {
	matches := ldJSONRe.FindAllSubmatch(html, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}

		var obj ldJSONVideoObject
		if err := json.Unmarshal(m[1], &obj); err != nil {
			// Could be an array
			var arr []ldJSONVideoObject
			if json.Unmarshal(m[1], &arr) == nil {
				for _, item := range arr {
					if item.Type == "VideoObject" && item.ContentURL != "" {
						obj = item
						break
					}
				}
			}
			if obj.ContentURL == "" {
				continue
			}
		}

		if obj.Type != "VideoObject" || obj.ContentURL == "" {
			continue
		}

		post := Post{
			Text:      obj.Description,
			MediaType: 2, // video
			Videos:    []MediaVersion{{URL: obj.ContentURL}},
		}

		if obj.UploadDate != "" {
			if t, err := time.Parse(time.RFC3339, obj.UploadDate); err == nil {
				post.CreatedAt = t
			}
		}

		return &Thread{Items: []Post{post}}, nil
	}
	return nil, fmt.Errorf("ld+json: no VideoObject found")
}

var (
	ogVideoRe      = regexp.MustCompile(`<meta\s+property="og:video"\s+content="([^"]+)"`)
	ogVideoAltRe   = regexp.MustCompile(`<meta\s+content="([^"]+)"\s+property="og:video"`)
	ogDescRe       = regexp.MustCompile(`<meta\s+property="og:description"\s+content="([^"]+)"`)
	ogDescAltRe    = regexp.MustCompile(`<meta\s+content="([^"]+)"\s+property="og:description"`)
)

// parseInstagramOGMeta extracts video URL from og:video meta tags.
func parseInstagramOGMeta(html []byte) (*Thread, error) {
	s := string(html)

	var videoURL string
	if m := ogVideoRe.FindStringSubmatch(s); len(m) > 1 {
		videoURL = m[1]
	} else if m := ogVideoAltRe.FindStringSubmatch(s); len(m) > 1 {
		videoURL = m[1]
	}

	if videoURL == "" {
		return nil, fmt.Errorf("og:video meta tag not found")
	}

	// Unescape HTML entities
	videoURL = strings.ReplaceAll(videoURL, "&amp;", "&")

	var description string
	if m := ogDescRe.FindStringSubmatch(s); len(m) > 1 {
		description = m[1]
	} else if m := ogDescAltRe.FindStringSubmatch(s); len(m) > 1 {
		description = m[1]
	}

	post := Post{
		Text:      description,
		MediaType: 2, // video
		Videos:    []MediaVersion{{URL: videoURL}},
	}

	return &Thread{Items: []Post{post}}, nil
}
