package threads

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetUser fetches a user profile by username.
// With Config.WowaURL set it uses the GraphQL API through the CDP transport;
// otherwise it falls back to SSR page scraping.
func (c *Client) GetUser(ctx context.Context, username string) (*ThreadsUser, error) {
	_, html, err := c.resolveUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("GetUser: %w", err)
	}
	user, err := parseUserFromSSR(html)
	if err != nil {
		return nil, fmt.Errorf("GetUser: %w", err)
	}
	return user, nil
}

// GetUserByID fetches a user profile by numeric userID.
// Requires Config.WowaURL (CDP transport); falls back to an error in SSR mode.
// NOTE: degraded — the profile doc_id has rotated (returns "user data is null"); prefer GetUser(username) (SSR) for reliable profiles.
func (c *Client) GetUserByID(ctx context.Context, userID string) (*ThreadsUser, error) {
	if c.wowa == nil {
		return nil, fmt.Errorf("GetUserByID: not supported in SSR mode, use GetUser(username) instead")
	}

	variables := map[string]any{"userID": userID}
	body, err := c.doGraphQL(ctx, flowAnon, "GetUser", docIDUserProfile, "BarcelonaProfileRootQuery", variables)
	if err != nil {
		return nil, fmt.Errorf("GetUserByID: %w", err)
	}
	user, err := parseUser(body)
	if err != nil {
		return nil, fmt.Errorf("GetUserByID: %w", err)
	}
	return user, nil
}

// GetUserThreads fetches recent threads by username.
// With Config.WowaURL set it uses the GraphQL API through the CDP transport;
// otherwise it falls back to SSR page scraping.
func (c *Client) GetUserThreads(ctx context.Context, username string, count int) ([]*Thread, error) {
	if c.wowa != nil {
		userID, _, err := c.resolveUsername(ctx, username)
		if err != nil {
			return nil, fmt.Errorf("GetUserThreads: %w", err)
		}
		variables := map[string]any{"userID": userID}
		body, err := c.doGraphQL(ctx, flowAnon, "GetUserThreads", docIDUserThreads, "BarcelonaProfileThreadsTabQuery", variables)
		if err != nil {
			return nil, fmt.Errorf("GetUserThreads: %w", err)
		}
		threads, err := parseUserThreads(body)
		if err != nil {
			return nil, fmt.Errorf("GetUserThreads: %w", err)
		}
		if count > 0 && len(threads) > count {
			threads = threads[:count]
		}
		return threads, nil
	}

	_, html, err := c.resolveUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("GetUserThreads: %w", err)
	}
	threads, err := parseThreadsFromSSR(html)
	if err != nil {
		return nil, fmt.Errorf("GetUserThreads: %w", err)
	}
	if count > 0 && len(threads) > count {
		threads = threads[:count]
	}
	return threads, nil
}

// GetUserWithThreads fetches both user profile and threads.
// With Config.WowaURL set it composes two GraphQL calls through the CDP
// transport; otherwise it falls back to SSR page scraping.
func (c *Client) GetUserWithThreads(ctx context.Context, username string, count int) (*ThreadsUser, []*Thread, error) {
	if c.wowa != nil {
		user, err := c.GetUser(ctx, username)
		if err != nil {
			return nil, nil, fmt.Errorf("GetUserWithThreads user: %w", err)
		}
		threads, err := c.GetUserThreads(ctx, username, count)
		if err != nil {
			return user, nil, nil
		}
		return user, threads, nil
	}

	_, html, err := c.resolveUsername(ctx, username)
	if err != nil {
		return nil, nil, fmt.Errorf("GetUserWithThreads: %w", err)
	}

	user, err := parseUserFromSSR(html)
	if err != nil {
		return nil, nil, fmt.Errorf("GetUserWithThreads user: %w", err)
	}

	threads, err := parseThreadsFromSSR(html)
	if err != nil {
		return user, nil, nil
	}
	if count > 0 && len(threads) > count {
		threads = threads[:count]
	}
	return user, threads, nil
}

// GetThread fetches a single thread and its replies by post code.
// The code is the short identifier in the URL: threads.net/@user/post/{code}
func (c *Client) GetThread(ctx context.Context, username, postCode string) (*Thread, []*Thread, error) {
	postURL := fmt.Sprintf("%s/@%s/post/%s", threadsBaseURL, username, postCode)
	html, err := c.fetchPage(ctx, flowAnon, "GetThread", postURL)
	if err != nil {
		return nil, nil, fmt.Errorf("GetThread: %w", err)
	}
	return parseThreadFromSSR(html, postCode)
}

