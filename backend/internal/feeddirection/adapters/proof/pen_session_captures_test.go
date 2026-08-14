package proof

import (
	"testing"
	"time"

	fdports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// TestPenSessionCaptureKeyMatchesTheClientToken pins the EXACT key the phone stamps on every feed
// proof upload.
//
// This is a string-equality lookup against client-supplied metadata, so drift between this function
// and the Kotlin feedCaptureGroupKey/partitionMatchToken does not fail loudly -- it silently returns
// NO matches, and the multi-operator flow quietly degrades to "nobody captured anything". The
// expectations below are REAL client_task_key values read from STG on 2026-08-14, not invented.
func TestPenSessionCaptureKeyMatchesTheClientToken(t *testing.T) {
	day := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	shed := "02fd0089-8610-548b-af93-87942fbadbb8"

	for _, tc := range []struct {
		name      string
		partition string
		session   int32
		workflow  string
		want      string
	}{
		{
			name: "numeric pen, observed on STG", partition: "10", session: 1, workflow: "normal",
			want: "feed-dist:2026-08-14:02fd0089-8610-548b-af93-87942fbadbb8:10:1:normal",
		},
		{
			name: "experiment workflow pen", partition: "1", session: 1, workflow: "experiment",
			want: "feed-dist:2026-08-14:02fd0089-8610-548b-af93-87942fbadbb8:1:1:experiment",
		},
		{
			// "Part 8" lowercases but keeps its word: the phone's token is NOT the server's
			// PartitionMatchKey, which would strip the "part " prefix to "8" and match nothing.
			name: "prefixed pen keeps its word, lowercased", partition: "Part 8", session: 1, workflow: "normal",
			want: "feed-dist:2026-08-14:02fd0089-8610-548b-af93-87942fbadbb8:part 8:1:normal",
		},
		{
			name: "undivided shed is the whole sentinel", partition: "", session: 2, workflow: "normal",
			want: "feed-dist:2026-08-14:02fd0089-8610-548b-af93-87942fbadbb8:whole:2:normal",
		},
		{
			// Whitespace and case variants of one pen must not split it into two keys.
			name: "padded, mixed case collapses", partition: "  PART   8 ", session: 1, workflow: "normal",
			want: "feed-dist:2026-08-14:02fd0089-8610-548b-af93-87942fbadbb8:part 8:1:normal",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := PenSessionCaptureKey(fdports.PenSessionCaptureQuery{
				ShedID:         shed,
				PartitionLabel: tc.partition,
				SessionNo:      tc.session,
				TargetDate:     day,
				Workflow:       tc.workflow,
			})
			if got != tc.want {
				t.Fatalf("key=%q, want %q -- server and phone must agree byte for byte", got, tc.want)
			}
		})
	}
}

// TestPenSessionCaptureKeySeparatesPens is the defect this module keeps paying for: two pens of one
// shed, same session and day, must never resolve to one key.
func TestPenSessionCaptureKeySeparatesPens(t *testing.T) {
	base := fdports.PenSessionCaptureQuery{
		ShedID:     "02fd0089-8610-548b-af93-87942fbadbb8",
		SessionNo:  1,
		TargetDate: time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC),
		Workflow:   "normal",
	}
	penOne, penTwo := base, base
	penOne.PartitionLabel, penTwo.PartitionLabel = "1", "2"
	if PenSessionCaptureKey(penOne) == PenSessionCaptureKey(penTwo) {
		t.Fatal("two pens of one shed resolved to the same key")
	}

	morning, evening := base, base
	morning.SessionNo, evening.SessionNo = 1, 2
	if PenSessionCaptureKey(morning) == PenSessionCaptureKey(evening) {
		t.Fatal("a pen's morning and evening resolved to the same key -- they are two separate bags")
	}
}
