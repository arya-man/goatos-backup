package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Proofs of the frozen-issue persistence against the REAL migration 000013 schema. The service
// state machine is proved without a database in ../../app; what only a database can prove here is
// the SQL boundary: the blocked-vs-zero CHECK, the grain-discriminating natural key, the UNNEST
// batch insert, the amend upsert/delete, and the feed_schedule_config clock read.

const (
	fdiTenant = "fd100000-0000-4000-8000-000000000001"
	fdiPark   = "fd100000-0000-4000-8000-000000003001"
	fdiShedA  = "fd100000-0000-4000-8000-000000004001"
	fdiShedB  = "fd100000-0000-4000-8000-000000004002"
)

func setupIssueDB(t *testing.T, ctx context.Context) (*Repository, *pgxpool.Pool) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Mesha Test', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, fdiTenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CBE', 'CBE', 'active')
ON CONFLICT (location_id) DO NOTHING`, fdiTenant, fdiPark)
	// A dispatch clock so the ScheduleReader has something to read.
	// valid_from is PINNED, not defaulted. The column is `NOT NULL DEFAULT CURRENT_DATE` and the
	// reader filters `valid_from <= <as-of>`, so leaving it to the default made this fixture invisible
	// to every test reading a pinned PAST date -- TestScheduleReaderReadsTheDispatchClock asks for
	// 2026-07-29 and got zero clocks on any run day after it. A time-dependent test that passes only
	// while the wall clock happens to sit before its own fixture date is the defect AGENTS.md's
	// pinned-clock rule exists to prevent: derive time-sensitive fixture fields from the same pinned
	// anchor, never from SQL's idea of today.
	exec(`INSERT INTO feed_schedule_config (tenant_id, park_id, workflow, direction_time, correction_time, transport_time, valid_from)
