package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestCastroETTTHistoryProjectionOneToManyPageBoundaryStatusMatrix(t *testing.T) {
	body, err := os.ReadFile("000345_castro1_ettt_history_z1z3_identity_repair.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)

	required := []string{
		"projection-review: membership=CBE Castro ET+TT source facts",
		"group_key=(tenant_id, animal_key, source_dose_code)",
		"join_cardinality=goat_identifiers is unique on tenant plus normalized_value and procurement_load_goats is a fallback",
		"pagination=none, this is a bounded one-off migration repair over the full source-fact set",
		"scope=tenant_id plus TEMP-CBE-CASTRO1/2/3 lineage and ET+TT vaccine header",
		"count(DISTINCT goat_id) INTO linked_goats",
		"IF source_animals > 0 AND linked_goats <> 204 THEN",
		"IF source_animals > 0 AND history_rows <> 408 THEN",
		"IF source_animals > 0 AND active_rule_bound_rows <> 408 THEN",
		"bad_z1z3_rows <> 0",
		"cbe-castro-ettt-history:",
		"castro1-ettt-history:%",
	}
	for _, want := range required {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
