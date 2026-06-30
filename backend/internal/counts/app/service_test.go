package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

type fakeRepo struct {
	anchor domain.BaseCountAnchor
	event  domain.ShiftingEvent
	inputs domain.ProjectionInputs
	snap   domain.ProjectionSnapshot
}

func (f *fakeRepo) RecordBaseCountAnchor(_ context.Context, in domain.BaseCountAnchor) (string, bool, error) {
	f.anchor = in
	return "anchor-1", false, nil
}
func (f *fakeRepo) RecordShiftingEvent(_ context.Context, in domain.ShiftingEvent) (string, bool, error) {
	f.event = in
	return "event-1", false, nil
}
func (f *fakeRepo) ProjectionInputs(context.Context, domain.ProjectionRecomputeRequest) (domain.ProjectionInputs, error) {
	return f.inputs, nil
}
func (f *fakeRepo) CreateProjectionSnapshot(_ context.Context, in domain.ProjectionSnapshot) (string, error) {
	f.snap = in
	return "snapshot-1", nil
}
func (f *fakeRepo) CountAsOf(_ context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error) {
	return domain.CountProjection{TenantID: req.TenantID, Horizon: "count_as_of", TotalRowCount: int64(req.Limit)}, nil
}
func (f *fakeRepo) ProjectedCountFor(_ context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error) {
	return domain.CountProjection{TenantID: req.TenantID, Horizon: "feed_target_date", TotalRowCount: int64(req.Limit)}, nil
}
func (f *fakeRepo) Readiness(context.Context, string) (domain.Readiness, error) {
	return domain.Readiness{}, nil
}

func TestRecordBaseCountAnchorDefaultsPhysicalSource(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	id, replay, err := svc.RecordBaseCountAnchor(context.Background(), domain.BaseCountAnchor{
		TenantID: "tenant", ParkID: "park", ShedID: "shed", BreedKey: "beetal", BreedLabel: "Beetal",
		CountedAt: time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		HeadCount: 42, SourceRef: "base-count-1", SourceHash: "hash-1",
		IdempotencyKey: "idem-1", RequestFingerprint: "fp-1",
	})
	if err != nil || replay || id != "anchor-1" {
		t.Fatalf("RecordBaseCountAnchor id=%q replay=%v err=%v", id, replay, err)
	}
	if repo.anchor.SourceSystem != "physical_base_count" {
		t.Fatalf("source system=%q", repo.anchor.SourceSystem)
	}
	if repo.anchor.DiscrepancyState != "not_checked" {
		t.Fatalf("discrepancy state=%q", repo.anchor.DiscrepancyState)
	}
}

func TestRecordBaseCountAnchorRejectsNegativeCount(t *testing.T) {
	_, _, err := NewService(&fakeRepo{}).RecordBaseCountAnchor(context.Background(), domain.BaseCountAnchor{
		TenantID: "tenant", ParkID: "park", ShedID: "shed", BreedKey: "beetal", BreedLabel: "Beetal",
		CountedAt: time.Now(), HeadCount: -1, SourceRef: "base-count-1", SourceHash: "hash-1",
		IdempotencyKey: "idem-1", RequestFingerprint: "fp-1",
	})
	if !errors.Is(err, ErrInvalidCount) {
		t.Fatalf("err=%v, want ErrInvalidCount", err)
	}
}

func TestRecordShiftingEventRequiresStructuredImpact(t *testing.T) {
	_, _, err := NewService(&fakeRepo{}).RecordShiftingEvent(context.Background(), domain.ShiftingEvent{
		TenantID: "tenant", LogicalShiftingEventKey: "shift-1", DestinationParkID: "park", DestinationShedID: "shed",
		RaisedAt: time.Now(), EffectiveAt: time.Now(), SourceRef: "shift-report-1",
		PayloadHash: "hash-1", IdempotencyKey: "idem-1", RequestFingerprint: "fp-1",
	})
	if !errors.Is(err, ErrMissingImpact) {
		t.Fatalf("err=%v, want ErrMissingImpact", err)
	}
}

func TestRecordShiftingEventRejectsInvalidHighRiskCounts(t *testing.T) {
	_, _, err := NewService(&fakeRepo{}).RecordShiftingEvent(context.Background(), domain.ShiftingEvent{
		TenantID: "tenant", LogicalShiftingEventKey: "shift-1", DestinationParkID: "park", DestinationShedID: "shed",
		RaisedAt: time.Now(), EffectiveAt: time.Now(), SourceRef: "shift-report-1",
		PayloadHash: "hash-1", IdempotencyKey: "idem-1", RequestFingerprint: "fp-1",
		Impacts: []domain.ShiftingEventImpact{{
			GrainKey: "shed:breed", BreedKey: "beetal", BreedLabel: "Beetal", HeadCount: 2, PregnantCount: 3,
		}},
	})
	if !errors.Is(err, ErrInvalidCount) {
		t.Fatalf("err=%v, want ErrInvalidCount", err)
	}
}

