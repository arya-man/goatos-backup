package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// vaccinationVersionWithDeferStates returns a fully-valid vaccination matrix
// version whose eligibility.defer_states array is swapped for deferJSON.
func vaccinationVersionWithDeferStates(t *testing.T, deferJSON string) domain.Version {
	t.Helper()
	dsl := validVaccinationMatrixRuleDSL()
	const fullSet = `"defer_states":["sick","under_treatment","icu","quarantine"]`
	if !strings.Contains(dsl, fullSet) {
		t.Fatalf("base matrix fixture no longer carries the expected mandatory defer_states token %q", fullSet)
	}
	dsl = strings.Replace(dsl, fullSet, `"defer_states":`+deferJSON, 1)
	return domain.Version{
		SopVersionID: "62000000-0000-4000-8000-000000000001",
		Category:     "vaccination",
		ProofPolicy:  []byte(`{"required":true,"types":["video"]}`),
		RuleDsl:      []byte(dsl),
	}
}

// C35-010: publishing a vaccination matrix whose eligibility.defer_states is
// present but omits a mandatory clinical safety state (sick, under_treatment,
// quarantine, icu) must be rejected — a partial list would let a sick animal's
// open work be cancelled instead of deferred. An empty/absent list is allowed
// (it maps to the safe full default).
func TestValidateVaccinationMatrixRejectsPartialClinicalDeferStates(t *testing.T) {
	rejected := []struct {
		name      string
		deferJSON string
	}{
		{name: "icu+quarantine drops sick and under_treatment", deferJSON: `["icu","quarantine"]`},
		{name: "drops only under_treatment", deferJSON: `["sick","quarantine","icu"]`},
		{name: "single state", deferJSON: `["sick"]`},
		{name: "single icu string", deferJSON: `["icu"]`},
	}
	for _, tc := range rejected {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			v := vaccinationVersionWithDeferStates(t, tc.deferJSON)
			err := ValidateExecutionContract(v)
			if !errors.Is(err, ErrNotPublishable) {
				t.Fatalf("partial defer_states %s should be not publishable, got %v", tc.deferJSON, err)
			}
		})
	}

	accepted := []struct {
		name      string
		deferJSON string
	}{
		{name: "full mandatory set", deferJSON: `["sick","under_treatment","icu","quarantine"]`},
		{name: "case-insensitive full set", deferJSON: `["Sick","UNDER_TREATMENT","ICU","Quarantine"]`},
		{name: "superset with extra authored state", deferJSON: `["sick","under_treatment","icu","quarantine","post_breeding_hold"]`},
		{name: "empty list maps to safe default", deferJSON: `[]`},
	}
	for _, tc := range accepted {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			v := vaccinationVersionWithDeferStates(t, tc.deferJSON)
			if err := ValidateExecutionContract(v); err != nil {
				t.Fatalf("safe defer_states %s should publish, got %v", tc.deferJSON, err)
			}
		})
	}
}
