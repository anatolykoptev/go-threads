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
	ID             string       `json:"id"`
	Pk             json.Number  `json:"pk"`
	Username       string       `json:"username"`
	FullName       string       `json:"full_name"`
	Biography      string       `json:"biography"`
	BioLinks       []rawBioLink `json:"bio_links"`
	ProfilePicURL  string       `json:"profile_pic_url"`
	IsVerified     bool         `json:"is_verified"`
	IsPrivate      bool         `json:"text_post_app_is_private"`
	IsPrivateIG    bool         `json:"is_private"`
	FollowerCount  int          `json:"follower_count"`
	FollowingCount int          `json:"following_count"`
	ThreadCount    int          `json:"text_post_app_threads_count,omitempty"`
	FollowedBy     *struct {
		Count int `json:"count"`
	} `json:"edge_followed_by,omitempty"`
	Following *struct {
		Count int `json:"count"`
	} `json:"edge_follow,omitempty"`
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

// flexBool is a bool that tolerates BOTH JSON encodings Instagram has shipped
// for flag fields: a real JSON boolean (true/false) AND a numeric 0/1. The
// authed /api/v1/media/<id>/info/ response delivers is_dash_eligible as a
// number (measured live: 1 for DO8cvGViIPu and DbISO9DIqoZ), while older
// responses and other flag fields use real booleans. Declaring the field as
// plain bool breaks the ENTIRE items[] unmarshal when the numeric form
// arrives (json: cannot unmarshal number into Go struct field ... of type
// bool), killing the authed CDP rung; declaring it int would just move the
// breakage to the boolean form. flexBool accepts both and exposes a plain
// bool, so the PUBLIC Post.IsDashEligible stays bool and consumers are
// unaffected.
type flexBool bool

// UnmarshalJSON accepts true/false, 0/1 (and numeric strings as a hedge).
// On any value it does not recognise it defaults to false and returns NO
// error — a single malformed boolean-ish field on one carousel slide must
// not be able to abort the whole items[] / carousel_media[] unmarshal (the
// v0.7.0 is_dash_eligible incident, #43, where one bad field killed the
// entire authed CDP rung). Tolerate-and-default is the safe direction.
func (f *flexBool) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	switch s {
	case "true", "1", `"1"`:
		*f = true
	case "false", "0", `"0"`, "null":
		*f = false
	default:
		*f = false
	}
	return nil
}

type rawPost struct {
	Pk      json.Number `json:"pk"`
	Code    string      `json:"code"`
	User    rawUser     `json:"user"`
	Caption *struct {
		Text string `json:"text"`
	} `json:"caption"`
	TakenAt   json.Number `json:"taken_at"`
	LikeCount int         `json:"like_count"`
	// Engagement counts the IG media API returns for reels/posts (captured live
	// from /api/v1/media/<id>/info/). play_count is the total views/plays
	// (includes FB crosspost plays); ig_play_count is the IG-only subset.
	// comment_count is the IG feed comment count (distinct from the Threads
	// direct_reply_count surfaced via text_post_app_info). media_repost_count
	// is the IG repost/reshare count. All optional — absent for image/text posts.
	ViewCount       int               `json:"play_count,omitempty"`
	IGPlayCount     int               `json:"ig_play_count,omitempty"`
	CommentCount    int               `json:"comment_count,omitempty"`
	RepostCount     int               `json:"media_repost_count,omitempty"`
	TextPostAppInfo *rawTextPostInfo  `json:"text_post_app_info"`
	MediaType       int               `json:"media_type"`
	ImageVersions2  *rawImageSet      `json:"image_versions2"`
	VideoVersions   []rawVideoVersion `json:"video_versions"`
	CarouselMedia   []rawCarouselItem `json:"carousel_media"`
	// DASH manifest fields — present only on the authed CDP/REST
	// /api/v1/media/<id>/info/ response (x-ig-app-id: 936619743392459). The
	// manifest is the ONLY place higher-than-720p video is available for
	// reels whose video_versions are all capped at 720x1280. Carried as a raw
	// XML STRING — go-threads owns the response shape, go-media owns MPD
	// parsing/selection/muxing. Absent on embed/SSR/proxy fallback rungs.
	VideoDashManifest string   `json:"video_dash_manifest,omitempty"`
	NumberOfQualities int      `json:"number_of_qualities,omitempty"`
	IsDashEligible    flexBool `json:"is_dash_eligible,omitempty"`
}

