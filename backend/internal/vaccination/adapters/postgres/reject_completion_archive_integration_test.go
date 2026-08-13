package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
)

// A rejected vaccination completion MOVES out of vaccination_completions into the rejection
// archive (migration 000093) instead of staying behind with status='rejected'.
//
// Why this matters on the ground: proof is captured one video per animal, so a verifier rejects
// ONE animal while the rest of the shed stands. That animal has to become outstanding work again
// and reappear on the operator's scan screen. While a rejected row remained in the completions
// table, every read that asks "is there a completion for this obligation" still answered yes, so
// the rejected animal stayed green on the scan screen and was never redone.
//
// Nothing is destroyed: "we injected this animal and the proof was refused" is a different fact
// from "this never happened", and the archive is where the first one lives.
func TestRejectCompletionMovesRowToArchive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID     = "00000000-0000-4000-8000-000000000001"
		obligationID = "40000000-0000-4000-8000-0000000000a1"
		goatID       = "40000000-0000-4000-8000-0000000000b1"
		completionID = "40000000-0000-4000-8000-0000000000c1"
		verifierID   = "40000000-0000-4000-8000-0000000000d1"
	)
	seedRejectArchiveCompletion(t, ctx, pool, tenantID, completionID, obligationID, goatID)

	repo := NewRepository(pool, 5*time.Second)
	verifier := verifierID
	applied, err := repo.RejectCompletion(ctx, tenantID, completionID, "Needle not visible in clip", &verifier)
	if err != nil {
		t.Fatalf("reject completion: %v", err)
	}
	if !applied {
		t.Fatalf("applied = false, want true on the first rejection")
	}

	// GONE from the live table: this is what makes the animal outstanding again on every read.
	if n := countRejectArchiveRows(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND completion_id=$2`,
		tenantID, completionID); n != 0 {
		t.Fatalf("completion rows left behind = %d, want 0 (a rejected animal must read as outstanding)", n)
	}

	// PRESERVED in the archive, with the verdict that caused it.
	var (
		gotGoat, gotObligation, gotOriginalStatus, gotReason string
		gotRejectedBy                                        string
	)
	if err := pool.QueryRow(ctx, `
SELECT goat_id::text, obligation_id::text, original_status, COALESCE(rejection_reason,''), COALESCE(rejected_by::text,'')
FROM vaccination_completion_rejections
WHERE tenant_id=$1 AND completion_id=$2`, tenantID, completionID).
		Scan(&gotGoat, &gotObligation, &gotOriginalStatus, &gotReason, &gotRejectedBy); err != nil {
		t.Fatalf("archive row: %v", err)
	}
	if gotGoat != goatID || gotObligation != obligationID {
		t.Fatalf("archive row = goat %s obligation %s, want %s / %s", gotGoat, gotObligation, goatID, obligationID)
	}
	if gotOriginalStatus != "recorded" {
		t.Fatalf("original_status = %q, want the status the row carried when it was moved", gotOriginalStatus)
	}
	if gotReason != "Needle not visible in clip" {
		t.Fatalf("rejection_reason = %q, want the verifier's own words preserved", gotReason)
	}
	if gotRejectedBy != verifierID {
		t.Fatalf("rejected_by = %q, want the verifier who made the call", gotRejectedBy)
	}
}

// Replay safety: a redelivered verdict must not archive the same completion twice, and must not
// report itself as newly applied.
func TestRejectCompletionIsIdempotentOnReplay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID     = "00000000-0000-4000-8000-000000000001"
		obligationID = "40000000-0000-4000-8000-0000000000a2"
		goatID       = "40000000-0000-4000-8000-0000000000b2"
		completionID = "40000000-0000-4000-8000-0000000000c2"
	)
	seedRejectArchiveCompletion(t, ctx, pool, tenantID, completionID, obligationID, goatID)

	repo := NewRepository(pool, 5*time.Second)
	if _, err := repo.RejectCompletion(ctx, tenantID, completionID, "blurred", nil); err != nil {
		t.Fatalf("first reject: %v", err)
	}
	applied, err := repo.RejectCompletion(ctx, tenantID, completionID, "blurred", nil)
	if err != nil {
		t.Fatalf("replayed reject: %v", err)
	}
	if applied {
		t.Fatalf("applied = true on replay, want false -- a redelivered verdict must be a no-op")
	}
	if n := countRejectArchiveRows(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completion_rejections WHERE tenant_id=$1 AND completion_id=$2`,
		tenantID, completionID); n != 1 {
		t.Fatalf("archive rows = %d, want exactly 1 after a replay", n)
	}
}

// An ACCEPTED animal is terminal (maintainer ruling): it is never re-judged and must never be
// pulled back out of the record by a late or replayed rejection.
func TestRejectCompletionRefusesAnAcceptedAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID     = "00000000-0000-4000-8000-000000000001"
		obligationID = "40000000-0000-4000-8000-0000000000a3"
		goatID       = "40000000-0000-4000-8000-0000000000b3"
		completionID = "40000000-0000-4000-8000-0000000000c3"
	)
	seedRejectArchiveCompletion(t, ctx, pool, tenantID, completionID, obligationID, goatID)
	if _, err := pool.Exec(ctx,
		`UPDATE vaccination_completions SET status='accepted' WHERE tenant_id=$1 AND completion_id=$2`,
		tenantID, completionID); err != nil {
		t.Fatalf("accept completion: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	applied, err := repo.RejectCompletion(ctx, tenantID, completionID, "too late", nil)
	if err != nil {
		t.Fatalf("reject accepted: %v", err)
	}
	if applied {
		t.Fatalf("applied = true, want false -- an accepted animal is terminal")
	}
	if n := countRejectArchiveRows(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND completion_id=$2 AND status='accepted'`,
		tenantID, completionID); n != 1 {
		t.Fatalf("accepted completion no longer intact -- acceptance must never be reverted")
	}
	if n := countRejectArchiveRows(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completion_rejections WHERE tenant_id=$1 AND completion_id=$2`,
		tenantID, completionID); n != 0 {
		t.Fatalf("archive rows = %d, want 0 -- an accepted animal must not be archived", n)
	}
}

func seedRejectArchiveCompletion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, completionID, obligationID, goatID string) {
	t.Helper()
	proto := protopg.NewRepository(pool, 5*time.Second)
	p := seedBUG017ETTTProtocol(t, ctx, proto, "vaccination.rejectarchive.c"+completionID[len(completionID)-4:])
	seedGenAdultProcuredGoat(t, ctx, pool, goatID, "alive", time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))

	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances
  (obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id, scope_type, scope_id,
   due_at, status, sequence, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', $5::uuid, 'tenant', $2::uuid,
        now(), 'due', 1, $6)`,
		obligationID, tenantID, p.versionID, p.w1RuleID, goatID, "reject-archive-obl-"+completionID); err != nil {
		t.Fatalf("seed obligation: %v", err)
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (
  completion_id, tenant_id, obligation_id, goat_id, adverse_reaction, cold_chain_verified,
  administered_at, status, idempotency_key, row_version, created_at, updated_at
) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, false, true, now(), 'recorded', $5, 1, now(), now())`,
		completionID, tenantID, obligationID, goatID, "reject-archive-"+completionID); err != nil {
		t.Fatalf("seed completion: %v", err)
	}
}

func countRejectArchiveRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
