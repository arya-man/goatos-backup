package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	countsTenant = "00000000-0000-4000-8000-000000000001"
	countsPark   = "00000000-0000-4000-8000-000000003001"
	countsShedA  = "00000000-0000-4000-8000-000000004001"
	countsShedB  = "00000000-0000-4000-8000-000000004002"
)

func TestRepositoryRecordsBaseCountAnchorIdempotently(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)

	in := baseAnchor("anchor-key-1", "fp-1")
	id, replay, err := repo.RecordBaseCountAnchor(ctx, in)
	if err != nil || replay || id == "" {
		t.Fatalf("first anchor id=%q replay=%v err=%v", id, replay, err)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND aggregate_id=$2::uuid
  AND event_type='counts.base_count_anchor.recorded'`, countsTenant, id); got != 1 {
		t.Fatalf("base anchor outbox rows=%d, want 1", got)
	}
	again, replay, err := repo.RecordBaseCountAnchor(ctx, in)
	if err != nil || !replay || again != id {
		t.Fatalf("replay anchor id=%q replay=%v err=%v, want id=%q replay=true", again, replay, err, id)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND aggregate_id=$2::uuid
  AND event_type='counts.base_count_anchor.recorded'`, countsTenant, id); got != 1 {
		t.Fatalf("base anchor replay outbox rows=%d, want 1", got)
	}
	in.RequestFingerprint = "different-fp"
	if _, _, err := repo.RecordBaseCountAnchor(ctx, in); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("different fingerprint err=%v, want ErrIdempotencyConflict", err)
	}
}

func TestRepositoryRecordsUnreportedShiftingExceptionOnBaseCountMismatch(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	previous := baseAnchorForAt(countsShedA, "mismatch-previous-anchor", "mismatch-previous-fp", 20, time.Date(2026, 6, 29, 6, 0, 0, 0, time.UTC))
	if _, replay, err := repo.RecordBaseCountAnchor(ctx, previous); err != nil || replay {
		t.Fatalf("previous anchor replay=%v err=%v", replay, err)
	}
	current := baseAnchorForAt(countsShedA, "mismatch-current-anchor", "mismatch-current-fp", 17, time.Date(2026, 6, 30, 6, 0, 0, 0, time.UTC))
	currentID, replay, err := repo.RecordBaseCountAnchor(ctx, current)
	if err != nil || replay || currentID == "" {
		t.Fatalf("current anchor id=%q replay=%v err=%v", currentID, replay, err)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM count_projection_exceptions
WHERE tenant_id=$1::uuid
  AND exception_type='unreported_shifting'
  AND source_key=$2
  AND status='open'
  AND work_state='owner_missing'
  AND count_projection_snapshot_id IS NULL`, countsTenant, "base_count_anchor:"+currentID); got != 1 {
		t.Fatalf("unreported shifting exceptions=%d, want 1", got)
	}
	exceptionID := projectionExceptionID(t, ctx, pool, "unreported_shifting", "base_count_anchor:"+currentID)
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND aggregate_id=$2::uuid
  AND event_type=$3`, countsTenant, exceptionID, domain.EventProjectionExceptionOpened); got != 1 {
		t.Fatalf("projection exception opened outbox rows=%d, want 1", got)
	}
	openedPayload := outboxPayloadForAggregate(t, ctx, pool, domain.EventProjectionExceptionOpened, exceptionID)
	openedEvent, err := eventbus.EventFromEnvelope(openedPayload, eventbus.Event{})
	if err != nil {
		t.Fatalf("opened EventFromEnvelope: %v", err)
	}
	if openedEvent.Type != domain.EventProjectionExceptionOpened || openedEvent.Key != exceptionID || openedEvent.TenantID != countsTenant {
		t.Fatalf("opened event=%+v, want projection exception aggregate", openedEvent)
	}
	var discrepancyState string
	if err := pool.QueryRow(ctx, `
SELECT discrepancy_state FROM count_base_anchors
WHERE tenant_id=$1::uuid AND base_count_anchor_id=$2::uuid`, countsTenant, currentID).Scan(&discrepancyState); err != nil {
		t.Fatalf("load discrepancy state: %v", err)
	}
	if discrepancyState != "investigating" {
		t.Fatalf("discrepancy_state=%q, want investigating", discrepancyState)
	}
	target := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	if _, err := repo.CreateProjectionSnapshot(ctx, domain.ProjectionSnapshot{
		TenantID: countsTenant, Horizon: "feed_target_date", ParkID: countsPark,
		TargetDate: target, AsOf: current.CountedAt, ProjectionStatus: "blocked",
		SourceContractVersion: domain.SourceContractVersionV1,
		SourceHash:            "mismatch-projection-source-hash",
		BaseAnchorIDsHash:     "mismatch-anchor-hash",
		ShiftingEventIDsHash:  "mismatch-shift-hash",
		GeneratedBy:           "test",
		Rows: []domain.ProjectionRow{{
			ParkID: countsPark, ShedID: countsShedA, TargetDate: target,
			GrainKey: countProjectionGrainKey(countsShedA, "beetal"), BreedKey: "beetal", BreedLabel: "Beetal",
			BaseCountAnchorID: currentID, IncludedShiftingEventIDsHash: "mismatch-shift-hash",
			HeadCount: current.HeadCount, RationContextResolutionState: "blocked",
			BlockerReason: strPtr("count mismatch investigation open"), SourceRowHash: "mismatch-row-hash",
		}},
	}); err != nil {
		t.Fatalf("create projection snapshot: %v", err)
	}
	projection, err := repo.ProjectedCountFor(ctx, domain.CountProjectionRequest{
		TenantID: countsTenant, ParkID: countsPark, TargetDate: target, ShedID: strPtr(countsShedA), BreedKey: strPtr("beetal"), Limit: 25,
	})
	if err != nil {
		t.Fatalf("projected count: %v", err)
	}
	if len(projection.Exceptions) != 1 || projection.Exceptions[0].ExceptionType != "unreported_shifting" {
		t.Fatalf("projection exceptions=%+v, want open unreported shifting", projection.Exceptions)
	}
	readiness, err := repo.Readiness(ctx, countsTenant)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.OpenExceptionCount != 1 || readiness.GenerationAllowed {
		t.Fatalf("readiness=%+v, want one open exception and no generation", readiness)
	}
}

