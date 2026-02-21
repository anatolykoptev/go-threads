package threads

import (
	"testing"
	"time"
)

func TestParseUserFromSSR(t *testing.T) {
	// Simulates the __bbox SSR block found in real page HTML
	html := []byte(`
		<html><script>some stuff</script>
		<script>"result":{"data":{"user":{"pk":"63404918397","text_post_app_is_private":false,"has_onboarded_to_text_post_app":true,"profile_pic_url":"https://example.com/pic.jpg","username":"instagram","follower_count":35930885,"hd_profile_pic_versions":[{"height":320,"url":"https://example.com/hd.jpg","width":320}],"is_verified":true,"biography":"Discover what's new","full_name":"Instagram","bio_links":[],"following_count":500,"text_post_app_threads_count":1234}},"sequence_number":0}</script>
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
	html := []byte(`<html>
		<script>"result":{"data":{"containing_thread":{"thread_items":[{"post":{"pk":"999888777","code":"MainThread1","user":{"pk":"12345","username":"zuck","full_name":"Mark Zuckerberg","is_verified":true},"caption":{"text":"Original post"},"taken_at":1700000000,"like_count":1000,"media_type":1,"text_post_app_info":{"is_reply":false,"direct_reply_count":50}}}]},"reply_threads":[{"thread_items":[{"post":{"pk":"111000111","code":"Reply1Code","user":{"pk":"67890","username":"replier","full_name":"Replier User","is_verified":false},"caption":{"text":"Great post!"},"taken_at":1700001000,"like_count":5,"media_type":1,"text_post_app_info":{"is_reply":true,"direct_reply_count":0}}}]}]},"sequence_number":0}</script>
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
