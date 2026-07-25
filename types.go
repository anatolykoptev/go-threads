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
	// CarouselItems is the ordered, structured view of a post's slides.
	// For a carousel post (MediaType==8) it carries one CarouselItem per
	// carousel_media[] entry, in slide order, each with its own per-slide
	// type and resolution candidates (NOT flattened). For a single-media
	// post (a plain photo or video) it is SYNTHESISED as a one-item list
	// carrying that post's media + DASH fields, so a consumer has ONE code
	// path (range CarouselItems) regardless of post shape. For a non-media
	// / text-only post it is a non-nil empty slice (range-safe).
	//
	// Post.Images / Post.Videos keep their existing flattened behaviour
	// unchanged (backwards compatible); CarouselItems is additive. go-threads
	// carries the shape; go-media / vaelor-agent pick candidates and download.
	CarouselItems []CarouselItem
	// DASH manifest — present only on the authed CDP/REST media-info path.
	// VideoDashManifest is the raw MPD XML string (the only source of
	// higher-than-720p renditions for capped reels). NumberOfQualities and
	// IsDashEligible are carried verbatim. Empty/zero on embed/SSR/proxy
	// fallback rungs. go-threads carries the string; go-media parses/muxes.
	VideoDashManifest string
	NumberOfQualities int
	IsDashEligible    bool
}

// MediaVersion represents a single media rendition.
type MediaVersion struct {
	URL    string
	Width  int
	Height int
}

// CarouselItem represents a single slide of an Instagram carousel post
// (media_type=8), preserving slide order and per-slide type. Each slide
// carries its own resolution candidates — Images (image_versions2.candidates)
// for a photo slide (and the poster frame of a video slide), Videos
// (video_versions) for a video slide — kept as candidates so the consumer
// picks the best rendition per slide; go-threads does NOT pick for them.
//
// Video slides also carry the per-slide DASH manifest fields (same shape as
// Post) so >720p renditions are recoverable per slide — the carousel
// analogue of #40. Photo slides leave them empty/zero.
//
// MediaType reuses the Post.MediaType vocabulary: 1=image, 2=video (NOT 8,
// which is the carousel container type on Post only). A CarouselItem is
// never itself a carousel.
type CarouselItem struct {
	MediaType         int            // per-slide: 1=image, 2=video
	Images            []MediaVersion // image_versions2.candidates for this slide
	Videos            []MediaVersion // video_versions for this slide (video slides)
	VideoDashManifest string         // per-slide DASH MPD XML (video slide only); "" for photo slides
	NumberOfQualities int            // per-slide DASH quality count (video slide only)
	IsDashEligible    bool           // per-slide DASH eligibility (video slide only)
}
