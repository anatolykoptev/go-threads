package threads

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// loadFixture reads a sanitised JSON fixture from testdata/ and parses it
// through the REAL parseInstagramPost entry point (the authed
// /api/v1/media/<id>/info/ shape). Fixtures are built from live captures
// (see the issue report) with CDN URLs and tokens sanitised; field names,
// types, and nesting are preserved exactly as Instagram delivers them.
func loadFixture(t *testing.T, name string) *Post {
	t.Helper()
	path := filepath.Join("testdata", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	post, err := parseInstagramPost(body)
	if err != nil {
		t.Fatalf("parseInstagramPost(%s): %v", name, err)
	}
	return post
}

// TestParseCarouselPhotoSlidesOrderAndType: a pure photo carousel
// (DFeH7jYt2tv, 10 slides) must yield 10 ordered CarouselItems, each typed
// as image (MediaType 1), each retaining its multiple resolution candidates
// (NOT flattened into one list).
func TestParseCarouselPhotoSlidesOrderAndType(t *testing.T) {
	post := loadFixture(t, "carousel_photo_DFeH7jYt2tv.json")
	if post.MediaType != 8 {
		t.Fatalf("MediaType = %d, want 8 (carousel)", post.MediaType)
	}
	if len(post.CarouselItems) != 10 {
		t.Fatalf("len(CarouselItems) = %d, want 10", len(post.CarouselItems))
	}
	for i, ci := range post.CarouselItems {
		if ci.MediaType != 1 {
			t.Errorf("slide %d MediaType = %d, want 1 (image)", i, ci.MediaType)
		}
		if len(ci.Images) < 2 {
			t.Errorf("slide %d len(Images) = %d, want >=2 candidates", i, len(ci.Images))
		}
		if len(ci.Videos) != 0 {
			t.Errorf("slide %d len(Videos) = %d, want 0 (photo slide)", i, len(ci.Videos))
		}
		if ci.VideoDashManifest != "" {
			t.Errorf("slide %d VideoDashManifest = %q, want empty (photo slide)", i, ci.VideoDashManifest)
		}
	}
}

// TestParseCarouselVideoSlidesRetainVersionsAndDASH: a video carousel
// (DWO51c8kfFH slides 0,1) must type each slide as video (MediaType 2),
// retain each slide's video_versions, AND carry the per-slide DASH manifest
// fields so >720p renditions are recoverable per slide (same discipline as
// #40 for single reels).
func TestParseCarouselVideoSlidesRetainVersionsAndDASH(t *testing.T) {
	post := loadFixture(t, "carousel_video_DWO51c8kfFH.json")
	if len(post.CarouselItems) != 2 {
		t.Fatalf("len(CarouselItems) = %d, want 2", len(post.CarouselItems))
	}
	for i, ci := range post.CarouselItems {
		if ci.MediaType != 2 {
			t.Errorf("slide %d MediaType = %d, want 2 (video)", i, ci.MediaType)
		}
		if len(ci.Videos) != 3 {
			t.Errorf("slide %d len(Videos) = %d, want 3 video_versions", i, len(ci.Videos))
		}
		if ci.VideoDashManifest == "" {
			t.Errorf("slide %d VideoDashManifest empty, want the per-slide MPD", i)
		}
		if ci.NumberOfQualities == 0 {
			t.Errorf("slide %d NumberOfQualities = 0, want >0", i)
		}
		if !ci.IsDashEligible {
			t.Errorf("slide %d IsDashEligible = false, want true (live is_dash_eligible=1)", i)
		}
	}
}

// TestParseCarouselMixedSlidesOrderAndPerSlideType: a mixed carousel
// (DWO51c8kfFH: 2 video slides + 1 photo slide) must preserve slide order
// AND carry the correct per-slide type [2,2,1]. This is the case the current
// flat Post.Images/Post.Videos list cannot express.
func TestParseCarouselMixedSlidesOrderAndPerSlideType(t *testing.T) {
	post := loadFixture(t, "carousel_mixed_DWO51c8kfFH.json")
	if len(post.CarouselItems) != 3 {
		t.Fatalf("len(CarouselItems) = %d, want 3", len(post.CarouselItems))
	}
	want := []int{2, 2, 1}
	for i, w := range want {
		if post.CarouselItems[i].MediaType != w {
			t.Errorf("slide %d MediaType = %d, want %d", i, post.CarouselItems[i].MediaType, w)
		}
	}
	// The photo slide (index 2) must have no video versions / DASH.
	if post.CarouselItems[2].MediaType != 1 || len(post.CarouselItems[2].Videos) != 0 ||
		post.CarouselItems[2].VideoDashManifest != "" {
		t.Errorf("slide 2 = %+v, want a photo slide with no video/DASH", post.CarouselItems[2])
	}
}

// TestParseSinglePhotoSynthesisedCarousel: a single photo post must be
// representable through the SAME accessor — CarouselItems has one item typed
// as image, carrying the photo's candidates. Consumers range CarouselItems
// uniformly without a separate single-media branch.
func TestParseSinglePhotoSynthesisedCarousel(t *testing.T) {
	body := []byte(`{
		"items": [
			{
				"pk": "111222333",
				"code": "CuXFPB7Mv52",
				"user": {"pk": "1", "username": "test", "full_name": "Test"},
				"caption": {"text": "one photo"},
				"taken_at": 1700000000,
				"media_type": 1,
				"like_count": 42,
				"image_versions2": {"candidates": [
					{"url": "https://example.com/a.jpg", "width": 1080, "height": 1350},
					{"url": "https://example.com/b.jpg", "width": 480, "height": 600}
				]}
			}
		]
	}`)
	post, err := parseInstagramPost(body)
	if err != nil {
		t.Fatalf("parseInstagramPost: %v", err)
	}
	if len(post.CarouselItems) != 1 {
		t.Fatalf("len(CarouselItems) = %d, want 1 (synthesised single-photo)", len(post.CarouselItems))
	}
	ci := post.CarouselItems[0]
	if ci.MediaType != 1 {
		t.Errorf("MediaType = %d, want 1", ci.MediaType)
	}
	if len(ci.Images) != 2 {
		t.Errorf("len(Images) = %d, want 2 candidates", len(ci.Images))
	}
	if len(ci.Videos) != 0 {
		t.Errorf("len(Videos) = %d, want 0", len(ci.Videos))
	}
}

// TestParseSingleVideoSynthesisedCarousel: a single video post must be
// representable through the SAME accessor — CarouselItems has one item typed
// as video, carrying the video versions AND the DASH manifest (so a consumer
// reading CarouselItems[0] gets the >720p path uniformly, not two code paths).
func TestParseSingleVideoSynthesisedCarousel(t *testing.T) {
	const mpd = `<?xml version="1.0"?><MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT12S"><Period><AdaptationSet mimeType="video/mp4" maxWidth="1080" maxHeight="1920"><Representation id="0" bandwidth="500000" width="720" height="1280"/><Representation id="1" bandwidth="2301000" width="1080" height="1920"/></AdaptationSet></Period></MPD>`
	item := map[string]any{
		"pk":         "3727980973477364718",
		"code":       "DO8cvGViIPu",
		"user":       map[string]any{"pk": "1513490980", "username": "alnassr", "full_name": "Al Nassr", "is_verified": true},
		"caption":    map[string]any{"text": "Goal!"},
		"taken_at":   1758630037,
		"media_type": 2,
		"like_count": 2244564,
		"play_count": 56964765,
		"video_versions": []map[string]any{
			{"url": "https://example.com/vid.mp4", "width": 720, "height": 1280, "type": 101},
		},
		"video_dash_manifest": mpd,
		"number_of_qualities": 9,
		"is_dash_eligible":    1,
	}
	body, err := json.Marshal(map[string]any{"items": []map[string]any{item}})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	post, err := parseInstagramPost(body)
	if err != nil {
		t.Fatalf("parseInstagramPost: %v", err)
	}
	if len(post.CarouselItems) != 1 {
		t.Fatalf("len(CarouselItems) = %d, want 1 (synthesised single-video)", len(post.CarouselItems))
	}
	ci := post.CarouselItems[0]
	if ci.MediaType != 2 {
		t.Errorf("MediaType = %d, want 2", ci.MediaType)
	}
	if len(ci.Videos) != 1 {
		t.Errorf("len(Videos) = %d, want 1", len(ci.Videos))
	}
	if ci.VideoDashManifest != mpd {
		t.Errorf("VideoDashManifest = %q, want the raw MPD", ci.VideoDashManifest)
	}
	if !ci.IsDashEligible {
		t.Errorf("IsDashEligible = false, want true")
	}
	if ci.NumberOfQualities != 9 {
		t.Errorf("NumberOfQualities = %d, want 9", ci.NumberOfQualities)
	}
}

// TestParseNonMediaPostEmptyCarousel: a post with no carousel media and no
// single-media versions (a text-only / non-media post) must yield an EMPTY
// CarouselItems — not nil-panicking — so consumers can range over it
// unconditionally.
func TestParseNonMediaPostEmptyCarousel(t *testing.T) {
	body := []byte(`{
		"items": [
			{
				"pk": "999",
				"code": "TextOnly",
				"user": {"pk": "1", "username": "test"},
				"caption": {"text": "just text"},
				"taken_at": 1700000000,
				"media_type": 1,
				"like_count": 5
			}
		]
	}`)
	post, err := parseInstagramPost(body)
	if err != nil {
		t.Fatalf("parseInstagramPost: %v", err)
	}
	if post.CarouselItems == nil {
		t.Fatal("CarouselItems = nil, want non-nil empty slice (range-safe)")
	}
	if len(post.CarouselItems) != 0 {
		t.Errorf("len(CarouselItems) = %d, want 0", len(post.CarouselItems))
	}
	// Ranging must not panic.
	for range post.CarouselItems {
		t.Error("ranged over an empty CarouselItems")
	}
}

// TestParseCarouselSlideBadFieldTypeNoAbort: one slide with an unexpected
// field type (is_dash_eligible as a string "maybe") must NOT abort the whole
// post decode. The post parses, every slide is present in order, and the bad
// slide degrades to IsDashEligible=false rather than killing the response.
// This is the carousel analogue of the v0.7.0 is_dash_eligible incident
// (#43), where one bad field aborted the entire items[] unmarshal.
func TestParseCarouselSlideBadFieldTypeNoAbort(t *testing.T) {
	body := []byte(`{
		"items": [
			{
				"pk": "888",
				"code": "BadSlide",
				"user": {"pk": "1", "username": "test"},
				"caption": {"text": "carousel"},
				"taken_at": 1700000000,
				"media_type": 8,
				"like_count": 10,
				"carousel_media": [
					{"media_type": 1, "image_versions2": {"candidates": [{"url": "https://example.com/s0.jpg", "width": 1080, "height": 1350}]}},
					{"media_type": 2, "video_versions": [{"url": "https://example.com/s1.mp4", "width": 720, "height": 1280, "type": 101}], "is_dash_eligible": "maybe", "number_of_qualities": 4, "video_dash_manifest": "<MPD/>"},
					{"media_type": 1, "image_versions2": {"candidates": [{"url": "https://example.com/s2.jpg", "width": 1080, "height": 1350}]}}
				]
			}
		]
	}`)
	post, err := parseInstagramPost(body)
	if err != nil {
		t.Fatalf("parseInstagramPost: whole post aborted by one bad slide: %v", err)
	}
	if len(post.CarouselItems) != 3 {
		t.Fatalf("len(CarouselItems) = %d, want 3 (all slides present, order kept)", len(post.CarouselItems))
	}
	want := []int{1, 2, 1}
	for i, w := range want {
		if post.CarouselItems[i].MediaType != w {
			t.Errorf("slide %d MediaType = %d, want %d", i, post.CarouselItems[i].MediaType, w)
		}
	}
	if post.CarouselItems[1].IsDashEligible {
		t.Errorf("bad slide IsDashEligible = true, want false (tolerated garbage -> false)")
	}
	if post.CarouselItems[1].VideoDashManifest != "<MPD/>" {
		t.Errorf("bad slide VideoDashManifest = %q, want preserved", post.CarouselItems[1].VideoDashManifest)
	}
}

// TestParseCarouselImagesVideosUnchangedRegression: Post.Images and
// Post.Videos keep their current flattened behaviour, unchanged. For the
// mixed carousel the flat list is the union of every slide's candidates /
// versions in slide order; CarouselItems is ADDITIVE. This proves no
// production regression for go-media v0.3.7 / vaelor-agent which are live
// against the flat fields right now.
func TestParseCarouselImagesVideosUnchangedRegression(t *testing.T) {
	post := loadFixture(t, "carousel_mixed_DWO51c8kfFH.json")
	// Mixed carousel = 2 video slides (each 2 img candidates + 3 vid versions)
	// + 1 photo slide (2 img candidates). Top-level image_versions2 adds 2
	// more img candidates. Flat Images = 2 (top) + 2 + 2 + 2 = 8; flat Videos
	// = 3 + 3 = 6. This is the pre-existing flatten behaviour, byte-identical.
	if got, want := len(post.Images), 8; got != want {
		t.Errorf("len(Images) = %d, want %d (flattened, unchanged)", got, want)
	}
	if got, want := len(post.Videos), 6; got != want {
		t.Errorf("len(Videos) = %d, want %d (flattened, unchanged)", got, want)
	}
	// And the structured view coexists.
	if len(post.CarouselItems) != 3 {
		t.Errorf("len(CarouselItems) = %d, want 3 (additive, not a replacement)", len(post.CarouselItems))
	}
}
