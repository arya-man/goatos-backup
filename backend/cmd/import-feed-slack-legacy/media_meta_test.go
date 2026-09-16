package main

import (
	"testing"

	feedports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// The legacy Slack import queues its three proofs under the SEEDED distribution card's titles, and
// each proof's kind is what the Slack file actually IS -- a weight "photo" Slack stored as a video
// plays as a video, and a file of neither kind is left unknown for the register to answer.
func TestLegacyImportMediaMetaTakesKindFromTheSlackFileMime(t *testing.T) {
	weight := preparedProof{ID: "w", File: file{MimeType: "image/jpeg"}}
	dist := preparedProof{ID: "d", File: file{MimeType: "video/quicktime"}}
	water := preparedProof{ID: "x", File: file{MimeType: "application/octet-stream"}}
	refs, meta := legacyDistributionMedia(weight, dist, water)
	if len(refs) != 3 || refs[0] != "w" || refs[1] != "d" || refs[2] != "x" {
		t.Fatalf("refs = %v", refs)
	}
	want := []feedports.ProofMeta{
		{Label: "Feed weight photo", Kind: "photo"},
		{Label: "Feed distribution video", Kind: "video"},
		{Label: "Water distribution video", Kind: ""},
	}
	for i := range want {
		if meta[i] != want[i] {
			t.Fatalf("meta = %+v, want %+v", meta, want)
		}
	}
}
