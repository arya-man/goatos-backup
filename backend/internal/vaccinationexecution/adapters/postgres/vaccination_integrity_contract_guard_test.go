package postgres

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This guard keeps the production failures from 2026-09-22/23 coupled in ordinary backend CI.
// They looked unrelated in the UI but shared one dangerous property: focused feature tests stayed
// green while deployment/runtime contracts had drifted.
func TestVaccinationExecutionDeploymentContracts(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(here), "../../../../.."))

	assertFileContains(t, repo, "infra/envs/stg/cloud_run_worker.tf",
		"min_instance_count = 1", "max_instance_count = 2")
	assertFileContains(t, repo, "backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql",
		"vaccination_capacity_config_max_per_day_check CHECK (((max_per_day >= 1) AND (max_per_day <= 200)))")
	assertFileContains(t, repo, "backend/migrations/postgres/000393_vaccination_operator_status_obligation_truth.sql",
		"vaccination_drive_assignment_members", "m.canceled_at IS NULL", "vaccination_completions")

	if !strings.Contains(executionClassifiedCTE, "COALESCE(assignment.assignment_id, oi.batch_id) AS execution_card_id") {
		t.Fatal("execution cards are no longer keyed by assignment identity with legacy batch fallback")
	}
	if !strings.Contains(executionClassifiedCTE, "GROUP BY located.park_uuid, located.shed_uuid, located.partition_key, located.execution_card_id, located.animal_id") {
		t.Fatal("animal rollup no longer deduplicates vaccines at assignment + animal grain")
	}
}

func assertFileContains(t *testing.T, repo, rel string, fragments ...string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	s := string(b)
	for _, fragment := range fragments {
		if !strings.Contains(s, fragment) {
			t.Fatalf("%s missing vaccination integrity contract %q", rel, fragment)
		}
	}
}
