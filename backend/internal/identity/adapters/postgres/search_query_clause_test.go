package postgres

import (
	"strings"
	"testing"
)

// goats/search with q: `display_id = q OR EXISTS(goat_identifiers ...)` cannot use either index,
// so every search walked the whole live herd in display order (stg p95 ~1 s). The clause must be
// an id set built from the unique (tenant_id, normalized_value) and (tenant_id, display_id) keys.
func TestSearchQueryClauseIsAnIndexedIDSet(t *testing.T) {
	clause := searchQueryClause(3, " AND gi.identifier_type = $4", "")
	if strings.Contains(clause, "OR EXISTS") || strings.Contains(clause, "gi.goat_id = g.goat_id") {
		t.Fatalf("search clause is still a per-goat correlated OR:\n%s", clause)
	}
	for _, want := range []string{"g.goat_id = ANY(ARRAY(", "gi.tenant_id = $1::uuid", "gi.normalized_value = $3", "g2.display_id = $3", "gi.identifier_type = $4"} {
		if !strings.Contains(clause, want) {
			t.Fatalf("search clause missing %q:\n%s", want, clause)
		}
	}
}