type rawTextPostInfo struct {
	IsReply    bool `json:"is_reply,omitempty"`
	ReplyCount int  `json:"direct_reply_count,omitempty"`
}

type rawImageSet struct {
	Candidates []rawImageVersion `json:"candidates"`
}

type rawCarouselItem struct {
	MediaType         int               `json:"media_type"`
	ImageVersions2    *rawImageSet      `json:"image_versions2"`
	VideoVersions     []rawVideoVersion `json:"video_versions"`
	VideoDashManifest string            `json:"video_dash_manifest,omitempty"`
	NumberOfQualities int               `json:"number_of_qualities,omitempty"`
	IsDashEligible    flexBool          `json:"is_dash_eligible,omitempty"`
}

type rawThreadItem struct {
	Post rawPost `json:"post"`
}

// --- SSR extraction ---

const ssrDataPrefix = `"result":{"data":`

// extractSSRBlocks finds all SSR data blocks in the HTML.
// Threads embeds preloaded query results as:
//
//	"result":{"data":{...}},"sequence_number":N
//
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
			User     *rawUser `json:"user"`
			UserData *struct {
				User rawUser `json:"user"`
			} `json:"userData"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal user: %w", err)
	}
	if raw.Data.User != nil {
		return convertUser(*raw.Data.User), nil
	}
	if raw.Data.UserData != nil {
		return convertUser(raw.Data.UserData.User), nil
	}
	return nil, fmt.Errorf("user data is null")
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

// parseSearchUsers parses a SearchUsers GraphQL response.
// Supports current (searchResults.edges[].node), legacy
// (xdt_api__v1__users__search_connection.edges[].node.text_post_app_user),
// and older (searchResults.users[]) formats.
func parseSearchUsers(body []byte) ([]*ThreadsUser, error) {
	var current struct {
		Data struct {
			SearchResults struct {
				Edges []struct {
					Node rawUser `json:"node"`
				} `json:"edges"`
			} `json:"searchResults"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &current) == nil && len(current.Data.SearchResults.Edges) > 0 {
		var users []*ThreadsUser
		for _, edge := range current.Data.SearchResults.Edges {
			if (edge.Node.Pk.String() == "" && edge.Node.ID == "") || edge.Node.Username == "" {
				continue
			}
			users = append(users, convertUser(edge.Node))
		}
		if len(users) > 0 {
			return users, nil
		}
	}

	var xdt struct {
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
	if json.Unmarshal(body, &xdt) == nil && len(xdt.Data.SearchConnection.Edges) > 0 {
		var users []*ThreadsUser
		for _, edge := range xdt.Data.SearchConnection.Edges {
			if edge.Node.User.Pk.String() == "" || edge.Node.User.Username == "" {
				continue
			}
			users = append(users, convertUser(edge.Node.User))
		}
		if len(users) > 0 {
			return users, nil
		}
	}

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
		if su.User.Pk.String() == "" || su.User.Username == "" {
			continue
		}
		users = append(users, convertUser(su.User))
	}
	return users, nil
}

