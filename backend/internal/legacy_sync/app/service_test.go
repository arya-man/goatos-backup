package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/legacy_sync/domain"
	"github.com/vgoats/goatos/backend/internal/legacy_sync/ports"
)

const (
	testTenant = "00000000-0000-4000-8000-000000000001"
	testActor  = "90000000-0000-4000-8000-000000000101"
	testRunID  = "70000000-0000-4000-8000-000000000001"
)

func TestCreateRunDryRunColdStartCompletesWithoutCounterMutation(t *testing.T) {
	repo := &fakeRepo{
		sources: []domain.Source{criticalSource("phase1_lifecycle_event_evidence", domain.DomainLifecycle, domain.FreshnessUnknown)},
		counter: domain.CounterStatus{FreshnessStatus: domain.FreshnessUnknown, StatusReason: "not rebuilt"},
	}
	service := NewService(repo, "prod")

	result, err := service.CreateRun(context.Background(), testTenant, testActor, domain.CreateRunRequest{
		Mode:   domain.ModeDryRun,
		Domain: domain.DomainLifecycle,
	}, "trace-1")
	if err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if result.Run.Status != domain.StatusCompleted {
		t.Fatalf("dry-run status=%s want completed", result.Run.Status)
	}
	if !result.Run.ColdStart {
		t.Fatal("dry-run without watermarks should be marked cold_start")
	}
	if result.Run.CountersRebuilt {
		t.Fatal("dry-run must not claim counters were rebuilt")
	}
	if repo.created.Mode != domain.ModeDryRun || repo.created.CounterCheckStatus != domain.CounterCheckSkipped {
		t.Fatalf("unexpected created run: %#v", repo.created)
	}
}

func TestCreateRunExecuteBlocksOutsideLocalDev(t *testing.T) {
	repo := &fakeRepo{
		sources: []domain.Source{criticalSource("phase1_lifecycle_event_evidence", domain.DomainLifecycle, domain.FreshnessGreen)},
		counter: domain.CounterStatus{FreshnessStatus: domain.FreshnessGreen, StatusReason: "ok"},
	}
	service := NewService(repo, "prod")

	result, err := service.CreateRun(context.Background(), testTenant, testActor, domain.CreateRunRequest{
		Mode:   domain.ModeExecute,
		Domain: domain.DomainLifecycle,
	}, "trace-1")
	if err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if result.Run.Status != domain.StatusBlocked {
		t.Fatalf("execute status=%s want blocked", result.Run.Status)
	}
	if result.Run.BlockedReason == nil || *result.Run.BlockedReason == "" {
		t.Fatal("blocked execute should carry a reason")
	}
}

func TestCreateRunExecuteBlocksStaleCriticalSource(t *testing.T) {
	repo := &fakeRepo{
		sources: []domain.Source{criticalSource("phase1_lifecycle_event_evidence", domain.DomainLifecycle, domain.FreshnessRed)},
		counter: domain.CounterStatus{FreshnessStatus: domain.FreshnessGreen, StatusReason: "ok"},
	}
	service := NewService(repo, "local")

	result, err := service.CreateRun(context.Background(), testTenant, testActor, domain.CreateRunRequest{
		Mode:   domain.ModeExecute,
		Domain: domain.DomainLifecycle,
	}, "trace-1")
	if err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if result.Run.Status != domain.StatusBlocked {
		t.Fatalf("execute status=%s want blocked", result.Run.Status)
	}
	if result.Run.CountersRebuilt {
		t.Fatal("blocked stale-source execute must not claim counter rebuild")
	}
}

