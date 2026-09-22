package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestCompletedProofReconciliationIgnoresCanceledDriveMembers(t *testing.T) {
	source, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}
	query := string(source)
	start := strings.Index(query, "func (r *Repository) RecordCompletionsFromCompletedProof")
	if start < 0 {
		t.Fatalf("RecordCompletionsFromCompletedProof not found")
	}
	query = query[start:]
	memberJoin := strings.Index(query, "FROM vaccination_drive_assignment_members member")
	if memberJoin < 0 {
		t.Fatalf("completed proof reconciliation no longer checks drive assignment members")
	}
	query = query[memberJoin:]
	batchMatch := strings.Index(query, "assignment.batch_id = tc.task_batch_id")
	if batchMatch < 0 {
		t.Fatalf("completed proof reconciliation no longer binds assignment batch")
	}
	query = query[:batchMatch]
	if !strings.Contains(query, "member.canceled_at IS NULL") {
		t.Fatalf("completed proof reconciliation must ignore canceled drive assignment members")
	}
}
