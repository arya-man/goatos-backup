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
	for _, want := range []string{"g.goat_id = ANY(ARRAY(", "gi.tenant_id = $1::uuid", "gi.normalized_value = upper(btrim($3::text))", "g2.display_id = $3", "gi.identifier_type = $4"} {
		if !strings.Contains(clause, want) {
			t.Fatalf("search clause missing %q:\n%s", want, clause)
		}
	}
}

// Tags are STORED normalized (trimmed, upper-cased: identity/app.normalizeIdentifier), so the
// search must normalize what was typed the same way. Comparing the raw text meant a temporary tag
// typed as "temp-cbe-7" -- or with a stray space -- never found "TEMP-CBE-7" on the Herd Register
// (Sales E2E, 2026-09-26). An all-digit RFID was unaffected, which is why it went unnoticed.
func TestSearchNormalizesTheTypedTagLikeTheStoredOne(t *testing.T) {
	clause := searchQueryClause(3, "", "")
	if strings.Contains(clause, "gi.normalized_value = $3\n") || !strings.Contains(clause, "gi.normalized_value = upper(btrim($3::text))") {
		t.Fatalf("the typed tag is not normalized before matching normalized_value:\n%s", clause)
	}
}
