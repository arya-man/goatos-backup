package domain

import "testing"

// Declared labels name the proof semantically — this is what tells a verifier which of the two feed
// proofs she is watching.
func TestMediaLabelForUsesDeclaredLabels(t *testing.T) {
	def := CategoryDefinition{
		ExpectedMedia: []string{"video", "photo_or_video"},
		MediaLabels:   []string{"Feed distribution video", "Water distribution proof"},
	}
	if got := def.MediaLabelFor(0, 2); got != "Feed distribution video" {
		t.Fatalf("label[0] = %q", got)
	}
	if got := def.MediaLabelFor(1, 2); got != "Water distribution proof" {
		t.Fatalf("label[1] = %q", got)
	}
}

// With no declared labels the header still must not be blank: it falls back to the humanised
// expected-media kind. A single video needs no ordinal.
func TestMediaLabelForFallsBackToExpectedMediaKind(t *testing.T) {
	def := CategoryDefinition{ExpectedMedia: []string{"video"}}
	if got := def.MediaLabelFor(0, 1); got != "Video" {
		t.Fatalf("label = %q, want %q", got, "Video")
	}
	mixed := CategoryDefinition{ExpectedMedia: []string{"video", "photo_or_video"}}
	if got := mixed.MediaLabelFor(1, 2); got != "Photo or video" {
		t.Fatalf("label = %q, want %q", got, "Photo or video")
	}
}

// Milk preparation carries five videos. Five rows of "Video" would be useless, so a repeated kind
// is numbered.
func TestMediaLabelForNumbersRepeatedKinds(t *testing.T) {
	def := CategoryDefinition{ExpectedMedia: []string{"video", "video", "video", "video", "video"}}
	for i, want := range []string{"Video 1", "Video 2", "Video 3", "Video 4", "Video 5"} {
		if got := def.MediaLabelFor(i, 5); got != want {
			t.Fatalf("label[%d] = %q, want %q", i, got, want)
		}
	}
}

// A category that declared nothing (or an item carrying more proofs than declared) still gets a
// header rather than an empty one.
func TestMediaLabelForNeverReturnsBlank(t *testing.T) {
	empty := CategoryDefinition{}
	if got := empty.MediaLabelFor(0, 1); got != "Proof" {
		t.Fatalf("label = %q, want %q", got, "Proof")
	}
	if got := empty.MediaLabelFor(1, 3); got != "Proof 2" {
		t.Fatalf("label = %q, want %q", got, "Proof 2")
	}
	// More media than the category declared: index past ExpectedMedia must not panic or blank out.
	short := CategoryDefinition{ExpectedMedia: []string{"video"}, MediaLabels: []string{"Shifting video"}}
	if got := short.MediaLabelFor(2, 3); got != "Proof 3" {
		t.Fatalf("overflow label = %q, want %q", got, "Proof 3")
	}
}

// Raw config tokens are never operator-facing copy (AGENTS.md UI-token rule).
func TestMediaLabelForNeverLeaksRawTokens(t *testing.T) {
	def := CategoryDefinition{ExpectedMedia: []string{"photo_or_video", "some_new_kind"}}
	for i, want := range []string{"Photo or video", "Some new kind"} {
		if got := def.MediaLabelFor(i, 2); got != want {
			t.Fatalf("label[%d] = %q, want %q", i, got, want)
		}
	}
}

// MediaLabels is positional against ExpectedMedia, so a mismatched length would name proofs the
// producer never sends. That is rejected at registration rather than surfacing as a mislabelled
// video in front of a verifier.
func TestRegisterRejectsMediaLabelLengthMismatch(t *testing.T) {
	r := NewRegistry()
	err := r.Register(CategoryDefinition{
		Vertical: "feed", Module: "feed", Category: "feed_distribution",
		ExpectedMedia: []string{"video"},
		MediaLabels:   []string{"Feed distribution video", "Water distribution proof"},
	})
	if err == nil {
		t.Fatal("expected a length-mismatch rejection")
	}
	ok := r.Register(CategoryDefinition{
		Vertical: "feed", Module: "feed", Category: "feed_packing",
		ExpectedMedia: []string{"video"},
		MediaLabels:   []string{"Feed packing video"},
	})
	if ok != nil {
		t.Fatalf("matched lengths must register: %v", ok)
	}
	// Declaring no labels at all stays legal — MediaLabelFor covers it.
	if err := r.Register(CategoryDefinition{
		Vertical: "counts", Module: "counts", Category: "birth_evidence",
		ExpectedMedia: []string{"video"},
	}); err != nil {
		t.Fatalf("absent labels must register: %v", err)
	}
}
