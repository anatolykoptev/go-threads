package threads

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// --- Raw response types for JSON unmarshalling ---

type rawBioLink struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type rawUser struct {
	Pk                   json.Number       `json:"pk"`
	Username             string            `json:"username"`
	FullName             string            `json:"full_name"`
	Biography            string            `json:"biography"`
	BioLinks             []rawBioLink      `json:"bio_links"`
	ProfilePicURL        string            `json:"profile_pic_url"`
	IsVerified           bool              `json:"is_verified"`
	IsPrivate            bool              `json:"text_post_app_is_private"`
	FollowerCount        int               `json:"follower_count"`
	FollowingCount       int               `json:"following_count"`
	ThreadCount          int               `json:"text_post_app_threads_count,omitempty"`
	HdProfilePicVersions []rawImageVersion `json:"hd_profile_pic_versions,omitempty"`
}

type rawImageVersion struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type rawVideoVersion struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Type   int    `json:"type"`
}

type rawPost struct {
	Pk        json.Number `json:"pk"`
	Code      string      `json:"code"`
	User      rawUser     `json:"user"`
	Caption   *struct {
		Text string `json:"text"`
	} `json:"caption"`
	TakenAt         json.Number      `json:"taken_at"`
	LikeCount       int              `json:"like_count"`
	TextPostAppInfo *rawTextPostInfo `json:"text_post_app_info"`
	MediaType       int              `json:"media_type"`
	ImageVersions2  *rawImageSet     `json:"image_versions2"`
	VideoVersions   []rawVideoVersion `json:"video_versions"`
	CarouselMedia   []rawCarouselItem `json:"carousel_media"`
}

type rawTextPostInfo struct {
	IsReply    bool `json:"is_reply,omitempty"`
	ReplyCount int  `json:"direct_reply_count,omitempty"`
}

type rawImageSet struct {
	Candidates []rawImageVersion `json:"candidates"`
}

type rawCarouselItem struct {
	MediaType      int              `json:"media_type"`
	ImageVersions2 *rawImageSet     `json:"image_versions2"`
	VideoVersions  []rawVideoVersion `json:"video_versions"`
}

type rawThreadItem struct {
	Post rawPost `json:"post"`
}

// --- SSR extraction ---

const ssrDataPrefix = `"result":{"data":`

// extractSSRBlocks finds all SSR data blocks in the HTML.
// Threads embeds preloaded query results as:
//   "result":{"data":{...}},"sequence_number":N
// We find each "result":{"data": prefix and extract the nested JSON object
// using brace-depth counting (regex won't work for nested JSON).
func extractSSRBlocks(html []byte) [][]byte {
	s := string(html)
	var blocks [][]byte
	searchFrom := 0

	for {
		idx := indexAt(s, ssrDataPrefix, searchFrom)
		if idx < 0 {
			break
		}
		// Position right after "result":{"data": — this is where the data object starts
		dataStart := idx + len(ssrDataPrefix)
		if dataStart >= len(s) || s[dataStart] != '{' {
			searchFrom = dataStart
			continue
		}

		// Extract the JSON object using brace-depth counting
		depth := 0
		dataEnd := -1
	scan:
		for i := dataStart; i < len(s); i++ {
			switch s[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					dataEnd = i + 1
					break scan
				}
			case '"':
				// Skip string contents (may contain braces)
				i++
				for i < len(s) && s[i] != '"' {
					if s[i] == '\\' {
						i++ // skip escaped char
					}
					i++
				}
			}
		}

		if dataEnd > dataStart {
			blocks = append(blocks, []byte(s[dataStart:dataEnd]))
		}
		searchFrom = max(dataEnd, dataStart+1)
	}
	return blocks
}

// indexAt is like strings.Index but starts searching from a given offset.
func indexAt(s, substr string, from int) int {
	if from >= len(s) {
		return -1
	}
	idx := strings.Index(s[from:], substr)
	if idx < 0 {
		return -1
	}
	return from + idx
}

// --- SSR Parsers ---

// parseUserFromSSR extracts user profile data from SSR HTML.
func parseUserFromSSR(html []byte) (*ThreadsUser, error) {
	for _, block := range extractSSRBlocks(html) {
		var probe struct {
			User *rawUser `json:"user"`
		}
		if json.Unmarshal(block, &probe) == nil && probe.User != nil && probe.User.Username != "" {
			return convertUser(*probe.User), nil
		}
	}
	return nil, fmt.Errorf("user data not found in SSR HTML")
}