func TestRecordShiftingEventRejectsInvalidRiskFlagsJSON(t *testing.T) {
	_, _, err := NewService(&fakeRepo{}).RecordShiftingEvent(context.Background(), domain.ShiftingEvent{
		TenantID: "tenant", LogicalShiftingEventKey: "shift-1", DestinationParkID: "park", DestinationShedID: "shed",
		RaisedAt: time.Now(), EffectiveAt: time.Now(), SourceRef: "shift-report-1",
		PayloadHash: "hash-1", IdempotencyKey: "idem-1", RequestFingerprint: "fp-1",
		Impacts: []domain.ShiftingEventImpact{{
			GrainKey: "shed:breed:pregnant", BreedKey: "beetal", BreedLabel: "Beetal",
			HeadCount: 3, PregnantCount: 3, RiskFlagsJSON: []byte(`["pregnant"]`),
		}},
	})
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("err=%v, want ErrInvalidJSON", err)
	}
}

func TestCreateProjectionSnapshotDefaultsBlocked(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	id, err := svc.CreateProjectionSnapshot(context.Background(), domain.ProjectionSnapshot{
		TenantID: "tenant", ParkID: "park", TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		AsOf:                  time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		SourceContractVersion: "counts-shifting-v1", SourceHash: "hash-1",
		BaseAnchorIDsHash: "anchors", ShiftingEventIDsHash: "shifts", GeneratedBy: "test",
	})
	if err != nil || id != "snapshot-1" {
		t.Fatalf("CreateProjectionSnapshot id=%q err=%v", id, err)
	}
	if repo.snap.Horizon != "feed_target_date" {
		t.Fatalf("horizon=%q", repo.snap.Horizon)
	}
	if repo.snap.ProjectionStatus != "blocked" {
		t.Fatalf("projection status=%q", repo.snap.ProjectionStatus)
	}
}

func TestCreateProjectionSnapshotRequiresRowSourceProvenance(t *testing.T) {
	_, err := NewService(&fakeRepo{}).CreateProjectionSnapshot(context.Background(), domain.ProjectionSnapshot{
		TenantID: "tenant", ParkID: "park", TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		AsOf:                  time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		SourceContractVersion: "counts-shifting-v1", SourceHash: "hash-1",
		BaseAnchorIDsHash: "anchors", ShiftingEventIDsHash: "shifts", GeneratedBy: "test",
		Rows: []domain.ProjectionRow{{
			ParkID: "park", ShedID: "shed", GrainKey: "shed:breed", BreedKey: "beetal", BreedLabel: "Beetal",
			HeadCount: 1, SourceRowHash: "row-hash-1",
		}},
	})
	if !errors.Is(err, ErrMissingRequiredField) {
		t.Fatalf("err=%v, want ErrMissingRequiredField", err)
	}
}

func TestCreateProjectionSnapshotRejectsInvalidExceptionEvidence(t *testing.T) {
	_, err := NewService(&fakeRepo{}).CreateProjectionSnapshot(context.Background(), domain.ProjectionSnapshot{
		TenantID: "tenant", ParkID: "park", TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		AsOf:                  time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		SourceContractVersion: "counts-shifting-v1", SourceHash: "hash-1",
		BaseAnchorIDsHash: "anchors", ShiftingEventIDsHash: "shifts", GeneratedBy: "test",
		Exceptions: []domain.ProjectionException{{
			ExceptionType: "destination_shortage", SourceKey: "shift-1", GrainKey: "shed:breed:pregnant",
			BlockerReason: "pregnant destination shed shortage", EvidenceJSON: []byte(`true`),
		}},
	})
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("err=%v, want ErrInvalidJSON", err)
	}
}

func TestCreateProjectionSnapshotRejectsExceptionWithoutBlocker(t *testing.T) {
	_, err := NewService(&fakeRepo{}).CreateProjectionSnapshot(context.Background(), domain.ProjectionSnapshot{
		TenantID: "tenant", ParkID: "park", TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		AsOf:                  time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		SourceContractVersion: "counts-shifting-v1", SourceHash: "hash-1",
		BaseAnchorIDsHash: "anchors", ShiftingEventIDsHash: "shifts", GeneratedBy: "test",
		Exceptions: []domain.ProjectionException{{
			ExceptionType: "destination_shortage", SourceKey: "shift-1", GrainKey: "shed:breed:pregnant",
		}},
	})
	if !errors.Is(err, ErrMissingRequiredField) {
		t.Fatalf("err=%v, want ErrMissingRequiredField", err)
	}
}

