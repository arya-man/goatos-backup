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

func TestCompletedProofReconciliationRequiresProofObligationCycle(t *testing.T) {
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
	for _, want := range []string{
		"obligation_cycles",
		"proof_obligation_id",
		"proof_obligation_row_version",
		"oi.obligation_id = tc.proof_obligation_id",
		"oi.row_version = tc.proof_obligation_row_version",
		"jsonb_array_elements",
		"(cycle ->> 'obligation_row_version')::integer = oi.row_version",
		"sop_task_scan_captures scan",
		"scan.captured_at >= tc.proof_captured_at - INTERVAL '10 minutes'",
		"obligation_status_events event",
		"event.event_type IN ('scheduled', 'deferred', 'rescoped', 'canceled', 'waived', 'missed', 'became_due')",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("completed proof reconciliation must require current proof obligation cycle; missing %q", want)
		}
	}
}

func TestListSubmissionCompletionsFallbackUsesDriveMemberScope(t *testing.T) {
	source, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}
	query := string(source)
	start := strings.Index(query, "func (r *Repository) ListSubmissionCompletions")
	if start < 0 {
		t.Fatalf("ListSubmissionCompletions not found")
	}
	query = query[start:]
	end := strings.Index(query, "func (r *Repository) CompletedProofRefsByTask")
	if end < 0 {
		t.Fatalf("CompletedProofRefsByTask not found")
	}
	query = query[:end]
	for _, want := range []string{
		"member_scope.canceled_at IS NULL",
		"assignment_scope.batch_id = ob_scope.batch_id",
		"assignment_scope.shed_id = NULLIF(substring(ss.idempotency_key FROM ':scope:([0-9a-fA-F-]{36})'), '')::uuid",
		"regexp_replace(lower(btrim(assignment_scope.partition_label)), '^part[[:space:]]+', '')",
		"regexp_replace(lower(btrim(ss.partition_label)), '^part[[:space:]]+', '')",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("submission completion fallback must be scoped to active assignment member; missing %q", want)
		}
	}
}
