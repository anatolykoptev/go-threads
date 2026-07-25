package threads

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseUserFromSSR(t *testing.T) {
	// Simulates the __bbox SSR block found in real page HTML
	html := []byte(`
		<html><script>some stuff</script>
		<script>"result":{"data":{"user":{"pk":"63404918397","text_post_app_is_private":false,"has_onboarded_to_text_post_app":true,"profile_pic_url":"https://example.com/pic.jpg","username":"instagram","follower_count":35930885,"hd_profile_pic_versions":[{"height":320,"url":"https://example.com/hd.jpg","width":320}],"is_verified":true,"biography":"Discover what's new","full_name":"Instagram","bio_links":[{"url":"https://instagram.com","title":"Instagram"}],"following_count":500,"text_post_app_threads_count":1234}},"sequence_number":0}</script>
	`)

	user, err := parseUserFromSSR(html)
	if err != nil {
		t.Fatalf("parseUserFromSSR: %v", err)
	}
	if user.ID != "63404918397" {
		t.Errorf("ID = %q, want %q", user.ID, "63404918397")
	}
	if user.Username != "instagram" {
		t.Errorf("Username = %q, want %q", user.Username, "instagram")
	}
	if user.FullName != "Instagram" {
		t.Errorf("FullName = %q, want %q", user.FullName, "Instagram")
	}
	if !user.IsVerified {
		t.Error("IsVerified = false, want true")
	}
	if user.IsPrivate {
		t.Error("IsPrivate = true, want false")
	}
	if user.FollowerCount != 35930885 {
		t.Errorf("FollowerCount = %d, want 35930885", user.FollowerCount)
	}
	if user.FollowingCount != 500 {
		t.Errorf("FollowingCount = %d, want 500", user.FollowingCount)
	}
	if user.ThreadCount != 1234 {
		t.Errorf("ThreadCount = %d, want 1234", user.ThreadCount)
	}
	if user.Bio != "Discover what's new" {
		t.Errorf("Bio = %q", user.Bio)
	}
	if len(user.BioLinks) != 1 {
		t.Fatalf("len(BioLinks) = %d, want 1", len(user.BioLinks))
	}
	if user.BioLinks[0].URL != "https://instagram.com" {
		t.Errorf("BioLinks[0].URL = %q", user.BioLinks[0].URL)
	}
	if user.BioLinks[0].Title != "Instagram" {
		t.Errorf("BioLinks[0].Title = %q", user.BioLinks[0].Title)
	}
}