func TestRepositoryDoesNotFlagBaseCountMismatchWhenAppliedShiftExplainsDelta(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	previous := baseAnchorForAt(countsShedA, "matched-previous-anchor", "matched-previous-fp", 20, time.Date(2026, 6, 29, 6, 0, 0, 0, time.UTC))
	if _, replay, err := repo.RecordBaseCountAnchor(ctx, previous); err != nil || replay {
		t.Fatalf("previous anchor replay=%v err=%v", replay, err)
	}
	shift := shiftingEvent("matched-shift-key", "matched-shift-idem", "matched-shift-payload", "matched-shift-fp")
	shift.EventStatus = "applied"
	shift.EffectiveAt = time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	if _, replay, err := repo.RecordShiftingEvent(ctx, shift); err != nil || replay {
		t.Fatalf("shift replay=%v err=%v", replay, err)
	}
	current := baseAnchorForAt(countsShedA, "matched-current-anchor", "matched-current-fp", 17, time.Date(2026, 6, 30, 6, 0, 0, 0, time.UTC))
	currentID, replay, err := repo.RecordBaseCountAnchor(ctx, current)
	if err != nil || replay || currentID == "" {
		t.Fatalf("current anchor id=%q replay=%v err=%v", currentID, replay, err)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM count_projection_exceptions
WHERE tenant_id=$1::uuid
  AND exception_type IN ('unreported_shifting','count_mismatch')
  AND source_key=$2
  AND status='open'`, countsTenant, "base_count_anchor:"+currentID); got != 0 {
		t.Fatalf("count mismatch exceptions=%d, want 0", got)
	}
}

func TestRepositoryScanCountMismatchesCreatesExceptionForStaleImportedAnchor(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	previous := baseAnchorForAt(countsShedA, "scan-previous-anchor", "scan-previous-fp", 20, time.Date(2026, 6, 28, 6, 0, 0, 0, time.UTC))
	if _, replay, err := repo.RecordBaseCountAnchor(ctx, previous); err != nil || replay {
		t.Fatalf("previous anchor replay=%v err=%v", replay, err)
	}
	current := baseAnchorForAt(countsShedA, "scan-current-anchor", "scan-current-fp", 17, time.Date(2026, 6, 29, 6, 0, 0, 0, time.UTC))
	current.SourceSystem = "import"
	current.SourceRef = "counting-db-values-only:row-17"
	current.SourceHash = "scan-current-source-hash"
	currentID := insertBaseCountAnchorDirect(t, ctx, pool, current)

	first, err := repo.ScanCountMismatches(ctx, domain.CountMismatchScanRequest{
		TenantID: countsTenant, ParkID: strPtr(countsPark),
		CountedBefore: time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Limit:         1,
	})
	if err != nil {
		t.Fatalf("scan first page: %v", err)
	}
	if first.ScannedAnchorCount != 1 || first.ExceptionWriteCount != 0 || first.NextCursor == nil {
		t.Fatalf("first scan page=%+v, want one earlier anchor and cursor", first)
	}
	if first.RunID == "" || first.Status != "completed" || first.CompletedAt == nil {
		t.Fatalf("first scan run=%+v, want completed durable run", first)
	}
	second, err := repo.ScanCountMismatches(ctx, domain.CountMismatchScanRequest{
		TenantID: countsTenant, ParkID: strPtr(countsPark),
		CountedBefore:   time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		CursorCountedAt: &first.NextCursor.CountedAt,
		CursorAnchorID:  &first.NextCursor.BaseCountAnchorID,
		Limit:           25,
	})
	if err != nil {
		t.Fatalf("scan second page: %v", err)
	}
	if second.ScannedAnchorCount != 1 || second.ExceptionWriteCount != 1 ||
		second.InvestigatingAnchorCount != 1 || second.NextCursor != nil {
		t.Fatalf("second scan page=%+v, want one mismatch and no further cursor", second)
	}
	if second.RunID == "" || second.Status != "completed" || second.CompletedAt == nil || second.LastError != nil {
		t.Fatalf("second scan run=%+v, want completed run with no error", second)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM count_mismatch_scan_runs
WHERE tenant_id=$1::uuid
  AND count_mismatch_scan_run_id=$2::uuid
  AND status='completed'
  AND scanned_anchor_count=1
  AND exception_write_count=1
  AND investigating_anchor_count=1
  AND next_cursor_anchor_id IS NULL
  AND last_error IS NULL`, countsTenant, second.RunID); got != 1 {
		t.Fatalf("completed mismatch scan run rows=%d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM count_projection_exceptions
WHERE tenant_id=$1::uuid
  AND exception_type='unreported_shifting'
  AND source_key=$2
  AND status='open'
  AND count_projection_snapshot_id IS NULL`, countsTenant, "base_count_anchor:"+currentID); got != 1 {
		t.Fatalf("stale import mismatch exceptions=%d, want 1", got)
	}
	var discrepancyState string
	if err := pool.QueryRow(ctx, `
SELECT discrepancy_state FROM count_base_anchors
WHERE tenant_id=$1::uuid AND base_count_anchor_id=$2::uuid`, countsTenant, currentID).Scan(&discrepancyState); err != nil {
		t.Fatalf("load discrepancy state: %v", err)
	}
	if discrepancyState != "investigating" {
		t.Fatalf("discrepancy_state=%q, want investigating", discrepancyState)
	}
	exceptionID := projectionExceptionID(t, ctx, pool, "unreported_shifting", "base_count_anchor:"+currentID)
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND aggregate_id=$2::uuid
  AND event_type=$3`, countsTenant, exceptionID, domain.EventProjectionExceptionOpened); got != 1 {
		t.Fatalf("scan-opened projection exception outbox rows=%d, want 1", got)
	}
	readiness, err := repo.Readiness(ctx, countsTenant)
	if err != nil {
		t.Fatalf("readiness after mismatch scan: %v", err)
	}
	csg6 := readinessSubgate(t, readiness, "CSG6")
	if csg6.Status != domain.ReadinessReady || csg6.EvidenceRef != "count_mismatch_scan_runs:"+second.RunID {
		t.Fatalf("CSG6=%+v, want ready with second run evidence", csg6)
	}
	csg10 := readinessSubgate(t, readiness, "CSG10")
	if csg10.Status != domain.ReadinessPending || csg10.EvidenceRef != "count_mismatch_scan_runs:"+second.RunID {
		t.Fatalf("CSG10=%+v, want pending with scan run evidence", csg10)
	}
	if _, err := repo.ScanCountMismatches(ctx, domain.CountMismatchScanRequest{
		TenantID: countsTenant, ParkID: strPtr(countsPark),
		CountedBefore: time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Limit:         25,
	}); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM count_projection_exceptions
WHERE tenant_id=$1::uuid
  AND exception_type='unreported_shifting'
  AND source_key=$2
  AND status='open'`, countsTenant, "base_count_anchor:"+currentID); got != 1 {
		t.Fatalf("stale import mismatch exceptions after rescan=%d, want 1", got)
	}
}

func TestRepositoryRecordsShiftingEventImpactAndRejectsLogicalConflict(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)

	in := shiftingEvent("shift-key-1", "shift-idem-1", "payload-1", "fp-1")
	id, replay, err := repo.RecordShiftingEvent(ctx, in)
	if err != nil || replay || id == "" {
		t.Fatalf("first shift id=%q replay=%v err=%v", id, replay, err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM shifting_event_impacts WHERE tenant_id=$1 AND shifting_event_id=$2::uuid`, countsTenant, id); got != 1 {
		t.Fatalf("impact rows=%d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND aggregate_id=$2::uuid
  AND event_type='counts.shifting_event.recorded'`, countsTenant, id); got != 1 {
		t.Fatalf("shifting event outbox rows=%d, want 1", got)
	}
	again, replay, err := repo.RecordShiftingEvent(ctx, in)
	if err != nil || !replay || again != id {
		t.Fatalf("shift replay id=%q replay=%v err=%v, want id=%q replay=true", again, replay, err, id)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND aggregate_id=$2::uuid
  AND event_type='counts.shifting_event.recorded'`, countsTenant, id); got != 1 {
		t.Fatalf("shifting event replay outbox rows=%d, want 1", got)
	}
	conflict := shiftingEvent("shift-key-1", "shift-idem-2", "payload-2", "fp-2")
	if _, _, err := repo.RecordShiftingEvent(ctx, conflict); !errors.Is(err, ports.ErrLogicalKeyConflict) {
		t.Fatalf("logical conflict err=%v, want ErrLogicalKeyConflict", err)
	}
}

func TestRepositoryCreatesBlockedProjectionSnapshotAndReadiness(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	blocker := "pregnant destination shed shortage: reviewed ration context missing"
	breed := "beetal"
	stage := "pregnant"
	anchorID, replay, err := repo.RecordBaseCountAnchor(ctx, baseAnchor("projection-anchor-key-1", "projection-anchor-fp-1"))
	if err != nil || replay || anchorID == "" {
		t.Fatalf("projection anchor id=%q replay=%v err=%v", anchorID, replay, err)
	}
	snapID, err := repo.CreateProjectionSnapshot(ctx, domain.ProjectionSnapshot{
		TenantID: countsTenant, Horizon: "feed_target_date", ParkID: countsPark,
		TargetDate:       time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		AsOf:             time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		ProjectionStatus: "blocked", SourceContractVersion: "counts-shifting-v1",
		SourceHash: "snapshot-hash-1", BaseAnchorIDsHash: "anchors-hash-1",
		ShiftingEventIDsHash: "shift-hash-1", GeneratedBy: "test",
		Rows: []domain.ProjectionRow{{
			ParkID: countsPark, ShedID: countsShedB, TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			GrainKey: "shed-b:beetal:pregnant", BreedKey: "beetal", BreedLabel: "Beetal",
			BaseCountAnchorID: anchorID, IncludedShiftingEventIDsHash: "shift-hash-1",
			StageTag: &stage, HeadCount: 12, PregnantCount: 12,
			RationContextResolutionState: "blocked", BlockerReason: &blocker, SourceRowHash: "row-hash-1",
		}},
		Exceptions: []domain.ProjectionException{{
			ExceptionType: "destination_shortage", SourceKey: "shift-key-1", GrainKey: "shed-b:beetal:pregnant",
			ParkID: strPtr(countsPark), ShedID: strPtr(countsShedB), BreedKey: &breed, StageTag: &stage,
			Severity: "critical", BlockerReason: blocker,
		}},
	})
	if err != nil || snapID == "" {
		t.Fatalf("snapshot id=%q err=%v", snapID, err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM count_projection_snapshot_rows WHERE tenant_id=$1 AND count_projection_snapshot_id=$2::uuid AND ration_context_resolution_state='blocked'`, countsTenant, snapID); got != 1 {
		t.Fatalf("blocked projection rows=%d, want 1", got)
	}
	readiness, err := repo.Readiness(ctx, countsTenant)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.GenerationAllowed || readiness.Status != domain.ReadinessBlocked {
		t.Fatalf("readiness=%+v, want blocked/no-generate", readiness)
	}
	if readiness.OpenExceptionCount != 1 {
		t.Fatalf("open exceptions=%d, want 1", readiness.OpenExceptionCount)
	}
	if readiness.LatestProjectionStatus != "blocked" || readiness.LatestProjectionRowCount != 1 {
		t.Fatalf("latest projection status=%q rows=%d", readiness.LatestProjectionStatus, readiness.LatestProjectionRowCount)
	}
	projection, err := repo.ProjectedCountFor(ctx, domain.CountProjectionRequest{
		TenantID: countsTenant, ParkID: countsPark, TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		ShedID: strPtr(countsShedB), BreedKey: &breed, RationContextResolutionState: strPtr("blocked"), Limit: 25,
	})
	if err != nil {
		t.Fatalf("projected count: %v", err)
	}
	if projection.SnapshotID != snapID || projection.ProjectionStatus != "blocked" || projection.ExceptionCount != 1 {
		t.Fatalf("projection header=%+v, want blocked snapshot %s with one exception", projection, snapID)
	}
	if len(projection.Rows) != 1 {
		t.Fatalf("projection rows=%d, want 1", len(projection.Rows))
	}
	row := projection.Rows[0]
	if row.BaseCountAnchorID != anchorID || row.IncludedShiftingEventIDsHash != "shift-hash-1" {
		t.Fatalf("row provenance anchor=%q shifts=%q", row.BaseCountAnchorID, row.IncludedShiftingEventIDsHash)
	}
	if row.PregnantCount != 12 || row.BlockerReason == nil || *row.BlockerReason != blocker {
		t.Fatalf("pregnant blocked row=%+v", row)
	}
	if len(projection.Exceptions) != 1 || projection.Exceptions[0].ExceptionType != "destination_shortage" {
		t.Fatalf("projection exceptions=%+v, want destination_shortage", projection.Exceptions)
	}
	ex := projection.Exceptions[0]
	if ex.WorkType != "counts_projection_exception" || ex.WorkState != "owner_missing" || ex.DueAt.IsZero() ||
		ex.NextAction == "" || ex.EvidenceLink == "" {
		t.Fatalf("exception work fields not populated: %+v", ex)
	}
}

func TestRepositoryRecordsProjectionRecomputeRunEvidence(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	if _, replay, err := repo.RecordBaseCountAnchor(ctx, baseAnchor("recompute-run-anchor-key", "recompute-run-anchor-fp")); err != nil || replay {
		t.Fatalf("base anchor replay=%v err=%v", replay, err)
	}
	result, err := countsapp.NewService(repo).RecomputeProjectionSnapshotWithResult(ctx, domain.ProjectionRecomputeRequest{
		TenantID: countsTenant, ParkID: countsPark, Horizon: "feed_target_date",
		TargetDate:            time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		AsOf:                  time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		SourceContractVersion: domain.SourceContractVersionV1,
		GeneratedBy:           "integration-test",
	})
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if result.RunID == "" || result.SnapshotID == "" || result.ProjectionStatus != "blocked" ||
		result.RowCount != 1 || result.ExceptionCount != 1 {
		t.Fatalf("recompute result=%+v, want run/snapshot and blocked row evidence", result)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM count_projection_recompute_runs
WHERE tenant_id=$1::uuid
  AND count_projection_recompute_run_id=$2::uuid
  AND snapshot_id=$3::uuid
  AND status='completed'
  AND projection_status='blocked'
  AND row_count=1
  AND exception_count=1`, countsTenant, result.RunID, result.SnapshotID); got != 1 {
		t.Fatalf("projection recompute run rows=%d, want completed run evidence", got)
	}
	readiness, err := repo.Readiness(ctx, countsTenant)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	csg10 := readinessSubgate(t, readiness, "CSG10")
	if csg10.Status != domain.ReadinessPending ||
		csg10.EvidenceRef != "count_projection_recompute_runs:"+result.RunID ||
		!strings.Contains(csg10.BlockerReason, "seeded local E2E") {
		t.Fatalf("CSG10=%+v, want pending recompute-run evidence", csg10)
	}
}

func TestRepositoryResolvesProjectionExceptionIdempotently(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	previous := baseAnchorForAt(countsShedA, "resolve-previous-anchor", "resolve-previous-fp", 20, time.Date(2026, 6, 29, 6, 0, 0, 0, time.UTC))
	if _, replay, err := repo.RecordBaseCountAnchor(ctx, previous); err != nil || replay {
		t.Fatalf("previous anchor replay=%v err=%v", replay, err)
	}
	current := baseAnchorForAt(countsShedA, "resolve-current-anchor", "resolve-current-fp", 17, time.Date(2026, 6, 30, 6, 0, 0, 0, time.UTC))
	currentID, replay, err := repo.RecordBaseCountAnchor(ctx, current)
	if err != nil || replay || currentID == "" {
		t.Fatalf("current anchor id=%q replay=%v err=%v", currentID, replay, err)
	}
	exceptionID := projectionExceptionID(t, ctx, pool, "unreported_shifting", "base_count_anchor:"+currentID)
	ref := "shift-report:resolved-by-feed-director"
	in := domain.ProjectionExceptionResolutionRequest{
		TenantID: countsTenant, ProjectionExceptionID: exceptionID, Action: "resolve",
		ResolvedByRef: "feed-director:ravi", ResolutionReason: "reviewed physical count and entered missing ShiftingEvent",
		ResolutionRef: &ref, IdempotencyKey: "resolve-exception-idem", RequestFingerprint: "resolve-exception-fp",
	}
	out, err := repo.ResolveProjectionException(ctx, in)
	if err != nil {
		t.Fatalf("resolve projection exception: %v", err)
	}
	if out.ProjectionExceptionID != exceptionID || out.Status != "resolved" || out.WorkState != "resolved" ||
		out.Action != "resolve" || out.Replayed || out.ResolutionRef == nil || *out.ResolutionRef != ref || out.ResolvedAt.IsZero() {
		t.Fatalf("resolution=%+v", out)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND aggregate_id=$2::uuid
  AND event_type=$3`, countsTenant, exceptionID, domain.EventProjectionExceptionClosed); got != 1 {
		t.Fatalf("projection exception closed outbox rows=%d, want 1", got)
	}
	closedPayload := outboxPayloadForAggregate(t, ctx, pool, domain.EventProjectionExceptionClosed, exceptionID)
	closedEvent, err := eventbus.EventFromEnvelope(closedPayload, eventbus.Event{})
	if err != nil {
		t.Fatalf("closed EventFromEnvelope: %v", err)
	}
	if closedEvent.Type != domain.EventProjectionExceptionClosed || closedEvent.Key != exceptionID ||
		!strings.Contains(string(closedEvent.Payload), out.ProjectionExceptionResolutionID) {
		t.Fatalf("closed event=%+v payload=%s", closedEvent, string(closedEvent.Payload))
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM count_projection_exceptions
WHERE tenant_id=$1::uuid
  AND count_projection_exception_id=$2::uuid
  AND status='resolved'
  AND work_state='resolved'
  AND resolved_by_ref='feed-director:ravi'
  AND resolution_reason=$3
  AND resolution_ref=$4`, countsTenant, exceptionID, in.ResolutionReason, ref); got != 1 {
		t.Fatalf("resolved exception rows=%d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM count_projection_exception_resolutions
WHERE tenant_id=$1::uuid
  AND count_projection_exception_id=$2::uuid
  AND idempotency_key=$3`, countsTenant, exceptionID, in.IdempotencyKey); got != 1 {
		t.Fatalf("resolution audit rows=%d, want 1", got)
	}
	var discrepancyState string
	if err := pool.QueryRow(ctx, `
SELECT discrepancy_state FROM count_base_anchors
WHERE tenant_id=$1::uuid AND base_count_anchor_id=$2::uuid`, countsTenant, currentID).Scan(&discrepancyState); err != nil {
		t.Fatalf("load discrepancy state: %v", err)
	}
	if discrepancyState != "resolved" {
		t.Fatalf("discrepancy_state=%q, want resolved", discrepancyState)
	}
	readiness, err := repo.Readiness(ctx, countsTenant)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.OpenExceptionCount != 0 || readiness.GenerationAllowed {
		t.Fatalf("readiness=%+v, want no open exceptions but generation still blocked by subgates/latest projection", readiness)
	}
	replayOut, err := repo.ResolveProjectionException(ctx, in)
	if err != nil || !replayOut.Replayed || replayOut.ProjectionExceptionResolutionID != out.ProjectionExceptionResolutionID {
		t.Fatalf("replay resolution=%+v err=%v, want replay of %s", replayOut, err, out.ProjectionExceptionResolutionID)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND aggregate_id=$2::uuid
  AND event_type=$3`, countsTenant, exceptionID, domain.EventProjectionExceptionClosed); got != 1 {
		t.Fatalf("projection exception closed replay outbox rows=%d, want 1", got)
	}
	conflict := in
	conflict.RequestFingerprint = "different-fp"
	if _, err := repo.ResolveProjectionException(ctx, conflict); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("conflict err=%v, want ErrIdempotencyConflict", err)
	}
	closedAgain := in
	closedAgain.IdempotencyKey = "resolve-exception-new-key"
	closedAgain.RequestFingerprint = "resolve-exception-new-fp"
	if _, err := repo.ResolveProjectionException(ctx, closedAgain); !errors.Is(err, ports.ErrProjectionExceptionClosed) {
		t.Fatalf("closed err=%v, want ErrProjectionExceptionClosed", err)
	}
}

func TestRepositoryDismissesProjectionException(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	anchorID, replay, err := repo.RecordBaseCountAnchor(ctx, baseAnchor("dismiss-anchor-key", "dismiss-anchor-fp"))
	if err != nil || replay || anchorID == "" {
		t.Fatalf("anchor id=%q replay=%v err=%v", anchorID, replay, err)
	}
	target := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	blocker := "pregnant destination shed shortage: reviewed ration context missing"
	if _, err := repo.CreateProjectionSnapshot(ctx, repeatedExceptionSnapshot(anchorID, target, "snapshot-dismiss-hash", blocker, "beetal", "pregnant")); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	exceptionID := projectionExceptionID(t, ctx, pool, "destination_shortage", "shift-key-relink")
	out, err := repo.ResolveProjectionException(ctx, domain.ProjectionExceptionResolutionRequest{
		TenantID: countsTenant, ProjectionExceptionID: exceptionID, Action: "dismiss",
		ResolvedByRef: "feed-director:ravi", ResolutionReason: "reviewed duplicate shortage warning from superseded sheet",
		IdempotencyKey: "dismiss-exception-idem", RequestFingerprint: "dismiss-exception-fp",
	})
	if err != nil {
		t.Fatalf("dismiss projection exception: %v", err)
	}
	if out.Status != "dismissed" || out.WorkState != "dismissed" || out.Action != "dismiss" {
		t.Fatalf("dismissal=%+v", out)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM count_projection_exceptions
WHERE tenant_id=$1::uuid
  AND count_projection_exception_id=$2::uuid
  AND status='dismissed'
  AND work_state='dismissed'`, countsTenant, exceptionID); got != 1 {
		t.Fatalf("dismissed exception rows=%d, want 1", got)
	}
}

func TestRepositoryListsProjectionExceptionsWithBoundedCursor(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	anchorID, replay, err := repo.RecordBaseCountAnchor(ctx, baseAnchor("list-exceptions-anchor-key", "list-exceptions-anchor-fp"))
	if err != nil || replay || anchorID == "" {
		t.Fatalf("anchor id=%q replay=%v err=%v", anchorID, replay, err)
	}
	target := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	blocker := "pregnant destination shed shortage: reviewed ration context missing"
	snapshot := repeatedExceptionSnapshot(anchorID, target, "snapshot-list-hash", blocker, "beetal", "pregnant")
	snapshot.Exceptions = append(snapshot.Exceptions, domain.ProjectionException{
		ExceptionType: "destination_shortage", SourceKey: "shift-key-list-2", GrainKey: "shed-b:beetal:pregnant:2",
		ParkID: strPtr(countsPark), ShedID: strPtr(countsShedB), BreedKey: strPtr("beetal"), StageTag: strPtr("pregnant"),
		Severity: "critical", BlockerReason: blocker,
	})
	if _, err := repo.CreateProjectionSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	first, err := repo.ListProjectionExceptions(ctx, domain.ProjectionExceptionQuery{
		TenantID: countsTenant, Status: "open", ParkID: strPtr(countsPark), Severity: strPtr("critical"), Limit: 1,
	})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("first page=%+v, want one item and cursor", first)
	}
	if first.Items[0].ProjectionExceptionID == "" || first.Items[0].ProjectionSnapshotID == nil ||
		first.Items[0].WorkType != "counts_projection_exception" || first.Items[0].DueAt.IsZero() ||
		first.Items[0].CreatedAt.IsZero() || first.Items[0].UpdatedAt.IsZero() {
		t.Fatalf("first exception missing work metadata: %+v", first.Items[0])
	}
	cursor, err := domain.DecodeProjectionExceptionCursor(*first.NextCursor)
	if err != nil {
		t.Fatalf("decode next cursor: %v", err)
	}
	second, err := repo.ListProjectionExceptions(ctx, domain.ProjectionExceptionQuery{
		TenantID: countsTenant, Status: "open", ParkID: strPtr(countsPark), Severity: strPtr("critical"), Cursor: &cursor, Limit: 1,
	})
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(second.Items) != 1 || second.NextCursor != nil {
		t.Fatalf("second page=%+v, want one final item", second)
	}
	if second.Items[0].ProjectionExceptionID == first.Items[0].ProjectionExceptionID {
		t.Fatalf("cursor repeated same exception %s", second.Items[0].ProjectionExceptionID)
	}
}

func TestRepositoryRelinksRepeatedOpenExceptionToLatestSnapshot(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	blocker := "pregnant destination shed shortage: reviewed ration context missing"
	breed := "beetal"
	stage := "pregnant"
	anchorID, replay, err := repo.RecordBaseCountAnchor(ctx, baseAnchor("projection-relink-anchor-key", "projection-relink-anchor-fp"))
	if err != nil || replay || anchorID == "" {
		t.Fatalf("projection anchor id=%q replay=%v err=%v", anchorID, replay, err)
	}
	firstTarget := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	secondTarget := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	if _, err := repo.CreateProjectionSnapshot(ctx, repeatedExceptionSnapshot(anchorID, firstTarget, "snapshot-relink-hash-1", blocker, breed, stage)); err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	secondID, err := repo.CreateProjectionSnapshot(ctx, repeatedExceptionSnapshot(anchorID, secondTarget, "snapshot-relink-hash-2", blocker, breed, stage))
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	projection, err := repo.ProjectedCountFor(ctx, domain.CountProjectionRequest{
		TenantID: countsTenant, ParkID: countsPark, TargetDate: secondTarget,
		ShedID: strPtr(countsShedB), BreedKey: &breed, RationContextResolutionState: strPtr("blocked"), Limit: 25,
	})
	if err != nil {
		t.Fatalf("projected count: %v", err)
	}
	if projection.SnapshotID != secondID {
		t.Fatalf("snapshot=%s, want latest %s", projection.SnapshotID, secondID)
	}
	if len(projection.Exceptions) != 1 || projection.Exceptions[0].ExceptionType != "destination_shortage" {
		t.Fatalf("projection exceptions=%+v, want relinked destination_shortage", projection.Exceptions)
	}
	if projection.Exceptions[0].WorkState != "owner_missing" || projection.Exceptions[0].EvidenceLink == "" {
		t.Fatalf("exception work fields=%+v", projection.Exceptions[0])
	}
	exceptionID := projectionExceptionID(t, ctx, pool, "destination_shortage", "shift-key-relink")
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND aggregate_id=$2::uuid
  AND event_type=$3`, countsTenant, exceptionID, domain.EventProjectionExceptionUpdated); got != 1 {
		t.Fatalf("projection exception updated outbox rows=%d, want 1", got)
	}
}

func TestRepositoryProjectionProviderFailsClosedWhenSnapshotMissing(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)

	projection, err := repo.CountAsOf(ctx, domain.CountProjectionRequest{
		TenantID: countsTenant, ParkID: countsPark, AsOf: time.Date(2026, 6, 30, 18, 0, 0, 0, time.UTC), Limit: 25,
	})
	if err != nil {
		t.Fatalf("count as of: %v", err)
	}
	if projection.ProjectionStatus != "blocked" || len(projection.Blockers) != 1 {
		t.Fatalf("projection=%+v, want blocked missing snapshot", projection)
	}
	if projection.Blockers[0].ExceptionType != "missing_projection_snapshot" {
		t.Fatalf("blocker=%+v", projection.Blockers[0])
	}
}

func TestServiceRecomputesProjectionWithAuthorizedShiftOnlyInProjectedHorizon(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	service := countsapp.NewService(repo)

	sourceAnchor, replay, err := repo.RecordBaseCountAnchor(ctx, baseAnchorFor(countsShedA, "projection-source-anchor", "projection-source-fp", 20))
	if err != nil || replay || sourceAnchor == "" {
		t.Fatalf("source anchor id=%q replay=%v err=%v", sourceAnchor, replay, err)
	}
	destAnchor, replay, err := repo.RecordBaseCountAnchor(ctx, baseAnchorFor(countsShedB, "projection-dest-anchor", "projection-dest-fp", 5))
	if err != nil || replay || destAnchor == "" {
		t.Fatalf("dest anchor id=%q replay=%v err=%v", destAnchor, replay, err)
	}
	if _, _, err := repo.RecordShiftingEvent(ctx, shiftingEvent("projection-shift-key", "projection-shift-idem", "projection-shift-payload", "projection-shift-fp")); err != nil {
		t.Fatalf("record shift: %v", err)
	}

	asOf := time.Date(2026, 6, 30, 18, 0, 0, 0, time.UTC)
	target := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	if _, err := service.RecomputeProjectionSnapshot(ctx, domain.ProjectionRecomputeRequest{
		TenantID: countsTenant, ParkID: countsPark, Horizon: "count_as_of", TargetDate: target, AsOf: asOf,
		SourceContractVersion: "counts-shifting-v1", GeneratedBy: "test",
	}); err != nil {
		t.Fatalf("recompute count_as_of: %v", err)
	}
	if _, err := service.RecomputeProjectionSnapshot(ctx, domain.ProjectionRecomputeRequest{
		TenantID: countsTenant, ParkID: countsPark, Horizon: "feed_target_date", TargetDate: target, AsOf: asOf,
		SourceContractVersion: "counts-shifting-v1", GeneratedBy: "test",
	}); err != nil {
		t.Fatalf("recompute projected: %v", err)
	}

	realized, err := repo.CountAsOf(ctx, domain.CountProjectionRequest{
		TenantID: countsTenant, ParkID: countsPark, AsOf: target, Limit: 25,
	})
	if err != nil {
		t.Fatalf("count as of read: %v", err)
	}
	projected, err := repo.ProjectedCountFor(ctx, domain.CountProjectionRequest{
		TenantID: countsTenant, ParkID: countsPark, TargetDate: target, Limit: 25,
	})
	if err != nil {
		t.Fatalf("projected read: %v", err)
	}
	if got := rowForShed(t, realized.Rows, countsShedA).HeadCount; got != 20 {
		t.Fatalf("realized source head_count=%d, want 20", got)
	}
	if got := rowForShed(t, realized.Rows, countsShedB).HeadCount; got != 5 {
		t.Fatalf("realized destination head_count=%d, want 5", got)
	}
	if got := rowForShed(t, projected.Rows, countsShedA).HeadCount; got != 17 {
		t.Fatalf("projected source head_count=%d, want 17", got)
	}
	projectedDest := rowForShed(t, projected.Rows, countsShedB)
	if projectedDest.HeadCount != 8 || projectedDest.PregnantCount != 3 || projectedDest.BlockerReason == nil {
		t.Fatalf("projected destination row=%+v, want 8 total, 3 pregnant, blocked", projectedDest)
	}
	foundShortage := false
	for _, ex := range projected.Exceptions {
		if ex.ExceptionType == "destination_shortage" {
			foundShortage = true
			break
		}
	}
	if !foundShortage {
		t.Fatalf("projected exceptions=%+v, want destination_shortage", projected.Exceptions)
	}
}

func TestServiceRecomputeNormalizesReviewedBreedAndStageAliases(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedCountDimensionAlias(t, ctx, pool, "breed", "*", "SIROHI", "Beetal", "Beetal")
	seedCountDimensionAlias(t, ctx, pool, "stage_tag", "*", "Pregnant", "pregnant", "Pregnant")
	repo := NewRepository(pool, 3*time.Second)
	service := countsapp.NewService(repo)

	if _, _, err := repo.RecordBaseCountAnchor(ctx, baseAnchorForBreed(countsShedA, "alias-source-anchor", "alias-source-fp", 20, "SIROHI", "Sirohi")); err != nil {
		t.Fatalf("record source anchor: %v", err)
	}
	if _, _, err := repo.RecordBaseCountAnchor(ctx, baseAnchorForBreed(countsShedB, "alias-dest-anchor", "alias-dest-fp", 5, "SIROHI", "Sirohi")); err != nil {
		t.Fatalf("record dest anchor: %v", err)
	}
	if _, _, err := repo.RecordShiftingEvent(ctx, shiftingEventWithBreedStage("alias-shift-key", "alias-shift-idem", "alias-shift-payload", "alias-shift-fp", "SIROHI", "Sirohi", "Pregnant")); err != nil {
		t.Fatalf("record shift: %v", err)
	}
	target := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	if _, err := service.RecomputeProjectionSnapshot(ctx, domain.ProjectionRecomputeRequest{
		TenantID: countsTenant, ParkID: countsPark, Horizon: "feed_target_date",
		TargetDate: target, AsOf: time.Date(2026, 6, 30, 18, 0, 0, 0, time.UTC),
		SourceContractVersion: domain.SourceContractVersionV1, GeneratedBy: "test",
	}); err != nil {
		t.Fatalf("recompute projected: %v", err)
	}
	projected, err := repo.ProjectedCountFor(ctx, domain.CountProjectionRequest{
		TenantID: countsTenant, ParkID: countsPark, TargetDate: target, Limit: 25,
	})
	if err != nil {
		t.Fatalf("projected read: %v", err)
	}
	dest := rowForShed(t, projected.Rows, countsShedB)
	if dest.BreedKey != "beetal" || dest.BreedLabel != "Beetal" {
		t.Fatalf("destination breed alias not normalized: %+v", dest)
	}
	for _, ex := range projected.Exceptions {
		if ex.ExceptionType == "alias_conflict" {
			t.Fatalf("unexpected alias_conflict with approved aliases: %+v", projected.Exceptions)
		}
	}
}

func TestServiceRecomputeFailsClosedOnUnreviewedStageAlias(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	service := countsapp.NewService(repo)

	if _, _, err := repo.RecordBaseCountAnchor(ctx, baseAnchorFor(countsShedA, "unreviewed-stage-source-anchor", "unreviewed-stage-source-fp", 20)); err != nil {
		t.Fatalf("record source anchor: %v", err)
	}
	if _, _, err := repo.RecordBaseCountAnchor(ctx, baseAnchorFor(countsShedB, "unreviewed-stage-dest-anchor", "unreviewed-stage-dest-fp", 5)); err != nil {
		t.Fatalf("record dest anchor: %v", err)
	}
	if _, _, err := repo.RecordShiftingEvent(ctx, shiftingEventWithBreedStage("unreviewed-stage-shift-key", "unreviewed-stage-shift-idem", "unreviewed-stage-shift-payload", "unreviewed-stage-shift-fp", "beetal", "Beetal", "Pregnant")); err != nil {
		t.Fatalf("record shift: %v", err)
	}
	target := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	if _, err := service.RecomputeProjectionSnapshot(ctx, domain.ProjectionRecomputeRequest{
		TenantID: countsTenant, ParkID: countsPark, Horizon: "feed_target_date",
		TargetDate: target, AsOf: time.Date(2026, 6, 30, 18, 0, 0, 0, time.UTC),
		SourceContractVersion: domain.SourceContractVersionV1, GeneratedBy: "test",
	}); err != nil {
		t.Fatalf("recompute projected: %v", err)
	}
	projected, err := repo.ProjectedCountFor(ctx, domain.CountProjectionRequest{
		TenantID: countsTenant, ParkID: countsPark, TargetDate: target, Limit: 25,
	})
	if err != nil {
		t.Fatalf("projected read: %v", err)
	}
	foundAliasConflict := false
	foundDestinationShortage := false
	for _, ex := range projected.Exceptions {
		switch ex.ExceptionType {
		case "alias_conflict":
			foundAliasConflict = true
		case "destination_shortage":
			foundDestinationShortage = true
		}
	}
	if !foundAliasConflict || !foundDestinationShortage {
		t.Fatalf("exceptions=%+v, want alias_conflict and destination_shortage", projected.Exceptions)
	}
	dest := rowForShed(t, projected.Rows, countsShedB)
	if dest.BlockerReason == nil || !strings.Contains(*dest.BlockerReason, "unreviewed stage_tag alias") {
		t.Fatalf("destination blocker=%v, want unreviewed stage alias", dest.BlockerReason)
	}
}

func TestProjectionInputOutboxEventRecomputesSnapshotsThroughHandler(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 3*time.Second)
	service := countsapp.NewService(repo)

	if _, _, err := repo.RecordBaseCountAnchor(ctx, baseAnchorFor(countsShedA, "event-source-anchor", "event-source-fp", 20)); err != nil {
		t.Fatalf("record source anchor: %v", err)
	}
	if _, _, err := repo.RecordBaseCountAnchor(ctx, baseAnchorFor(countsShedB, "event-dest-anchor", "event-dest-fp", 5)); err != nil {
		t.Fatalf("record dest anchor: %v", err)
	}
	shiftID, replay, err := repo.RecordShiftingEvent(ctx, shiftingEvent("event-shift-key", "event-shift-idem", "event-shift-payload", "event-shift-fp"))
	if err != nil || replay || shiftID == "" {
		t.Fatalf("record shift id=%q replay=%v err=%v", shiftID, replay, err)
	}

	eventPayload := outboxPayloadForAggregate(t, ctx, pool, domain.EventShiftingEventRecorded, shiftID)
	event, err := eventbus.EventFromEnvelope(eventPayload, eventbus.Event{})
	if err != nil {
		t.Fatalf("EventFromEnvelope: %v", err)
	}
	bus := eventbus.NewInProcessBus()
	countsapp.NewProjectionInputHandler(service).Register(bus)
	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("publish projection input event: %v", err)
	}

	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM count_projection_snapshots
