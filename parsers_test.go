package threads

import (
	"testing"
	"time"
)

func TestParseUser(t *testing.T) {
	body := []byte(`{
		"data": {
			"userData": {
				"user": {
					"pk": "25025320",
					"username": "instagram",
					"full_name": "Instagram",
					"biography": "Bringing you closer to the people and things you love.",
					"profile_pic_url": "https://example.com/pic.jpg",
					"is_verified": true,
					"is_private": false,
					"follower_count": 1000000,
					"following_count": 500,
					"threads_published_count": 1234
				}
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
	if user.FollowerCount != 1000000 {
		t.Errorf("FollowerCount = %d, want %d", user.FollowerCount, 1000000)
	}
	if user.FollowingCount != 500 {
		t.Errorf("FollowingCount = %d, want %d", user.FollowingCount, 500)
	}
	if user.ThreadCount != 1234 {
		t.Errorf("ThreadCount = %d, want %d", user.ThreadCount, 1234)
	}
	if user.ProfilePicURL != "https://example.com/pic.jpg" {
		t.Errorf("ProfilePicURL = %q, want %q", user.ProfilePicURL, "https://example.com/pic.jpg")
	}
}

func TestParseUserThreads(t *testing.T) {
	body := []byte(`{
		"data": {
			"mediaData": {
				"threads": [
					{
						"thread_items": [
							{
								"post": {
									"pk": "111222333",
									"code": "CuXFPB7Mv52",
									"user": {
										"pk": "25025320",
										"username": "instagram",
										"full_name": "Instagram",
										"is_verified": true
									},
									"caption": {
										"text": "Hello Threads!"
									},
									"taken_at": 1700000000,
									"like_count": 42,
									"media_type": 1,
									"text_post_app_info": {
										"is_reply": false,
										"direct_reply_count": 10
									},
									"image_versions2": {
										"candidates": [
											{"url": "https://example.com/img1.jpg", "width": 1080, "height": 1920}
										]
									}
								}
							}
						]
					},
					{
						"thread_items": [
							{
								"post": {
									"pk": "444555666",
									"code": "AbCdEf12345",
									"user": {
										"pk": "25025320",
										"username": "instagram",
										"full_name": "Instagram",
										"is_verified": true
									},
									"caption": {
										"text": "Second post"
									},
									"taken_at": 1700001000,
									"like_count": 99,
									"media_type": 2,
									"text_post_app_info": {
										"is_reply": false,
										"direct_reply_count": 5
									},
									"video_versions": [
										{"url": "https://example.com/vid.mp4", "width": 720, "height": 1280, "type": 101}
									]
								}
							}
						]
					}
				]
			}
		}
	}`)

	threads, err := parseUserThreads(body)
	if err != nil {
		t.Fatalf("parseUserThreads: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("len(threads) = %d, want 2", len(threads))
	}

	// First thread
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
	if post.MediaType != 1 {
		t.Errorf("post.MediaType = %d, want 1", post.MediaType)
	}
	if post.IsReply {
		t.Error("post.IsReply = true, want false")
	}
	if post.Author.Username != "instagram" {
		t.Errorf("post.Author.Username = %q, want %q", post.Author.Username, "instagram")
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

	// Second thread (video)
	post2 := threads[1].Items[0]
	if post2.MediaType != 2 {
		t.Errorf("post2.MediaType = %d, want 2", post2.MediaType)
	}
	if len(post2.Videos) != 1 {
		t.Fatalf("len(post2.Videos) = %d, want 1", len(post2.Videos))
	}
	if post2.Videos[0].URL != "https://example.com/vid.mp4" {
		t.Errorf("post2.Videos[0].URL = %q", post2.Videos[0].URL)
	}
}

func TestParseThread(t *testing.T) {
	body := []byte(`{
		"data": {
			"data": {
				"containing_thread": {
					"thread_items": [
						{
							"post": {
								"pk": "999888777",
								"code": "MainThread1",
								"user": {
									"pk": "12345",
									"username": "zuck",
									"full_name": "Mark Zuckerberg",
									"is_verified": true
								},
								"caption": {"text": "Original post"},
								"taken_at": 1700000000,
								"like_count": 1000,
								"media_type": 1,
								"text_post_app_info": {
									"is_reply": false,
									"direct_reply_count": 50
								}
							}
						}
					]
				},
				"reply_threads": [
					{
						"thread_items": [
							{
								"post": {
									"pk": "111000111",
									"code": "Reply1Code",
									"user": {
										"pk": "67890",
										"username": "replier",
										"full_name": "Replier User",
										"is_verified": false
									},
									"caption": {"text": "Great post!"},
									"taken_at": 1700001000,
									"like_count": 5,
									"media_type": 1,
									"text_post_app_info": {
										"is_reply": true,
										"direct_reply_count": 0
									}
								}
							}
						]
					}
				]
			}
		}
	}`)

	main, replies, err := parseThread(body)
	if err != nil {
		t.Fatalf("parseThread: %v", err)
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
		t.Errorf("reply text = %q, want %q", replies[0].Items[0].Text, "Great post!")
	}
	if !replies[0].Items[0].IsReply {
		t.Error("reply.IsReply = false, want true")
	}
}

func TestParseLikers(t *testing.T) {
	body := []byte(`{
		"data": {
			"likers": {
				"users": [
					{
						"pk": "111",
						"username": "alice",
						"full_name": "Alice A",
						"is_verified": false,
						"follower_count": 100
					},
					{
						"pk": "222",
						"username": "bob",
						"full_name": "Bob B",
						"is_verified": true,
						"follower_count": 5000
					}
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
		t.Errorf("users[0].Username = %q, want %q", users[0].Username, "alice")
	}
	if users[1].Username != "bob" {
		t.Errorf("users[1].Username = %q, want %q", users[1].Username, "bob")
	}
	if !users[1].IsVerified {
		t.Error("users[1].IsVerified = false, want true")
	}
}

func TestParseUserNullCaption(t *testing.T) {
	body := []byte(`{
		"data": {
			"mediaData": {
				"threads": [
					{
						"thread_items": [
							{
								"post": {
									"pk": "999",
									"code": "NoCap",
									"user": {"pk": "1", "username": "test"},
									"caption": null,
									"taken_at": 1700000000,
									"like_count": 0,
									"media_type": 1
								}
							}
						]
					}
				]
			}
		}
	}`)

	threads, err := parseUserThreads(body)
	if err != nil {
		t.Fatalf("parseUserThreads (null caption): %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("len(threads) = %d, want 1", len(threads))
	}
	if threads[0].Items[0].Text != "" {
		t.Errorf("text = %q, want empty", threads[0].Items[0].Text)
	}
}

func TestParseCarouselMedia(t *testing.T) {
	body := []byte(`{
		"data": {
			"mediaData": {
				"threads": [
					{
						"thread_items": [
							{
								"post": {
									"pk": "888",
									"code": "Carousel1",
									"user": {"pk": "1", "username": "test"},
									"caption": {"text": "Carousel post"},
									"taken_at": 1700000000,
									"like_count": 10,
									"media_type": 8,
									"carousel_media": [
										{
											"media_type": 1,
											"image_versions2": {
												"candidates": [
													{"url": "https://example.com/c1.jpg", "width": 640, "height": 640}
												]
											}
										},
										{
											"media_type": 2,
											"video_versions": [
												{"url": "https://example.com/c2.mp4", "width": 720, "height": 1280}
											]
										}
									]
								}
							}
						]
					}
				]
			}
		}
	}`)

	threads, err := parseUserThreads(body)
	if err != nil {
		t.Fatalf("parseUserThreads (carousel): %v", err)
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
