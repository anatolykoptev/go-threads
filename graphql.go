package threads

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetUser fetches a user profile by username.
// Extracts data from the SSR-rendered profile page.
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
// Note: requires an extra request since we need the username for the page URL.
// Prefer GetUser(username) when the username is known.
func (c *Client) GetUserByID(ctx context.Context, userID string) (*ThreadsUser, error) {
	// Threads doesn't expose a direct userID→profile page mapping.
	// The SSR approach requires a username. This method is kept for API
	// compatibility but callers should prefer GetUser() with a username.
	return nil, fmt.Errorf("GetUserByID: not supported in SSR mode, use GetUser(username) instead")
}

// GetUserThreads fetches recent threads by username.
func (c *Client) GetUserThreads(ctx context.Context, username string, count int) ([]*Thread, error) {
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

// GetUserWithThreads fetches both user profile and threads in a single request.
// More efficient than calling GetUser + GetUserThreads separately.
func (c *Client) GetUserWithThreads(ctx context.Context, username string, count int) (*ThreadsUser, []*Thread, error) {
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
		// User exists but threads might be empty — return user with nil threads
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
