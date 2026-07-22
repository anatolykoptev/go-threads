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
func (c *Client) GetUserByID(ctx context.Context, userID string) (*ThreadsUser, error) {
	if c.wowa == nil {
		return nil, fmt.Errorf("GetUserByID: not supported in SSR mode, use GetUser(username) instead")
	}

	variables := map[string]any{"userID": userID}
	body, err := c.doGraphQL(ctx, "GetUser", docIDUserProfile, "BarcelonaProfileRootQuery", variables)
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
		body, err := c.doGraphQL(ctx, "GetUserThreads", docIDUserThreads, "BarcelonaProfileThreadsTabQuery", variables)
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
	html, err := c.fetchPage(ctx, "GetThread", postURL)
	if err != nil {
		return nil, nil, fmt.Errorf("GetThread: %w", err)
	}
	return parseThreadFromSSR(html)
}

// parseThreadFromSSR extracts a single thread + replies from SSR HTML.
// The SSR structure is: data.data.edges[].node.thread_items[]
// Edge 0 = main post, Edges 1+ = replies.
func parseThreadFromSSR(html []byte) (*Thread, []*Thread, error) {
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
		body, err := c.doGraphQL(ctx, "GetUserReplies", docIDUserReplies, "BarcelonaProfileRepliesTabQuery", variables)
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
	html, err := c.fetchPage(ctx, "GetUserReplies", repliesURL)
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
	body, err := c.doGraphQL(ctx, "GetThreadLikers", docIDGetThreadLikers, "BarcelonaMediaLikersQuery", variables)
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
		body, err := c.doGraphQL(ctx, "SearchUsers", docIDSearchUsers, "BarcelonaSearchUserResultsQuery", variables)
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