func TestParseThreadsFromSSR(t *testing.T) {
	html := []byte(`<html>
		<script>"result":{"data":{"mediaData":{"edges":[{"node":{"thread_items":[{"post":{"pk":"111222333","code":"CuXFPB7Mv52","user":{"pk":"25025320","username":"instagram","full_name":"Instagram","is_verified":true},"caption":{"text":"Hello Threads!"},"taken_at":1700000000,"like_count":42,"media_type":1,"text_post_app_info":{"is_reply":false,"direct_reply_count":10},"image_versions2":{"candidates":[{"url":"https://example.com/img1.jpg","width":1080,"height":1920}]}}}]}},{"node":{"thread_items":[{"post":{"pk":"444555666","code":"AbCdEf12345","user":{"pk":"25025320","username":"instagram","full_name":"Instagram","is_verified":true},"caption":{"text":"Second post"},"taken_at":1700001000,"like_count":99,"media_type":2,"text_post_app_info":{"is_reply":false,"direct_reply_count":5},"video_versions":[{"url":"https://example.com/vid.mp4","width":720,"height":1280,"type":101}]}}]}}]}},"sequence_number":0}</script>
	`)

	threads, err := parseThreadsFromSSR(html)
	if err != nil {
		t.Fatalf("parseThreadsFromSSR: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("len(threads) = %d, want 2", len(threads))
	}

	post := threads[0].Items[0]
	if post.ID != "111222333" {
		t.Errorf("post.ID = %q, want %q", post.ID, "111222333")
	}
	if post.Code != "CuXFPB7Mv52" {
		t.Errorf("post.Code = %q, want %q", post.Code, "CuXFPB7Mv52")
	}
	if post.Text != "Hello Threads!" {
		t.Errorf("post.Text = %q, want %q", post.Text, "Hello Threads!")
	}
	if post.LikeCount != 42 {
		t.Errorf("post.LikeCount = %d, want 42", post.LikeCount)
	}
	if post.ReplyCount != 10 {
		t.Errorf("post.ReplyCount = %d, want 10", post.ReplyCount)
	}
	if post.IsReply {
		t.Error("post.IsReply = true, want false")
	}
	if post.Author.Username != "instagram" {
		t.Errorf("post.Author.Username = %q", post.Author.Username)
	}
	expectedTime := time.Unix(1700000000, 0)
	if !post.CreatedAt.Equal(expectedTime) {
		t.Errorf("post.CreatedAt = %v, want %v", post.CreatedAt, expectedTime)
	}
	if len(post.Images) != 1 {
		t.Fatalf("len(post.Images) = %d, want 1", len(post.Images))
	}
	if post.Images[0].Width != 1080 {
		t.Errorf("post.Images[0].Width = %d, want 1080", post.Images[0].Width)
	}

	post2 := threads[1].Items[0]
	if post2.MediaType != 2 {
		t.Errorf("post2.MediaType = %d, want 2", post2.MediaType)
	}
	if len(post2.Videos) != 1 {
		t.Fatalf("len(post2.Videos) = %d, want 1", len(post2.Videos))
	}
}

func TestParseThreadFromSSR(t *testing.T) {
	// Thread detail pages use data.data.edges[] — edge 0 = main post, edges 1+ = replies
	html := []byte(`<html>
		<script>"result":{"data":{"data":{"edges":[{"node":{"thread_items":[{"post":{"pk":"999888777","code":"MainThread1","user":{"pk":"12345","username":"zuck","full_name":"Mark Zuckerberg","is_verified":true},"caption":{"text":"Original post"},"taken_at":1700000000,"like_count":1000,"media_type":1,"text_post_app_info":{"is_reply":false,"direct_reply_count":50}}}]}},{"node":{"thread_items":[{"post":{"pk":"111000111","code":"Reply1Code","user":{"pk":"67890","username":"replier","full_name":"Replier User","is_verified":false},"caption":{"text":"Great post!"},"taken_at":1700001000,"like_count":5,"media_type":1,"text_post_app_info":{"is_reply":true,"direct_reply_count":0}}}]}}]}},"sequence_number":0}</script>
	`)

	main, replies, err := parseThreadFromSSR(html)
	if err != nil {
		t.Fatalf("parseThreadFromSSR: %v", err)
	}

	if len(main.Items) != 1 {
		t.Fatalf("len(main.Items) = %d, want 1", len(main.Items))
	}
	if main.Items[0].Text != "Original post" {
		t.Errorf("main text = %q, want %q", main.Items[0].Text, "Original post")
	}
	if main.Items[0].LikeCount != 1000 {
		t.Errorf("main likes = %d, want 1000", main.Items[0].LikeCount)
	}

	if len(replies) != 1 {
		t.Fatalf("len(replies) = %d, want 1", len(replies))
	}
	if replies[0].Items[0].Text != "Great post!" {
		t.Errorf("reply text = %q", replies[0].Items[0].Text)
	}
	if !replies[0].Items[0].IsReply {
		t.Error("reply.IsReply = false, want true")
	}
}

func TestParseUser(t *testing.T) {
	body := []byte(`{
		"data": {
			"user": {
				"pk": "25025320",
				"username": "instagram",
				"full_name": "Instagram",
				"biography": "Bringing you closer to the people and things you love.",
				"profile_pic_url": "https://example.com/pic.jpg",
				"is_verified": true,
				"text_post_app_is_private": false,
				"follower_count": 1000000,
				"following_count": 500,
				"text_post_app_threads_count": 1234
			}
		}
	}`)

	user, err := parseUser(body)
	if err != nil {
		t.Fatalf("parseUser: %v", err)
	}
	if user.ID != "25025320" {
		t.Errorf("ID = %q, want %q", user.ID, "25025320")
	}
	if user.Username != "instagram" {
		t.Errorf("Username = %q", user.Username)
	}
	if !user.IsVerified {
		t.Error("IsVerified = false, want true")
	}
	if user.FollowerCount != 1000000 {
		t.Errorf("FollowerCount = %d", user.FollowerCount)
	}
}

func TestParseLikers(t *testing.T) {
	body := []byte(`{
		"data": {
			"likers": {
				"users": [
					{"pk": "111", "username": "alice", "full_name": "Alice A", "is_verified": false, "follower_count": 100},
					{"pk": "222", "username": "bob", "full_name": "Bob B", "is_verified": true, "follower_count": 5000}
				]
			}
		}
	}`)

	users, err := parseLikers(body)
	if err != nil {
		t.Fatalf("parseLikers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	if users[0].Username != "alice" {
		t.Errorf("users[0].Username = %q", users[0].Username)
	}
	if !users[1].IsVerified {
		t.Error("users[1].IsVerified = false, want true")
	}
}

func TestParseNullCaption(t *testing.T) {
	html := []byte(`<html>
		<script>"result":{"data":{"mediaData":{"edges":[{"node":{"thread_items":[{"post":{"pk":"999","code":"NoCap","user":{"pk":"1","username":"test"},"caption":null,"taken_at":1700000000,"like_count":0,"media_type":1}}]}}]}},"sequence_number":0}</script>
	`)

	threads, err := parseThreadsFromSSR(html)
	if err != nil {
		t.Fatalf("parseThreadsFromSSR (null caption): %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("len(threads) = %d, want 1", len(threads))
	}
	if threads[0].Items[0].Text != "" {
		t.Errorf("text = %q, want empty", threads[0].Items[0].Text)
	}
}

func TestParseCarouselMedia(t *testing.T) {
	html := []byte(`<html>
		<script>"result":{"data":{"mediaData":{"edges":[{"node":{"thread_items":[{"post":{"pk":"888","code":"Carousel1","user":{"pk":"1","username":"test"},"caption":{"text":"Carousel post"},"taken_at":1700000000,"like_count":10,"media_type":8,"carousel_media":[{"media_type":1,"image_versions2":{"candidates":[{"url":"https://example.com/c1.jpg","width":640,"height":640}]}},{"media_type":2,"video_versions":[{"url":"https://example.com/c2.mp4","width":720,"height":1280}]}]}}]}}]}},"sequence_number":0}</script>
	`)

	threads, err := parseThreadsFromSSR(html)
	if err != nil {
		t.Fatalf("parseThreadsFromSSR (carousel): %v", err)
	}
	post := threads[0].Items[0]
	if post.MediaType != 8 {
		t.Errorf("MediaType = %d, want 8", post.MediaType)
	}
	if len(post.Images) != 1 {
		t.Errorf("len(Images) = %d, want 1", len(post.Images))
	}
	if len(post.Videos) != 1 {
		t.Errorf("len(Videos) = %d, want 1", len(post.Videos))
	}
}

func TestParsePrivateUserList(t *testing.T) {
	body := []byte(`{
		"users": [
			{"pk": "111", "username": "follower1", "full_name": "Follower One", "is_verified": false, "follower_count": 50},
			{"pk": "222", "username": "follower2", "full_name": "Follower Two", "is_verified": true, "follower_count": 1000}
		],
		"next_max_id": "cursor_abc123"
	}`)

	users, nextMaxID, err := parsePrivateUserList(body)
	if err != nil {
		t.Fatalf("parsePrivateUserList: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	if users[0].Username != "follower1" {
		t.Errorf("users[0].Username = %q", users[0].Username)
	}
	if nextMaxID != "cursor_abc123" {
		t.Errorf("nextMaxID = %q, want %q", nextMaxID, "cursor_abc123")
	}
}

func TestParsePrivateThread(t *testing.T) {
	body := []byte(`{
		"containing_thread": {
			"thread_items": [
				{"post": {"pk": "999", "code": "MainPost", "user": {"pk": "1", "username": "author"}, "caption": {"text": "Main post"}, "taken_at": 1700000000, "like_count": 100, "media_type": 1, "text_post_app_info": {"is_reply": false, "direct_reply_count": 2}}}
			]
		},
		"reply_threads": [
			{"thread_items": [
				{"post": {"pk": "1001", "code": "Reply1", "user": {"pk": "2", "username": "replier"}, "caption": {"text": "Nice post!"}, "taken_at": 1700001000, "like_count": 5, "media_type": 1, "text_post_app_info": {"is_reply": true, "direct_reply_count": 0}}}
			]}
		]
	}`)

	main, replies, err := parsePrivateThread(body)
	if err != nil {
		t.Fatalf("parsePrivateThread: %v", err)
	}
	if len(main.Items) != 1 {
		t.Fatalf("len(main.Items) = %d, want 1", len(main.Items))
	}
	if main.Items[0].Text != "Main post" {
		t.Errorf("main text = %q", main.Items[0].Text)
	}
	if len(replies) != 1 {
		t.Fatalf("len(replies) = %d, want 1", len(replies))
	}
	if replies[0].Items[0].Text != "Nice post!" {
		t.Errorf("reply text = %q", replies[0].Items[0].Text)
	}
}

func TestParseSearchUsersSkipsBlankUser(t *testing.T) {
	body := []byte(`{
		"data": {
			"xdt_api__v1__users__search_connection": {
				"edges": [
					{"node": {"text_post_app_user": {"pk": "1", "username": "alice"}}},
					{"node": {}},
					{"node": {"text_post_app_user": null}},
					{"node": {"text_post_app_user": {"pk": "2", "username": "bob"}}}
				]
			}
		}
	}`)

	users, err := parseSearchUsers(body)
	if err != nil {
		t.Fatalf("parseSearchUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	if users[0].Username != "alice" {
		t.Errorf("users[0].Username = %q, want alice", users[0].Username)
	}
	if users[1].Username != "bob" {
		t.Errorf("users[1].Username = %q, want bob", users[1].Username)
	}
}

func TestParseInstagramPostEngagementCounts(t *testing.T) {
	// Fixture mirrors the REAL items[0] shape returned by the IG web
	// /api/v1/media/<id>/info/ endpoint (captured live via the CDP transport).
	// The engagement-count keys confirmed present: like_count, play_count,
	// ig_play_count, comment_count, media_repost_count.
	body := []byte(`{
		"items": [
			{
				"pk": "3727980973477364718",
				"code": "DO8cvGViIPu",
				"user": {"pk": "1513490980", "username": "alnassr", "full_name": "Al Nassr", "is_verified": true},
				"caption": {"text": "Goal!"},
				"taken_at": 1758630037,
				"media_type": 2,
				"like_count": 2244564,
				"play_count": 56964765,
				"ig_play_count": 56344732,
				"comment_count": 24885,
				"media_repost_count": 27745,
				"video_versions": [{"url": "https://example.com/vid.mp4", "width": 720, "height": 1280, "type": 101}]
			}
		]
	}`)

	post, err := parseInstagramPost(body)
	if err != nil {
		t.Fatalf("parseInstagramPost: %v", err)
	}
	if post.LikeCount != 2244564 {
		t.Errorf("LikeCount = %d, want 2244564", post.LikeCount)
	}
	if post.ViewCount != 56964765 {
		t.Errorf("ViewCount = %d, want 56964765 (play_count)", post.ViewCount)
	}
	if post.IGPlayCount != 56344732 {
		t.Errorf("IGPlayCount = %d, want 56344732 (ig_play_count)", post.IGPlayCount)
	}
	if post.CommentCount != 24885 {
		t.Errorf("CommentCount = %d, want 24885 (comment_count)", post.CommentCount)
	}
	if post.RepostCount != 27745 {
		t.Errorf("RepostCount = %d, want 27745 (media_repost_count)", post.RepostCount)
	}
}

func TestParseInstagramPostMissingOptionalCounts(t *testing.T) {
	// A post WITHOUT the optional engagement-count keys (e.g. an image post
	// or a Threads text post surfaced via the IG media API) must still parse:
	// counts default to 0, no error.
	body := []byte(`{
		"items": [
			{
				"pk": "111222333",
				"code": "CuXFPB7Mv52",
				"user": {"pk": "1", "username": "test", "full_name": "Test"},
				"caption": {"text": "hi"},
				"taken_at": 1700000000,
				"media_type": 1,
				"like_count": 42
			}
		]
	}`)

	post, err := parseInstagramPost(body)
	if err != nil {
		t.Fatalf("parseInstagramPost: %v", err)
	}
	if post.LikeCount != 42 {
		t.Errorf("LikeCount = %d, want 42", post.LikeCount)
	}
	if post.ViewCount != 0 {
		t.Errorf("ViewCount = %d, want 0 (key absent)", post.ViewCount)
	}
	if post.IGPlayCount != 0 {
		t.Errorf("IGPlayCount = %d, want 0 (key absent)", post.IGPlayCount)
	}
	if post.CommentCount != 0 {
		t.Errorf("CommentCount = %d, want 0 (key absent)", post.CommentCount)
	}
	if post.RepostCount != 0 {
		t.Errorf("RepostCount = %d, want 0 (key absent)", post.RepostCount)
	}
}

func TestParseInstagramPostDashManifest(t *testing.T) {
	// Fixture mirrors the REAL items[0] shape returned by the authed IG
	// /api/v1/media/<id>/info/ endpoint (x-ig-app-id: 936619743392459) for a
	// reel where every video_versions entry is capped at 720x1280 but the
	// DASH MPD carries up to 1080p. The manifest is a raw XML STRING (~17.7 KB
	// on the live sample); here it is trimmed to a structurally faithful MPD
	// skeleton. number_of_qualities and is_dash_eligible are carried verbatim.
	// go-threads owns the response SHAPE only; go-media owns MPD parsing/muxing.
	const mpd = `<?xml version="1.0"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT12.34S">
  <Period>
    <AdaptationSet mimeType="video/mp4" maxWidth="1080" maxHeight="1920">
      <Representation id="0" bandwidth="501000" width="270" height="480" frameRate="30"/>
      <Representation id="1" bandwidth="2301000" width="540" height="960" frameRate="30"/>
      <Representation id="2" bandwidth="2301000" width="1080" height="1920" frameRate="30"/>
    </AdaptationSet>
    <AdaptationSet mimeType="audio/mp4">
      <Representation id="3" bandwidth="128000"/>
    </AdaptationSet>
  </Period>
</MPD>`
	// Build the response via json.Marshal so the manifest (newlines, quotes)
	// is escaped exactly as the live transport delivers it.
	item := map[string]any{
		"pk":         "3727980973477364718",
		"code":       "DO8cvGViIPu",
		"user":       map[string]any{"pk": "1513490980", "username": "alnassr", "full_name": "Al Nassr", "is_verified": true},
		"caption":    map[string]any{"text": "Goal!"},
		"taken_at":   1758630037,
		"media_type": 2,
		"like_count": 2244564,
		"play_count": 56964765,
		"video_versions": []map[string]any{
			{"url": "https://example.com/vid.mp4", "width": 720, "height": 1280, "type": 101},
		},
		"video_dash_manifest": mpd,
		"number_of_qualities": 9,
		"is_dash_eligible":    true,
	}
	body, err := json.Marshal(map[string]any{"items": []map[string]any{item}})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	post, err := parseInstagramPost(body)
	if err != nil {
		t.Fatalf("parseInstagramPost: %v", err)
	}
	if post.VideoDashManifest != mpd {
		t.Errorf("VideoDashManifest = %q, want the raw MPD XML string", post.VideoDashManifest)
	}
	if post.NumberOfQualities != 9 {
		t.Errorf("NumberOfQualities = %d, want 9", post.NumberOfQualities)
	}
	if !post.IsDashEligible {
		t.Errorf("IsDashEligible = false, want true")
	}
}

func TestParseInstagramPostNoDashManifest(t *testing.T) {
	// The embed / SSR / proxy fallback rungs never receive the DASH manifest
	// (it is only on the authed CDP/REST path). A response WITHOUT the three
	// DASH keys must still parse cleanly with the fields left empty/zero and
	// NO error — fallback rungs must not fail or warn because the manifest is
	// missing.
	body := []byte(`{
		"items": [
			{
				"pk": "111222333",
				"code": "CuXFPB7Mv52",
				"user": {"pk": "1", "username": "test", "full_name": "Test"},
				"caption": {"text": "hi"},
				"taken_at": 1700000000,
				"media_type": 2,
				"like_count": 42,
				"video_versions": [{"url": "https://example.com/vid.mp4", "width": 720, "height": 1280, "type": 101}]
			}
		]
	}`)

	post, err := parseInstagramPost(body)
	if err != nil {
		t.Fatalf("parseInstagramPost: %v", err)
	}
	if post.VideoDashManifest != "" {
		t.Errorf("VideoDashManifest = %q, want empty (key absent)", post.VideoDashManifest)
	}
	if post.NumberOfQualities != 0 {
		t.Errorf("NumberOfQualities = %d, want 0 (key absent)", post.NumberOfQualities)
	}
	if post.IsDashEligible {
		t.Errorf("IsDashEligible = true, want false (key absent)")
	}
}

func TestParseBioLinksEmpty(t *testing.T) {
	html := []byte(`
		<html><script>"result":{"data":{"user":{"pk":"123","username":"nolinks","full_name":"No Links","biography":"Hello","bio_links":[],"profile_pic_url":"https://example.com/pic.jpg","is_verified":false,"text_post_app_is_private":false,"follower_count":10,"following_count":5}},"sequence_number":0}</script>
	`)

	user, err := parseUserFromSSR(html)
	if err != nil {
		t.Fatalf("parseUserFromSSR: %v", err)
	}
	if len(user.BioLinks) != 0 {
		t.Errorf("len(BioLinks) = %d, want 0", len(user.BioLinks))
	}
}
