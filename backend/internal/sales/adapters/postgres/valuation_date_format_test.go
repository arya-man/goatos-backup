package postgres

import (
	"strings"
	"testing"
)

// The valuation card's "Last saved" is composed here and rendered verbatim, so it must carry the
// one visible date shape (DD/MM/YYYY, maintainer lock 2026-09-10). It read "25-09-2026 17:26".
func TestValuationLastSavedIsDDMMYYYY(t *testing.T) {
	if !strings.Contains(valuationReadSQL, "'DD/MM/YYYY HH24:MI'") {
		t.Fatalf("valuation updated_at must be composed DD/MM/YYYY HH24:MI:\n%s", valuationReadSQL)
	}
	if strings.Contains(valuationReadSQL, "DD-MM-YYYY") {
		t.Fatalf("the retired dash date shape is back in the valuation read")
	}
}