func TestProjectedCountForDefaultsLimitAndRejectsUnboundedLimit(t *testing.T) {
	repo := &fakeRepo{}
	got, err := NewService(repo).ProjectedCountFor(context.Background(), domain.CountProjectionRequest{
		TenantID: "tenant", ParkID: "park", TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ProjectedCountFor default limit err=%v", err)
	}
	if got.TotalRowCount != int64(defaultProjectionLimit) {
		t.Fatalf("default limit row count marker=%d, want %d", got.TotalRowCount, defaultProjectionLimit)
	}
	_, err = NewService(repo).ProjectedCountFor(context.Background(), domain.CountProjectionRequest{
		TenantID: "tenant", ParkID: "park", TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit: maxProjectionLimit + 1,
	})
	if !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("err=%v, want ErrInvalidLimit", err)
	}
}

func TestRecomputeProjectionSnapshotBlocksPregnantDestinationShortage(t *testing.T) {
	stage := "pregnant"
	blocker := "destination shed ration context unresolved"
	repo := &fakeRepo{inputs: domain.ProjectionInputs{
		Anchors: []domain.ProjectionBaseAnchor{
			{BaseCountAnchorID: "anchor-source", ParkID: "park", ShedID: "shed-a", BreedKey: "beetal", BreedLabel: "Beetal", HeadCount: 20, SourceHash: "anchor-a"},
			{BaseCountAnchorID: "anchor-dest", ParkID: "park", ShedID: "shed-b", BreedKey: "beetal", BreedLabel: "Beetal", HeadCount: 5, SourceHash: "anchor-b"},
		},
		Movements: []domain.ProjectionMovementImpact{{
			ShiftingEventID: "shift-1", LogicalShiftingEventKey: "shift-key-1", SourceShedID: strPtr("shed-a"),
			DestinationShedID: "shed-b", EffectiveAt: time.Date(2026, 6, 30, 13, 0, 0, 0, time.UTC),
			GrainKey: "beetal:pregnant", BreedKey: "beetal", BreedLabel: "Beetal", StageTag: &stage,
			HeadCount: 3, PregnantCount: 3, RationContextResolutionState: "blocked", BlockerReason: &blocker,
		}},
	}}
	result, err := NewService(repo).RecomputeProjectionSnapshotWithResult(context.Background(), domain.ProjectionRecomputeRequest{
		TenantID: "tenant", ParkID: "park", Horizon: "feed_target_date",
		TargetDate:            time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		AsOf:                  time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		SourceContractVersion: "counts-shifting-v1", GeneratedBy: "test",
	})
	if err != nil || result.SnapshotID != "snapshot-1" {
		t.Fatalf("RecomputeProjectionSnapshot result=%+v err=%v", result, err)
	}
	if result.ProjectionStatus != "blocked" || result.RowCount != 2 || result.ExceptionCount != 3 {
		t.Fatalf("recompute result=%+v, want blocked with 2 rows and 3 exceptions", result)
	}
	if repo.snap.ProjectionStatus != "blocked" {
		t.Fatalf("projection status=%q, want blocked", repo.snap.ProjectionStatus)
	}
	byGrain := map[string]domain.ProjectionRow{}
	for _, row := range repo.snap.Rows {
		byGrain[row.GrainKey] = row
	}
	if byGrain["shed-a:beetal"].HeadCount != 17 {
		t.Fatalf("source head_count=%d, want 17", byGrain["shed-a:beetal"].HeadCount)
	}
	dest := byGrain["shed-b:beetal"]
	if dest.HeadCount != 8 || dest.PregnantCount != 3 || dest.BlockerReason == nil || *dest.BlockerReason != blocker {
		t.Fatalf("destination row=%+v, want blocked 8 total / 3 pregnant", dest)
	}
	found := false
	for _, ex := range repo.snap.Exceptions {
		if ex.ExceptionType == "destination_shortage" && ex.Severity == "critical" {
			found = true
		}
	}
	if !found {
		t.Fatalf("exceptions=%+v, want critical destination_shortage", repo.snap.Exceptions)
	}
}

func TestRecomputeProjectionSnapshotFailsClosedWithoutBaseAnchors(t *testing.T) {
	repo := &fakeRepo{}
	id, err := NewService(repo).RecomputeProjectionSnapshot(context.Background(), domain.ProjectionRecomputeRequest{
		TenantID: "tenant", ParkID: "park", Horizon: "feed_target_date",
		TargetDate:            time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		AsOf:                  time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		SourceContractVersion: "counts-shifting-v1", GeneratedBy: "test",
	})
	if err != nil || id != "snapshot-1" {
		t.Fatalf("RecomputeProjectionSnapshot id=%q err=%v", id, err)
	}
	if repo.snap.ProjectionStatus != "blocked" || len(repo.snap.Rows) != 0 {
		t.Fatalf("snapshot status=%q rows=%d, want blocked empty rows", repo.snap.ProjectionStatus, len(repo.snap.Rows))
	}
	if len(repo.snap.Exceptions) != 1 || repo.snap.Exceptions[0].ExceptionType != "missing_base_count" {
		t.Fatalf("exceptions=%+v, want missing_base_count", repo.snap.Exceptions)
	}
}

func strPtr(s string) *string { return &s }
