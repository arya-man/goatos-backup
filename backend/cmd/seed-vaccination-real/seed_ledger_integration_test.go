package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const (
	testMeshaParty = "00000000-0000-4000-8000-000000001001"
	testCbePark    = "00000000-0000-4000-8000-000000003001"
	testLedgerGoat = "10000000-0000-4000-8000-0000000000cc"
)

// TestVerifyPersistedSourceFactsRollsBackOnDroppedRow is the VACC-REV-02 injected-failure proof:
// when the lineage ledger expects a committed obligation/completion that was NOT persisted (an
// ON CONFLICT DO NOTHING silent drop), the in-transaction verify fails and the WHOLE seed
// transaction rolls back — nothing it wrote survives.
func TestVerifyPersistedSourceFactsRollsBackOnDroppedRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	// A sentinel row written inside the same transaction proves the rollback actually discards work.
	sentinelRun := "aaaaaaaa-0000-4000-8000-0000000000f1"
	if _, err := tx.Exec(ctx,
		`INSERT INTO seed_runs (seed_run_id, tenant_id, command, state) VALUES ($1,$2,'seed-vaccination-real','loading')`,
		sentinelRun, defaultTenantID); err != nil {
		t.Fatalf("insert sentinel seed_run: %v", err)
	}

	// The ledger claims a past fact was imported as a completion, but we deliberately never insert
	// that obligation/completion row — simulating a silent collision drop.
	facts := []sourceFact{
		mkFact("goat-dropped", "ET+TT", "first", 1, "2026-06-21", dispositionImportedCompletion),
	}
	facts[0].obligationIdem = "vacc-real-obl:history:goat-dropped:et_tt:first:2026-06-21"
	facts[0].completionIdem = "vacc-real-cmp:goat-dropped:et_tt:first:2026-06-21"
	if err := insertSourceFactLedger(ctx, tx, defaultTenantID, sentinelRun, facts); err != nil {
		t.Fatalf("insert ledger: %v", err)
	}

	verr := verifyPersistedSourceFactsInTx(ctx, tx, defaultTenantID, facts)
	if verr == nil || !strings.Contains(verr.Error(), "source-fact drop detected") {
		t.Fatalf("expected in-transaction drop detection, got %v", verr)
	}

	// The seed's deferred rollback fires on any error return: emulate it.
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	// Proof the whole transaction rolled back: neither the sentinel nor the ledger survived.
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM seed_runs WHERE seed_run_id=$1`, sentinelRun); got != 0 {
		t.Fatalf("sentinel seed_run survived rollback: %d rows", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_source_facts WHERE tenant_id=$1`, defaultTenantID); got != 0 {
		t.Fatalf("source-fact ledger survived rollback: %d rows", got)
	}
}

// TestVerifyPersistedSourceFactsPassesWhenRowsCommitted proves the positive path: when every
// non-excluded ledger fact's obligation/completion row IS persisted in the transaction, the in-txn
// verify passes and the transaction commits.
func TestVerifyPersistedSourceFactsPassesWhenRowsCommitted(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := seedProtocolFixture(t, ctx, pool)

	// Insert one real obligation + completion carrying the exact idempotency keys the ledger expects.
	oblIdem := "vacc-real-obl:history:goat-ok:et_tt:first:2026-06-21"
	cmpIdem := "vacc-real-cmp:goat-ok:et_tt:first:2026-06-21"

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	oblID := "20000000-0000-4000-8000-0000000000d1"
	completedAt := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)
	if _, err := tx.Exec(ctx, `
		INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
			target_type, target_id, scope_type, scope_id, due_at, status, completed_at, sequence,
			idempotency_key, created_at, updated_at)
		VALUES ($1,$2,$3,$4,'goat',$5,'park',$6,$7,'completed',$7,1,$8,now(),now())`,
		oblID, defaultTenantID, versionID, ruleID, testLedgerGoat, testCbePark, completedAt, oblIdem); err != nil {
		t.Fatalf("insert obligation: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id,
			doses, dose_ml_given, administered_at, verified_at, status, idempotency_key, created_at, updated_at)
		VALUES ($1,$2,$3,$4,1,1,$5,$5,'accepted',$6,now(),now())`,
		"30000000-0000-4000-8000-0000000000d1", defaultTenantID, oblID, testLedgerGoat, completedAt, cmpIdem); err != nil {
		t.Fatalf("insert completion: %v", err)
	}

	facts := []sourceFact{mkFact("goat-ok", "ET+TT", "first", 1, "2026-06-21", dispositionImportedCompletion)}
	facts[0].obligationIdem = oblIdem
	facts[0].completionIdem = cmpIdem
	if err := insertSourceFactLedger(ctx, tx, defaultTenantID, "", facts); err != nil {
		t.Fatalf("insert ledger: %v", err)
	}

	if err := verifyPersistedSourceFactsInTx(ctx, tx, defaultTenantID, facts); err != nil {
		t.Fatalf("verify should pass when rows are present: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	committed = true

	if got := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_source_facts WHERE tenant_id=$1 AND disposition='imported_completion'`, defaultTenantID); got != 1 {
		t.Fatalf("expected 1 committed ledger fact, got %d", got)
	}
}

// seedProtocolFixture creates the goat + published-enough protocol version + rule needed as FK
// targets for a real obligation/completion, reusing the migration-seeded baseline tenant/party/park.
func seedProtocolFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (versionID, ruleID string) {
	t.Helper()

	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
			 VALUES ($1,$2,'alive','goat',$3,'female',$4,$4)`,
		testLedgerGoat, defaultTenantID, testMeshaParty, testCbePark); err != nil {
		t.Fatalf("seed goat: %v", err)
	}

	proto := protocolpg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protocoldomain.NewDefinition{
		TenantID: defaultTenantID, Code: "vaccination.ledger_test", Name: "Ledger Test", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("seed definition: %v", err)
	}
	versionID, err = proto.CreateVersion(ctx, protocoldomain.NewVersion{
		TenantID: defaultTenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("seed version: %v", err)
	}
	ruleID, err = proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: defaultTenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	return versionID, ruleID
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
