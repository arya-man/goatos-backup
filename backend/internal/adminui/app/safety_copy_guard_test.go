package app

import (
	"fmt"
	"strings"
	"testing"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestSafetyRuleCopyMatchesEnforcedDefaults locks the read-only "Automatic safety
// rules" copy shown in the vaccination Config UI to the constants the engine actually
// enforces. The Config UI shows these values read-only, and the copy strings are
// hand-written prose in pageSpecificCopy("vaccination-plan"); the enforced values live in the
// vaccination/obligation defaults. If someone changes an enforced default (e.g.
// live-to-live 28 -> 21) without updating the UI sentence — or edits the sentence
// without changing enforcement — this test fails. That guarantees the screen can never
// display a number the engine does not actually enforce.
func TestSafetyRuleCopyMatchesEnforcedDefaults(t *testing.T) {
	copyMap := pageSpecificCopy("vaccination-plan")

	cases := []struct {
		key     string
		enforce int32
	}{
		{"modal.rule_editor.guided.safety_live_live", vaccapp.DefaultLiveToLiveGapDays},
		{"modal.rule_editor.guided.safety_killed_live", vaccapp.DefaultLiveToKilledGapDays},
		{"modal.rule_editor.guided.safety_max_shots", obldomain.DefaultMaxShotsPerAnimalPerDrive},
		{"modal.rule_editor.guided.safety_batch", obldomain.DefaultMaxBatchingHoldDays},
	}

	for _, tc := range cases {
		got, ok := copyMap[tc.key]
		if !ok || got == "" {
			t.Fatalf("config page copy is missing key %q", tc.key)
		}
		want := fmt.Sprintf("%d", tc.enforce)
		if !strings.Contains(got, want) {
			t.Errorf(
				"safety copy %q = %q but the engine enforces %s — the UI would show a stale/wrong number (enforcement/display drift)",
				tc.key, got, want,
			)
		}
	}

	// KilledToKilledGapDays shares the "14 days" sentence with LiveToKilledGapDays;
	// assert they stay equal so the single sentence remains accurate for both.
	if vaccapp.DefaultLiveToKilledGapDays != vaccapp.DefaultKilledToKilledGapDays {
		t.Errorf(
			"safety_killed_live copy states one spacing value, but live->killed (%d) and killed->killed (%d) defaults differ; split the copy",
			vaccapp.DefaultLiveToKilledGapDays, vaccapp.DefaultKilledToKilledGapDays,
		)
	}
}
