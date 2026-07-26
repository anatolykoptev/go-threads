package threads

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- SSR fixture helpers ---
//
// Tests build SSR HTML inline (the repo's existing style) and parse it
// with parseThreadFromSSR, then drive buildAuthorChain with a mock
// fetcher so no network is touched.

// ssrThreadPage wraps thread edges in the data.data.edges SSR shape that
// parseThreadFromSSR probes (edge 0 = main, edges 1+ = replies).
func ssrThreadPage(edges ...string) []byte {
	return []byte(`<html><script>"result":{"data":{"data":{"edges":[` +
		strings.Join(edges, ",") + `]}},"sequence_number":0}</script>`)
}

// edge builds one data.data.edges[].node with the given thread_items.
func edge(items ...string) string {
	return `{"node":{"thread_items":[` + strings.Join(items, ",") + `]}}`
}

// post builds one thread_items[] entry. isReply sets text_post_app_info.is_reply.
// takenAt sets taken_at (unix seconds). mediaType sets media_type.
func post(pk, code, text, authorPk, authorUser string, isReply bool, takenAt int64, mediaType int) string {
	return fmt.Sprintf(
		`{"post":{"pk":%q,"code":%q,"user":{"pk":%q,"username":%q},"caption":{"text":%q},"taken_at":%d,"media_type":%d,"text_post_app_info":{"is_reply":%t,"direct_reply_count":0}}}`,
		pk, code, authorPk, authorUser, text, takenAt, mediaType, isReply,
	)
}

// postCarousel is post() with a carousel_media[] of n slides (media_type=8).
func postCarousel(pk, code, text, authorPk, authorUser string, isReply bool, takenAt int64, slides int) string {
	media := make([]string, slides)
	for i := range media {
		media[i] = fmt.Sprintf(`{"media_type":1,"image_versions2":{"candidates":[{"url":"https://example.com/c%d.jpg","width":1080,"height":1080}]}}`, i)
	}
	return fmt.Sprintf(
		`{"post":{"pk":%q,"code":%q,"user":{"pk":%q,"username":%q},"caption":{"text":%q},"taken_at":%d,"media_type":8,"carousel_media":[%s],"text_post_app_info":{"is_reply":%t,"direct_reply_count":0}}}`,
		pk, code, authorPk, authorUser, text, takenAt, strings.Join(media, ","), isReply,
	)
}

// mockChainFetcher serves pre-parsed (main, replies) by post code.
type mockChainFetcher struct {
	pages map[string]*struct {
		main    *Thread
		replies []*Thread
	}
	calls []string
}

func (m *mockChainFetcher) fetch(_ context.Context, _, code string) (*Thread, []*Thread, error) {
	m.calls = append(m.calls, code)
	p, ok := m.pages[code]
	if !ok {
		return nil, nil, fmt.Errorf("no fixture for code %q", code)
	}
	return p.main, p.replies, nil
}

// parsePage parses an SSR HTML page into (main, replies) and panics on error.
func parsePage(t *testing.T, html []byte) (*Thread, []*Thread) {
	t.Helper()
	main, replies, err := parseThreadFromSSR(html)
	if err != nil {
		t.Fatalf("parseThreadFromSSR: %v", err)
	}
	return main, replies
}

const (
	authorPk   = "100"
	authorUser = "natgeo"
)

// codesAndTexts extracts (code, text) pairs from a chain for order assertions.
func codesAndTexts(chain *Chain) [][2]string {
	out := make([][2]string, 0, len(chain.Posts))
	for _, p := range chain.Posts {
		out = append(out, [2]string{p.Code, p.Text})
	}
	return out
}

// mockChainLister serves the author's listing (GetUserThreads shape) for
// the ambiguous-branch cross-check. calls counts invocations so a test
// can assert the listing was NOT consulted on the non-truncated path.
type mockChainLister struct {
	threads []*Thread
	err     error
	calls   int
}

