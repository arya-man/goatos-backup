package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// STG incident (obligation 70bbb693, kid DOB 2026-09-16, et_tt_kid_4w floor DOB+28 = 2026-10-14):
// after a manual repair to 10-14, the row was silently put back to 2026-09-24 and the sweeper booked
// it onto the 09-24 drive. A drive day is a vaccination date, so the sweeper's batch attach must
// prove it: a kid whose floor is later than the drive must stay where it is, however early its
// persisted due_at is, on every sweep.
func TestVaccinationSweeperBatchAttachHonoursAgeFloor(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	goatID, ruleID := seedVaccinationAgeFloor(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	early := time.Date(2026, time.September, 24, 0, 0, 0, 0, biztime.DefaultLocation())
	floor := time.Date(2026, time.October, 14, 0, 0, 0, 0, biztime.DefaultLocation())

	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: floor, Status: "scheduled", IdempotencyKey: "kid-ettt-floor",
		RuleIdentityKey: "et+tt|et_tt_kid_4w|1", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert floor row id=%q applied=%v err=%v", id, applied, err)
	}
	// The row as STG holds it: due_at rewritten to the early drive day by a pre-guard writer.
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET due_at=$2, window_start=$2, status='due' WHERE obligation_id=$1::uuid`, id, early); err != nil {
		t.Fatalf("seed early legacy row: %v", err)
	}
	var eventsBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM obligation_status_events WHERE obligation_id=$1::uuid`, id).Scan(&eventsBefore); err != nil {
		t.Fatalf("count events: %v", err)
	}

	sweep := func(planned time.Time) (string, int64) {
		t.Helper()
		batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
			TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
			PlannedDate: &planned, Status: "planned", EstimatedTargets: 1,
		}, []string{id})
		if err != nil {
			t.Fatalf("sweeper batch attach on %s: %v", planned.Format("2006-01-02"), err)
		}
		return batchID, attached
	}
	assertUntouched := func(pass int) {
		t.Helper()
		var (
			batch   *string
			due     time.Time
			events  int
			batches int
		)
		if err := pool.QueryRow(ctx, `SELECT batch_id::text, due_at FROM obligation_instances WHERE obligation_id=$1::uuid`, id).Scan(&batch, &due); err != nil {
			t.Fatalf("read row: %v", err)
		}
		if batch != nil {
			t.Fatalf("pass %d: kid obligation pulled into drive batch %s before its DOB+28 floor", pass, *batch)
		}
		if !due.Equal(early) {
			t.Fatalf("pass %d: refused attach moved due_at to %s; the row must stay where it is", pass, due)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM obligation_status_events WHERE obligation_id=$1::uuid`, id).Scan(&events); err != nil {
			t.Fatalf("count events: %v", err)
		}
		if events != eventsBefore {
			t.Fatalf("pass %d: refused attach wrote %d status events", pass, events-eventsBefore)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND planned_date=$2::date`, tenantID, early.Format("2006-01-02")).Scan(&batches); err != nil {
			t.Fatalf("count batches: %v", err)
		}
		if batches != 0 {
			t.Fatalf("pass %d: %d empty drive batches left on the early date", pass, batches)
		}
	}
	for pass := 1; pass <= 3; pass++ { // repeated sweeps stay idempotent
		if batchID, attached := sweep(early); batchID != "" || attached != 0 {
			t.Fatalf("pass %d: sweeper attached %d to batch %q on %s, before the DOB+28 floor", pass, attached, batchID, early.Format("2006-01-02"))
		}
		assertUntouched(pass)
	}

	// A legitimate drive on/after the floor still books the dose.
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET due_at=$2, window_start=$2 WHERE obligation_id=$1::uuid`, id, floor); err != nil {
		t.Fatalf("restore floor row: %v", err)
	}
	if batchID, attached := sweep(floor); batchID == "" || attached != 1 {
		t.Fatalf("floor-day drive batch=%q attached=%d, want the kid booked on %s", batchID, attached, floor.Format("2006-01-02"))
	}
}

// A reconcile that moves a row back to a date it held before must still leave its ledger row; the
// event key used to name only the destination, so the repeat move was a silent due_at rewrite.
func TestRuleIdentityReconcileRepeatMoveIsNeverSilent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	goatID, ruleID := seedVaccinationAgeFloor(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	floor := time.Date(2026, time.October, 14, 0, 0, 0, 0, biztime.DefaultLocation())
	later := floor.AddDate(0, 0, 3)
	row := func(due time.Time) domain.NewObligation {
		return domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: "scheduled", IdempotencyKey: "kid-ettt-reconcile-" + due.Format("20060102"),
			RuleIdentityKey: "et+tt|et_tt_kid_4w|1", Sequence: 1,
		}
	}
	id, applied, err := repo.InsertObligation(ctx, row(floor))
	if err != nil || !applied {
		t.Fatalf("insert id=%q applied=%v err=%v", id, applied, err)
	}
	reconcile := func(pass int) {
		t.Helper()
		if _, found, err := repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, row(later), later); err != nil || !found {
			t.Fatalf("reconcile pass %d found=%v err=%v", pass, found, err)
		}
		var got time.Time
		if err := pool.QueryRow(ctx, `SELECT due_at FROM obligation_instances WHERE obligation_id=$1::uuid`, id).Scan(&got); err != nil {
			t.Fatalf("read due: %v", err)
		}
		if !got.Equal(later) {
			t.Fatalf("reconcile pass %d due=%s, want %s", pass, got, later)
		}
	}
	reconcile(1)
	// The STG shape: a manual repair puts the row back (no event from the repair itself) ...
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET due_at=$2, window_start=$2 WHERE obligation_id=$1::uuid`, id, floor); err != nil {
		t.Fatalf("manual repair: %v", err)
	}
	// ... and the next reconcile moves it to the date it already visited once.
	reconcile(2)
	if got := countReconcileEvents(t, ctx, pool, id); got != 2 {
		t.Fatalf("reconcile events=%d, want 2 (every due_at move is ledgered)", got)
	}
}

func countReconcileEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM obligation_status_events
WHERE obligation_id=$1::uuid AND payload->>'reason' = 'rule_identity_reconciled'`, id).Scan(&n); err != nil {
		t.Fatalf("count reconcile events: %v", err)
	}
	return n
}