func TestCreateRunExecuteBlocksCounterRebuildRequirement(t *testing.T) {
	repo := &fakeRepo{
		sources: []domain.Source{criticalSource("phase1_lifecycle_event_evidence", domain.DomainLifecycle, domain.FreshnessGreen)},
		counter: domain.CounterStatus{FreshnessStatus: domain.FreshnessRed, StatusReason: "rebuild_required", RebuildRequired: true},
	}
	service := NewService(repo, "dev")

	result, err := service.CreateRun(context.Background(), testTenant, testActor, domain.CreateRunRequest{
		Mode:   domain.ModeExecute,
		Domain: domain.DomainLifecycle,
	}, "trace-1")
	if err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if result.Run.Status != domain.StatusBlocked {
		t.Fatalf("execute status=%s want blocked", result.Run.Status)
	}
	if result.Run.CounterCheckStatus != domain.CounterCheckFailed {
		t.Fatalf("counter check=%s want failed", result.Run.CounterCheckStatus)
	}
}

func TestCreateRunExecuteLocalHealthyIsBlockedUntilExecutorExists(t *testing.T) {
	repo := &fakeRepo{
		sources:      []domain.Source{criticalSource("phase1_lifecycle_event_evidence", domain.DomainLifecycle, domain.FreshnessYellow)},
		counter:      domain.CounterStatus{FreshnessStatus: domain.FreshnessGreen, StatusReason: "ok"},
		hasWatermark: true,
	}
	service := NewService(repo, "local")

	result, err := service.CreateRun(context.Background(), testTenant, testActor, domain.CreateRunRequest{
		Mode:   domain.ModeExecute,
		Domain: domain.DomainLifecycle,
	}, "trace-1")
	if err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if result.Run.Status != domain.StatusBlocked {
		t.Fatalf("execute status=%s want blocked", result.Run.Status)
	}
	if result.Run.CountersRebuilt {
		t.Fatal("execute v1 must not claim counters were rebuilt")
	}
	if result.Run.CounterCheckStatus != domain.CounterCheckSkipped {
		t.Fatalf("counter check=%s want skipped", result.Run.CounterCheckStatus)
	}
	if result.Run.BlockedReason == nil || !strings.Contains(*result.Run.BlockedReason, "not implemented") {
		t.Fatalf("blocked reason=%v want not implemented", result.Run.BlockedReason)
	}
	if result.Run.ColdStart {
		t.Fatal("existing watermark should not be a cold start")
	}
	foundApply := false
	for _, step := range repo.created.Steps {
		if step.StepName != "apply_delta" {
			continue
		}
		foundApply = true
		if step.Status != domain.StepBlocked {
			t.Fatalf("apply step status=%s want blocked", step.Status)
		}
		if step.Details["executor"] == "no_mutation_needed_for_empty_plan" {
			t.Fatal("execute v1 must not record a fake completed no-op executor")
		}
	}
	if !foundApply {
		t.Fatal("execute run should record an apply guard step")
	}
}

func TestCriticalFreshnessIgnoresNoncriticalRed(t *testing.T) {
	sources := []domain.Source{
		criticalSource("phase1_lifecycle_event_evidence", domain.DomainLifecycle, domain.FreshnessGreen),
		{
			SourceID:        "feed_loadwise_summary",
			Domain:          "feed",
			Criticality:     domain.CriticalityNoncritical,
			Enabled:         true,
			FreshnessStatus: domain.FreshnessRed,
		},
	}
	if got := CriticalFreshness(sources); got != domain.FreshnessGreen {
		t.Fatalf("CriticalFreshness=%s want green", got)
	}
}

func TestSourceFreshnessHonorsSixtyMinuteAndTwelveHourThresholds(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	sixtyWatermark := now.Add(-89 * time.Minute)
	twelveHourWatermark := now.Add(-14 * time.Hour)

	status, _ := SourceFreshness(now, domain.Source{
		SourceID:            "phase1_current_location_evidence",
		CadenceSeconds:      3600,
		GreenWithinSeconds:  5400,
		YellowWithinSeconds: 7200,
		SourceWatermarkAt:   &sixtyWatermark,
	})
	if status != domain.FreshnessGreen {
		t.Fatalf("60m source status=%s want green before 90m threshold", status)
	}

	status, _ = SourceFreshness(now, domain.Source{
		SourceID:            "phase1_active_count_parity",
		CadenceSeconds:      43200,
		GreenWithinSeconds:  46800,
		YellowWithinSeconds: 54000,
		SourceWatermarkAt:   &twelveHourWatermark,
	})
	if status != domain.FreshnessYellow {
		t.Fatalf("12h source status=%s want yellow before 15h threshold", status)
	}
}

