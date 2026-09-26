package domain_test

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
)

// Every stage the product names in code -- the newborn stage births are recorded at, Flushing, and
// every rung of the growth ladder -- must be a built-in row on the Stages register. Otherwise the
// farm can delete one on Configuration and quietly break births, flushing or a growth step.
func TestProtectedStageCodesCoverEveryStageTheProductNames(t *testing.T) {
	for _, code := range countsdomain.ProductNamedStageCodes() {
		if !domain.IsBuiltinCode(domain.RegStages, code) {
			t.Errorf("stage %q is named by counts/domain but is not protected on the Stages register", code)
		}
	}
}

func TestBuiltinCodesCompareCaseInsensitively(t *testing.T) {
	for _, c := range []struct {
		register, code string
		want           bool
	}{
		{domain.RegStages, "K0", true},
		{domain.RegStages, " f2-Male ", true},
		{domain.RegStages, "Warmup", false},
		{domain.RegSpecies, "Goat", true},
		{domain.RegSexes, "male", true},
		{domain.RegSexes, "neuter", false},
	} {
		if got := domain.IsBuiltinCode(c.register, c.code); got != c.want {
			t.Errorf("IsBuiltinCode(%q, %q) = %v, want %v", c.register, c.code, got, c.want)
		}
	}
}