WHERE tenant_id=$1::uuid
  AND park_id=$2::uuid
  AND generated_by='counts-projection-event-handler'`, countsTenant, countsPark); got != 2 {
		t.Fatalf("event-driven snapshots=%d, want 2", got)
	}
	projected, err := repo.ProjectedCountFor(ctx, domain.CountProjectionRequest{
		TenantID: countsTenant, ParkID: countsPark, TargetDate: time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC), Limit: 25,
	})
	if err != nil {
		t.Fatalf("projected count after event: %v", err)
	}
	if projected.SnapshotID == "" || projected.SourceContractVersion != domain.SourceContractVersionV1 {
		t.Fatalf("projected header=%+v", projected)
	}
	dest := rowForShed(t, projected.Rows, countsShedB)
	if dest.HeadCount != 8 || dest.PregnantCount != 3 || dest.BlockerReason == nil {
		t.Fatalf("projected destination row=%+v, want shifted pregnant blocked in destination", dest)
	}
}

func setupCountsDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	seedCountsScope(t, ctx, pool)
	return pool
}

func seedCountsScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Mesha Test', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, countsTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT', 'CPT', 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant, countsPark); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ($3::uuid, $1::uuid, 'shed', 'CPT-S1', 'CPT Shed 1', $2::uuid, 'active'),
  ($4::uuid, $1::uuid, 'shed', 'CPT-S2', 'CPT Shed 2', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING;`,
		countsTenant, countsPark, countsShedA, countsShedB); err != nil {
		t.Fatalf("seed sheds: %v", err)
	}
}

