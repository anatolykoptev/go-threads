package threads

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Bounds for the author-chain walk. See the report's BOUNDS section for
// justification.
const (
	// maxChainPosts caps the number of posts collected into a chain. A real
	// author chain is 5-10 posts; 50 absorbs any sane outlier while
	// preventing a pathological thread from consuming unbounded memory.
	maxChainPosts = 50
	// maxChainReqs caps the number of SSR page fetches the walk issues. The
	// common case (non-truncated reply page) completes in 1 fetch; the
	// truncated-page recovery path (popular post whose continuation fell
	// beyond the 20-reply-thread page) uses the remaining budget to fetch
	// the last chain post's own page and look for the continuation there.
	maxChainReqs = 5
	// replyPageCap is the maximum number of reply threads a single SSR page
	// surfaces, measured live on a zuck thread with 746 replies (only 20
	// reply threads returned). When a page hits this cap the walk cannot
	// prove it saw every reply thread, so it must report possible
	// incompleteness.
	replyPageCap = 20
)

// Chain is a Threads author chain: the sequence of posts one author wrote
// in a single thread, in writing order, plus honest completeness metadata.
//
// A Threads "thread" from the reader's perspective is often a CHAIN the
// author wrote in sequence (commonly 5, 7, 10 posts). GetThread returns
// only the ancestor path of the linked post plus one page of reply
// threads; Chain reconstructs the full author sequence and says plainly
// whether it reached both ends.
type Chain struct {
	// Username is the author's username as returned by the API (casing
	// preserved). Falls back to the requested username when the API
	// response carries none.
	Username string
	// AuthorID is the author's numeric pk — the stable identity used to
	// decide chain membership (usernames can be renamed; pk cannot).
	// Empty only when the API response carried no pk.
	AuthorID string
	// Posts are the chain posts in author writing order (oldest first).
	// Deduplicated by Post.Code.
	Posts []Post
	// Complete is true iff the walk proved it reached BOTH the chain root
	// (no missing ancestors) AND the chain tail (no further same-author
	// continuation on a non-truncated reply page, within bounds).
	// When false, Reason explains why.
	Complete bool
	// Reason is a human-readable completeness explanation. Empty when
	// Complete is true. A consumer that cannot distinguish "3 posts,
	// complete" from "3 posts, and there may be more" will silently
	// present a truncated chain as whole — always check Complete.
	Reason string
	// Requests is the number of SSR page fetches the walk issued.
	// 1 for the common non-truncated case; up to maxChainReqs for the
	// truncated-page recovery path.
	Requests int
}

// GetAuthorChain fetches the full author chain for a Threads post.
//
// postCode may be ANY post in the chain (root, middle, or tail); the walk
// reconstructs the full chain in author writing order. The author is
// identified by the pk of the linked post, and a post joins the chain iff
// it is by the same author. Third-party replies are never included.
//
// Completeness is reported, never assumed: when the reply page is
// truncated (the SSR surfaces at most replyPageCap reply threads) the
// walk cannot prove it saw the author's continuation, so Chain.Complete
// is false and Chain.Reason explains the truncation. The walk may issue
// follow-up fetches (up to maxChainReqs) to recover a continuation that
// fell beyond a truncated page; if it still cannot prove the tail,
// Complete stays false.
//
// The listing endpoint (GetUserThreads) is NOT consulted automatically:
// it carries the flat chain but only for the author's RECENT posts, so it
// is unreliable for older threads and costs an extra request. A consumer
// that wants a cross-check can call GetUserThreads itself and merge by
// Post.Code.
func (c *Client) GetAuthorChain(ctx context.Context, username, postCode string) (*Chain, error) {
	main, replies, err := c.GetThread(ctx, username, postCode)
	if err != nil {
		return nil, fmt.Errorf("GetAuthorChain: %w", err)
	}
	return buildAuthorChain(ctx, c.GetThread, username, postCode, main, replies, 1)
}

// chainFetcher is the fetch seam buildAuthorChain uses for follow-up
// page fetches. GetAuthorChain passes c.GetThread; tests pass a mock.
type chainFetcher func(ctx context.Context, username, postCode string) (*Thread, []*Thread, error)