func (m *mockChainLister) list(_ context.Context, _ string) ([]*Thread, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return m.threads, nil
}

// truncatedNoContinuationPage builds an SSR page that hits replyPageCap
// with NO same-author continuation — the false-alarm trigger. The linked
// post (rootCode) is the only same-author post on the page.
func truncatedNoContinuationPage(rootCode string) []byte {
	edges := []string{edge(post("1001", rootCode, "post 1", authorPk, authorUser, false, 1700000000, 1))}
	for i := 0; i < replyPageCap; i++ {
		edges = append(edges, edge(post(
			fmt.Sprintf("3%d", i), fmt.Sprintf("TP%d", i), "third party",
			"300", "troll", true, 1700001000+int64(i), 1,
		)))
	}
	return ssrThreadPage(edges...)
}

// --- Tests ---

// TestBuildAuthorChain_LinkAtRoot_3PostChain: link points at the root of
// a 3-post chain; the continuation (posts 2, 3) is in a same-author reply
// thread. Expect all 3 in author order, complete.
func TestBuildAuthorChain_LinkAtRoot_3PostChain(t *testing.T) {
	page := ssrThreadPage(
		edge(post("1001", "ROOT", "post 1", authorPk, authorUser, false, 1700000000, 1)),
		edge(
			post("1002", "P2", "post 2", authorPk, authorUser, true, 1700001000, 1),
			post("1003", "P3", "post 3", authorPk, authorUser, true, 1700002000, 1),
		),
		edge(post("2001", "R1", "third-party reply", "200", "otheruser", true, 1700003000, 1)),
	)
	main, replies := parsePage(t, page)

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, nil, authorUser, "ROOT", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if got := codesAndTexts(chain); len(got) != 3 ||
		got[0] != [2]string{"ROOT", "post 1"} ||
		got[1] != [2]string{"P2", "post 2"} ||
		got[2] != [2]string{"P3", "post 3"} {
		t.Errorf("chain order = %v, want [ROOT, P2, P3]", got)
	}
	if !chain.Complete {
		t.Errorf("Complete = false, want true (Reason=%q)", chain.Reason)
	}
	if chain.Requests != 1 {
		t.Errorf("Requests = %d, want 1", chain.Requests)
	}
	if chain.AuthorID != authorPk {
		t.Errorf("AuthorID = %q, want %q", chain.AuthorID, authorPk)
	}
}

// TestBuildAuthorChain_LinkAtMiddle: link points at post 2; main.Items
// carries [root, post 2] (ancestor path). A same-author reply thread
// carries post 3. Expect the full chain [root, post 2, post 3] in order.
func TestBuildAuthorChain_LinkAtMiddle(t *testing.T) {
	page := ssrThreadPage(
		edge(
			post("1001", "ROOT", "post 1", authorPk, authorUser, false, 1700000000, 1),
			post("1002", "MID", "post 2", authorPk, authorUser, true, 1700001000, 1),
		),
		edge(post("1003", "P3", "post 3", authorPk, authorUser, true, 1700002000, 1)),
		edge(post("2001", "R1", "third-party", "200", "other", true, 1700003000, 1)),
	)
	main, replies := parsePage(t, page)

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, nil, authorUser, "MID", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if got := codesAndTexts(chain); len(got) != 3 ||
		got[0] != [2]string{"ROOT", "post 1"} ||
		got[1] != [2]string{"MID", "post 2"} ||
		got[2] != [2]string{"P3", "post 3"} {
		t.Errorf("chain order = %v, want [ROOT, MID, P3]", got)
	}
	if !chain.Complete {
		t.Errorf("Complete = false, want true (Reason=%q)", chain.Reason)
	}
}