// parseInstagramPost parses the /p/<shortcode>/?__a=1&__d=dis JSON response.
func parseInstagramPost(body []byte) (*Post, error) {
	var raw struct {
		Items []rawPost `json:"items"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal instagram post: %w", err)
	}
	if len(raw.Items) == 0 {
		return nil, fmt.Errorf("instagram post: no items")
	}
	post := convertPost(raw.Items[0])
	return &post, nil
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
	id := ru.Pk.String()
	if id == "" {
		id = ru.ID
	}
	isPrivate := ru.IsPrivate || ru.IsPrivateIG
	followerCount := ru.FollowerCount
	followingCount := ru.FollowingCount
	if ru.FollowedBy != nil && followerCount == 0 {
		followerCount = ru.FollowedBy.Count
	}
	if ru.Following != nil && followingCount == 0 {
		followingCount = ru.Following.Count
	}
	return &ThreadsUser{
		ID:             id,
		Username:       ru.Username,
		FullName:       ru.FullName,
		Bio:            ru.Biography,
		BioLinks:       bioLinks,
		ProfilePicURL:  picURL,
		IsVerified:     ru.IsVerified,
		IsPrivate:      isPrivate,
		FollowerCount:  followerCount,
		FollowingCount: followingCount,
		ThreadCount:    ru.ThreadCount,
	}
}

func convertPost(rp rawPost) Post {
	p := Post{
		ID:                rp.Pk.String(),
		Code:              rp.Code,
		Author:            *convertUser(rp.User),
		MediaType:         rp.MediaType,
		LikeCount:         rp.LikeCount,
		ViewCount:         rp.ViewCount,
		IGPlayCount:       rp.IGPlayCount,
		CommentCount:      rp.CommentCount,
		RepostCount:       rp.RepostCount,
		VideoDashManifest: rp.VideoDashManifest,
		NumberOfQualities: rp.NumberOfQualities,
		IsDashEligible:    bool(rp.IsDashEligible),
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

	p.CarouselItems = buildCarouselItems(rp)

	return p
}

// buildCarouselItems produces the ordered, structured slide list for a post.
//
// For a carousel (carousel_media non-empty) it maps each carousel_media[]
// entry to one CarouselItem in slide order, carrying the slide's own
// media_type, its image candidates, its video versions, and — for video
// slides — the per-slide DASH manifest fields.
//
// For a single-media post (no carousel_media but image_versions2 or
// video_versions present) it SYNTHESISES a one-item CarouselItem carrying
// the post's own media + DASH fields, so a consumer has ONE code path
// (range CarouselItems) regardless of whether the post is a carousel or a
// single photo/video. MediaType on the synthesised item is the post's own
// media_type (1 or 2), preserving the original type.
//
// For a non-media / text-only post it returns a non-nil empty slice so
// consumers can range over it unconditionally without a nil-panic.
//
// Post.Images / Post.Videos are populated separately and unchanged; this
// function is purely additive.
func buildCarouselItems(rp rawPost) []CarouselItem {
	switch {
	case len(rp.CarouselMedia) > 0:
		items := make([]CarouselItem, 0, len(rp.CarouselMedia))
		for _, ci := range rp.CarouselMedia {
			items = append(items, carouselItemFromRaw(ci.MediaType, ci.ImageVersions2, ci.VideoVersions,
				ci.VideoDashManifest, ci.NumberOfQualities, bool(ci.IsDashEligible)))
		}
		return items
	case rp.ImageVersions2 != nil || len(rp.VideoVersions) > 0:
		return []CarouselItem{carouselItemFromRaw(rp.MediaType, rp.ImageVersions2, rp.VideoVersions,
			rp.VideoDashManifest, rp.NumberOfQualities, bool(rp.IsDashEligible))}
	default:
		return []CarouselItem{}
	}
}

// carouselItemFromRaw fills a CarouselItem from the raw media fields. It does
// NOT flatten candidates across slides — each item carries only its own.
func carouselItemFromRaw(mediaType int, iv *rawImageSet, vv []rawVideoVersion, dashManifest string, nQual int, dashElig bool) CarouselItem {
	item := CarouselItem{
		MediaType:         mediaType,
		VideoDashManifest: dashManifest,
		NumberOfQualities: nQual,
		IsDashEligible:    dashElig,
	}
	if iv != nil {
		for _, img := range iv.Candidates {
			item.Images = append(item.Images, MediaVersion{URL: img.URL, Width: img.Width, Height: img.Height})
		}
	}
	for _, vid := range vv {
		item.Videos = append(item.Videos, MediaVersion{URL: vid.URL, Width: vid.Width, Height: vid.Height})
	}
	return item
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
