package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestListOpenByGoatUsesAssignmentPlannedDateForVaccinationDrives(t *testing.T) {
	raw, err := os.ReadFile("sqlc/query.sql")
	if err != nil {
		t.Fatalf("read query.sql: %v", err)
	}
	sql := string(raw)
	start := strings.Index(sql, "-- name: ListOpenObligationsByGoat :many")
	if start < 0 {
		t.Fatalf("ListOpenObligationsByGoat query not found")
	}
	end := strings.Index(sql[start+1:], "-- name:")
	if end < 0 {
		end = len(sql) - start
	}
	query := sql[start : start+end]

	for _, want := range []string{
		"FROM obligation_instances oi",
		"LEFT JOIN obligation_batches ob",
		"ob.batch_id = oi.batch_id",
		"LEFT JOIN goats g",
		"LEFT JOIN LATERAL",
		"FROM vaccination_drive_assignments assignment",
		"assignment.batch_id = oi.batch_id",
		"assignment.shed_id = g.shed_id",
		"oi.rule_id = ANY(assignment.vaccine_rule_ids)",
		"COALESCE(vda.assignment_planned_at, (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), oi.due_at)::timestamptz AS due_at",
		"ORDER BY COALESCE(vda.assignment_planned_at, (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), oi.due_at)::timestamptz ASC",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("ListOpenObligationsByGoat must use live vaccination assignment dates; missing %q in:\n%s", want, query)
		}
	}

	if strings.Contains(query, "COALESCE(vda.assignment_planned_at, oi.due_at)") {
		t.Fatalf("ListOpenObligationsByGoat must not skip the batch planned_date fallback")
	}
}