// buildAuthorChain is the pure, network-free core of GetAuthorChain.
// It takes the initial SSR result (main + replies) and a fetch function
// for follow-up fetches, and returns the completed chain.
func buildAuthorChain(ctx context.Context, fetch chainFetcher, username, postCode string, main *Thread, replies []*Thread, requests int) (*Chain, error) {
	if main == nil || len(main.Items) == 0 {
		return nil, fmt.Errorf("buildAuthorChain: no items in main thread")
	}

	// The author is the author of the linked post. main.Items is the
	// ancestor path (root -> linked post), so the linked post is the LAST
	// item (measured live: link-at-root -> 1 item; link-at-middle ->
	// [root, linked] -> 2 items).
	requestedPost := main.Items[len(main.Items)-1]
	authorID, authorHandle := authorIdentity(requestedPost)

	chain := &Chain{Username: authorHandle, AuthorID: authorID}
	if chain.Username == "" {
		chain.Username = username
	}

	// Ancestor path: same-author posts from main.Items. main.Items is
	// already in ancestor order (oldest first).
	var posts []Post
	for _, p := range main.Items {
		if sameAuthor(p, authorID, authorHandle) {
			posts = append(posts, p)
		}
	}
	if len(posts) == 0 {
		return nil, fmt.Errorf("buildAuthorChain: no same-author posts in main thread")
	}

	// Walk down: follow same-author reply threads, flattening their nested
	// Items (a reply thread's Items carry the full continuation subtree:
	// measured live — a natgeo reply thread held [post 2, post 3] in one
	// Items slice). When the reply page is truncated (hit the
	// replyPageCap), fetch the last chain post's own page to look for a
	// continuation that fell beyond the page.
	pageTruncated := len(replies) >= replyPageCap
	hitPostBound := false
	hitReqBound := false
	var fetchErr error

	for {
		added := false
		for _, rt := range replies {
			if len(rt.Items) == 0 || !sameAuthor(rt.Items[0], authorID, authorHandle) {
				continue // third-party reply thread, not a chain continuation
			}
			for _, p := range rt.Items {
				if sameAuthor(p, authorID, authorHandle) && !hasCode(posts, p.Code) {
					posts = append(posts, p)
					added = true
					if len(posts) >= maxChainPosts {
						hitPostBound = true
						break
					}
				}
			}
			if hitPostBound {
				break
			}
		}

		if hitPostBound {
			break
		}

		// A non-truncated page is complete: if we found a continuation,
		// its reply-thread Items captured the full subtree; if we did
		// not, the author wrote nothing further. Either way we are done.
		if !pageTruncated {
			break
		}

		// Truncated page: the author's continuation may be beyond it.
		// Only a follow-up that found NEW posts is worth chasing — the
		// new last post's own page may surface further continuation
		// beyond the truncated original page. If no continuation was
		// found, re-fetching the same last post would return the same
		// truncated page, so we stop and report possible incompleteness.
		if !added {
			break
		}
		if requests >= maxChainReqs {
			hitReqBound = true
			break
		}
		lastPost := posts[len(posts)-1]
		var subReplies []*Thread
		_, subReplies, fetchErr = fetch(ctx, username, lastPost.Code)
		if fetchErr != nil {
			break
		}
		requests++
		replies = subReplies
		pageTruncated = len(replies) >= replyPageCap
	}

	posts = orderChain(posts)
	chain.Posts = posts
	chain.Requests = requests

	// Completeness — the single most important property of this type.
	// A consumer that cannot tell "complete" from "may have more" will
	// silently present a truncated chain as whole.
	ancestorMissing := len(posts) > 0 && posts[0].IsReply
	switch {
	case ancestorMissing:
		chain.Complete = false
		chain.Reason = "ancestor path incomplete: first chain post is marked as a reply (chain root not on the page)"
	case hitPostBound:
		chain.Complete = false
		chain.Reason = fmt.Sprintf("max posts bound reached (%d); chain may continue beyond it", maxChainPosts)
	case hitReqBound:
		chain.Complete = false
		chain.Reason = fmt.Sprintf("max requests bound reached (%d); chain may continue beyond it", maxChainReqs)
	case fetchErr != nil:
		chain.Complete = false
		chain.Reason = fmt.Sprintf("follow-up fetch failed: %v", fetchErr)
	case pageTruncated:
		chain.Complete = false
		chain.Reason = fmt.Sprintf("reply page truncated (%d reply threads, cap %d); further author continuation may exist beyond the page", len(replies), replyPageCap)
	default:
		chain.Complete = true
	}

	return chain, nil
}

