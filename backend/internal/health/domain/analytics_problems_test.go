package domain

import (
	"testing"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
)

// A farm that reads "1-3 months" on the mortality board and "1-3 months" on the health-problems
// chart is entitled to assume the two mean the same animals. The two lists are declared in
// separate packages because Health must not depend on Counts at runtime, so this is the thing
// holding them together: edit either side alone and it goes red.
func TestHealthProblemAgeBandsMatchMortality(t *testing.T) {
	if len(HealthProblemAgeBandOrder) != len(countsdomain.MortalityAgeBandOrder) {
		t.Fatalf("band count drifted: health has %d, mortality has %d", len(HealthProblemAgeBandOrder), len(countsdomain.MortalityAgeBandOrder))
	}
	for i, key := range HealthProblemAgeBandOrder {
		if other := countsdomain.MortalityAgeBandOrder[i]; key != other {
			t.Fatalf("band %d drifted: health has %q, mortality has %q", i, key, other)
		}
		// The LABEL matters as much as the key: two charts keyed the same but captioned
		// differently are read as two different questions.
		if got, want := HealthProblemAgeBandLabel(key), countsdomain.MortalityAgeBandLabel(key); got != want {
			t.Fatalf("band %q label drifted: health says %q, mortality says %q", key, got, want)
		}
	}
}

// The two real sides must never swap places between windows: a chart whose bars reorder by size
// invites exactly the elevated-versus-non-elevated misreading it exists to prevent. Unclassified
// is always last, because it is not a third kind of pen -- it is the pens nobody has typed.
func TestPenTypeOrderIsFixedAndUnclassifiedIsLast(t *testing.T) {
	want := []string{"elevated", "non_elevated", "unclassified"}
	if len(HealthPenTypeOrder) != len(want) {
		t.Fatalf("pen type spine changed length: %v", HealthPenTypeOrder)
	}
	for i, key := range want {
		if HealthPenTypeOrder[i] != key {
			t.Fatalf("pen type %d is %q, want %q", i, HealthPenTypeOrder[i], key)
		}
		if HealthPenTypeLabel(key) == key {
			t.Fatalf("pen type %q has no farm copy; the client renders the label verbatim", key)
		}
	}
	// The retired weighing key must not come back under the new spine.
	for _, key := range HealthPenTypeOrder {
		if key == "ground" {
			t.Fatal("the retired 'ground' key is back; one farm concept has one name")
		}
	}
}
