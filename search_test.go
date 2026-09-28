package threads

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func searchClient(t *testing.T, respBody string) (*Client, *string) {
	t.Helper()
	withZeroDelays(t) // cdp_test.go:243 — retries must not sleep in tests
	fr, _ := json.Marshal(fetchResult{Status: 200, Body: respBody})
	ts, _, gotScript := cdpTestServer(t, "/api/v1/chrome/interact", string(fr))
	t.Cleanup(ts.Close)
	c, err := NewClient(Config{WowaURL: ts.URL, Session: "test"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.lsd, c.lsdAt, c.fbDtsg = "lsd", time.Now(), "dtsg"
	return c, gotScript
}

func TestSearchPosts_Fixtures(t *testing.T) {
	for _, tc := range []struct {
		file string
		mode SearchMode
	}{{"testdata/search_posts_top.json", SearchTop}, {"testdata/search_posts_recent.json", SearchRecent}} {
		t.Run(string(tc.mode), func(t *testing.T) {
			raw, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			c, gotScript := searchClient(t, string(raw))
			got, err := c.SearchPosts(context.Background(), "openai", SearchPostsOpts{Mode: tc.mode, Count: 5})
			if err != nil {
				t.Fatalf("SearchPosts: %v", err)
			}
			if len(got) == 0 || len(got) > 5 {
				t.Fatalf("got %d threads, want 1..5", len(got))
			}
			p := got[0].Items[0]
			if p.Code == "" || p.Text == "" || p.Author.Username == "" || p.CreatedAt.IsZero() {
				t.Errorf("first post missing fields: %+v", p)
			}
			if !strings.Contains(*gotScript, docIDSearchPosts) || !strings.Contains(*gotScript, friendlySearchPosts) {
				t.Errorf("script does not target the search doc_id / friendly name")
			}
		})
	}
}

// Silent-failure guards: each of these must be an ERROR, never an empty slice.
func TestSearchPosts_NonResultBodiesAreErrors(t *testing.T) {
	for name, body := range map[string]string{
		"graphql_errors": `{"errors":[{"message":"doc_id not found"}],"data":null}`,
		"null_data":      `{"data":null}`,
		"shape_changed":  `{"data":{"somethingElse":{}}}`,
		// searchResults present, edges present, but every edge carries an
		// unrecognised node — the rotated-inner-shape silent failure.
		"unparseable_edges": `{"data":{"searchResults":{"edges":[{"node":{"unrelated":{}}}]}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := searchClient(t, body)
			got, err := c.SearchPosts(context.Background(), "openai", SearchPostsOpts{})
			if err == nil {
				t.Fatalf("want error, got %d threads and nil error", len(got))
			}
			if (name == "shape_changed" || name == "unparseable_edges") && !errors.Is(err, ErrUnexpectedShape) {
				t.Errorf("want ErrUnexpectedShape, got %v", err)
			}
		})
	}
}

// The no-WowaURL guard is what makes the missing-config failure legible:
// without it the call falls into the stealth path and errors with whatever
// ensureLSD happens to return. Assert on the guard's own message.
func TestSearchPosts_RequiresWowa(t *testing.T) {
	c, err := NewClient(Config{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.SearchPosts(context.Background(), "openai", SearchPostsOpts{})
	if err == nil || !strings.Contains(err.Error(), "WowaURL not configured") {
		t.Fatalf("want the WowaURL guard error, got %v", err)
	}
}

func TestSearchPosts_EmptyQueryRejected(t *testing.T) {
	c, _ := searchClient(t, `{}`)
	if _, err := c.SearchPosts(context.Background(), "  ", SearchPostsOpts{}); err == nil {
		t.Fatal("want error for blank query")
	}
}

func TestSearchPostsVariables_Template(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(searchPostsVarsJSON), &m); err != nil {
		t.Fatalf("searchPostsVarsJSON is not valid JSON: %v", err)
	}
	v := searchPostsVariables("q1", SearchRecent)
	if v[searchPostsQueryKey] != "q1" || v[searchPostsModeKey] != searchPostsModeRecent {
		t.Fatalf("variables not applied: %v", v)
	}
	if searchPostsModeTop == searchPostsModeRecent {
		t.Fatal("top and recent mode values are identical")
	}
}