// parseThreadsFromSSR extracts thread/post data from SSR HTML.
func parseThreadsFromSSR(html []byte) ([]*Thread, error) {
	for _, block := range extractSSRBlocks(html) {
		var probe struct {
			MediaData *struct {
				Edges []struct {
					Node struct {
						ThreadItems []rawThreadItem `json:"thread_items"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"mediaData"`
		}
		if json.Unmarshal(block, &probe) == nil && probe.MediaData != nil && len(probe.MediaData.Edges) > 0 {
			var threads []*Thread
			for _, edge := range probe.MediaData.Edges {
				t := &Thread{}
				for _, item := range edge.Node.ThreadItems {
					t.Items = append(t.Items, convertPost(item.Post))
				}
				if len(t.Items) > 0 {
					threads = append(threads, t)
				}
			}
			return threads, nil
		}
	}
	return nil, fmt.Errorf("thread data not found in SSR HTML")
}

// --- Legacy GraphQL parsers (kept for potential future use with authenticated API) ---

// parseUser parses a GetUser GraphQL response.
func parseUser(body []byte) (*ThreadsUser, error) {
	var raw struct {
		Data struct {
			User *rawUser `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal user: %w", err)
	}
	if raw.Data.User == nil {
		return nil, fmt.Errorf("user data is null")
	}
	return convertUser(*raw.Data.User), nil
}

// parseUserThreads parses a GetUserThreads GraphQL response.
func parseUserThreads(body []byte) ([]*Thread, error) {
	var raw struct {
		Data struct {
			MediaData struct {
				Threads []struct {
					ThreadItems []rawThreadItem `json:"thread_items"`
				} `json:"threads"`
			} `json:"mediaData"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal user threads: %w", err)
	}

	var threads []*Thread
	for _, rt := range raw.Data.MediaData.Threads {
		t := &Thread{}
		for _, item := range rt.ThreadItems {
			t.Items = append(t.Items, convertPost(item.Post))
		}
		if len(t.Items) > 0 {
			threads = append(threads, t)
		}
	}
	return threads, nil
}

// parseThread parses a GetThread (single thread + replies) GraphQL response.
func parseThread(body []byte) (*Thread, []*Thread, error) {
	var raw struct {
		Data struct {
			Data struct {
				ContainingThread struct {
					ThreadItems []rawThreadItem `json:"thread_items"`
				} `json:"containing_thread"`
				ReplyThreads []struct {
					ThreadItems []rawThreadItem `json:"thread_items"`
				} `json:"reply_threads"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("unmarshal thread: %w", err)
	}

	main := &Thread{}
	for _, item := range raw.Data.Data.ContainingThread.ThreadItems {
		main.Items = append(main.Items, convertPost(item.Post))
	}

	var replies []*Thread
	for _, rt := range raw.Data.Data.ReplyThreads {
		t := &Thread{}
		for _, item := range rt.ThreadItems {
			t.Items = append(t.Items, convertPost(item.Post))
		}
		if len(t.Items) > 0 {
			replies = append(replies, t)
		}
	}
	return main, replies, nil
}

// parseLikers parses a GetThreadLikers GraphQL response.
func parseLikers(body []byte) ([]*ThreadsUser, error) {
	var raw struct {
		Data struct {
			Likers struct {
				Users []rawUser `json:"users"`
			} `json:"likers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal likers: %w", err)
	}

	var users []*ThreadsUser
	for _, ru := range raw.Data.Likers.Users {
		users = append(users, convertUser(ru))
	}
	return users, nil
}

// --- Converters ---

func convertUser(ru rawUser) *ThreadsUser {
	picURL := ru.ProfilePicURL
	if picURL == "" && len(ru.HdProfilePicVersions) > 0 {
		picURL = ru.HdProfilePicVersions[0].URL
	}
	var bioLinks []BioLink
	for _, bl := range ru.BioLinks {
		bioLinks = append(bioLinks, BioLink{URL: bl.URL, Title: bl.Title})
	}
	return &ThreadsUser{
		ID:             ru.Pk.String(),
		Username:       ru.Username,
		FullName:       ru.FullName,
		Bio:            ru.Biography,
		BioLinks:       bioLinks,
		ProfilePicURL:  picURL,
		IsVerified:     ru.IsVerified,
		IsPrivate:      ru.IsPrivate,
		FollowerCount:  ru.FollowerCount,
		FollowingCount: ru.FollowingCount,
		ThreadCount:    ru.ThreadCount,
	}
}

func convertPost(rp rawPost) Post {
	p := Post{
		ID:        rp.Pk.String(),
		Code:      rp.Code,
		Author:    *convertUser(rp.User),
		MediaType: rp.MediaType,
		LikeCount: rp.LikeCount,
	}

	if rp.Caption != nil {
		p.Text = rp.Caption.Text
	}

	if ts, err := rp.TakenAt.Int64(); err == nil && ts > 0 {
		p.CreatedAt = time.Unix(ts, 0)
	}

	if rp.TextPostAppInfo != nil {
		p.IsReply = rp.TextPostAppInfo.IsReply
		p.ReplyCount = rp.TextPostAppInfo.ReplyCount
	}

	if rp.ImageVersions2 != nil {
		for _, img := range rp.ImageVersions2.Candidates {
			p.Images = append(p.Images, MediaVersion{URL: img.URL, Width: img.Width, Height: img.Height})
		}
	}

	for _, vid := range rp.VideoVersions {
		p.Videos = append(p.Videos, MediaVersion{URL: vid.URL, Width: vid.Width, Height: vid.Height})
	}

	for _, ci := range rp.CarouselMedia {
		if ci.ImageVersions2 != nil {
			for _, img := range ci.ImageVersions2.Candidates {
				p.Images = append(p.Images, MediaVersion{URL: img.URL, Width: img.Width, Height: img.Height})
			}
		}
		for _, vid := range ci.VideoVersions {
			p.Videos = append(p.Videos, MediaVersion{URL: vid.URL, Width: vid.Width, Height: vid.Height})
		}
	}

	return p
}

// --- GraphQL parsers ---

// parseSearchUsers parses a SearchUsers GraphQL response.
// Supports both legacy (searchResults.users) and current (xdt_api__v1__users__search_connection.edges) formats.
func parseSearchUsers(body []byte) ([]*ThreadsUser, error) {
	// Try current format first: data.xdt_api__v1__users__search_connection.edges
	var current struct {
		Data struct {
			SearchConnection struct {
				Edges []struct {
					Node struct {
						User rawUser `json:"text_post_app_user"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"xdt_api__v1__users__search_connection"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &current) == nil && len(current.Data.SearchConnection.Edges) > 0 {
		var users []*ThreadsUser
		for _, edge := range current.Data.SearchConnection.Edges {
			users = append(users, convertUser(edge.Node.User))
		}
		return users, nil
	}

	// Fallback: legacy format data.searchResults.users
	var legacy struct {
		Data struct {
			SearchResults struct {
				Users []struct {
					User rawUser `json:"user"`
				} `json:"users"`
			} `json:"searchResults"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &legacy); err != nil {
		return nil, fmt.Errorf("unmarshal search users: %w", err)
	}
	var users []*ThreadsUser
	for _, su := range legacy.Data.SearchResults.Users {
		users = append(users, convertUser(su.User))
	}
	return users, nil
}

// --- Private API parsers ---

// parsePrivateUserList parses a followers/following private API response.
func parsePrivateUserList(body []byte) ([]*ThreadsUser, string, error) {
	var raw struct {
		Users     []rawUser `json:"users"`
		NextMaxID string    `json:"next_max_id"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, "", fmt.Errorf("unmarshal private user list: %w", err)
	}
	var users []*ThreadsUser
	for _, ru := range raw.Users {
		users = append(users, convertUser(ru))
	}
	return users, raw.NextMaxID, nil
}

// parsePrivateThread parses a private API thread (text_feed) response.
func parsePrivateThread(body []byte) (*Thread, []*Thread, error) {
	var raw struct {
		ContainingThread struct {
			ThreadItems []rawThreadItem `json:"thread_items"`
		} `json:"containing_thread"`
		ReplyThreads []struct {
			ThreadItems []rawThreadItem `json:"thread_items"`
		} `json:"reply_threads"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("unmarshal private thread: %w", err)
	}

	main := &Thread{}
	for _, item := range raw.ContainingThread.ThreadItems {
		main.Items = append(main.Items, convertPost(item.Post))
	}

	var replies []*Thread
	for _, rt := range raw.ReplyThreads {
		t := &Thread{}
		for _, item := range rt.ThreadItems {
			t.Items = append(t.Items, convertPost(item.Post))
		}
		if len(t.Items) > 0 {
			replies = append(replies, t)
		}
	}
	return main, replies, nil
}
