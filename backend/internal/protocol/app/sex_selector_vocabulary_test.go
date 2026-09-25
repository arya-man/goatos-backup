package app

import (
	"errors"
	"testing"
)

// OPEN UP TO NEW SPECIES (maintainer decision 2026-09-25): a vaccination rule may select a gender
// the farm added on Configuration, exactly as it could always name any species; "unknown" and a
// value that is not even a code are still refused.
func TestSexSelectorAcceptsAConfiguredGenderCode(t *testing.T) {
	for _, ok := range []string{"female", "male", "all", "castrated", " Castrated "} {
		if _, err := normalizeSelectorValue("sex", ok); err != nil {
			t.Fatalf("sex selector %q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"unknown", "male or female", "9lives"} {
		if _, err := normalizeSelectorValue("sex", bad); !errors.Is(err, ErrNotPublishable) {
			t.Fatalf("sex selector %q: err = %v, want ErrNotPublishable", bad, err)
		}
	}
}
