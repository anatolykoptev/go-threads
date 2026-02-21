package threads

import "time"

// BioLink represents a link in a user's bio.
type BioLink struct {
	URL   string
	Title string
}

// ThreadsUser represents a Threads user profile.
type ThreadsUser struct {
	ID             string
	Username       string
	FullName       string
	Bio            string
	BioLinks       []BioLink
	ProfilePicURL  string
	IsVerified     bool
	IsPrivate      bool
	FollowerCount  int
	FollowingCount int
	ThreadCount    int
}

// Thread represents a thread (a post and its inline items).
type Thread struct {
	Items []Post
}

// Post represents a single post within a thread.
type Post struct {
	ID         string
	Code       string // short code for URL (threads.net/@user/post/{code})
	Text       string
	CreatedAt  time.Time
	LikeCount  int
	ReplyCount int
	MediaType  int // 1=image, 2=video, 8=carousel
	Author     ThreadsUser
	IsReply    bool
	Images     []MediaVersion
	Videos     []MediaVersion
}

// MediaVersion represents a single media rendition.
type MediaVersion struct {
	URL    string
	Width  int
	Height int
}
