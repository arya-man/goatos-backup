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
		"LEFT JOIN vaccination_drive_assignment_members m",
		"m.obligation_id = oi.obligation_id",
		"m.goat_id = oi.target_id",
		"LEFT JOIN vaccination_drive_assignments assignment",
		"assignment.assignment_id = m.assignment_id",
		") vda_member ON assignment.assignment_id IS NOT NULL",
		"LEFT JOIN LATERAL",
		"FROM vaccination_drive_assignments guess",
		"WHERE m.assignment_id IS NULL",
		"guess.batch_id = oi.batch_id",
		"guess.shed_id = g.shed_id",
		"oi.rule_id = ANY(guess.vaccine_rule_ids)",
		"COALESCE(vda_member.assignment_planned_at, vda_guess.assignment_planned_at, (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), oi.due_at)::timestamptz AS due_at",
		"ORDER BY COALESCE(vda_member.assignment_planned_at, vda_guess.assignment_planned_at, (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), oi.due_at)::timestamptz ASC",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("ListOpenObligationsByGoat must use live vaccination assignment dates; missing %q in:\n%s", want, query)
		}
	}

	if strings.Contains(query, "FROM vaccination_drive_assignments assignment\n  WHERE assignment.tenant_id = oi.tenant_id") {
		t.Fatalf("ListOpenObligationsByGoat must bind assignments through vaccination_drive_assignment_members before any fuzzy guess")
	}
	if strings.Contains(query, "COALESCE(vda_member.assignment_planned_at, oi.due_at)") {
		t.Fatalf("ListOpenObligationsByGoat must not skip the batch planned_date fallback")
	}
}