func TestUnknownSourceIsNeverSilentlyGreen(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	watermark := now.Add(-time.Minute)
	status, reason := SourceFreshness(now, domain.Source{
		SourceID:          "unregistered_live_source",
		IsUnknownSource:   true,
		SourceWatermarkAt: &watermark,
	})
	if status == domain.FreshnessGreen {
		t.Fatal("unknown source must not be green")
	}
	if reason != domain.EvidenceReasonUnregisteredSource {
		t.Fatalf("unknown source reason=%s want %s", reason, domain.EvidenceReasonUnregisteredSource)
	}
}

func TestLegacyChangedAfterHumanReviewReasonIsRegistered(t *testing.T) {
	label, ok := domain.EvidenceReasonLabel(domain.EvidenceReasonLegacyChangedAfterHumanReview)
	if !ok {
		t.Fatal("legacy_changed_after_human_review should be registered")
	}
	if label == "" {
		t.Fatal("registered evidence reason should have a UI label")
	}
}

func criticalSource(sourceID, sourceDomain, freshness string) domain.Source {
	return domain.Source{
		SourceID:        sourceID,
		SourceName:      sourceID,
		Domain:          sourceDomain,
		Criticality:     domain.CriticalityCritical,
		Enabled:         true,
		FreshnessStatus: freshness,
		StatusReason:    freshness,
	}
}

type fakeRepo struct {
	sources      []domain.Source
	counter      domain.CounterStatus
	hasWatermark bool
	created      ports.CreateRunParams
}

func (f *fakeRepo) ListSources(_ context.Context, filter ports.SourceFilter) ([]domain.Source, error) {
	if filter.Domain == "" {
		return f.sources, nil
	}
	out := []domain.Source{}
	for _, source := range f.sources {
		if source.Domain == filter.Domain {
			out = append(out, source)
		}
	}
	return out, nil
}

func (f *fakeRepo) OverallCounterStatus(context.Context, string) (domain.CounterStatus, error) {
	return f.counter, nil
}

func (f *fakeRepo) HasSuccessWatermark(context.Context, string, string) (bool, error) {
	return f.hasWatermark, nil
}

func (f *fakeRepo) CreateRun(_ context.Context, params ports.CreateRunParams) (*domain.Run, error) {
	f.created = params
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	run := domain.Run{
		SyncRunID:          testRunID,
		Mode:               params.Mode,
		Domain:             params.Domain,
		Status:             params.Status,
		ColdStart:          params.ColdStart,
		SourceWindowStart:  params.SourceWindowStart,
		SourceWindowEnd:    params.SourceWindowEnd,
		ETASeconds:         params.ETASeconds,
		Summary:            params.Summary,
		CountersRebuilt:    params.CountersRebuilt,
		CounterCheckStatus: params.CounterCheckStatus,
		FreshnessStatus:    params.FreshnessStatus,
		BlockedReason:      params.BlockedReason,
		StartedAt:          now,
	}
	if params.Status == domain.StatusCompleted || params.Status == domain.StatusBlocked {
		run.CompletedAt = &now
	}
	return &run, nil
}

func (f *fakeRepo) ListRuns(context.Context, ports.RunFilter) ([]domain.Run, error) {
	return []domain.Run{}, nil
}

func (f *fakeRepo) GetRun(context.Context, string, string) (*domain.RunDetailResponse, error) {
	return nil, ports.ErrNotFound
}

func (f *fakeRepo) CancelRun(context.Context, string, string) (*domain.Run, error) {
	return &domain.Run{SyncRunID: testRunID, Status: domain.StatusCanceled}, nil
}
