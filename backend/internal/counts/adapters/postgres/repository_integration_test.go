package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
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
	again, replay, err := repo.RecordBaseCountAnchor(ctx, in)
	if err != nil || !replay || again != id {
		t.Fatalf("replay anchor id=%q replay=%v err=%v, want id=%q replay=true", again, replay, err, id)
	}
	in.RequestFingerprint = "different-fp"
	if _, _, err := repo.RecordBaseCountAnchor(ctx, in); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("different fingerprint err=%v, want ErrIdempotencyConflict", err)
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
	again, replay, err := repo.RecordShiftingEvent(ctx, in)
	if err != nil || !replay || again != id {
		t.Fatalf("shift replay id=%q replay=%v err=%v, want id=%q replay=true", again, replay, err, id)
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

func baseAnchor(key, fp string) domain.BaseCountAnchor {
	return domain.BaseCountAnchor{
		TenantID: countsTenant, ParkID: countsPark, ShedID: countsShedA,
		BreedKey: "beetal", BreedLabel: "Beetal", CountedAt: time.Date(2026, 6, 30, 6, 0, 0, 0, time.UTC),
		HeadCount: 20, SourceSystem: "physical_base_count", SourceRef: "base-count:cpt-s1:2026-06-30",
		SourceHash: "base-hash-1", DiscrepancyState: "not_checked", IdempotencyKey: key, RequestFingerprint: fp,
	}
}

func shiftingEvent(logicalKey, idemKey, payloadHash, fp string) domain.ShiftingEvent {
	stage := "pregnant"
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
			GrainKey: "beetal:pregnant", BreedKey: "beetal", BreedLabel: "Beetal", StageTag: &stage,
			HeadCount: 3, PregnantCount: 3, RiskFlagsJSON: []byte(`{"pregnant":true}`),
			RationContextResolutionState: "blocked", BlockerReason: strPtr("destination shed ration context unresolved"),
		}},
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

func strPtr(s string) *string { return &s }