// parseThreadFromSSR extracts a single thread + replies from SSR HTML.
// Two page shapes exist in the wild:
//   - old: data.data.edges[].node.thread_items[] (edge 0 = main, 1+ = replies)
//   - current (2026-09): {"media":<post>} for the main post and
//     media.text_post_app_info.direct_replies.edges[].node.posts.edges[].node
//     for replies, spread across several result.data blocks (issue #57).
//
// wantCode pins the media-path main post to the requested post code (""
// disables the check — used by chain tests that drive parseThreadFromSSR on
// captured pages).
func parseThreadFromSSR(html []byte, wantCode string) (*Thread, []*Thread, error) {
	if main, replies, err := parseThreadEdges(html); err == nil {
		return main, replies, nil
	}
	return parseThreadMedia(html, wantCode)
}

// parseThreadEdges handles the legacy thread_items shape: edge 0 is the main
// post chain, edges 1+ are replies.
func parseThreadEdges(html []byte) (*Thread, []*Thread, error) {
	for _, block := range extractSSRBlocks(html) {
		var probe struct {
			Data *struct {
				Edges []struct {
					Node struct {
						ThreadItems []rawThreadItem `json:"thread_items"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"data"`
		}
		if json.Unmarshal(block, &probe) != nil || probe.Data == nil || len(probe.Data.Edges) == 0 {
			continue
		}
		// Verify this is a thread page (has thread_items, not mediaData)
		if len(probe.Data.Edges[0].Node.ThreadItems) == 0 {
			continue
		}

		// Edge 0 = main thread
		main := &Thread{}
		for _, item := range probe.Data.Edges[0].Node.ThreadItems {
			main.Items = append(main.Items, convertPost(item.Post))
		}

		// Edges 1+ = replies
		var replies []*Thread
		for _, edge := range probe.Data.Edges[1:] {
			t := &Thread{}
			for _, item := range edge.Node.ThreadItems {
				t.Items = append(t.Items, convertPost(item.Post))
			}
			if len(t.Items) > 0 {
				replies = append(replies, t)
			}
		}
		return main, replies, nil
	}
	return nil, nil, fmt.Errorf("thread data not found in SSR HTML")
}

// ssrPostEdge wraps one reply-list edge: node.posts.edges[].node = rawPost.
type ssrPostEdge struct {
	Node struct {
		Posts struct {
			Edges []struct {
				Node rawPost `json:"node"`
			} `json:"edges"`
		} `json:"posts"`
	} `json:"node"`
}

// ssrPostTPAI is the text_post_app_info subtree that carries reply lists and
// the author's self-thread continuation on post pages.
type ssrPostTPAI struct {
	TextPostAppInfo *struct {
		DirectReplies *struct {
			Edges []ssrPostEdge `json:"edges"`
		} `json:"direct_replies"`
		PinnedReplies *struct {
			Edges []ssrPostEdge `json:"edges"`
		} `json:"pinned_replies"`
		SelfThread *struct {
			Posts struct {
				Edges []struct {
					Node rawPost `json:"node"`
				} `json:"edges"`
			} `json:"posts"`
		} `json:"self_thread"`
	} `json:"text_post_app_info"`
}

// parseThreadMedia handles the current post-page shape: one block carries the
// main post at {"media":<post>}; reply chains live in separate media-fragment
// blocks under text_post_app_info.direct_replies / pinned_replies, and the
// author's continuation posts under self_thread.posts.
func parseThreadMedia(html []byte, wantCode string) (*Thread, []*Thread, error) {
	var mainPost *rawPost
	var selfPosts []rawPost
	var pinned, direct []ssrPostEdge

	for _, block := range extractSSRBlocks(html) {
		var mb struct {
			Media json.RawMessage `json:"media"`
		}
		if json.Unmarshal(block, &mb) != nil || mb.Media == nil {
			continue
		}
		var rp rawPost
		if json.Unmarshal(mb.Media, &rp) != nil {
			continue
		}
		// Fragment blocks carry only {id, text_post_app_info} — no pk/code/user.
		// The main post must carry the requested code when wantCode is set;
		// without it any pk/code-bearing media block wins (first-wins).
		if mainPost == nil && (wantCode == "" && (rp.Pk.String() != "" || rp.Code != "") || rp.Code == wantCode) {
			mainPost = &rp
		}
		var tpai ssrPostTPAI
		if json.Unmarshal(mb.Media, &tpai) != nil || tpai.TextPostAppInfo == nil {
			continue
		}
		if st := tpai.TextPostAppInfo.SelfThread; st != nil {
			for _, e := range st.Posts.Edges {
				if e.Node.Pk.String() == "" && e.Node.Code == "" {
					continue
				}
				selfPosts = append(selfPosts, e.Node)
			}
		}
		if r := tpai.TextPostAppInfo.PinnedReplies; r != nil {
			pinned = append(pinned, r.Edges...)
		}
		if r := tpai.TextPostAppInfo.DirectReplies; r != nil {
			direct = append(direct, r.Edges...)
		}
	}

	if mainPost == nil {
		return nil, nil, fmt.Errorf("thread data not found in SSR HTML")
	}
	main := &Thread{Items: []Post{convertPost(*mainPost)}}
	for _, rp := range selfPosts {
		if rp.Pk.String() == mainPost.Pk.String() {
			continue // self_thread can echo the main post
		}
		main.Items = append(main.Items, convertPost(rp))
	}

	// Pinned replies globally precede direct ones; dedupe across both lists
	// (a pinned reply can also appear in direct_replies).
	var replies []*Thread
	seenReply := map[string]bool{}
	for _, edge := range append(pinned, direct...) {
		t := &Thread{}
		key := ""
		for _, pe := range edge.Node.Posts.Edges {
			if pe.Node.Pk.String() == "" && pe.Node.Code == "" {
				continue // relay tombstone/null node — same guard as parseSearchPosts
			}
			if key == "" {
				key = pe.Node.Pk.String()
			}
			t.Items = append(t.Items, convertPost(pe.Node))
		}
		if len(t.Items) > 0 && !seenReply[key] {
			seenReply[key] = true
			replies = append(replies, t)
		}
	}
	return main, replies, nil
}

// --- SSR-based GraphQL methods ---

// GetUserReplies fetches reply threads by username.
// With Config.WowaURL set it uses the GraphQL API through the CDP transport;
// otherwise it falls back to SSR page scraping.
func (c *Client) GetUserReplies(ctx context.Context, username string, count int) ([]*Thread, error) {
	if c.wowa != nil {
		userID, _, err := c.resolveUsername(ctx, username)
		if err != nil {
			return nil, fmt.Errorf("GetUserReplies: %w", err)
		}
		variables := map[string]any{"userID": userID}
		body, err := c.doGraphQL(ctx, flowAnon, "GetUserReplies", docIDUserReplies, "BarcelonaProfileRepliesTabQuery", variables)
		if err != nil {
			return nil, fmt.Errorf("GetUserReplies: %w", err)
		}
		threads, err := parseUserThreads(body)
		if err != nil {
			return nil, fmt.Errorf("GetUserReplies: %w", err)
		}
		if count > 0 && len(threads) > count {
			threads = threads[:count]
		}
		return threads, nil
	}

	repliesURL := threadsBaseURL + "/@" + username + "/replies"
	html, err := c.fetchPage(ctx, flowAnon, "GetUserReplies", repliesURL)
	if err != nil {
		return nil, fmt.Errorf("GetUserReplies: %w", err)
	}
	threads, err := parseThreadsFromSSR(html)
	if err != nil {
		return nil, fmt.Errorf("GetUserReplies: %w", err)
	}
	if count > 0 && len(threads) > count {
		threads = threads[:count]
	}
	return threads, nil
}

// --- GraphQL API methods ---

// GetThreadLikers fetches users who liked a thread by its ID.
func (c *Client) GetThreadLikers(ctx context.Context, threadID string, count int) ([]*ThreadsUser, error) {
	variables := map[string]any{
		"mediaID": threadID,
	}
	body, err := c.doGraphQL(ctx, flowAnon, "GetThreadLikers", docIDGetThreadLikers, "BarcelonaMediaLikersQuery", variables)
	if err != nil {
		return nil, fmt.Errorf("GetThreadLikers: %w", err)
	}
	users, err := parseLikers(body)
	if err != nil {
		return nil, fmt.Errorf("GetThreadLikers: %w", err)
	}
	if count > 0 && len(users) > count {
		users = users[:count]
	}
	return users, nil
}

// SearchUsers searches for users by query string.
// With Config.WowaURL set it uses the Threads GraphQL API through the CDP
// transport; otherwise it delegates to the Private API (requires authentication).
func (c *Client) SearchUsers(ctx context.Context, query string, count int) ([]*ThreadsUser, error) {
	if c.wowa != nil {
		variables := map[string]any{
			"query":          query,
			"search_surface": nil,
			"__relay_internal__pv__BarcelonaIsInternalUserrelayprovider": false,
			"__relay_internal__pv__BarcelonaIsLoggedInrelayprovider":     true,
			"__relay_internal__pv__BarcelonaIsCrawlerrelayprovider":      false,
		}
		body, err := c.doGraphQL(ctx, flowAuthed, "SearchUsers", docIDSearchUsers, "BarcelonaSearchUserResultsQuery", variables)
		if err != nil {
			return nil, fmt.Errorf("SearchUsers: %w", err)
		}
		users, err := parseSearchUsers(body)
		if err != nil {
			return nil, fmt.Errorf("SearchUsers: %w", err)
		}
		if count > 0 && len(users) > count {
			users = users[:count]
		}
		return users, nil
	}

	if !c.IsAuthenticated() {
		return nil, fmt.Errorf("SearchUsers: authentication required (set Token in Config)")
	}
	users, err := c.SearchUserPrivate(ctx, query)
	if err != nil {
		return nil, err
	}
	if count > 0 && len(users) > count {
		users = users[:count]
	}
	return users, nil
}
