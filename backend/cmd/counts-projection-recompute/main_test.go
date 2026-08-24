package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

var fixedNow = time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)

func TestParseFlagsRequiresTargetDateForProjectedHorizon(t *testing.T) {
	_, err := parseFlags([]string{
		"-tenant-id", "tenant-1",
		"-park-id", "park-1",
		"-horizon", "feed_target_date",
	}, func() time.Time { return fixedNow })
	if err == nil || !strings.Contains(err.Error(), "target-date is required") {
		t.Fatalf("err=%v, want target-date required", err)
	}
}

func TestParseFlagsAllowsCountAsOfWithoutTargetDate(t *testing.T) {
	cfg, err := parseFlags([]string{
		"-tenant-id", "tenant-1",
		"-park-id", "park-1",
		"-horizon", "count_as_of",
		"-as-of", "2026-06-30T13:30:00Z",
	}, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !cfg.TargetDate.IsZero() {
		t.Fatalf("target date=%s, want zero before service normalization", cfg.TargetDate)
	}
	if cfg.AsOf.Format(time.RFC3339) != "2026-06-30T19:00:00+05:30" {
		t.Fatalf("as_of=%s", cfg.AsOf.Format(time.RFC3339))
	}
}

func TestParseFlagsNormalizesTargetDate(t *testing.T) {
	cfg, err := parseFlags([]string{
		"-tenant-id", "tenant-1",
		"-park-id", "park-1",
		"-target-date", "2026-07-01T15:45:00Z",
	}, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if got := cfg.TargetDate.Format(time.RFC3339); got != "2026-07-01T00:00:00+05:30" {
		t.Fatalf("target_date=%s, want day boundary", got)
	}
	if got := horizons(cfg.Horizon); len(got) != 2 || got[0] != horizonCountAsOf || got[1] != horizonFeedTargetDate {
		t.Fatalf("horizons=%v, want count_as_of/feed_target_date", got)
	}
}

func TestParseFlagsRejectsInvalidHorizon(t *testing.T) {
	_, err := parseFlags([]string{
		"-tenant-id", "tenant-1",
		"-park-id", "park-1",
		"-horizon", "all_time",
		"-target-date", "2026-07-01",
	}, func() time.Time { return fixedNow })
	if err == nil || !strings.Contains(err.Error(), "horizon must be") {
		t.Fatalf("err=%v, want invalid horizon", err)
	}
}

func TestParseDateOrInstantRejectsInvalidDate(t *testing.T) {
	if _, err := parseDateOrInstant("tomorrow"); err == nil || !strings.Contains(err.Error(), "target-date must be") {
		t.Fatalf("err=%v, want invalid target-date", err)
	}
}

func TestProjectionRecomputeReplayAgainstMigratedPostgresUpdatesCSG8(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProjectionReplayScope(t, ctx, pool)

	repo := countspg.NewRepository(pool, 5*time.Second)
	service := countsapp.NewService(repo)
	if _, replay, err := repo.RecordBaseCountAnchor(ctx, projectionReplayBaseAnchor(projectionReplayShedA, "projection-replay-source-anchor", "projection-replay-source-fp", 20)); err != nil || replay {
		t.Fatalf("record source anchor replay=%v err=%v", replay, err)
	}
	if _, replay, err := repo.RecordBaseCountAnchor(ctx, projectionReplayBaseAnchor(projectionReplayShedB, "projection-replay-dest-anchor", "projection-replay-dest-fp", 5)); err != nil || replay {
		t.Fatalf("record destination anchor replay=%v err=%v", replay, err)
	}
	if _, replay, err := repo.RecordShiftingEvent(ctx, projectionReplayPregnantShift()); err != nil || replay {
		t.Fatalf("record pregnant shift replay=%v err=%v", replay, err)
	}

	req := countsdomain.ProjectionRecomputeRequest{
		TenantID: projectionReplayTenant, ParkID: projectionReplayPark,
		Horizon:               horizonFeedTargetDate,
		TargetDate:            time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		AsOf:                  time.Date(2026, 6, 30, 18, 0, 0, 0, time.UTC),
		SourceContractVersion: countsdomain.SourceContractVersionV1,
		GeneratedBy:           "counts-projection-recompute-test",
	}
	first, err := service.RecomputeProjectionSnapshotWithResult(ctx, req)
	if err != nil {
		t.Fatalf("first recompute: %v", err)
	}
	second, err := service.RecomputeProjectionSnapshotWithResult(ctx, req)
	if err != nil {
		t.Fatalf("second recompute replay: %v", err)
	}
	if first.RunID == "" || second.RunID == "" || first.RunID == second.RunID {
		t.Fatalf("run ids first=%q second=%q, want distinct audit runs", first.RunID, second.RunID)
	}
	if first.SnapshotID == "" || second.SnapshotID != first.SnapshotID {
		t.Fatalf("snapshot ids first=%q second=%q, want replayed immutable snapshot", first.SnapshotID, second.SnapshotID)
	}
	if first.RowCount != 2 || second.RowCount != first.RowCount || first.ExceptionCount == 0 || second.ExceptionCount != first.ExceptionCount {
		t.Fatalf("recompute rows/exceptions first=%+v second=%+v, want stable replay counts", first, second)
	}
	if got := projectionReplayCountRows(t, ctx, pool, `SELECT count(*) FROM count_projection_recompute_runs WHERE tenant_id=$1::uuid`, projectionReplayTenant); got != 2 {
		t.Fatalf("recompute runs=%d, want two visible audit runs", got)
	}
	if got := projectionReplayCountRows(t, ctx, pool, `SELECT count(*) FROM count_projection_snapshots WHERE tenant_id=$1::uuid`, projectionReplayTenant); got != 1 {
		t.Fatalf("snapshots=%d, want one immutable snapshot after replay", got)
	}
	if got := projectionReplayCountRows(t, ctx, pool, `SELECT count(*) FROM count_projection_snapshot_rows WHERE tenant_id=$1::uuid AND count_projection_snapshot_id=$2::uuid`, projectionReplayTenant, first.SnapshotID); got != first.RowCount {
		t.Fatalf("snapshot rows=%d, want %d without replay duplicates", got, first.RowCount)
	}
	if got := projectionReplayCountRows(t, ctx, pool, `SELECT count(*) FROM count_projection_exceptions WHERE tenant_id=$1::uuid AND count_projection_snapshot_id=$2::uuid`, projectionReplayTenant, first.SnapshotID); got != first.ExceptionCount {
		t.Fatalf("snapshot exceptions=%d, want %d without replay duplicates", got, first.ExceptionCount)
	}

	projected, err := repo.ProjectedCountFor(ctx, countsdomain.CountProjectionRequest{
		TenantID: projectionReplayTenant, ParkID: projectionReplayPark,
		TargetDate: req.TargetDate, Limit: 25,
	})
	if err != nil {
		t.Fatalf("read replayed projection: %v", err)
	}
	destination := projectionReplayRowForShed(t, projected.Rows, projectionReplayShedB)
	if destination.HeadCount != 8 || destination.PregnantCount != 3 || destination.BlockerReason == nil {
		t.Fatalf("destination row=%+v, want shifted pregnant cohort blocked at destination", destination)
	}
	foundShortage := false
	for _, ex := range projected.Exceptions {
		if ex.ExceptionType == "destination_shortage" && ex.ShedID != nil && *ex.ShedID == projectionReplayShedB {
			foundShortage = true
			break
		}
	}
	if !foundShortage {
		t.Fatalf("exceptions=%+v, want destination_shortage for pregnant destination shed", projected.Exceptions)
	}

	var csg8Status, csg8Evidence, csg8Blocker, csg10Status, csg10Evidence string
	if err := pool.QueryRow(ctx, `
SELECT status, evidence_ref, blocker_reason
FROM counts_shifting_readiness_subgates
WHERE tenant_id=$1::uuid AND subgate_id='CSG8'`, projectionReplayTenant).Scan(&csg8Status, &csg8Evidence, &csg8Blocker); err != nil {
		t.Fatalf("load CSG8 readiness: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT status, evidence_ref
FROM counts_shifting_readiness_subgates
WHERE tenant_id=$1::uuid AND subgate_id='CSG10'`, projectionReplayTenant).Scan(&csg10Status, &csg10Evidence); err != nil {
		t.Fatalf("load CSG10 readiness: %v", err)
	}
	if csg8Status != "pending" || !strings.Contains(csg8Evidence, first.SnapshotID) || !strings.Contains(csg8Blocker, "seeded local E2E") {
		t.Fatalf("CSG8 status=%s evidence=%s blocker=%q, want pending replayed snapshot evidence", csg8Status, csg8Evidence, csg8Blocker)
	}
	if csg10Status != "pending" || !strings.Contains(csg10Evidence, second.RunID) {
		t.Fatalf("CSG10 status=%s evidence=%s, want second recompute run evidence", csg10Status, csg10Evidence)
	}
}

const (
	projectionReplayTenant = "00000000-0000-4000-8000-000000000001"
	projectionReplayPark   = "65000000-0000-4000-8000-000000000001"
	projectionReplayShedA  = "65000000-0000-4000-8000-000000000002"
	projectionReplayShedB  = "65000000-0000-4000-8000-000000000003"
)

func seedProjectionReplayScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Mesha Projection Replay Test', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, projectionReplayTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT-PROJECTION-REPLAY', 'CPT Projection Replay', 'active')
ON CONFLICT (location_id) DO NOTHING`, projectionReplayTenant, projectionReplayPark); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ($3::uuid, $1::uuid, 'shed', 'CPT-PROJECTION-REPLAY-SOURCE', 'CPT Projection Replay Source', $2::uuid, 'active'),
  ($4::uuid, $1::uuid, 'shed', 'CPT-PROJECTION-REPLAY-DEST', 'CPT Projection Replay Destination', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`,
		projectionReplayTenant, projectionReplayPark, projectionReplayShedA, projectionReplayShedB); err != nil {
		t.Fatalf("seed sheds: %v", err)
	}
}

func projectionReplayBaseAnchor(shedID, key, fp string, count int32) countsdomain.BaseCountAnchor {
	return countsdomain.BaseCountAnchor{
		TenantID: projectionReplayTenant, ParkID: projectionReplayPark, ShedID: shedID,
		BreedKey: "beetal", BreedLabel: "Beetal",
		CountedAt:        time.Date(2026, 6, 30, 6, 0, 0, 0, time.UTC),
		HeadCount:        count,
		SourceSystem:     "physical_base_count",
		SourceRef:        "projection-replay-base-count:" + shedID,
		SourceHash:       "projection-replay-base-hash:" + shedID,
		DiscrepancyState: "not_checked",
		IdempotencyKey:   key, RequestFingerprint: fp,
	}
}

func projectionReplayPregnantShift() countsdomain.ShiftingEvent {
	stage := "pregnant"
	age := "adult"
	sex := "female"
	return countsdomain.ShiftingEvent{
		TenantID: projectionReplayTenant, LogicalShiftingEventKey: "projection-replay-pregnant-shift",
		Priority: "high", Category: "growth",
		SourceParkID: strPtr(projectionReplayPark), SourceShedID: strPtr(projectionReplayShedA),
		DestinationParkID: projectionReplayPark, DestinationShedID: projectionReplayShedB,
		RaisedAt:           time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
		EffectiveAt:        time.Date(2026, 6, 30, 13, 0, 0, 0, time.UTC),
		AuthorizationState: "authorized", VerificationState: "verified", EventStatus: "authorized",
		SourceSystem: "feed_shiftings_docx", SourceRef: "Feed, Shiftings and Count.docx:projection-replay-shift",
		PayloadHash: "projection-replay-shift-payload", IdempotencyKey: "projection-replay-shift-idem",
		RequestFingerprint: "projection-replay-shift-fp",
		Impacts: []countsdomain.ShiftingEventImpact{{
			GrainKey: "beetal:pregnant", BreedKey: "beetal", BreedLabel: "Beetal",
			StageTag: &stage, AgeClass: &age, Sex: &sex,
			HeadCount: 3, PregnantCount: 3, RiskFlagsJSON: []byte(`{"pregnant":true}`),
			RationContextResolutionState: "blocked",
			BlockerReason:                strPtr("destination shed ration context unresolved for pregnant cohort"),
		}},
	}
}

func projectionReplayCountRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func projectionReplayRowForShed(t *testing.T, rows []countsdomain.ProjectionRow, shedID string) countsdomain.ProjectionRow {
	t.Helper()
	for _, row := range rows {
		if row.ShedID == shedID {
			return row
		}
	}
	t.Fatalf("missing projection row for shed %s in %+v", shedID, rows)
	return countsdomain.ProjectionRow{}
}

func strPtr(v string) *string {
	return &v
}