// TestBuildAuthorChain_ThirdPartyRepliesExcluded: the reply page has
// third-party reply threads interleaved with the author's continuation.
// Third-party posts must never appear in the chain.
func TestBuildAuthorChain_ThirdPartyRepliesExcluded(t *testing.T) {
	page := ssrThreadPage(
		edge(post("1001", "ROOT", "post 1", authorPk, authorUser, false, 1700000000, 1)),
		edge(post("2001", "TP1", "third party 1", "200", "troll", true, 1700000500, 1)),
		edge(post("1002", "P2", "post 2", authorPk, authorUser, true, 1700001000, 1)),
		edge(post("2002", "TP2", "third party 2", "201", "another", true, 1700001500, 1)),
		edge(post("1003", "P3", "post 3", authorPk, authorUser, true, 1700002000, 1)),
	)
	main, replies := parsePage(t, page)

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, nil, authorUser, "ROOT", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if got := codesAndTexts(chain); len(got) != 3 ||
		got[0][0] != "ROOT" || got[1][0] != "P2" || got[2][0] != "P3" {
		t.Errorf("chain = %v, want only [ROOT, P2, P3] (no third-party)", got)
	}
	for _, p := range chain.Posts {
		if p.Author.ID != authorPk {
			t.Errorf("post %q has author %q, want %q", p.Code, p.Author.ID, authorPk)
		}
	}
}

// TestBuildAuthorChain_NestedContinuation: a same-author reply thread
// whose Items hold TWO chain posts (post 2, post 3) — must be flattened
// correctly, order preserved.
func TestBuildAuthorChain_NestedContinuation(t *testing.T) {
	page := ssrThreadPage(
		edge(post("1001", "ROOT", "post 1", authorPk, authorUser, false, 1700000000, 1)),
		edge(
			post("1002", "P2", "post 2", authorPk, authorUser, true, 1700001000, 1),
			post("1003", "P3", "post 3", authorPk, authorUser, true, 1700002000, 1),
		),
	)
	main, replies := parsePage(t, page)

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, nil, authorUser, "ROOT", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if got := codesAndTexts(chain); len(got) != 3 ||
		got[0] != [2]string{"ROOT", "post 1"} ||
		got[1] != [2]string{"P2", "post 2"} ||
		got[2] != [2]string{"P3", "post 3"} {
		t.Errorf("flattened chain = %v, want [ROOT, P2, P3]", got)
	}
}

// TestBuildAuthorChain_TruncatedPage_Incomplete: the reply page hits the
// replyPageCap (20 reply threads) and none are same-author. No
// continuation was found, so re-fetching the same last post would return
// the same truncated page — the walk stops and reports possible
// incompleteness. The rendered text must say so.
func TestBuildAuthorChain_TruncatedPage_Incomplete(t *testing.T) {
	// Initial page: 1 main post + 20 third-party reply threads (hits cap).
	edges := []string{edge(post("1001", "ROOT", "post 1", authorPk, authorUser, false, 1700000000, 1))}
	for i := 0; i < replyPageCap; i++ {
		edges = append(edges, edge(post(
			fmt.Sprintf("3%d", i), fmt.Sprintf("TP%d", i), "third party",
			"300", "troll", true, 1700001000+int64(i), 1,
		)))
	}
	page := ssrThreadPage(edges...)
	main, replies := parsePage(t, page)

	// No mock pages needed: the walk finds no continuation and does not
	// re-fetch the same post.
	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, nil, authorUser, "ROOT", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if chain.Complete {
		t.Errorf("Complete = true, want false (page truncated, cannot prove tail)")
	}
	if chain.Reason == "" {
		t.Error("Reason = empty, want an explanation")
	}
	if !strings.Contains(chain.Reason, "truncated") {
		t.Errorf("Reason = %q, want it to mention truncation", chain.Reason)
	}
	if len(chain.Posts) != 1 {
		t.Errorf("len(Posts) = %d, want 1 (only the root, no continuation found)", len(chain.Posts))
	}
	if chain.Requests != 1 {
		t.Errorf("Requests = %d, want 1 (no useless re-fetch of the same post)", chain.Requests)
	}

	// Rendered text must state possible incompleteness.
	rendered := RenderChain(chain)
	if !strings.Contains(rendered, "chain may be incomplete") {
		t.Errorf("rendered text missing incompleteness note:\n%s", rendered)
	}
}

