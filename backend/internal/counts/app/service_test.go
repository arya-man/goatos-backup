package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

type fakeRepo struct {
	anchor     domain.BaseCountAnchor
	event      domain.ShiftingEvent
	inputs     domain.ProjectionInputs
	inputsErr  error
	snap       domain.ProjectionSnapshot
	createErr  error
	beginReq   domain.ProjectionRecomputeRequest
	finishRun  string
	finish     domain.ProjectionRecomputeResult
	finishErr  error
	scanReq    domain.CountMismatchScanRequest
	query      domain.ProjectionExceptionQuery
	resolution domain.ProjectionExceptionResolutionRequest
}

func (f *fakeRepo) RecordBaseCountAnchor(_ context.Context, in domain.BaseCountAnchor) (string, bool, error) {
	f.anchor = in
	return "anchor-1", false, nil
}
func (f *fakeRepo) RecordShiftingEvent(_ context.Context, in domain.ShiftingEvent) (string, bool, error) {
	f.event = in
	return "event-1", false, nil
}
func (f *fakeRepo) ScanCountMismatches(_ context.Context, req domain.CountMismatchScanRequest) (domain.CountMismatchScanResult, error) {
	f.scanReq = req
	return domain.CountMismatchScanResult{TenantID: req.TenantID, ScannedAnchorCount: req.Limit}, nil
}
func (f *fakeRepo) BeginProjectionRecomputeRun(_ context.Context, req domain.ProjectionRecomputeRequest) (string, error) {
	f.beginReq = req
	return "run-1", nil
}
func (f *fakeRepo) FinishProjectionRecomputeRun(_ context.Context, runID string, result domain.ProjectionRecomputeResult, recomputeErr error) error {
	f.finishRun = runID
	f.finish = result
	f.finishErr = recomputeErr
	return nil
}
func (f *fakeRepo) ProjectionInputs(context.Context, domain.ProjectionRecomputeRequest) (domain.ProjectionInputs, error) {
	if f.inputsErr != nil {
		return domain.ProjectionInputs{}, f.inputsErr
	}
	return f.inputs, nil
}
func (f *fakeRepo) CreateProjectionSnapshot(_ context.Context, in domain.ProjectionSnapshot) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.snap = in
	return "snapshot-1", nil
}
func (f *fakeRepo) CountAsOf(_ context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error) {
	return domain.CountProjection{TenantID: req.TenantID, Horizon: "count_as_of", TotalRowCount: int64(req.Limit)}, nil
}
func (f *fakeRepo) ProjectedCountFor(_ context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error) {
	return domain.CountProjection{TenantID: req.TenantID, Horizon: "feed_target_date", TotalRowCount: int64(req.Limit)}, nil
}
func (f *fakeRepo) ListProjectionExceptions(_ context.Context, req domain.ProjectionExceptionQuery) (domain.ProjectionExceptionList, error) {
	f.query = req
	return domain.ProjectionExceptionList{Items: []domain.ProjectionException{{
		ProjectionExceptionID: "77000000-0000-4000-8000-000000000001",
		ExceptionType:         "destination_shortage",
		SourceKey:             "shift-key-1",
		GrainKey:              "shed-b:beetal:pregnant",
		Severity:              "critical",
		Status:                req.Status,
		BlockerReason:         "destination shed ration context unresolved",
	}}}, nil
}
func (f *fakeRepo) ResolveProjectionException(_ context.Context, in domain.ProjectionExceptionResolutionRequest) (domain.ProjectionExceptionResolution, error) {
	f.resolution = in
	return domain.ProjectionExceptionResolution{
		ProjectionExceptionResolutionID: "resolution-1",
		ProjectionExceptionID:           in.ProjectionExceptionID,
		Action:                          in.Action,
		Status:                          "resolved",
		WorkState:                       "resolved",
		ResolvedByRef:                   in.ResolvedByRef,
		ResolutionReason:                in.ResolutionReason,
		ResolutionRef:                   in.ResolutionRef,
		ResolvedAt:                      time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
	}, nil
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

func TestCreateProjectionSnapshotDefaultsExceptionWorkFields(t *testing.T) {
	repo := &fakeRepo{}
	asOf := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	_, err := NewService(repo).CreateProjectionSnapshot(context.Background(), domain.ProjectionSnapshot{
		TenantID: "tenant", ParkID: "park", TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		AsOf:                  asOf,
		SourceContractVersion: "counts-shifting-v1", SourceHash: "hash-1",
		BaseAnchorIDsHash: "anchors", ShiftingEventIDsHash: "shifts", GeneratedBy: "test",
		Exceptions: []domain.ProjectionException{{
			ExceptionType: "destination_shortage", SourceKey: "shift-1", GrainKey: "shed:breed:pregnant",
			Severity: "critical", BlockerReason: "pregnant destination shortage",
		}},
	})
	if err != nil {
		t.Fatalf("CreateProjectionSnapshot err=%v", err)
	}
	if len(repo.snap.Exceptions) != 1 {
		t.Fatalf("exceptions=%d, want 1", len(repo.snap.Exceptions))
	}
	ex := repo.snap.Exceptions[0]
	if ex.WorkType != "counts_projection_exception" || ex.WorkState != "blocked" {
		t.Fatalf("work defaults=%+v", ex)
	}
	if !ex.DueAt.Equal(asOf) {
		t.Fatalf("due_at=%s, want %s", ex.DueAt, asOf)
	}
	if ex.NextAction != "Resolve destination ration context before Feed generation" {
		t.Fatalf("next_action=%q", ex.NextAction)
	}
	if ex.EvidenceLink != "/feed-direction/counts-projection/exceptions/shift-1" {
		t.Fatalf("evidence_link=%q", ex.EvidenceLink)
	}
}

func TestResolveProjectionExceptionNormalizesAndPassesThrough(t *testing.T) {
	ref := "  shift-report:123  "
	repo := &fakeRepo{}
	out, err := NewService(repo).ResolveProjectionException(context.Background(), domain.ProjectionExceptionResolutionRequest{
		TenantID:              " tenant ",
		ProjectionExceptionID: " exception-1 ",
		Action:                " RESOLVE ",
		ResolvedByRef:         " feed-director:ravi ",
		ResolutionReason:      " reviewed pregnant cohort ration context ",
		ResolutionRef:         &ref,
		IdempotencyKey:        " resolve-exception-1 ",
		RequestFingerprint:    " fp-1 ",
	})
	if err != nil {
		t.Fatalf("ResolveProjectionException err=%v", err)
	}
	if out.Action != "resolve" || out.Status != "resolved" {
		t.Fatalf("resolution=%+v", out)
	}
	if repo.resolution.TenantID != "tenant" || repo.resolution.ProjectionExceptionID != "exception-1" ||
		repo.resolution.ResolutionRef == nil || *repo.resolution.ResolutionRef != "shift-report:123" {
		t.Fatalf("normalized request=%+v", repo.resolution)
	}
}

func TestResolveProjectionExceptionRejectsBadEnvelope(t *testing.T) {
	_, err := NewService(&fakeRepo{}).ResolveProjectionException(context.Background(), domain.ProjectionExceptionResolutionRequest{
		TenantID: "tenant", ProjectionExceptionID: "exception-1", Action: "approve",
		ResolvedByRef: "feed-director:ravi", ResolutionReason: "reviewed", IdempotencyKey: "idem", RequestFingerprint: "fp",
	})
	if !errors.Is(err, ErrInvalidResolutionAction) {
		t.Fatalf("err=%v, want ErrInvalidResolutionAction", err)
	}
	_, err = NewService(&fakeRepo{}).ResolveProjectionException(context.Background(), domain.ProjectionExceptionResolutionRequest{
		TenantID: "tenant", ProjectionExceptionID: "exception-1", Action: "dismiss",
		ResolvedByRef: "feed-director:ravi", IdempotencyKey: "idem", RequestFingerprint: "fp",
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

func TestListProjectionExceptionsDefaultsAndValidatesFilters(t *testing.T) {
	repo := &fakeRepo{}
	severity := " CRITICAL "
	workState := " BLOCKED "
	out, err := NewService(repo).ListProjectionExceptions(context.Background(), domain.ProjectionExceptionQuery{
		TenantID: " tenant ", Severity: &severity, WorkState: &workState,
	})
	if err != nil {
		t.Fatalf("ListProjectionExceptions err=%v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items=%d, want 1", len(out.Items))
	}
	if repo.query.TenantID != "tenant" || repo.query.Status != "open" ||
		repo.query.Severity == nil || *repo.query.Severity != "critical" ||
		repo.query.WorkState == nil || *repo.query.WorkState != "blocked" ||
		repo.query.Limit != defaultProjectionExceptionLimit {
		t.Fatalf("normalized query=%+v", repo.query)
	}

	_, err = NewService(repo).ListProjectionExceptions(context.Background(), domain.ProjectionExceptionQuery{
		TenantID: "tenant", Status: "all",
	})
	if !errors.Is(err, ErrInvalidExceptionFilter) {
		t.Fatalf("status err=%v, want ErrInvalidExceptionFilter", err)
	}
	_, err = NewService(repo).ListProjectionExceptions(context.Background(), domain.ProjectionExceptionQuery{
		TenantID: "tenant", Limit: maxProjectionExceptionLimit + 1,
	})
	if !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("limit err=%v, want ErrInvalidLimit", err)
	}
}

func TestScanCountMismatchesDefaultsAndValidatesWindow(t *testing.T) {
	repo := &fakeRepo{}
	after := time.Date(2026, 6, 1, 0, 0, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
	before := time.Date(2026, 6, 30, 23, 59, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
	out, err := NewService(repo).ScanCountMismatches(context.Background(), domain.CountMismatchScanRequest{
		TenantID: " tenant ", CountedAfter: &after, CountedBefore: before,
	})
	if err != nil {
		t.Fatalf("ScanCountMismatches err=%v", err)
	}
	if out.ScannedAnchorCount != defaultCountMismatchScanLimit {
		t.Fatalf("scan result=%+v, want default limit marker %d", out, defaultCountMismatchScanLimit)
	}
	if repo.scanReq.TenantID != "tenant" || repo.scanReq.Limit != defaultCountMismatchScanLimit ||
		repo.scanReq.CountedAfter == nil || repo.scanReq.CountedAfter.Location() != time.UTC ||
		repo.scanReq.CountedBefore.Location() != time.UTC {
		t.Fatalf("normalized scan request=%+v", repo.scanReq)
	}

	_, err = NewService(repo).ScanCountMismatches(context.Background(), domain.CountMismatchScanRequest{
		TenantID: "tenant", CountedAfter: &before, CountedBefore: after,
	})
	if !errors.Is(err, ErrInvalidScanWindow) {
		t.Fatalf("window err=%v, want ErrInvalidScanWindow", err)
	}
	_, err = NewService(repo).ScanCountMismatches(context.Background(), domain.CountMismatchScanRequest{
		TenantID: "tenant", CountedBefore: before, Limit: maxCountMismatchScanLimit + 1,
	})
	if !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("limit err=%v, want ErrInvalidLimit", err)
	}
	cursorAt := before
	_, err = NewService(repo).ScanCountMismatches(context.Background(), domain.CountMismatchScanRequest{
		TenantID: "tenant", CountedBefore: before, CursorCountedAt: &cursorAt,
	})
	if !errors.Is(err, ErrMissingRequiredField) {
		t.Fatalf("cursor err=%v, want ErrMissingRequiredField", err)
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
	if result.RunID != "run-1" || repo.finishRun != "run-1" || repo.finishErr != nil {
		t.Fatalf("run evidence result=%+v finish=%s finish_err=%v", result, repo.finishRun, repo.finishErr)
	}
	if repo.finish.SnapshotID != "snapshot-1" || repo.finish.RowCount != 2 || repo.finish.ExceptionCount != 3 {
		t.Fatalf("finish result=%+v, want snapshot/row/exception counts", repo.finish)
	}
}

func TestRecomputeProjectionSnapshotAppliesCrossParkMovementOnlyForRequestedPark(t *testing.T) {
	stage := "pregnant"
	sourcePark := "park-source"
	destinationPark := "park-destination"
	repo := &fakeRepo{inputs: domain.ProjectionInputs{
		Anchors: []domain.ProjectionBaseAnchor{{
			BaseCountAnchorID: "anchor-source", ParkID: sourcePark, ShedID: "shed-a",
			BreedKey: "beetal", BreedLabel: "Beetal", HeadCount: 20, SourceHash: "anchor-a",
		}},
		Movements: []domain.ProjectionMovementImpact{{
			ShiftingEventID: "shift-out", LogicalShiftingEventKey: "shift-key-out",
			SourceParkID: strPtr(sourcePark), SourceShedID: strPtr("shed-a"),
			DestinationParkID: destinationPark, DestinationShedID: "shed-other",
			EffectiveAt: time.Date(2026, 6, 30, 13, 0, 0, 0, time.UTC),
			GrainKey:    "beetal:pregnant", BreedKey: "beetal", BreedLabel: "Beetal", StageTag: &stage,
			HeadCount: 3, PregnantCount: 3, RationContextResolutionState: "blocked",
			BlockerReason: strPtr("destination park is handled by its own projection"),
		}},
	}}
	result, err := NewService(repo).RecomputeProjectionSnapshotWithResult(context.Background(), domain.ProjectionRecomputeRequest{
		TenantID: "tenant", ParkID: sourcePark, Horizon: "feed_target_date",
		TargetDate:            time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		AsOf:                  time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		SourceContractVersion: "counts-shifting-v1", GeneratedBy: "test",
	})
	if err != nil || result.SnapshotID != "snapshot-1" {
		t.Fatalf("RecomputeProjectionSnapshot result=%+v err=%v", result, err)
	}
	if result.RowCount != 1 || result.ExceptionCount != 1 {
		t.Fatalf("result=%+v, want only source park row plus its base ration exception", result)
	}
	row := repo.snap.Rows[0]
	if row.ShedID != "shed-a" || row.HeadCount != 17 {
		t.Fatalf("source row=%+v, want source-only subtraction", row)
	}
	for _, ex := range repo.snap.Exceptions {
		if ex.SourceKey == "shift-key-out" || ex.ExceptionType == "destination_shortage" {
			t.Fatalf("exception=%+v, want no destination exception in source park projection", ex)
		}
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

func TestRecomputeProjectionSnapshotRecordsFailedRunEvidence(t *testing.T) {
	repo := &fakeRepo{inputsErr: errors.New("input lookup failed")}
	result, err := NewService(repo).RecomputeProjectionSnapshotWithResult(context.Background(), domain.ProjectionRecomputeRequest{
		TenantID: "tenant", ParkID: "park", Horizon: "feed_target_date",
		TargetDate:            time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		AsOf:                  time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		SourceContractVersion: "counts-shifting-v1", GeneratedBy: "test",
	})
	if err == nil || !strings.Contains(err.Error(), "input lookup failed") {
		t.Fatalf("err=%v, want input lookup failure", err)
	}
	if result.RunID != "run-1" || repo.finishRun != "run-1" || repo.finishErr == nil ||
		!strings.Contains(repo.finishErr.Error(), "input lookup failed") {
		t.Fatalf("failed run evidence result=%+v finish_run=%s finish_err=%v", result, repo.finishRun, repo.finishErr)
	}
	if repo.finish.SnapshotID != "" || repo.finish.RowCount != 0 || repo.finish.ExceptionCount != 0 {
		t.Fatalf("failed finish result=%+v, want empty snapshot counts", repo.finish)
	}
}

func strPtr(s string) *string { return &s }
