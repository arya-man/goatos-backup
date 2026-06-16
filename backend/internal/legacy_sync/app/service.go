package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/legacy_sync/domain"
	"github.com/vgoats/goatos/backend/internal/legacy_sync/ports"
)

type Service struct {
	repo        ports.Repository
	environment string
	now         func() time.Time
}

func NewService(repo ports.Repository, environment string) *Service {
	return &Service{
		repo:        repo,
		environment: strings.TrimSpace(environment),
		now:         func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) ListSources(ctx context.Context, tenantID, selectedDomain, traceID string) (*domain.SourceListResponse, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	selectedDomain = normalizeDomainFilter(selectedDomain)
	if !validSourceDomainFilter(selectedDomain) {
		return nil, BadRequest("invalid_domain", "domain is not supported")
	}
	sources, err := s.repo.ListSources(ctx, ports.SourceFilter{TenantID: tenantID, Domain: selectedDomain})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if sources == nil {
		sources = []domain.Source{}
	}
	sources = s.applySourceFreshness(sources)
	return &domain.SourceListResponse{Items: sources, TraceID: traceID}, nil
}

func (s *Service) OverallStatus(ctx context.Context, tenantID, traceID string) (*domain.OverallStatusResponse, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	sources, err := s.repo.ListSources(ctx, ports.SourceFilter{TenantID: tenantID, Domain: ""})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	sources = s.applySourceFreshness(sources)
	counter, err := s.repo.OverallCounterStatus(ctx, tenantID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	critical := CriticalFreshness(sources)
	overall := WorstFreshness(critical, counter.FreshnessStatus)
	return &domain.OverallStatusResponse{
		OverallFreshness:  overall,
		CriticalFreshness: critical,
		CounterFreshness:  counter,
		Sources:           sources,
		TraceID:           traceID,
	}, nil
}

func (s *Service) ListRuns(ctx context.Context, tenantID string, limit int, traceID string) (*domain.RunListResponse, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 50 {
		return nil, BadRequest("invalid_limit", "limit must be between 1 and 50")
	}
	runs, err := s.repo.ListRuns(ctx, ports.RunFilter{TenantID: tenantID, Limit: limit})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if runs == nil {
		runs = []domain.Run{}
	}
	return &domain.RunListResponse{Items: runs, TraceID: traceID}, nil
}

func (s *Service) GetRun(ctx context.Context, tenantID, runID, traceID string) (*domain.RunDetailResponse, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, BadRequest("invalid_sync_run_id", "sync_run_id is required")
	}
	result, err := s.repo.GetRun(ctx, tenantID, runID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	result.TraceID = traceID
	return result, nil
}

func (s *Service) CreateRun(ctx context.Context, tenantID, actorID string, req domain.CreateRunRequest, traceID string) (*domain.CreateRunResponse, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(actorID) == "" {
		return nil, Unauthorized("missing_actor", "authenticated actor is required")
	}
	mode := normalizeMode(req.Mode)
	if !validMode(mode) {
		return nil, BadRequest("invalid_mode", "mode is not supported")
	}
	selectedDomain := normalizeRunDomain(req.Domain)
	if !validRunDomain(selectedDomain) {
		return nil, BadRequest("invalid_domain", "domain is not supported")
	}
	if req.SourceWindowStart != nil && req.SourceWindowEnd != nil && !req.SourceWindowStart.Before(*req.SourceWindowEnd) {
		return nil, BadRequest("invalid_source_window", "source_window_start must be before source_window_end")
	}

	sources, err := s.repo.ListSources(ctx, ports.SourceFilter{TenantID: tenantID, Domain: domainFilterForRun(selectedDomain)})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	sources = s.applySourceFreshness(sources)
	criticalFreshness := CriticalFreshness(sources)
	counter, err := s.repo.OverallCounterStatus(ctx, tenantID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	hasWatermark, err := s.repo.HasSuccessWatermark(ctx, tenantID, selectedDomain)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	coldStart := !hasWatermark
	blockedReason := s.blockedReason(mode, criticalFreshness, counter)
	status := domain.StatusCompleted
	counterCheck := domain.CounterCheckSkipped
	countersRebuilt := false
	if blockedReason != nil {
		status = domain.StatusBlocked
		counterCheck = counterCheckForBlock(counter)
	}

	steps := s.planSteps(sources, mode, coldStart, status, blockedReason, req.SourceWindowStart, req.SourceWindowEnd, counter)
	run, err := s.repo.CreateRun(ctx, ports.CreateRunParams{
		TenantID:           tenantID,
		ActorID:            actorID,
		Mode:               mode,
		Domain:             selectedDomain,
		Status:             status,
		ColdStart:          coldStart,
		SourceWindowStart:  req.SourceWindowStart,
		SourceWindowEnd:    req.SourceWindowEnd,
		Summary:            domain.RunSummary{},
		CountersRebuilt:    countersRebuilt,
		CounterCheckStatus: counterCheck,
		FreshnessStatus:    WorstFreshness(criticalFreshness, counter.FreshnessStatus),
		BlockedReason:      blockedReason,
		TraceID:            traceID,
		Steps:              steps,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.CreateRunResponse{Run: *run, TraceID: traceID}, nil
}

func (s *Service) CancelRun(ctx context.Context, tenantID, runID, traceID string) (*domain.CancelRunResponse, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	run, err := s.repo.CancelRun(ctx, tenantID, strings.TrimSpace(runID))
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.CancelRunResponse{Run: *run, TraceID: traceID}, nil
}

func (s *Service) blockedReason(mode, criticalFreshness string, counter domain.CounterStatus) *string {
	if mode == domain.ModeDryRun {
		return nil
	}
	if !localOrDev(s.environment) {
		reason := "execute is blocked outside local/dev until the production-safe legacy sync executor is configured"
		return &reason
	}
	if criticalFreshness == domain.FreshnessRed || criticalFreshness == domain.FreshnessUnknown {
		reason := "critical legacy source freshness is not green or yellow"
		return &reason
	}
	if counter.FreshnessStatus == domain.FreshnessRed || counter.RebuildRequired {
		reason := "identity counter rebuild is required before sync can mark dashboards fresh"
		return &reason
	}
	if mode == domain.ModeNightlyDeepReconcile {
		reason := "legacy sync nightly deep reconcile is not implemented in v1; run dry-run until the parity executor is configured"
		return &reason
	}
	reason := "legacy sync execute is not implemented in v1; run dry-run and use the approved replay tooling until the executor is configured"
	return &reason
}

func (s *Service) applySourceFreshness(sources []domain.Source) []domain.Source {
	now := s.now()
	for i := range sources {
		status, reason := SourceFreshness(now, sources[i])
		sources[i].FreshnessStatus = status
		sources[i].StatusReason = reason
	}
	return sources
}

func (s *Service) planSteps(sources []domain.Source, mode string, coldStart bool, runStatus string, blockedReason *string, windowStart, windowEnd *time.Time, counter domain.CounterStatus) []ports.CreateStepParams {
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		sourceIDs = append(sourceIDs, source.SourceID)
	}
	details := map[string]any{
		"cold_start": coldStart,
		"source_ids": sourceIDs,
	}
	if windowStart != nil && windowEnd != nil {
		details["bounded_window"] = true
	}
	if coldStart {
		details["cold_start_rule"] = "dry-runnable, explicit, bounded, auditable, resumable"
	}
	if mode == domain.ModeDryRun {
		details["mutation"] = "none"
	}
	steps := []ports.CreateStepParams{
		{
			StepName:  "source_window",
			Status:    domain.StepCompleted,
			Details:   details,
			Completed: true,
		},
		{
			StepName:  "plan_delta",
			Status:    domain.StepCompleted,
			Details:   map[string]any{"planner": "backend_owned_legacy_sync_v1", "rows_planned": 0},
			Completed: true,
		},
	}
	counterDetails := map[string]any{
		"counter_freshness": counter.FreshnessStatus,
		"rebuild_required":  counter.RebuildRequired,
		"status_reason":     counter.StatusReason,
	}
	if blockedReason != nil {
		counterDetails["blocked_reason"] = *blockedReason
	}
	counterStepStatus := domain.StepCompleted
	if runStatus == domain.StatusBlocked {
		counterStepStatus = domain.StepBlocked
	}
	steps = append(steps, ports.CreateStepParams{
		StepName:  "counter_guard",
		Status:    counterStepStatus,
		Details:   counterDetails,
		Completed: true,
	})
	if mode != domain.ModeDryRun {
		applyStatus := domain.StepBlocked
		applyDetails := map[string]any{"executor": "legacy_sync_v1_not_implemented", "mutation": "blocked_before_apply"}
		if runStatus == domain.StatusBlocked {
			if blockedReason != nil {
				applyDetails["blocked_reason"] = *blockedReason
			}
		} else {
			applyStatus = domain.StepPending
			applyDetails = map[string]any{"executor": "legacy_sync_executor_not_configured", "mutation": "not_started"}
		}
		steps = append(steps, ports.CreateStepParams{
			StepName:  "apply_delta",
			Status:    applyStatus,
			Details:   applyDetails,
			Completed: true,
		})
	}
	return steps
}

func CriticalFreshness(sources []domain.Source) string {
	statuses := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Criticality != domain.CriticalityCritical || !source.Enabled {
			continue
		}
		statuses = append(statuses, source.FreshnessStatus)
	}
	if len(statuses) == 0 {
		return domain.FreshnessUnknown
	}
	result := domain.FreshnessGreen
	for _, status := range statuses {
		result = WorstFreshness(result, status)
	}
	return result
}

func WorstFreshness(a, b string) string {
	rank := map[string]int{
		domain.FreshnessGreen:   0,
		domain.FreshnessYellow:  1,
		domain.FreshnessUnknown: 2,
		domain.FreshnessRed:     3,
	}
	if rank[normalizeFreshness(b)] > rank[normalizeFreshness(a)] {
		return normalizeFreshness(b)
	}
	return normalizeFreshness(a)
}

func SourceFreshness(now time.Time, source domain.Source) (string, string) {
	if source.KnownDegraded {
		return domain.FreshnessRed, "source is marked known degraded"
	}
	if source.IsUnknownSource {
		return domain.FreshnessUnknown, domain.EvidenceReasonUnregisteredSource
	}
	if source.SourceWatermarkAt == nil {
		return domain.FreshnessUnknown, "no successful watermark recorded"
	}
	age := int(now.Sub(*source.SourceWatermarkAt).Seconds())
	if age <= source.GreenWithinSeconds {
		return domain.FreshnessGreen, "source watermark is within green threshold"
	}
	if age <= source.YellowWithinSeconds {
		return domain.FreshnessYellow, "source watermark is within yellow threshold"
	}
	return domain.FreshnessRed, "source watermark is older than yellow threshold"
}

func normalizeFreshness(value string) string {
	switch value {
	case domain.FreshnessGreen, domain.FreshnessYellow, domain.FreshnessRed, domain.FreshnessUnknown:
		return value
	default:
		return domain.FreshnessUnknown
	}
}

func counterCheckForBlock(counter domain.CounterStatus) string {
	if counter.FreshnessStatus == domain.FreshnessRed || counter.RebuildRequired {
		return domain.CounterCheckFailed
	}
	return domain.CounterCheckSkipped
}

func requireTenant(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return Unauthorized("missing_tenant_scope", "tenant scope is required")
	}
	return nil
}

func normalizeMode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return domain.ModeDryRun
	}
	return value
}

