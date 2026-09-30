package app

import (
	"strings"
	"testing"

	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// The admin-web drawer renders the line under a flagged box from THIS copy, keyed by the producer's
// direction code; the phone renders the producer's own sentence. The two surfaces must say the same
// thing about the same reading, for packing (500 g) and distribution (5%) alike -- and the shared
// banner, keyed by the one refusal code both categories use, must name neither tolerance.
func TestVerificationVarianceCopyMatchesTheProducersSentences(t *testing.T) {
	copy := pageSpecificCopy("verification-review")
	for code, want := range map[string]string{
		feeddirectiondomain.PackingEntryAbovePlan:      feeddirectiondomain.PackingEntryVarianceMessage(feeddirectiondomain.PackingEntryAbovePlan),
		feeddirectiondomain.PackingEntryBelowPlan:      feeddirectiondomain.PackingEntryVarianceMessage(feeddirectiondomain.PackingEntryBelowPlan),
		feeddirectiondomain.DistributionEntryAbovePlan: feeddirectiondomain.DistributionEntryVarianceMessage(feeddirectiondomain.DistributionEntryAbovePlan),
		feeddirectiondomain.DistributionEntryBelowPlan: feeddirectiondomain.DistributionEntryVarianceMessage(feeddirectiondomain.DistributionEntryBelowPlan),
	} {
		if got := copy["verdict.variance."+code]; got != want {
			t.Errorf("verdict.variance.%s = %q, want the producer's %q", code, got, want)
		}
	}
	banner := copy["feedback.measurement_confirmation_required"]
	if banner == "" {
		t.Fatal("the confirm banner copy is missing")
	}
	if strings.Contains(banner, "500 g") || strings.Contains(banner, "%") {
		t.Errorf("the shared banner %q names one category's tolerance", banner)
	}
}