// authorIdentity returns the stable (id) and display (handle) identity of
// a post's author. The id is the numeric pk (stable across renames); the
// handle is the username (display casing).
func authorIdentity(p Post) (id, handle string) {
	return p.Author.ID, p.Author.Username
}

// sameAuthor reports whether p is by the author identified by (id, handle).
// Prefers the numeric pk (stable; usernames can be renamed and casing
// varies between the requested username and the API-returned one). Falls
// back to case-insensitive username match only when a pk is missing.
func sameAuthor(p Post, id, handle string) bool {
	if id != "" && p.Author.ID != "" {
		return p.Author.ID == id
	}
	if handle != "" && p.Author.Username != "" {
		return strings.EqualFold(p.Author.Username, handle)
	}
	return false
}

// hasCode reports whether posts already contains a post with the given code.
func hasCode(posts []Post, code string) bool {
	if code == "" {
		return false
	}
	for _, p := range posts {
		if p.Code == code {
			return true
		}
	}
	return false
}

// orderChain sorts chain posts into author writing order (oldest first)
// by CreatedAt (taken_at). A stable sort preserves collection order
// (nesting position) for posts with equal or zero timestamps. When
// timestamp and nesting position disagree, timestamp wins — it is the
// ground truth for when the author wrote each post.
func orderChain(posts []Post) []Post {
	ordered := make([]Post, len(posts))
	copy(ordered, posts)
	sort.SliceStable(ordered, func(i, j int) bool {
		ti, tj := ordered[i].CreatedAt, ordered[j].CreatedAt
		if ti.IsZero() || tj.IsZero() {
			return false // preserve collection order for undated posts
		}
		return ti.Before(tj)
	})
	return ordered
}

// RenderChain renders a Chain as a single plain-text string suitable for
// a human reader or LLM input. Posts are separated so boundaries are
// visible, per-post media presence is noted (a chain post can carry
// images or video — the text must not imply a post was text-only when it
// carried media), and a possibly-incomplete chain is flagged at the end.
//
// The output is plain text (no HTML). When the chain is a single post no
// separator or index prefix is emitted. When the chain is possibly
// incomplete a trailing note says so — never is a truncated chain
// rendered as if it were whole.
func RenderChain(chain *Chain) string {
	if chain == nil || len(chain.Posts) == 0 {
		return ""
	}
	n := len(chain.Posts)
	var b strings.Builder
	for i, p := range chain.Posts {
		if n > 1 {
			fmt.Fprintf(&b, "[%d/%d] ", i+1, n)
		}
		b.WriteString(strings.TrimSpace(p.Text))
		if note := mediaNote(p); note != "" {
			b.WriteString("\n")
			b.WriteString(note)
		}
		if i < n-1 {
			b.WriteString("\n\n---\n\n")
		}
	}
	if !chain.Complete && chain.Reason != "" {
		b.WriteString("\n\n[chain may be incomplete: ")
		b.WriteString(chain.Reason)
		b.WriteString("]")
	}
	return b.String()
}

// mediaNote describes a post's media presence in plain text, or "" for a
// text-only post. A chain post can carry a photo, a video, or a carousel
// of slides; the render must not imply text-only when media is present.
func mediaNote(p Post) string {
	switch p.MediaType {
	case 1:
		return "[media: photo]"
	case 2:
		return "[media: video]"
	case 8:
		if n := len(p.CarouselItems); n > 0 {
			return fmt.Sprintf("[media: carousel, %d slides]", n)
		}
		return "[media: carousel]"
	default:
		if len(p.Images) > 0 || len(p.Videos) > 0 {
			return "[media: attached]"
		}
		return ""
	}
}
