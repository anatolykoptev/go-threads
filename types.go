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
	// SourceMethod identifies which transport tier produced this thread:
	// "cdp" (full engagement metrics), "embed" (public embed, no metrics),
	// "ssr" (server-rendered page), "proxy" (3rd-party proxy, video URL only).
	// Empty when set by older code paths that don't tag the source.
	SourceMethod string
}

// Post represents a single post within a thread.
type Post struct {
	ID         string
	Code       string // short code for URL (threads.net/@user/post/{code})
	Text       string
	CreatedAt  time.Time
	LikeCount  int
	ReplyCount int
	// Engagement counts captured from the IG media API for reels/posts.
	// ViewCount is the total play/view count (play_count). IGPlayCount is the
	// IG-only play subset. CommentCount is the IG feed comment count (distinct
	// from ReplyCount, which is the Threads direct_reply_count). RepostCount is
	// the IG repost/reshare count (media_repost_count). Zero when the API does
	// not return the key (e.g. image/text posts).
	ViewCount    int
	IGPlayCount  int
	CommentCount int
	RepostCount  int
	MediaType    int // 1=image, 2=video, 8=carousel
	Author       ThreadsUser
	IsReply      bool
	Images       []MediaVersion
	Videos       []MediaVersion
}

// MediaVersion represents a single media rendition.
type MediaVersion struct {
	URL    string
	Width  int
	Height int
}
