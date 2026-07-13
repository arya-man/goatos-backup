package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// seedOpenVaccinationObligation inserts one additional open (scheduled) vaccination obligation for
// testGoatID at the seeded protocol version, so the cancel paths exercise the bulk UNNEST insert
// with more than one row (len(ids) > 1).
func seedOpenVaccinationObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository, idemKey string, seq int) string {
	t.Helper()
	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: mustVersionOf(t, ctx, pool), RuleID: mustRuleOf(t, ctx, pool),
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 9, seq, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: idemKey, Sequence: int32(seq),
	})
	if err != nil || !applied {
		t.Fatalf("insert %s: applied=%v err=%v", idemKey, applied, err)
	}
	return id
}

func canceledEventCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID string) int {
	t.Helper()
	return countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='canceled'`,
		tenantID, obligationID)
}

// TestCancelOpenVaccinationObligationsForGoatVersion covers the bulk cancel path in
// recordCanceledObligationRows via CancelOpenVaccinationObligationsForGoatVersion. It is the
// real-Postgres regression that the sibling TestSM3CancelOpenForGoat did NOT provide: SM3 exercises
// CancelOpenForGoat, a different method that does not reach the bulk status-event insert. The bug
// (a status-event INSERT ... ON CONFLICT (idempotency_key) with no matching unique index) rolls the
// whole transaction back with SQLSTATE 42P10 whenever there is at least one obligation to cancel, so
// this test fails on the pre-fix code and passes once the invalid conflict target is removed.
func TestCancelOpenVaccinationObligationsForGoatVersion(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool) // 'obl-1' scheduled for testGoatID at the vaccination version
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	obB := seedOpenVaccinationObligation(t, ctx, pool, repo, "obl-vax-2", 1)

	n, err := repo.CancelOpenVaccinationObligationsForGoatVersion(ctx, tenantID, testGoatID, versionID, "ineligible_after_recheck", time.Now().UTC())
	if err != nil {
		t.Fatalf("cancel by version: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 cancelled (obl-1 + obl-vax-2), got %d", n)
	}

	for _, id := range []string{obA, obB} {
		if got := scanStatus(t, ctx, pool, id); got != "canceled" {
			t.Fatalf("obligation %s: want canceled, got %s", id, got)
		}
		if got := canceledEventCount(t, ctx, pool, id); got != 1 {
			t.Fatalf("obligation %s: want 1 canceled status-event, got %d", id, got)
		}
	}

	// The cancellation durably enqueues its lifecycle outbox event in the same transaction.
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='goat.obligations_canceled'`,
		tenantID); got < 1 {
		t.Fatalf("expected canceled obligations outbox event, got %d", got)
	}

	// Replay is idempotent: the rows are already canceled, so the UPDATE ... RETURNING selects
	// nothing, the bulk insert is skipped, and no duplicate status-event/outbox row is written.
	n2, err := repo.CancelOpenVaccinationObligationsForGoatVersion(ctx, tenantID, testGoatID, versionID, "ineligible_after_recheck", time.Now().UTC())
	if err != nil {
		t.Fatalf("replay cancel by version: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("replay should cancel 0, got %d", n2)
	}
	for _, id := range []string{obA, obB} {
		if got := canceledEventCount(t, ctx, pool, id); got != 1 {
			t.Fatalf("obligation %s: replay must not duplicate events, got %d", id, got)
		}
	}
}

// TestCancelOpenVaccinationObligationsForGoatExceptVersions covers the same bulk path via the
// except-versions method. Passing an empty effective-version set cancels every open vaccination
// obligation for the goat, again exercising the multi-row UNNEST insert and the 42P10 regression.
func TestCancelOpenVaccinationObligationsForGoatExceptVersions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	obB := seedOpenVaccinationObligation(t, ctx, pool, repo, "obl-vax-2", 1)

	// No effective versions => every open vaccination obligation for the goat is non-effective.
	n, err := repo.CancelOpenVaccinationObligationsForGoatExceptVersions(ctx, tenantID, testGoatID, nil, "version_no_longer_effective", time.Now().UTC())
	if err != nil {
		t.Fatalf("cancel except-versions: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 cancelled, got %d", n)
	}
	for _, id := range []string{obA, obB} {
		if got := scanStatus(t, ctx, pool, id); got != "canceled" {
			t.Fatalf("obligation %s: want canceled, got %s", id, got)
		}
		if got := canceledEventCount(t, ctx, pool, id); got != 1 {
			t.Fatalf("obligation %s: want 1 canceled status-event, got %d", id, got)
		}
	}
}