VALUES ($1::uuid, $2::uuid, 'normal', '07:00', '14:00', '15:45', DATE '2026-01-01'),
       ($1::uuid, $2::uuid, 'experiment', '14:00', '14:00', '15:45', DATE '2026-01-01')`, fdiTenant, fdiPark)
	return NewRepository(pool, 10*time.Second), pool
}

func kg(v string) *string { return &v }

// A small frozen sheet: shed A has two grains (Beetal + Sojat share a ration group) in session 1,
// each with a resolved cell, an AUTHORED ZERO, and a BLOCKED cell -- exactly the states that must
// survive the round trip distinctly.
func sampleCells() []domain.StoredCell {
	mk := func(shed, breed string, rowSeq int32, concentrate string) []domain.StoredCell {
		code := domain.BlockReasonNoRationRate
		detail := "no rate for Hay"
		return []domain.StoredCell{
			{ParkID: fdiPark, ParkLabel: "CBE", ShedID: shed, ShedLabel: shed, ShedTag: "Non-Pregnant", Breed: breed, RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning", HeadCount: 10, Workflow: domain.WorkflowNormal, FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", QuantityKg: kg(concentrate), GramsPerHead: kg("100.000"), ShedFactor: kg("1.0000"), SessionTotalKg: concentrate, RowSeq: rowSeq, ItemSeq: 0},
			{ParkID: fdiPark, ParkLabel: "CBE", ShedID: shed, ShedLabel: shed, ShedTag: "Non-Pregnant", Breed: breed, RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning", HeadCount: 10, Workflow: domain.WorkflowNormal, FeedItemLabel: "Milk", FeedItemKey: "milk", QuantityKg: kg("0.000"), GramsPerHead: kg("0.000"), ShedFactor: kg("1.0000"), SessionTotalKg: concentrate, RowSeq: rowSeq, ItemSeq: 1},
			{ParkID: fdiPark, ParkLabel: "CBE", ShedID: shed, ShedLabel: shed, ShedTag: "Non-Pregnant", Breed: breed, RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning", HeadCount: 10, Workflow: domain.WorkflowNormal, FeedItemLabel: "Hay", FeedItemKey: "hay", QuantityKg: nil, BlockedReasonCode: &code, BlockedReasonDetail: &detail, SessionTotalKg: concentrate, RowSeq: rowSeq, ItemSeq: 2},
		}
	}
	out := mk(fdiShedA, "Beetal", 0, "1.000")
	out = append(out, mk(fdiShedB, "Sojat", 1, "2.000")...)
	return out
}

func issueCmd(cells []domain.StoredCell, fingerprint string, issuedAt time.Time) ports.PersistIssueCommand {
	return ports.PersistIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal,
		IssuedAt: issuedAt, Fingerprint: fingerprint,
		IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":2026-07-30:normal",
		GeneratedBy:    "test", Cells: cells,
	}
}

func TestPersistIssueFreezesScopeAndReplays(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())

	res, err := repo.PersistIssue(ctx, issueCmd(sampleCells(), "fp-1", issuedAt))
	if err != nil {
		t.Fatalf("PersistIssue: %v", err)
	}
	if res.Outcome != ports.IssueOutcomeInserted {
		t.Fatalf("outcome = %q, want inserted", res.Outcome)
	}

	rowsByIssue, err := repo.LoadIssueRows(ctx, fdiTenant, []string{res.Header.IssueID})
	if err != nil {
		t.Fatalf("LoadIssueRows: %v", err)
	}
	cells := rowsByIssue[res.Header.IssueID]
	if len(cells) != 6 {
		t.Fatalf("stored cells = %d, want 6 (the whole scope)", len(cells))
	}

	// BLOCKED-vs-ZERO survives distinctly.
	var zero, blocked *domain.StoredCell
	for i := range cells {
		switch cells[i].FeedItemLabel {
		case "Milk":
			zero = &cells[i]
		case "Hay":
			blocked = &cells[i]
		}
	}
	if zero == nil || zero.QuantityKg == nil || *zero.QuantityKg != "0.000" {
		t.Fatalf("authored zero must round-trip as 0.000, got %+v", zero)
	}
	if blocked == nil || blocked.QuantityKg != nil || blocked.BlockedReasonCode == nil {
		t.Fatalf("blocked cell must round-trip as NULL quantity + reason, got %+v", blocked)
	}

	// Exact re-issue (same fingerprint) is a no-op replay.
	replay, err := repo.PersistIssue(ctx, issueCmd(sampleCells(), "fp-1", issuedAt))
	if err != nil {
		t.Fatalf("re-issue: %v", err)
	}
	if replay.Outcome != ports.IssueOutcomeReplayed {
		t.Fatalf("re-issue outcome = %q, want replayed", replay.Outcome)
	}

	// A changed-fingerprint re-issue while still 'issued' replaces the rows in place.
	changed := sampleCells()
	changed[0].QuantityKg = kg("5.000")
	reissue, err := repo.PersistIssue(ctx, issueCmd(changed, "fp-2", issuedAt))
	if err != nil {
		t.Fatalf("re-issue changed: %v", err)
	}
	if reissue.Outcome != ports.IssueOutcomeReissued {
		t.Fatalf("re-issue outcome = %q, want reissued", reissue.Outcome)
	}
}

func TestAmendMarksAffectedShedAndLockRefusesFurtherAmend(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	res, err := repo.PersistIssue(ctx, issueCmd(sampleCells(), "fp-1", issuedAt))
	if err != nil {
		t.Fatalf("PersistIssue: %v", err)
	}

	// Change only shed B's concentrate.
	changed := sampleCells()
	for i := range changed {
		if changed[i].ShedID == fdiShedB && changed[i].FeedItemLabel == "Concentrate" {
			changed[i].QuantityKg = kg("3.000")
			changed[i].SessionTotalKg = "3.000"
		}
	}
	amendAt := time.Date(2026, 7, 29, 14, 0, 0, 0, biztime.DefaultLocation())
	amend, err := repo.AmendIssue(ctx, ports.AmendIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal,
		AmendedAt: amendAt, Fingerprint: "fp-amended", Cells: changed,
	})
	if err != nil {
		t.Fatalf("AmendIssue: %v", err)
	}
	if amend.Outcome != ports.AmendOutcomeAmended {
		t.Fatalf("outcome = %q, want amended", amend.Outcome)
	}
	if len(amend.AffectedShedIDs) != 1 || amend.AffectedShedIDs[0] != fdiShedB {
		t.Fatalf("affected sheds = %v, want only shed B", amend.AffectedShedIDs)
	}

	// Only shed B's cells are marked amended in the DB.
	var amendedA, amendedB int
	if err := pool.QueryRow(ctx, `SELECT
  count(*) FILTER (WHERE shed_id = $2::uuid AND amended),
  count(*) FILTER (WHERE shed_id = $3::uuid AND amended)
