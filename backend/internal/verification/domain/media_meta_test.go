package domain

import (
	"reflect"
	"testing"
)

// An `either` slot the register never judged, or a kind nobody recognises, must stay UNKNOWN so
// the queue asks the proof register instead of guessing a player.
func TestNormalizeMediaKindMapsEitherAndUnknownToBlank(t *testing.T) {
	for in, want := range map[string]string{
		"video": "video", " Video ": "video", "photo": "photo", "image": "photo", "IMAGE": "photo",
		"either": "", "photo_or_video": "", "": "", "attachment": "", "audio": "",
	} {
		if got := NormalizeMediaKind(in); got != want {
			t.Fatalf("NormalizeMediaKind(%q) = %q, want %q", in, got, want)
		}
	}
	if got := (MediaMeta{Kind: "either"}).MimeType(); got != "" {
		t.Fatalf("either MimeType = %q, want blank", got)
	}
}

func TestBuildMediaMetaIsPositionalAndTrimmed(t *testing.T) {
	got := BuildMediaMeta([]ProofCapture{
		{Title: "  Feed weight ", Kind: "image"},
		{Title: "Distribution", Kind: "VIDEO"},
		{Title: "", Kind: "either"},
	})
	want := []MediaMeta{{Label: "Feed weight", Kind: "photo"}, {Label: "Distribution", Kind: "video"}, {Label: "", Kind: ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildMediaMeta = %+v, want %+v", got, want)
	}
	if BuildMediaMeta(nil) != nil {
		t.Fatal("no captures must build no meta")
	}
}

// Two proofs a producer titled the same are told apart as "k of N"; distinct and blank titles are
// untouched (a blank is the registry fallback's job), and composing twice changes nothing.
func TestComposeMediaLabelsNumbersRepeatedTitlesKOfN(t *testing.T) {
	in := []string{"Iodine dipping", "Water", "Iodine dipping", "", "Iodine dipping", " "}
	want := []string{"Iodine dipping 1 of 3", "Water", "Iodine dipping 2 of 3", "", "Iodine dipping 3 of 3", " "}
	got := ComposeMediaLabels(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComposeMediaLabels = %q, want %q", got, want)
	}
	if again := ComposeMediaLabels(got); !reflect.DeepEqual(again, want) {
		t.Fatalf("ComposeMediaLabels is not idempotent: %q", again)
	}
	if got := ComposeMediaLabels([]string{"Video", "Video"}); got[0] != "Video 1 of 2" {
		t.Fatalf("repeat = %q", got)
	}
}
