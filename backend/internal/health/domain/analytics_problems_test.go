package domain

import (
	"os"
	"strings"
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

// Pen types are the farm's own register (migration 000437, maintainer instruction 2026-09-25):
// "not set" is the ONLY pen-type key this package may name. A key or label for elevated,
// non-elevated or any other kind typed here is exactly the hard-coding the register replaced --
// a type the farm adds would then be missing from, or mislabelled on, the chart.
func TestHealthNamesNoPenTypeOfItsOwn(t *testing.T) {
	text, err := os.ReadFile("analytics.go")
	if err != nil {
		t.Fatalf("read analytics.go: %v", err)
	}
	code := strings.ToLower(string(text))
	for _, banned := range []string{`"elevated"`, `"non_elevated"`, `"ground"`, `"elevated pen"`, `"non-elevated pen"`} {
		if strings.Contains(code, banned) {
			t.Fatalf("health/domain names the pen type %s; pen types come from the Pen types register", banned)
		}
	}
	if HealthPenTypeUnclassifiedLabel == HealthPenTypeUnclassified {
		t.Fatal("the not-set bucket has no farm copy; the client renders the label verbatim")
	}
}