func repeatedExceptionSnapshot(anchorID string, target time.Time, sourceHash, blocker, breed, stage string) domain.ProjectionSnapshot {
	return domain.ProjectionSnapshot{
		TenantID: countsTenant, Horizon: "feed_target_date", ParkID: countsPark,
		TargetDate:       target,
		AsOf:             time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		ProjectionStatus: "blocked", SourceContractVersion: domain.SourceContractVersionV1,
		SourceHash: sourceHash, BaseAnchorIDsHash: "anchors-relink",
		ShiftingEventIDsHash: "shift-relink", GeneratedBy: "test",
		Rows: []domain.ProjectionRow{{
			ParkID: countsPark, ShedID: countsShedB, TargetDate: target,
			GrainKey: "shed-b:beetal:pregnant", BreedKey: "beetal", BreedLabel: "Beetal",
			BaseCountAnchorID: anchorID, IncludedShiftingEventIDsHash: "shift-relink",
			StageTag: &stage, HeadCount: 12, PregnantCount: 12,
			RationContextResolutionState: "blocked", BlockerReason: &blocker, SourceRowHash: sourceHash + ":row",
		}},
		Exceptions: []domain.ProjectionException{{
			ExceptionType: "destination_shortage", SourceKey: "shift-key-relink", GrainKey: "shed-b:beetal:pregnant",
			ParkID: strPtr(countsPark), ShedID: strPtr(countsShedB), BreedKey: &breed, StageTag: &stage,
			Severity: "critical", BlockerReason: blocker,
		}},
	}
}

