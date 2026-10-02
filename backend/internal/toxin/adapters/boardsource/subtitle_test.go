package boardsource

import (
	"strings"
	"testing"
)

// A weighbridge reading like 10259.997 kg reached the Work Board card verbatim. The card names
// the load to a person, so it carries at most one decimal.
func TestSubtitleRoundsTheLoadWeightForReading(t *testing.T) {
	got := Subtitle(353, "Sanchit", 10259.997, "", "", "")
	if strings.Contains(got, "10259.997") {
		t.Fatalf("subtitle leaks the raw weighbridge figure: %q", got)
	}
	if !strings.Contains(got, "10260 kg") {
		t.Fatalf("subtitle = %q, want the load weight rounded to 10260 kg", got)
	}
	if got := Subtitle(354, "Sanchit", 6250, "", "", ""); !strings.Contains(got, "6250 kg") {
		t.Fatalf("whole kilograms must stay whole: %q", got)
	}
}
