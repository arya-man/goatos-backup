package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestMigration000201DownToleratesIdentityGoatRows is the VACC-REV-13 guard. The identity_goat
// decision type is added by the VACC-REV-11 split (000199 ADD → 000200 VALIDATE → 000201 SWAP);
// migration 000201's DOWN is the phase that restores the narrower canonical constraint. That down
// must SUCCEED even after the feature has produced real identity_decisions rows with
// decision_type='identity_goat'. The narrower rolled-back constraint EXCLUDES identity_goat, so a
// VALIDATE of it would scan those legitimately-produced rows and fail — a down must never fail on data.
// The down therefore re-adds the narrow constraint NOT VALID (no VALIDATE scan): it tolerates the
// existing rows while still enforcing the rollback on every NEW identity_goat write.
func TestMigration000201DownToleratesIdentityGoatRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, _ := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	// The up-migration (already applied) admits identity_goat. Insert a real decision row of that type,
	// exactly what an IdentityGoat correction records in production.
	insert := `INSERT INTO identity_decisions
	  (tenant_id, decision_type, decision_result, decision_state, decided_by_type, policy_version, evidence)
	  VALUES ($1, 'identity_goat', 'applied', 'approved', 'human', 'v1', '{}'::jsonb)`
	if _, err := pool.Exec(ctx, insert, meshaTenant); err != nil {
		t.Fatalf("seed identity_goat decision row: %v", err)
	}

	// Run the 000201 DOWN section statement-by-statement (it is a NO TRANSACTION migration).
	for _, stmt := range extract000201Down(t) {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("000201 down failed with an existing identity_goat row (VACC-REV-13): %v\nstmt: %s", err, stmt)
		}
	}

	// The rolled-back constraint survives under the canonical name.
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='identity_decisions_decision_type_check')`).Scan(&exists); err != nil {
		t.Fatalf("check constraint existence: %v", err)
	}
	if !exists {
		t.Fatal("identity_decisions_decision_type_check missing after down")
	}

	// The pre-existing identity_goat row is tolerated (NOT VALID skips the historical scan).
	if n := countRows(t, pool, `SELECT count(*) FROM identity_decisions WHERE decision_type='identity_goat'`); n != 1 {
		t.Fatalf("existing identity_goat rows after down = %d, want 1 (tolerated)", n)
	}

	// A NEW identity_goat insert is now rejected — a NOT VALID CHECK still enforces on new writes, so
	// the rollback is real, not cosmetic.
	if _, err := pool.Exec(ctx, insert, meshaTenant); err == nil {
		t.Fatal("new identity_goat insert succeeded after down; rolled-back constraint did not enforce")
	}
}

// extract000201Down returns the executable statements of the 000201 DOWN section, dropping goose
// directives and comment lines and splitting on the statement terminator.
func extract000201Down(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(repoRoot(t), "backend", "migrations", "postgres", "000201_goat_identity_swap_constraint.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read 000201: %v", err)
	}
	var body strings.Builder
	inDown := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "-- +goose Down") {
			inDown = true
			continue
		}
		if !inDown {
			continue
		}
		if strings.HasPrefix(line, "-- +goose") || strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	var stmts []string
	for _, s := range strings.Split(body.String(), ";") {
		if strings.TrimSpace(s) != "" {
			stmts = append(stmts, strings.TrimSpace(s)+";")
		}
	}
	if len(stmts) == 0 {
		t.Fatal("no down statements extracted from 000201")
	}
	return stmts
}