func baseAnchor(key, fp string) domain.BaseCountAnchor {
	return baseAnchorFor(countsShedA, key, fp, 20)
}

func baseAnchorFor(shedID, key, fp string, count int32) domain.BaseCountAnchor {
	return baseAnchorForBreed(shedID, key, fp, count, "beetal", "Beetal")
}

func baseAnchorForBreed(shedID, key, fp string, count int32, breedKey, breedLabel string) domain.BaseCountAnchor {
	return baseAnchorForBreedAt(shedID, key, fp, count, breedKey, breedLabel, time.Date(2026, 6, 30, 6, 0, 0, 0, time.UTC))
}

func baseAnchorForAt(shedID, key, fp string, count int32, countedAt time.Time) domain.BaseCountAnchor {
	return baseAnchorForBreedAt(shedID, key, fp, count, "beetal", "Beetal", countedAt)
}

func baseAnchorForBreedAt(shedID, key, fp string, count int32, breedKey, breedLabel string, countedAt time.Time) domain.BaseCountAnchor {
	return domain.BaseCountAnchor{
		TenantID: countsTenant, ParkID: countsPark, ShedID: shedID,
		BreedKey: breedKey, BreedLabel: breedLabel, CountedAt: countedAt,
		HeadCount: count, SourceSystem: "physical_base_count", SourceRef: "base-count:" + shedID + ":2026-06-30",
		SourceHash: "base-hash-" + shedID, DiscrepancyState: "not_checked", IdempotencyKey: key, RequestFingerprint: fp,
	}
}