func normalizeRunDomain(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return domain.DomainAll
	}
	return value
}

func normalizeDomainFilter(value string) string {
	return strings.TrimSpace(value)
}

func validMode(value string) bool {
	switch value {
	case domain.ModeDryRun, domain.ModeExecute, domain.ModeNightlyDeepReconcile:
		return true
	default:
		return false
	}
}

func validRunDomain(value string) bool {
	switch value {
	case domain.DomainAll, domain.DomainIdentity, domain.DomainLifecycle, domain.DomainCurrentLocation, domain.DomainActiveCount:
		return true
	default:
		return false
	}
}

func validSourceDomainFilter(value string) bool {
	if value == "" {
		return true
	}
	switch value {
	case domain.DomainIdentity, domain.DomainLifecycle, domain.DomainCurrentLocation, domain.DomainActiveCount:
		return true
	default:
		return false
	}
}

func domainFilterForRun(value string) string {
	if value == domain.DomainAll {
		return ""
	}
	return value
}

func localOrDev(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "local", "dev":
		return true
	default:
		return false
	}
}

func mapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return NotFound("not_found", "legacy sync record was not found")
	case errors.Is(err, ports.ErrInvalidCursor):
		return BadRequest("invalid_cursor", "cursor is invalid")
	case errors.Is(err, ports.ErrWriteConflict):
		return Conflict("write_conflict", "legacy sync run state changed")
	default:
		return err
	}
}
