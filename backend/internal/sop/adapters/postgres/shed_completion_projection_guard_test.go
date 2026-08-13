package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestShedCompletionScheduledDateMultipleDimensionsPageBoundaryScopeHierarchyStatusMatrixQueryGuards(t *testing.T) {
	sourceBytes, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}
	source := string(sourceBytes)

	requireSQLShape(t, source, "future scan write gate uses assignment execution date", []string{
		"WHERE NOT EXISTS (",
		"FROM obligation_instances oi",
		"FROM vaccination_drive_assignments assignment",
		"assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'",
		"COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) > now()",
	})
	requireSQLShape(t, source, "assignment match uses exact shed grain", []string{
		"assignment.batch_id = oi.batch_id",
		"assignment.shed_id = g.shed_id",
		"cardinality(assignment.vaccine_rule_ids) = 0",
		"assignment.vaccine_rule_ids @> ARRAY[oi.rule_id]",
	})
	requireSQLShape(t, source, "shed readiness counts the full eligible result instead of a UI page", []string{
		"expected AS (",
		"SELECT count(*) AS n",
		"handled AS (",
		"SELECT count(DISTINCT c.goat_id) AS n",
		"LIMIT 2000",
	})
	requireSQLShape(t, source, "shed readiness resolves explicit shed scope without park bleed", []string{
		"target_shed AS (",
		"WHEN nullif($4, '')::uuid IS NOT NULL THEN nullif($4, '')::uuid",
		"WHEN ts.scope_type = 'shed' THEN ts.scope_id",
		"AND (target.shed_id IS NULL OR g.shed_id = target.shed_id)",
	})
	requireSQLShape(t, source, "shed readiness excludes only terminal obligation statuses", []string{
		"oi.status NOT IN ('completed', 'waived', 'canceled', 'superseded')",
		"oi.status = 'scheduled'",
	})
}

func requireSQLShape(t *testing.T, source, label string, needles []string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(source, needle) {
			t.Fatalf("%s: repository.go missing SQL shape %q", label, needle)
		}
	}
}