func insertBaseCountAnchorDirect(t *testing.T, ctx context.Context, pool *pgxpool.Pool, in domain.BaseCountAnchor) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
INSERT INTO count_base_anchors (
  tenant_id, park_id, shed_id, breed_id, breed_key, breed_label, counted_at, head_count,
  source_system, source_ref, source_hash, discrepancy_state, idempotency_key, request_fingerprint, recorded_by
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, nullif($4::text, '')::uuid, $5, $6, $7, $8,
  $9, $10, $11, $12, $13, $14, nullif($15::text, '')::uuid
)
RETURNING base_count_anchor_id::text`,
		in.TenantID, in.ParkID, in.ShedID, ptrValue(in.BreedID), in.BreedKey, in.BreedLabel,
		in.CountedAt, in.HeadCount, in.SourceSystem, in.SourceRef, in.SourceHash, in.DiscrepancyState,
		in.IdempotencyKey, in.RequestFingerprint, ptrValue(in.RecordedBy)).Scan(&id); err != nil {
		t.Fatalf("insert direct base count anchor: %v", err)
	}
	return id
}

func shiftingEvent(logicalKey, idemKey, payloadHash, fp string) domain.ShiftingEvent {
	return shiftingEventWithBreedStage(logicalKey, idemKey, payloadHash, fp, "beetal", "Beetal", "pregnant")
}

func shiftingEventWithBreedStage(logicalKey, idemKey, payloadHash, fp, breedKey, breedLabel, stageValue string) domain.ShiftingEvent {
	stage := stageValue
	return domain.ShiftingEvent{
		TenantID: countsTenant, LogicalShiftingEventKey: logicalKey, Priority: "high", Category: "pregnancy",
		SourceParkID: strPtr(countsPark), SourceShedID: strPtr(countsShedA),
		DestinationParkID: countsPark, DestinationShedID: countsShedB,
		RaisedAt:           time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
		EffectiveAt:        time.Date(2026, 6, 30, 13, 0, 0, 0, time.UTC),
		AuthorizationState: "authorized", VerificationState: "verified", EventStatus: "authorized",
		SourceSystem: "feed_shiftings_docx", SourceRef: "shift-report-1",
		PayloadHash: payloadHash, IdempotencyKey: idemKey, RequestFingerprint: fp,
		Impacts: []domain.ShiftingEventImpact{{
			GrainKey: breedKey + ":" + stageValue, BreedKey: breedKey, BreedLabel: breedLabel, StageTag: &stage,
			HeadCount: 3, PregnantCount: 3, RiskFlagsJSON: []byte(`{"pregnant":true}`),
			RationContextResolutionState: "blocked", BlockerReason: strPtr("destination shed ration context unresolved"),
		}},
	}
}

func seedCountDimensionAlias(t *testing.T, ctx context.Context, pool *pgxpool.Pool, dimension, sourceSystem, sourceValue, canonicalValue, canonicalLabel string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO count_dimension_aliases (
  tenant_id, dimension, source_system, source_value, source_value_norm,
  canonical_value, canonical_label, review_status, source_ref, source_hash,
  approved_at
) VALUES (
  $1::uuid, $2, $3, $4, $5, $6, $7, 'approved', $8, $9, now()
)`,
		countsTenant, dimension, sourceSystem, sourceValue, countAliasNorm(sourceValue),
		canonicalValue, canonicalLabel, "test:"+dimension+":"+sourceValue, "hash:"+dimension+":"+sourceValue); err != nil {
		t.Fatalf("seed count alias: %v", err)
	}
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func projectionExceptionID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, exceptionType, sourceKey string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
SELECT count_projection_exception_id::text
FROM count_projection_exceptions
WHERE tenant_id=$1::uuid
  AND exception_type=$2
  AND source_key=$3
  AND status='open'`, countsTenant, exceptionType, sourceKey).Scan(&id); err != nil {
		t.Fatalf("load projection exception id: %v", err)
	}
	return id
}

func outboxPayloadForAggregate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventType, aggregateID string) []byte {
	t.Helper()
	var payload []byte
	if err := pool.QueryRow(ctx, `
SELECT payload
FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND event_type=$2
  AND aggregate_id=$3::uuid`, countsTenant, eventType, aggregateID).Scan(&payload); err != nil {
		t.Fatalf("load outbox payload: %v", err)
	}
	return payload
}

func rowForShed(t *testing.T, rows []domain.ProjectionRow, shedID string) domain.ProjectionRow {
	t.Helper()
	for _, row := range rows {
		if row.ShedID == shedID {
			return row
		}
	}
	t.Fatalf("missing projection row for shed %s in %+v", shedID, rows)
	return domain.ProjectionRow{}
}

func readinessSubgate(t *testing.T, readiness domain.Readiness, id string) domain.ReadinessSubgate {
	t.Helper()
	for _, subgate := range readiness.Subgates {
		if subgate.ID == id {
			return subgate
		}
	}
	t.Fatalf("missing readiness subgate %s in %+v", id, readiness.Subgates)
	return domain.ReadinessSubgate{}
}

func strPtr(s string) *string { return &s }