// TestBuildAuthorChain_TruncatedPage_RecoversViaFollowUp: the initial
// page is truncated but DOES have a same-author continuation (P2). The
// walk fetches P2's own page, which is non-truncated and has P3. Expect
// the full chain [ROOT, P2, P3], complete.
func TestBuildAuthorChain_TruncatedPage_RecoversViaFollowUp(t *testing.T) {
	// Initial page: ROOT + 20 replies (truncated), one same-author
	// reply thread with [P2].
	edges := []string{edge(post("1001", "ROOT", "post 1", authorPk, authorUser, false, 1700000000, 1))}
	edges = append(edges, edge(post("1002", "P2", "post 2", authorPk, authorUser, true, 1700001000, 1)))
	for i := 0; i < replyPageCap-1; i++ {
		edges = append(edges, edge(post(
			fmt.Sprintf("3%d", i), fmt.Sprintf("TP%d", i), "third party",
			"300", "troll", true, 1700002000+int64(i), 1,
		)))
	}
	page := ssrThreadPage(edges...)
	main, replies := parsePage(t, page)

	// Follow-up: P2's own page has [P3] in a same-author reply thread,
	// non-truncated (only 2 reply threads).
	p2Page := ssrThreadPage(
		edge(post("1002", "P2", "post 2", authorPk, authorUser, false, 1700001000, 1)),
		edge(post("1003", "P3", "post 3", authorPk, authorUser, true, 1700002000, 1)),
		edge(post("2001", "TP", "third party", "200", "other", true, 1700003000, 1)),
	)
	p2Main, p2Replies := parsePage(t, p2Page)

	mf := &mockChainFetcher{
		pages: map[string]*struct {
			main    *Thread
			replies []*Thread
		}{
			"P2": {main: p2Main, replies: p2Replies},
		},
	}

	chain, err := buildAuthorChain(context.Background(), mf.fetch, nil, authorUser, "ROOT", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if got := codesAndTexts(chain); len(got) != 3 ||
		got[0][0] != "ROOT" || got[1][0] != "P2" || got[2][0] != "P3" {
		t.Errorf("chain = %v, want [ROOT, P2, P3] (recovered via follow-up)", got)
	}
	if !chain.Complete {
		t.Errorf("Complete = false, want true (Reason=%q)", chain.Reason)
	}
	if chain.Requests != 2 {
		t.Errorf("Requests = %d, want 2 (initial + P2 follow-up)", chain.Requests)
	}
}

// TestBuildAuthorChain_BoundReached_Incomplete: the walk hits the
// maxChainPosts bound. Expect incompleteness rather than a silent stop.
func TestBuildAuthorChain_BoundReached_Incomplete(t *testing.T) {
	// Build a chain with maxChainPosts+5 posts all in one same-author
	// reply thread's Items. The walk collects maxChainPosts and stops.
	continuationItems := make([]string, maxChainPosts+4) // +4 beyond the root
	for i := range continuationItems {
		continuationItems[i] = post(
			fmt.Sprintf("1%03d", i+2), fmt.Sprintf("P%d", i+2), fmt.Sprintf("post %d", i+2),
			authorPk, authorUser, true, 1700001000+int64(i)*1000, 1,
		)
	}
	page := ssrThreadPage(
		edge(post("1001", "ROOT", "post 1", authorPk, authorUser, false, 1700000000, 1)),
		edge(continuationItems...),
	)
	main, replies := parsePage(t, page)

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, nil, authorUser, "ROOT", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if chain.Complete {
		t.Errorf("Complete = true, want false (hit post bound)")
	}
	if !strings.Contains(chain.Reason, "max posts bound") {
		t.Errorf("Reason = %q, want 'max posts bound'", chain.Reason)
	}
	if len(chain.Posts) != maxChainPosts {
		t.Errorf("len(Posts) = %d, want %d", len(chain.Posts), maxChainPosts)
	}
}

// TestBuildAuthorChain_SinglePost: a thread with one post and no
// same-author continuation. Expect a chain of one, complete, and the
// rendered text has no separator noise.
func TestBuildAuthorChain_SinglePost(t *testing.T) {
	page := ssrThreadPage(
		edge(post("1001", "SOLO", "just one post", authorPk, authorUser, false, 1700000000, 1)),
		edge(post("2001", "TP", "third party", "200", "other", true, 1700001000, 1)),
	)
	main, replies := parsePage(t, page)

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, nil, authorUser, "SOLO", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if len(chain.Posts) != 1 {
		t.Fatalf("len(Posts) = %d, want 1", len(chain.Posts))
	}
	if !chain.Complete {
		t.Errorf("Complete = false, want true (Reason=%q)", chain.Reason)
	}
	rendered := RenderChain(chain)
	if strings.Contains(rendered, "---") {
		t.Errorf("single-post render has separator:\n%s", rendered)
	}
	if strings.Contains(rendered, "[1/1]") {
		t.Errorf("single-post render has index prefix:\n%s", rendered)
	}
	if !strings.Contains(rendered, "just one post") {
		t.Errorf("single-post render missing text:\n%s", rendered)
	}
}

// --- Render tests ---

// TestRenderChain_MultiPost_WithMedia: a 3-post chain where post 2 has a
// photo and post 3 has a carousel. The render must note media per post
// and separate posts visibly.
func TestRenderChain_MultiPost_WithMedia(t *testing.T) {
	chain := &Chain{
		Username: authorUser,
		Complete: true,
		Posts: []Post{
			{Code: "A", Text: "first", MediaType: 1, CreatedAt: time.Unix(1700000000, 0)},
			{Code: "B", Text: "second", MediaType: 1, CreatedAt: time.Unix(1700001000, 0)},
			{Code: "C", Text: "third", MediaType: 8, CarouselItems: []CarouselItem{{}, {}}, CreatedAt: time.Unix(1700002000, 0)},
		},
	}
	rendered := RenderChain(chain)
	if !strings.Contains(rendered, "[1/3] first") {
		t.Errorf("missing [1/3] first:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[2/3] second") {
		t.Errorf("missing [2/3] second:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[3/3] third") {
		t.Errorf("missing [3/3] third:\n%s", rendered)
	}
	if !strings.Contains(rendered, "---") {
		t.Errorf("missing separator:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[media: photo]") {
		t.Errorf("missing photo media note:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[media: carousel, 2 slides]") {
		t.Errorf("missing carousel media note:\n%s", rendered)
	}
	if strings.Contains(rendered, "incomplete") {
		t.Errorf("complete chain rendered as incomplete:\n%s", rendered)
	}
}

// TestRenderChain_Incomplete: a possibly-incomplete chain must be flagged
// at the end of the rendered text.
func TestRenderChain_Incomplete(t *testing.T) {
	chain := &Chain{
		Username: authorUser,
		Complete: false,
		Reason:   "reply page truncated (20 reply threads, cap 20); further author continuation may exist beyond the page",
		Posts: []Post{
			{Code: "A", Text: "first", MediaType: 1, CreatedAt: time.Unix(1700000000, 0)},
			{Code: "B", Text: "second", MediaType: 1, CreatedAt: time.Unix(1700001000, 0)},
		},
	}
	rendered := RenderChain(chain)
	if !strings.Contains(rendered, "[chain may be incomplete:") {
		t.Errorf("missing incompleteness note:\n%s", rendered)
	}
	if !strings.Contains(rendered, "truncated") {
		t.Errorf("incompleteness note missing reason text:\n%s", rendered)
	}
	// The note must be at the END, not interleaved.
	if !strings.HasSuffix(strings.TrimSpace(rendered), "]") {
		t.Errorf("rendered text does not end with the incompleteness note:\n%s", rendered)
	}
}

// TestRenderChain_NilOrEmpty: nil or empty chain renders as empty string.
func TestRenderChain_NilOrEmpty(t *testing.T) {
	if got := RenderChain(nil); got != "" {
		t.Errorf("RenderChain(nil) = %q, want empty", got)
	}
	if got := RenderChain(&Chain{}); got != "" {
		t.Errorf("RenderChain(empty) = %q, want empty", got)
	}
}

// TestOrderChain_TimestampWins: when timestamps disagree with collection
// order, timestamp wins (author writing order = chronological).
func TestOrderChain_TimestampWins(t *testing.T) {
	posts := []Post{
		{Code: "LATE", CreatedAt: time.Unix(1700003000, 0)},
		{Code: "EARLY", CreatedAt: time.Unix(1700001000, 0)},
		{Code: "MID", CreatedAt: time.Unix(1700002000, 0)},
	}
	ordered := orderChain(posts)
	if ordered[0].Code != "EARLY" || ordered[1].Code != "MID" || ordered[2].Code != "LATE" {
		t.Errorf("order = %s %s %s, want EARLY MID LATE", ordered[0].Code, ordered[1].Code, ordered[2].Code)
	}
}

// TestSameAuthor_PkPreferred: pk match is preferred over username; a
// different username with the same pk is the same author.
func TestSameAuthor_PkPreferred(t *testing.T) {
	p := Post{Author: ThreadsUser{ID: "100", Username: "NewName"}}
	if !sameAuthor(p, "100", "OldName") {
		t.Error("sameAuthor by pk = false, want true (pk match, username differs)")
	}
}

// TestSameAuthor_UsernameFallback: when pk is empty, falls back to
// case-insensitive username match.
func TestSameAuthor_UsernameFallback(t *testing.T) {
	p := Post{Author: ThreadsUser{Username: "NatGeo"}}
	if !sameAuthor(p, "", "natgeo") {
		t.Error("sameAuthor by username (case-insensitive) = false, want true")
	}
	if sameAuthor(p, "", "other") {
		t.Error("sameAuthor by username for different user = true, want false")
	}
}

// --- Listing cross-check tests (ambiguous truncated-page branch) ---
//
// The ambiguous branch: reply page hit replyPageCap AND no same-author
// continuation was found on it. Without the listing cross-check the walk
// reports Complete=false (the false alarm — fires on popularity, not on
// truncation of an actual chain). The listing is the author's own recent
// entries; each entry's Items carry that entry's chain flat. If the
// listing covers the linked post, its Items are authoritative and
// Complete upgrades to true.

// TestBuildAuthorChain_TruncatedNoContinuation_ListingCovers_Single: the
// false alarm. Truncated page, no continuation, listing covers the linked
// post with a single-item entry (the post was never a chain). Expect
// Complete=true, 1 post, NO incompleteness reason. This is RED before the
// fix — today the walk reports Complete=false with the truncated reason.
func TestBuildAuthorChain_TruncatedNoContinuation_ListingCovers_Single(t *testing.T) {
	main, replies := parsePage(t, truncatedNoContinuationPage("SOLO"))

	lister := &mockChainLister{threads: []*Thread{
		{Items: []Post{
			{Code: "SOLO", Text: "post 1", Author: ThreadsUser{ID: authorPk, Username: authorUser}, CreatedAt: time.Unix(1700000000, 0)},
		}},
	}}

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, lister.list, authorUser, "SOLO", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if !chain.Complete {
		t.Errorf("Complete = false, want true (listing covers the post — false alarm) Reason=%q", chain.Reason)
	}
	if chain.Reason != "" {
		t.Errorf("Reason = %q, want empty (chain is proven complete by the listing)", chain.Reason)
	}
	if len(chain.Posts) != 1 {
		t.Errorf("len(Posts) = %d, want 1", len(chain.Posts))
	}
	if chain.Posts[0].Code != "SOLO" {
		t.Errorf("Posts[0].Code = %q, want SOLO", chain.Posts[0].Code)
	}
	if chain.Requests != 2 {
		t.Errorf("Requests = %d, want 2 (initial + listing)", chain.Requests)
	}
	if lister.calls != 1 {
		t.Errorf("lister.calls = %d, want 1", lister.calls)
	}
	rendered := RenderChain(chain)
	if strings.Contains(rendered, "incomplete") {
		t.Errorf("rendered text has incompleteness note for a proven-complete single post:\n%s", rendered)
	}
}

// TestBuildAuthorChain_TruncatedNoContinuation_ListingCovers_3Items: the
// listing covers the linked post with a 3-item chain (the continuation
// fell beyond the 20-reply-thread cap and is recovered from the listing).
// Expect all 3 posts present, in writing order, Complete=true.
func TestBuildAuthorChain_TruncatedNoContinuation_ListingCovers_3Items(t *testing.T) {
	main, replies := parsePage(t, truncatedNoContinuationPage("ROOT"))

	// Listing entry: the full 3-post chain flat, NOT in writing order
	// (listing order is not chronological — measured earlier). orderChain
	// must restore writing order by CreatedAt.
	lister := &mockChainLister{threads: []*Thread{
		{Items: []Post{
			{Code: "P3", Text: "post 3", Author: ThreadsUser{ID: authorPk, Username: authorUser}, CreatedAt: time.Unix(1700002000, 0)},
			{Code: "ROOT", Text: "post 1", Author: ThreadsUser{ID: authorPk, Username: authorUser}, CreatedAt: time.Unix(1700000000, 0)},
			{Code: "P2", Text: "post 2", Author: ThreadsUser{ID: authorPk, Username: authorUser}, CreatedAt: time.Unix(1700001000, 0)},
		}},
	}}

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, lister.list, authorUser, "ROOT", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if got := codesAndTexts(chain); len(got) != 3 ||
		got[0] != [2]string{"ROOT", "post 1"} ||
		got[1] != [2]string{"P2", "post 2"} ||
		got[2] != [2]string{"P3", "post 3"} {
		t.Errorf("chain order = %v, want [ROOT, P2, P3] in writing order", got)
	}
	if !chain.Complete {
		t.Errorf("Complete = false, want true (listing covers the chain) Reason=%q", chain.Reason)
	}
	if chain.Reason != "" {
		t.Errorf("Reason = %q, want empty", chain.Reason)
	}
	if chain.Requests != 2 {
		t.Errorf("Requests = %d, want 2 (initial + listing)", chain.Requests)
	}
}

// TestBuildAuthorChain_TruncatedNoContinuation_ListingMisses: the listing
// does NOT contain the linked post (an older post, beyond the listing
// window). Nothing was learned — Complete stays false and the truncation
// reason stands unchanged. The listing fetch still counts as a request.
func TestBuildAuthorChain_TruncatedNoContinuation_ListingMisses(t *testing.T) {
	main, replies := parsePage(t, truncatedNoContinuationPage("OLD"))

	// Listing has recent entries that do NOT include OLD.
	lister := &mockChainLister{threads: []*Thread{
		{Items: []Post{
			{Code: "OTHER", Text: "unrelated recent post", Author: ThreadsUser{ID: authorPk, Username: authorUser}, CreatedAt: time.Unix(1800000000, 0)},
		}},
	}}

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, lister.list, authorUser, "OLD", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if chain.Complete {
		t.Errorf("Complete = true, want false (listing did not cover the post)")
	}
	if !strings.Contains(chain.Reason, "truncated") {
		t.Errorf("Reason = %q, want the truncation reason unchanged", chain.Reason)
	}
	if len(chain.Posts) != 1 {
		t.Errorf("len(Posts) = %d, want 1 (only the root)", len(chain.Posts))
	}
	if chain.Requests != 2 {
		t.Errorf("Requests = %d, want 2 (initial + listing attempt)", chain.Requests)
	}
}

// TestBuildAuthorChain_NonTruncatedPage_ListingNotConsulted: a
// non-truncated page completes in 1 request; the listing must never be
// consulted. Asserts the lister seam was not called and Requests == 1.
func TestBuildAuthorChain_NonTruncatedPage_ListingNotConsulted(t *testing.T) {
	page := ssrThreadPage(
		edge(post("1001", "ROOT", "post 1", authorPk, authorUser, false, 1700000000, 1)),
		edge(post("1002", "P2", "post 2", authorPk, authorUser, true, 1700001000, 1)),
		edge(post("2001", "TP", "third party", "200", "other", true, 1700002000, 1)),
	)
	main, replies := parsePage(t, page)

	lister := &mockChainLister{threads: []*Thread{
		{Items: []Post{{Code: "ROOT", Author: ThreadsUser{ID: authorPk, Username: authorUser}}}},
	}}

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, lister.list, authorUser, "ROOT", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if lister.calls != 0 {
		t.Errorf("lister.calls = %d, want 0 (non-truncated page must not consult the listing)", lister.calls)
	}
	if chain.Requests != 1 {
		t.Errorf("Requests = %d, want 1 (common case must cost exactly 1 request)", chain.Requests)
	}
	if !chain.Complete {
		t.Errorf("Complete = false, want true (Reason=%q)", chain.Reason)
	}
}

// TestBuildAuthorChain_TruncatedNoContinuation_ListingFetchError: the
// listing fetch errors. The walk degrades to today's behaviour —
// Complete=false with the truncation reason — and no error is surfaced to
// the caller.
func TestBuildAuthorChain_TruncatedNoContinuation_ListingFetchError(t *testing.T) {
	main, replies := parsePage(t, truncatedNoContinuationPage("ROOT"))

	lister := &mockChainLister{err: fmt.Errorf("listing endpoint down")}

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, lister.list, authorUser, "ROOT", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v (listing error must not surface to caller)", err)
	}
	if chain.Complete {
		t.Errorf("Complete = true, want false (listing fetch failed — degrade to truncated behaviour)")
	}
	if !strings.Contains(chain.Reason, "truncated") {
		t.Errorf("Reason = %q, want the truncation reason (degraded to today's behaviour)", chain.Reason)
	}
	if len(chain.Posts) != 1 {
		t.Errorf("len(Posts) = %d, want 1", len(chain.Posts))
	}
}

// TestBuildAuthorChain_TruncatedNoContinuation_ListingDifferentAuthor: a
// listing entry contains the linked post's code but its items are by a
// different author (pk mismatch). The entry is ignored — author identity
// is decided by pk, not by the listing's grouping. Complete stays false.
func TestBuildAuthorChain_TruncatedNoContinuation_ListingDifferentAuthor(t *testing.T) {
	main, replies := parsePage(t, truncatedNoContinuationPage("ROOT"))

	// The entry contains ROOT's code but is authored by a different pk.
	// (Defensive: the listing is the author's own, but a pk mismatch must
	// not be trusted.)
	lister := &mockChainLister{threads: []*Thread{
		{Items: []Post{
			{Code: "ROOT", Text: "post 1", Author: ThreadsUser{ID: "999", Username: "impersonator"}, CreatedAt: time.Unix(1700000000, 0)},
		}},
	}}

	chain, err := buildAuthorChain(context.Background(), (&mockChainFetcher{}).fetch, lister.list, authorUser, "ROOT", main, replies, 1)
	if err != nil {
		t.Fatalf("buildAuthorChain: %v", err)
	}
	if chain.Complete {
		t.Errorf("Complete = true, want false (pk-mismatched listing entry must be ignored)")
	}
	if !strings.Contains(chain.Reason, "truncated") {
		t.Errorf("Reason = %q, want the truncation reason (entry ignored)", chain.Reason)
	}
}