FROM feed_direction_issue_rows WHERE feed_direction_issue_id = $1::uuid`,
		res.Header.IssueID, fdiShedA, fdiShedB).Scan(&amendedA, &amendedB); err != nil {
		t.Fatalf("count amended: %v", err)
	}
	if amendedA != 0 || amendedB == 0 {
		t.Fatalf("amended flags: shedA=%d shedB=%d, want 0 and >0", amendedA, amendedB)
	}

	// Lock, then prove lock is idempotent and amend is refused.
	lockAt := time.Date(2026, 7, 29, 15, 45, 0, 0, biztime.DefaultLocation())
	lock, err := repo.LockIssue(ctx, ports.LockIssueCommand{TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal, LockedAt: lockAt})
	if err != nil || lock.Outcome != ports.LockOutcomeLocked {
		t.Fatalf("LockIssue = (%v, %v), want locked", lock.Outcome, err)
	}
	lock2, err := repo.LockIssue(ctx, ports.LockIssueCommand{TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal, LockedAt: lockAt})
	if err != nil || lock2.Outcome != ports.LockOutcomeAlreadyDone {
		t.Fatalf("second LockIssue = (%v, %v), want already_locked", lock2.Outcome, err)
	}
	if _, err := repo.AmendIssue(ctx, ports.AmendIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal,
		AmendedAt: lockAt, Fingerprint: "fp-after-lock", Cells: sampleCells(),
	}); !errors.Is(err, ports.ErrAmendAfterLock) {
		t.Fatalf("amend after lock err = %v, want ErrAmendAfterLock", err)
	}
}

func TestAmendMissingIssueIsRefused(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	if _, err := repo.AmendIssue(ctx, ports.AmendIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal,
		AmendedAt: time.Now(), Fingerprint: "x", Cells: sampleCells(),
	}); !errors.Is(err, ports.ErrIssueNotFound) {
		t.Fatalf("amend without issue err = %v, want ErrIssueNotFound", err)
	}
}

// The blocked-shape CHECK must reject a physically impossible cell (a quantity AND a reason, or
// neither) -- the schema, not just Go, keeps blocked-vs-zero un-collapsible.
func TestBlockedShapeCheckRejectsImpossibleCell(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	res, err := repo.PersistIssue(ctx, issueCmd(sampleCells(), "fp-1", time.Now()))
	if err != nil {
		t.Fatalf("PersistIssue: %v", err)
	}
	// A cell with BOTH a quantity and a reason violates the CHECK.
	_, err = pool.Exec(ctx, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label, shed_id, shed_label, session_no, head_count, head_count_informational, workflow, feed_item_label, quantity_kg, blocked_reason_code, session_total_kg, overdue_pending, row_seq, item_seq)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $3::uuid, 'x', 1, 0, false, 'normal', 'Bad', 1.0, 'no_ration_rate', 0.0, false, 9, 9)`,
		fdiTenant, res.Header.IssueID, fdiPark)
	if err == nil {
		t.Fatal("a cell with both a quantity and a blocked reason must violate the blocked-shape CHECK")
	}
}

func TestScheduleReaderReadsTheDispatchClock(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	asOf := time.Date(2026, 7, 29, 0, 0, 0, 0, biztime.DefaultLocation())
	clocks, err := repo.ListScheduleClocks(ctx, fdiTenant, fdiPark, asOf)
	if err != nil {
		t.Fatalf("ListScheduleClocks: %v", err)
	}
	if len(clocks) != 2 {
		t.Fatalf("clocks = %d, want 2 (normal + experiment)", len(clocks))
	}
	byWorkflow := map[string]domain.WorkflowClock{}
	for _, c := range clocks {
		byWorkflow[c.Workflow] = c
	}
	if byWorkflow["normal"].DirectionTime != "07:00:00" {
		t.Fatalf("normal direction_time = %q, want 07:00:00", byWorkflow["normal"].DirectionTime)
	}
	parks, err := repo.ListScheduledParks(ctx, fdiTenant, asOf)
	if err != nil {
		t.Fatalf("ListScheduledParks: %v", err)
	}
	if len(parks) != 1 || parks[0] != fdiPark {
		t.Fatalf("scheduled parks = %v, want [%s]", parks, fdiPark)
	}
}
