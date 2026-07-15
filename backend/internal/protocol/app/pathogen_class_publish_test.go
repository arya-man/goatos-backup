package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// TestPublishRejectsVaccineTypeValueInPathogenClass proves publish validation
// rejects a matrix whose vaccine.pathogen_class carries a vaccine-type value
// (live/killed/toxoid/combo) instead of an organism class (BUG1). A valid
// bacterial/viral matrix must still publish.
func TestPublishRejectsVaccineTypeValueInPathogenClass(t *testing.T) {
	base := validVaccinationMatrixRulesetDSL()
	if !strings.Contains(base, `"pathogen_class":"viral"`) {
		t.Fatalf("test fixture drift: expected a viral pathogen_class in the canonical matrix ruleset")
	}

	version := func(ruleDSL string) domain.Version {
		return domain.Version{
			SopVersionID: "62000000-0000-4000-8000-000000000001",
			ProofPolicy:  []byte(`{"required":true,"types":["video"]}`),
			RuleDsl:      []byte(ruleDSL),
		}
	}

	// Sanity: the reviewed matrix (Sheep Pox live/viral) is publishable.
	if err := ValidateExecutionContract(version(base)); err != nil {
		t.Fatalf("reviewed matrix ruleset must be publishable, got: %v", err)
	}

	for _, bad := range []string{"live", "killed", "toxoid", "combo"} {
		t.Run("pathogen_class="+bad, func(t *testing.T) {
			malformed := strings.Replace(base, `"pathogen_class":"viral"`, `"pathogen_class":"`+bad+`"`, 1)
			err := ValidateExecutionContract(version(malformed))
			if !errors.Is(err, ErrNotPublishable) {
				t.Fatalf("pathogen_class=%q must fail publish with ErrNotPublishable, got: %v", bad, err)
			}
		})
	}
}
