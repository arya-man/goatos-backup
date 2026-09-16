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
	got := ComposeMediaLabels(in, nil)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComposeMediaLabels = %q, want %q", got, want)
	}
	if again := ComposeMediaLabels(got, nil); !reflect.DeepEqual(again, want) {
		t.Fatalf("ComposeMediaLabels is not idempotent: %q", again)
	}
	if got := ComposeMediaLabels([]string{"Video", "Video"}, nil); got[0] != "Video 1 of 2" {
		t.Fatalf("repeat = %q", got)
	}
}

// A step with one video and one photo under the SAME title reads "Title · video" / "Title · photo"
// rather than "1 of 2 / 2 of 2", which named two different kinds of proof as if they were a series.
// "k of N" stays for repeats of the same kind, and a kind nobody resolved keeps plain numbering.
func TestComposeMediaLabelsSplitsARepeatedTitleByKind(t *testing.T) {
	cases := []struct {
		name          string
		labels, kinds []string
		want          []string
	}{
		{"video and photo", []string{"Gate latched", "Gate latched"}, []string{"video", "image/jpeg"},
			[]string{"Gate latched · video", "Gate latched · photo"}},
		{"two videos one photo", []string{"Kid", "Kid", "Kid", "Water"}, []string{"video/mp4", "photo", "video", "video"},
			[]string{"Kid · video 1 of 2", "Kid · photo", "Kid · video 2 of 2", "Water"}},
		{"same kind stays k of N", []string{"Kid", "Kid"}, []string{"video", "video/mp4"},
			[]string{"Kid 1 of 2", "Kid 2 of 2"}},
		{"unknown kinds stay k of N", []string{"Kid", "Kid"}, []string{"", "application/octet-stream"},
			[]string{"Kid 1 of 2", "Kid 2 of 2"}},
		{"short kinds slice", []string{"Kid", "Kid"}, []string{"video"},
			[]string{"Kid 1 of 2", "Kid 2 of 2"}},
		{"single proof untouched", []string{"Weighing video"}, []string{"video"}, []string{"Weighing video"}},
	}
	for _, tc := range cases {
		got := ComposeMediaLabels(tc.labels, tc.kinds)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: ComposeMediaLabels = %q, want %q", tc.name, got, tc.want)
		}
		if again := ComposeMediaLabels(got, tc.kinds); !reflect.DeepEqual(again, tc.want) {
			t.Errorf("%s: not idempotent: %q", tc.name, again)
		}
	}
}
