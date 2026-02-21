package threads

import (
	"encoding/json"
	"fmt"
	"time"
)

// --- Raw response types for JSON unmarshalling ---

type rawUser struct {
	Pk                    json.Number `json:"pk"`
	Username              string      `json:"username"`
	FullName              string      `json:"full_name"`
	Biography             string      `json:"biography"`
	ProfilePicURL         string      `json:"profile_pic_url"`
	IsVerified            bool        `json:"is_verified"`
	IsPrivate             bool        `json:"is_private"`
	FollowerCount         int         `json:"follower_count"`
	FollowingCount        int         `json:"following_count"`
	ThreadsPublishedCount int         `json:"threads_published_count,omitempty"`

	// Alternative field names seen in different response shapes
	HdProfilePicVersions []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"hd_profile_pic_versions,omitempty"`
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
	TakenAt           json.Number      `json:"taken_at"`
	LikeCount         int              `json:"like_count"`
	TextPostAppInfo   *rawTextPostInfo `json:"text_post_app_info"`
	MediaType         int              `json:"media_type"`
	ImageVersions2    *rawImageSet     `json:"image_versions2"`
	VideoVersions     []rawVideoVersion `json:"video_versions"`
	CarouselMedia     []rawCarouselItem `json:"carousel_media"`
	OriginalWidth     int              `json:"original_width"`
	OriginalHeight    int              `json:"original_height"`
}

type rawTextPostInfo struct {
	IsReply     bool `json:"is_reply,omitempty"`
	ReplyCount  int  `json:"direct_reply_count,omitempty"`
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

type rawThread struct {
	ThreadItems []rawThreadItem `json:"thread_items"`
}

// --- Parsers ---

// parseUser parses a GetUser (user profile) response.
func parseUser(body []byte) (*ThreadsUser, error) {
	var raw struct {
		Data struct {
			UserData struct {
				User rawUser `json:"user"`
			} `json:"userData"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal user: %w", err)
	}
	return convertUser(raw.Data.UserData.User), nil
}

// parseUserThreads parses a GetUserThreads or GetUserReplies response.
func parseUserThreads(body []byte) ([]*Thread, error) {
	var raw struct {
		Data struct {
			MediaData struct {
				Threads []rawThread `json:"threads"`
			} `json:"mediaData"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal user threads: %w", err)
	}

	var threads []*Thread
	for _, rt := range raw.Data.MediaData.Threads {
		t := convertThread(rt)
		if len(t.Items) > 0 {
			threads = append(threads, t)
		}
	}
	return threads, nil
}

// parseThread parses a GetThread (single thread + replies) response.
// Returns (main thread, reply threads, error).
func parseThread(body []byte) (*Thread, []*Thread, error) {
	var raw struct {
		Data struct {
			Data struct {
				ContainingThread rawThread   `json:"containing_thread"`
				ReplyThreads     []rawThread `json:"reply_threads"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("unmarshal thread: %w", err)
	}

	main := convertThread(raw.Data.Data.ContainingThread)

	var replies []*Thread
	for _, rt := range raw.Data.Data.ReplyThreads {
		t := convertThread(rt)
		if len(t.Items) > 0 {
			replies = append(replies, t)
		}
	}
	return main, replies, nil
}

// parseLikers parses a GetThreadLikers response.
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
	return &ThreadsUser{
		ID:             ru.Pk.String(),
		Username:       ru.Username,
		FullName:       ru.FullName,
		Bio:            ru.Biography,
		ProfilePicURL:  picURL,
		IsVerified:     ru.IsVerified,
		IsPrivate:      ru.IsPrivate,
		FollowerCount:  ru.FollowerCount,
		FollowingCount: ru.FollowingCount,
		ThreadCount:    ru.ThreadsPublishedCount,
	}
}

func convertThread(rt rawThread) *Thread {
	t := &Thread{}
	for _, item := range rt.ThreadItems {
		t.Items = append(t.Items, convertPost(item.Post))
	}
	return t
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

	// Images
	if rp.ImageVersions2 != nil {
		for _, img := range rp.ImageVersions2.Candidates {
			p.Images = append(p.Images, MediaVersion{
				URL:    img.URL,
				Width:  img.Width,
				Height: img.Height,
			})
		}
	}

	// Videos
	for _, vid := range rp.VideoVersions {
		p.Videos = append(p.Videos, MediaVersion{
			URL:    vid.URL,
			Width:  vid.Width,
			Height: vid.Height,
		})
	}

	// Carousel items
	for _, ci := range rp.CarouselMedia {
		if ci.ImageVersions2 != nil {
			for _, img := range ci.ImageVersions2.Candidates {
				p.Images = append(p.Images, MediaVersion{
					URL:    img.URL,
					Width:  img.Width,
					Height: img.Height,
				})
			}
		}
		for _, vid := range ci.VideoVersions {
			p.Videos = append(p.Videos, MediaVersion{
				URL:    vid.URL,
				Width:  vid.Width,
				Height: vid.Height,
			})
		}
	}

	return p
}
