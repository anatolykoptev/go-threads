package threads

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SearchMode selects the Threads search results tab.
type SearchMode string

const (
	SearchTop    SearchMode = "top"
	SearchRecent SearchMode = "recent"
)

// SearchPostsOpts configures SearchPosts. Zero value = top results, no cap.
type SearchPostsOpts struct {
	Mode  SearchMode
	Count int
}

// SearchPosts searches Threads posts by keyword using the web GraphQL search
// endpoint (BarcelonaSearchResultsQuery) captured from threads.com/search.
// Requires Config.WowaURL — Threads has no public post-search API, so unlike
// SearchUsers there is no stealth/Private-API fallback path.
func (c *Client) SearchPosts(ctx context.Context, query string, opts SearchPostsOpts) ([]*Thread, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("SearchPosts: empty query")
	}
	if c.wowa == nil {
		return nil, fmt.Errorf("SearchPosts: WowaURL not configured — keyword post search has no Private API fallback")
	}
	body, err := c.doGraphQL(ctx, "SearchPosts", docIDSearchPosts, friendlySearchPosts, searchPostsVariables(query, opts.Mode))
	if err != nil {
		return nil, fmt.Errorf("SearchPosts: %w", err)
	}
	threads, err := parseSearchPosts(body)
	if err != nil {
		return nil, fmt.Errorf("SearchPosts: %w", err)
	}
	if opts.Count > 0 && len(threads) > opts.Count {
		threads = threads[:opts.Count]
	}
	return threads, nil
}

// searchPostsVariables stamps query/mode into the verbatim captured variable
// set. searchPostsVarsJSON is a compile-time constant validated by
// TestSearchPostsVariables_Template; the error is unreachable.
func searchPostsVariables(query string, mode SearchMode) map[string]any {
	var vars map[string]any
	_ = json.Unmarshal([]byte(searchPostsVarsJSON), &vars)
	vars[searchPostsQueryKey] = query
	if mode == SearchRecent {
		vars[searchPostsModeKey] = searchPostsModeRecent
	} else {
		vars[searchPostsModeKey] = searchPostsModeTop
	}
	return vars
}
