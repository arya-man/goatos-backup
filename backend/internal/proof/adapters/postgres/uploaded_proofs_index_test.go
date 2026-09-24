package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ListUploadedProofs (/app/proofs/uploads) filters metadata->>'client_task_key' inside one scope.
// On proof_artifacts_scope_idx that key is a Filter, so every call read the shed's whole proof
// history (3,639 rows removed for 1 returned on the clone's busiest shed) and grew with it. Pin a
// migration that builds the task-key index CONCURRENTLY in a NO TRANSACTION migration.
func TestUploadedProofsTaskKeyIndexMigration(t *testing.T) {
	files, err := filepath.Glob("../../../../migrations/postgres/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob migrations: %v (%d files)", err, len(files))
	}
	create := regexp.MustCompile(`(?s)CREATE INDEX CONCURRENTLY IF NOT EXISTS proof_artifacts_scope_task_key_idx\s+ON public\.proof_artifacts \(\s*tenant_id,\s*scope_type,\s*scope_id,\s*\(metadata->>'client_task_key'\),\s*created_at DESC,\s*proof_id DESC\s*\)\s*WHERE upload_state = 'completed'`)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !create.Match(src) {
			continue
		}
		if !strings.Contains(string(src), "-- +goose NO TRANSACTION") {
			t.Fatalf("%s builds the index CONCURRENTLY but is not a NO TRANSACTION migration", f)
		}
		return
	}
	t.Fatal("no migration creates proof_artifacts_scope_task_key_idx (tenant, scope, client_task_key, created_at DESC, proof_id DESC) WHERE completed")
}
