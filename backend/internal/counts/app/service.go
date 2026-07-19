// Package app coordinates Counts/Shifting projection use-cases.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

var (
	ErrMissingRequiredField    = errors.New("counts: missing required field")
	ErrInvalidCount            = errors.New("counts: invalid count")
	ErrInvalidJSON             = errors.New("counts: invalid json object")
	ErrInvalidLimit            = errors.New("counts: invalid limit")
	ErrInvalidHorizon          = errors.New("counts: invalid projection horizon")
	ErrMissingImpact           = errors.New("counts: shifting event requires structured impact")
	ErrInvalidResolutionAction = errors.New("counts: invalid projection exception resolution action")
	ErrInvalidExceptionFilter  = errors.New("counts: invalid projection exception filter")
	ErrInvalidScanWindow       = errors.New("counts: invalid mismatch scan window")
	ErrInvalidOffset           = errors.New("counts: invalid offset")
	ErrInvalidTargetDate       = errors.New("counts: invalid feed target date")

	// ErrImpactNotDerivable is returned when a shifting request omits impacts but does not name
	// EXACTLY ONE animal. Impacts describe a cohort at breed grain ("12 head of Boer"); the server
	// can only derive that description from the animals themselves when there is exactly one animal
	// to describe. For two or more, guessing how the operator wanted the cohort split would invent
	// business data, so the caller must state the impacts.
	ErrImpactNotDerivable = errors.New("counts: impacts can only be derived for exactly one goat")
)

const (
	defaultProjectionLimit          = int32(100)
	maxProjectionLimit              = int32(500)
	defaultProjectionExceptionLimit = int32(50)
	maxProjectionExceptionLimit     = int32(200)
	defaultCountMismatchScanLimit   = int32(100)
	maxCountMismatchScanLimit       = int32(500)
	defaultFeedProjectedCountLimit  = int32(50)
	maxFeedProjectedCountLimit      = int32(200)
	// maxFeedProjectedCountOffset bounds the OFFSET walk over the pre-aggregated grain set. A
	// caller paging past this is not reading a screen, and an unbounded offset is the growable
	// offset the scale rules ban.
	maxFeedProjectedCountOffset = int32(5000)
	// maxFeedProjectedCountShedIDs caps the shed-SET batch filter. It is sized well
	// above any real shed page (the feed-direction generator pages sheds in tens)
	// so a legitimate batch never trips it, while still refusing an unbounded
	// IN-list that would smuggle a whole-tenant scan past the paging limits.
	maxFeedProjectedCountShedIDs = 500
)

type Service struct {
	repo ports.Repository
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) RecordBaseCountAnchor(ctx context.Context, in domain.BaseCountAnchor) (string, bool, error) {
	in.SourceSystem = defaultString(in.SourceSystem, "physical_base_count")
	in.DiscrepancyState = defaultString(in.DiscrepancyState, "not_checked")
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ParkID) == "" ||
		strings.TrimSpace(in.ShedID) == "" || strings.TrimSpace(in.BreedKey) == "" ||
		strings.TrimSpace(in.BreedLabel) == "" || in.CountedAt.IsZero() ||
		strings.TrimSpace(in.SourceRef) == "" || strings.TrimSpace(in.SourceHash) == "" ||
		strings.TrimSpace(in.IdempotencyKey) == "" || strings.TrimSpace(in.RequestFingerprint) == "" {
		return "", false, ErrMissingRequiredField
	}
	if in.HeadCount < 0 {
		return "", false, ErrInvalidCount
	}
	return s.repo.RecordBaseCountAnchor(ctx, in)
}

func (s *Service) RecordShiftingEvent(ctx context.Context, in domain.ShiftingEvent) (string, bool, error) {
	in.Priority = defaultString(in.Priority, "normal")
	in.Category = defaultString(in.Category, "routine")
	in.AuthorizationState = defaultString(in.AuthorizationState, "pending")
	in.VerificationState = defaultString(in.VerificationState, "unverified")
	in.EventStatus = defaultString(in.EventStatus, "pending")
	in.SourceSystem = defaultString(in.SourceSystem, "manual_review")
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.LogicalShiftingEventKey) == "" ||
		strings.TrimSpace(in.DestinationParkID) == "" || strings.TrimSpace(in.DestinationShedID) == "" ||
		in.RaisedAt.IsZero() || in.EffectiveAt.IsZero() || strings.TrimSpace(in.SourceRef) == "" ||
		strings.TrimSpace(in.PayloadHash) == "" || strings.TrimSpace(in.IdempotencyKey) == "" ||
		strings.TrimSpace(in.RequestFingerprint) == "" {
		return "", false, ErrMissingRequiredField
	}
	if len(in.Impacts) == 0 {
		return "", false, ErrMissingImpact
	}
	for _, impact := range in.Impacts {
		if strings.TrimSpace(impact.GrainKey) == "" || strings.TrimSpace(impact.BreedKey) == "" ||
			strings.TrimSpace(impact.BreedLabel) == "" {
			return "", false, ErrMissingRequiredField
		}
		if impact.HeadCount <= 0 || impact.PregnantCount < 0 || impact.LactatingCount < 0 || impact.WarmupCount < 0 ||
			impact.PregnantCount > impact.HeadCount || impact.LactatingCount > impact.HeadCount || impact.WarmupCount > impact.HeadCount {
			return "", false, ErrInvalidCount
		}
		if !isJSONObject(impact.RiskFlagsJSON) {
			return "", false, ErrInvalidJSON
		}
	}
	return s.repo.RecordShiftingEvent(ctx, in)
}

func (s *Service) ScanCountMismatches(ctx context.Context, req domain.CountMismatchScanRequest) (domain.CountMismatchScanResult, error) {
	req, err := normalizeCountMismatchScanRequest(req)
	if err != nil {
		return domain.CountMismatchScanResult{}, err
	}
	return s.repo.ScanCountMismatches(ctx, req)
}

func (s *Service) CreateProjectionSnapshot(ctx context.Context, in domain.ProjectionSnapshot) (string, error) {
	in.Horizon = defaultString(in.Horizon, "feed_target_date")
	in.ProjectionStatus = defaultString(in.ProjectionStatus, "blocked")
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ParkID) == "" ||
		in.TargetDate.IsZero() || in.AsOf.IsZero() || strings.TrimSpace(in.SourceContractVersion) == "" ||
		strings.TrimSpace(in.SourceHash) == "" || strings.TrimSpace(in.BaseAnchorIDsHash) == "" ||
		strings.TrimSpace(in.ShiftingEventIDsHash) == "" || strings.TrimSpace(in.GeneratedBy) == "" {
		return "", ErrMissingRequiredField
	}
	for _, row := range in.Rows {
		if strings.TrimSpace(row.ParkID) == "" || strings.TrimSpace(row.ShedID) == "" ||
			strings.TrimSpace(row.GrainKey) == "" || strings.TrimSpace(row.BreedKey) == "" ||
			strings.TrimSpace(row.BreedLabel) == "" || strings.TrimSpace(row.BaseCountAnchorID) == "" ||
			strings.TrimSpace(row.IncludedShiftingEventIDsHash) == "" || strings.TrimSpace(row.SourceRowHash) == "" {
			return "", ErrMissingRequiredField
		}
		if row.HeadCount < 0 || row.PregnantCount < 0 || row.LactatingCount < 0 || row.WarmupCount < 0 ||
			row.PregnantCount > row.HeadCount || row.LactatingCount > row.HeadCount || row.WarmupCount > row.HeadCount {
			return "", ErrInvalidCount
		}
	}
	for i := range in.Exceptions {
		normalizeProjectionExceptionWork(&in.Exceptions[i], in.AsOf)
		exception := in.Exceptions[i]
		if strings.TrimSpace(exception.ExceptionType) == "" || strings.TrimSpace(exception.SourceKey) == "" ||
			strings.TrimSpace(exception.GrainKey) == "" || strings.TrimSpace(exception.Severity) == "" ||
			strings.TrimSpace(exception.WorkType) == "" || strings.TrimSpace(exception.WorkState) == "" ||
			exception.DueAt.IsZero() || strings.TrimSpace(exception.NextAction) == "" ||
			strings.TrimSpace(exception.EvidenceLink) == "" || strings.TrimSpace(exception.BlockerReason) == "" {
			return "", ErrMissingRequiredField
		}
		if !isJSONObject(exception.EvidenceJSON) {
			return "", ErrInvalidJSON
		}
	}
	return s.repo.CreateProjectionSnapshot(ctx, in)
}

func (s *Service) RecomputeProjectionSnapshot(ctx context.Context, req domain.ProjectionRecomputeRequest) (string, error) {
	result, err := s.RecomputeProjectionSnapshotWithResult(ctx, req)
	if err != nil {
		return "", err
	}
	return result.SnapshotID, nil
}

func (s *Service) RecomputeProjectionSnapshotWithResult(ctx context.Context, req domain.ProjectionRecomputeRequest) (domain.ProjectionRecomputeResult, error) {
	req, err := normalizeRecomputeRequest(req)
	if err != nil {
		return domain.ProjectionRecomputeResult{}, err
	}
	runID, err := s.repo.BeginProjectionRecomputeRun(ctx, req)
	if err != nil {
		return domain.ProjectionRecomputeResult{}, err
	}
	result := domain.ProjectionRecomputeResult{
		RunID:      runID,
		Horizon:    req.Horizon,
		TargetDate: req.TargetDate,
		AsOf:       req.AsOf,
	}
	inputs, err := s.repo.ProjectionInputs(ctx, req)
	if err != nil {
		return s.finishProjectionRecomputeRun(ctx, runID, result, err)
	}
	snapshot := buildProjectionSnapshot(req, inputs)
	id, err := s.CreateProjectionSnapshot(ctx, snapshot)
	if err != nil {
		return s.finishProjectionRecomputeRun(ctx, runID, result, err)
	}
	result = domain.ProjectionRecomputeResult{
		RunID:            runID,
		SnapshotID:       id,
		Horizon:          snapshot.Horizon,
		TargetDate:       snapshot.TargetDate,
		AsOf:             snapshot.AsOf,
		ProjectionStatus: snapshot.ProjectionStatus,
		RowCount:         len(snapshot.Rows),
		ExceptionCount:   len(snapshot.Exceptions),
	}
	return s.finishProjectionRecomputeRun(ctx, runID, result, nil)
}

func (s *Service) finishProjectionRecomputeRun(ctx context.Context, runID string, result domain.ProjectionRecomputeResult, recomputeErr error) (domain.ProjectionRecomputeResult, error) {
	if err := s.repo.FinishProjectionRecomputeRun(ctx, runID, result, recomputeErr); err != nil {
		if recomputeErr != nil {
			return result, fmt.Errorf("%w; finish projection recompute run: %v", recomputeErr, err)
		}
		return result, err
	}
	return result, recomputeErr
}

func (s *Service) CountAsOf(ctx context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error) {
	req, err := normalizeProjectionRequest(req, true)
	if err != nil {
		return domain.CountProjection{}, err
	}
	return s.repo.CountAsOf(ctx, req)
}

func (s *Service) ProjectedCountFor(ctx context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error) {
	req, err := normalizeProjectionRequest(req, false)
	if err != nil {
		return domain.CountProjection{}, err
	}
	return s.repo.ProjectedCountFor(ctx, req)
}

func (s *Service) ListProjectionExceptions(ctx context.Context, req domain.ProjectionExceptionQuery) (domain.ProjectionExceptionList, error) {
	req, err := normalizeProjectionExceptionQuery(req)
	if err != nil {
		return domain.ProjectionExceptionList{}, err
	}
	return s.repo.ListProjectionExceptions(ctx, req)
}

func (s *Service) ResolveProjectionException(ctx context.Context, in domain.ProjectionExceptionResolutionRequest) (domain.ProjectionExceptionResolution, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ProjectionExceptionID = strings.TrimSpace(in.ProjectionExceptionID)
	in.Action = strings.ToLower(strings.TrimSpace(in.Action))
	in.ResolvedByRef = strings.TrimSpace(in.ResolvedByRef)
	in.ResolutionReason = strings.TrimSpace(in.ResolutionReason)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.RequestFingerprint = strings.TrimSpace(in.RequestFingerprint)
	if in.ResolutionRef != nil {
		ref := strings.TrimSpace(*in.ResolutionRef)
		in.ResolutionRef = ptrIfNotEmpty(ref)
	}
	if in.TenantID == "" || in.ProjectionExceptionID == "" || in.ResolvedByRef == "" ||
		in.ResolutionReason == "" || in.IdempotencyKey == "" || in.RequestFingerprint == "" {
		return domain.ProjectionExceptionResolution{}, ErrMissingRequiredField
	}
	if in.Action != "resolve" && in.Action != "dismiss" {
		return domain.ProjectionExceptionResolution{}, ErrInvalidResolutionAction
	}
	return s.repo.ResolveProjectionException(ctx, in)
}

func (s *Service) Readiness(ctx context.Context, tenantID string) (domain.Readiness, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.Readiness{}, ErrMissingRequiredField
	}
	return s.repo.Readiness(ctx, tenantID)
}

type projectionRowAccumulator struct {
	row         domain.ProjectionRow
	movementIDs []string
}

func normalizeRecomputeRequest(req domain.ProjectionRecomputeRequest) (domain.ProjectionRecomputeRequest, error) {
	req.Horizon = defaultString(req.Horizon, "feed_target_date")
	if req.Horizon != "feed_target_date" && req.Horizon != "count_as_of" {
		return domain.ProjectionRecomputeRequest{}, ErrInvalidHorizon
	}
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ParkID) == "" ||
		strings.TrimSpace(req.SourceContractVersion) == "" || strings.TrimSpace(req.GeneratedBy) == "" ||
		req.AsOf.IsZero() {
		return domain.ProjectionRecomputeRequest{}, ErrMissingRequiredField
	}
	if req.TargetDate.IsZero() {
		if req.Horizon == "count_as_of" {
			req.TargetDate = req.AsOf
		} else {
			return domain.ProjectionRecomputeRequest{}, ErrMissingRequiredField
		}
	}
	req.TargetDate = dateOnly(req.TargetDate)
	return req, nil
}

func normalizeCountMismatchScanRequest(req domain.CountMismatchScanRequest) (domain.CountMismatchScanRequest, error) {
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.ParkID = trimOptional(req.ParkID)
	req.ShedID = trimOptional(req.ShedID)
	if req.TenantID == "" || req.CountedBefore.IsZero() {
		return domain.CountMismatchScanRequest{}, ErrMissingRequiredField
	}
	req.CountedBefore = req.CountedBefore.UTC()
	if req.CountedAfter != nil {
		countedAfter := req.CountedAfter.UTC()
		req.CountedAfter = &countedAfter
		if !countedAfter.Before(req.CountedBefore) {
			return domain.CountMismatchScanRequest{}, ErrInvalidScanWindow
		}
	}
	if (req.CursorCountedAt == nil) != (req.CursorAnchorID == nil) {
		return domain.CountMismatchScanRequest{}, ErrMissingRequiredField
	}
	if req.CursorCountedAt != nil {
		cursorCountedAt := req.CursorCountedAt.UTC()
		cursorAnchorID := strings.TrimSpace(*req.CursorAnchorID)
		if cursorAnchorID == "" {
			return domain.CountMismatchScanRequest{}, ErrMissingRequiredField
		}
		if cursorCountedAt.After(req.CountedBefore) {
			return domain.CountMismatchScanRequest{}, ErrInvalidScanWindow
		}
		req.CursorCountedAt = &cursorCountedAt
		req.CursorAnchorID = &cursorAnchorID
	}
	if req.Limit == 0 {
		req.Limit = defaultCountMismatchScanLimit
	}
	if req.Limit < 0 || req.Limit > maxCountMismatchScanLimit {
		return domain.CountMismatchScanRequest{}, ErrInvalidLimit
	}
	return req, nil
}

func buildProjectionSnapshot(req domain.ProjectionRecomputeRequest, inputs domain.ProjectionInputs) domain.ProjectionSnapshot {
	rows := map[string]*projectionRowAccumulator{}
	exceptions := make([]domain.ProjectionException, 0)
	anchorIDs := make([]string, 0, len(inputs.Anchors))
	movementIDs := make([]string, 0, len(inputs.Movements))

	for _, anchor := range inputs.Anchors {
		key := projectionGrainKey(anchor.ShedID, anchor.BreedKey)
		anchorIDs = append(anchorIDs, anchor.BaseCountAnchorID)
		blocker := "reviewed ration context is unresolved for this physical count row"
		if aliasBlocker := ptrValue(anchor.AliasBlockerReason); aliasBlocker != "" {
			blocker = combineBlockerReasons(blocker, aliasBlocker)
		}
		rows[key] = &projectionRowAccumulator{row: domain.ProjectionRow{
			ParkID: req.ParkID, ShedID: anchor.ShedID, TargetDate: req.TargetDate,
			GrainKey: key, BaseCountAnchorID: anchor.BaseCountAnchorID, IncludedShiftingEventIDsHash: "no-shifting-events",
			BreedID: anchor.BreedID, BreedKey: anchor.BreedKey, BreedLabel: anchor.BreedLabel,
			HeadCount: anchor.HeadCount, RationContextResolutionState: "blocked",
			BlockerReason: &blocker,
		}}
		exceptions = append(exceptions, projectionException("ration_context_unresolved", "base:"+anchor.BaseCountAnchorID, key, req.ParkID, anchor.ShedID, anchor.BreedKey, nil, "blocking", blocker))
		if aliasBlocker := ptrValue(anchor.AliasBlockerReason); aliasBlocker != "" {
			exceptions = append(exceptions, projectionException("alias_conflict", "base:"+anchor.BaseCountAnchorID, key, req.ParkID, anchor.ShedID, anchor.BreedKey, nil, "blocking", aliasBlocker))
		}
	}
	if len(inputs.Anchors) == 0 {
		reason := "no adopted Base Count anchors exist for this tenant, park, and projection horizon"
		exceptions = append(exceptions, domain.ProjectionException{
			ExceptionType: "missing_base_count",
			SourceKey:     "base_anchor:" + req.ParkID,
			GrainKey:      "park:" + req.ParkID,
			ParkID:        &req.ParkID,
			Severity:      "blocking",
			BlockerReason: reason,
			EvidenceJSON:  []byte(fmt.Sprintf(`{"source":"counts_projection_recompute","reason":%q}`, reason)),
		})
	}

	for _, movement := range inputs.Movements {
		movementIDs = append(movementIDs, movement.ShiftingEventID)
		aliasBlocker := ptrValue(movement.AliasBlockerReason)
		if aliasBlocker != "" {
			if movementDestinationAppliesToPark(req.ParkID, movement) {
				exceptions = append(exceptions, projectionException("alias_conflict", movement.LogicalShiftingEventKey, projectionGrainKey(movement.DestinationShedID, movement.BreedKey), req.ParkID, movement.DestinationShedID, movement.BreedKey, movement.StageTag, "blocking", aliasBlocker))
			} else if movement.SourceShedID != nil && movementSourceAppliesToPark(req.ParkID, movement) {
				exceptions = append(exceptions, projectionException("alias_conflict", movement.LogicalShiftingEventKey, projectionGrainKey(*movement.SourceShedID, movement.BreedKey), req.ParkID, *movement.SourceShedID, movement.BreedKey, movement.StageTag, "blocking", aliasBlocker))
			}
		}
		if movement.SourceShedID != nil && movementSourceAppliesToPark(req.ParkID, movement) {
			sourceKey := projectionGrainKey(*movement.SourceShedID, movement.BreedKey)
			if sourceRow, ok := rows[sourceKey]; ok {
				sourceRow.row.HeadCount -= movement.HeadCount
				sourceRow.movementIDs = append(sourceRow.movementIDs, movement.ShiftingEventID)
				if aliasBlocker != "" {
					sourceRow.row.RationContextResolutionState = "blocked"
					sourceRow.row.BlockerReason = appendBlockerReason(sourceRow.row.BlockerReason, aliasBlocker)
				}
				if sourceRow.row.HeadCount < 0 {
					sourceRow.row.HeadCount = 0
					reason := "shifting impact exceeds source Base Count for the shed/breed grain"
					sourceRow.row.RationContextResolutionState = "blocked"
					sourceRow.row.BlockerReason = &reason
					exceptions = append(exceptions, projectionException("count_mismatch", movement.LogicalShiftingEventKey, sourceKey, req.ParkID, *movement.SourceShedID, movement.BreedKey, movement.StageTag, "critical", reason))
				}
			} else {
				reason := "source shed Base Count is missing for shifting impact"
				exceptions = append(exceptions, projectionException("missing_base_count", movement.LogicalShiftingEventKey, sourceKey, req.ParkID, *movement.SourceShedID, movement.BreedKey, movement.StageTag, "blocking", reason))
			}
		}

		if !movementDestinationAppliesToPark(req.ParkID, movement) {
			continue
		}
		destinationKey := projectionGrainKey(movement.DestinationShedID, movement.BreedKey)
		destinationRow, ok := rows[destinationKey]
		if !ok {
			reason := "destination shed Base Count is missing for shifting impact"
			exceptions = append(exceptions, projectionException("missing_base_count", movement.LogicalShiftingEventKey, destinationKey, req.ParkID, movement.DestinationShedID, movement.BreedKey, movement.StageTag, "blocking", reason))
			continue
		}
		destinationRow.row.HeadCount += movement.HeadCount
		destinationRow.row.PregnantCount += movement.PregnantCount
		destinationRow.row.LactatingCount += movement.LactatingCount
		destinationRow.row.WarmupCount += movement.WarmupCount
		destinationRow.movementIDs = append(destinationRow.movementIDs, movement.ShiftingEventID)
		if movement.RationContextResolutionState == "resolved" || movement.RationContextResolutionState == "not_required" {
			if aliasBlocker != "" {
				destinationRow.row.RationContextResolutionState = "blocked"
				destinationRow.row.BlockerReason = appendBlockerReason(destinationRow.row.BlockerReason, aliasBlocker)
				continue
			}
			destinationRow.row.RationContextResolutionState = movement.RationContextResolutionState
			destinationRow.row.RationContextRef = movement.RationContextRef
			destinationRow.row.BlockerReason = nil
		} else {
			reason := defaultString(ptrValue(movement.BlockerReason), "destination shed ration context is unresolved for shifted cohort")
			if aliasBlocker != "" {
				reason = combineBlockerReasons(reason, aliasBlocker)
			}
			destinationRow.row.RationContextResolutionState = "blocked"
			destinationRow.row.BlockerReason = &reason
			exType := "ration_context_unresolved"
			severity := "blocking"
			if movement.PregnantCount > 0 || movement.LactatingCount > 0 || movement.WarmupCount > 0 {
				exType = "destination_shortage"
				severity = "critical"
			}
			exceptions = append(exceptions, projectionException(exType, movement.LogicalShiftingEventKey, destinationKey, req.ParkID, movement.DestinationShedID, movement.BreedKey, movement.StageTag, severity, reason))
		}
	}

	outRows := make([]domain.ProjectionRow, 0, len(rows))
	for _, acc := range rows {
		sort.Strings(acc.movementIDs)
		if len(acc.movementIDs) > 0 {
			acc.row.IncludedShiftingEventIDsHash = hashStrings(acc.movementIDs)
		}
		acc.row.SourceRowHash = projectionRowHash(acc.row)
		outRows = append(outRows, acc.row)
	}
	sort.Slice(outRows, func(i, j int) bool { return outRows[i].GrainKey < outRows[j].GrainKey })
	sort.Slice(exceptions, func(i, j int) bool {
		if exceptions[i].GrainKey == exceptions[j].GrainKey {
			return exceptions[i].ExceptionType < exceptions[j].ExceptionType
		}
		return exceptions[i].GrainKey < exceptions[j].GrainKey
	})

	baseHash := hashStrings(anchorIDs)
	movementHash := hashStrings(movementIDs)
	sourceHash := hashStrings([]string{req.TenantID, req.ParkID, req.Horizon, req.TargetDate.Format("2006-01-02"), req.AsOf.UTC().Format(time.RFC3339Nano), baseHash, movementHash, rowHashSet(outRows), exceptionHashSet(exceptions)})
	status := "ready"
	if len(exceptions) > 0 {
		status = "blocked"
	}
	return domain.ProjectionSnapshot{
		TenantID: req.TenantID, Horizon: req.Horizon, ParkID: req.ParkID,
		TargetDate: req.TargetDate, AsOf: req.AsOf, ProjectionStatus: status,
		SourceContractVersion: req.SourceContractVersion, SourceHash: sourceHash,
		BaseAnchorIDsHash: baseHash, ShiftingEventIDsHash: movementHash,
		GeneratedBy: req.GeneratedBy, TraceID: req.TraceID, Rows: outRows, Exceptions: exceptions,
	}
}

func movementSourceAppliesToPark(parkID string, movement domain.ProjectionMovementImpact) bool {
	if movement.SourceParkID == nil || strings.TrimSpace(*movement.SourceParkID) == "" {
		return true
	}
	return strings.TrimSpace(*movement.SourceParkID) == parkID
}

func movementDestinationAppliesToPark(parkID string, movement domain.ProjectionMovementImpact) bool {
	if strings.TrimSpace(movement.DestinationParkID) == "" {
		return true
	}
	return strings.TrimSpace(movement.DestinationParkID) == parkID
}

func projectionException(exceptionType, sourceKey, grainKey, parkID, shedID, breedKey string, stageTag *string, severity, reason string) domain.ProjectionException {
	return domain.ProjectionException{
		ExceptionType: exceptionType, SourceKey: sourceKey, GrainKey: grainKey,
		ParkID: &parkID, ShedID: &shedID, BreedKey: &breedKey, StageTag: stageTag,
		Severity: severity, BlockerReason: reason,
		EvidenceJSON: []byte(fmt.Sprintf(`{"source":"counts_projection_recompute","reason":%q}`, reason)),
	}
}

func normalizeProjectionExceptionWork(exception *domain.ProjectionException, anchor time.Time) {
	exception.Severity = defaultString(exception.Severity, "blocking")
	exception.WorkType = defaultString(exception.WorkType, "counts_projection_exception")
	if strings.TrimSpace(exception.WorkState) == "" {
		exception.WorkState = "blocked"
	}
	if anchor.IsZero() {
		anchor = time.Now().In(biztime.DefaultLocation())
	}
	if exception.DueAt.IsZero() {
		exception.DueAt = exceptionDueAt(anchor, exception.Severity)
	}
	exception.NextAction = defaultString(exception.NextAction, exceptionNextAction(exception.ExceptionType))
	exception.EvidenceLink = defaultString(exception.EvidenceLink, "/feed-direction/counts-projection/exceptions/"+exception.SourceKey)
}

func exceptionDueAt(anchor time.Time, severity string) time.Time {
	anchor = anchor.In(biztime.DefaultLocation())
	switch severity {
	case "critical":
		return anchor
	case "warning":
		return anchor.Add(24 * time.Hour)
	default:
		return anchor.Add(2 * time.Hour)
	}
}

func exceptionNextAction(exceptionType string) string {
	switch exceptionType {
	case "missing_base_count":
		return "Record or adopt the physical Base Count anchor"
	case "missing_structured_impact":
		return "Capture structured source/destination cohort impact"
	case "unreported_shifting":
		return "Review mismatch and create or confirm the missing ShiftingEvent"
	case "count_mismatch":
		return "Investigate count mismatch before Feed generation"
	case "alias_conflict":
		return "Review and approve the Counts dimension alias mapping"
	case "ration_context_unresolved":
		return "Resolve ration context for the projection row"
	case "destination_shortage":
		return "Resolve destination ration context before Feed generation"
	case "unsafe_surplus":
		return "Review unsafe surplus or wastage risk before Feed generation"
	case "query_plan_unproven":
		return "Run bounded query-plan proof for the Counts projection"
	default:
		return "Review Counts/Shifting projection exception"
	}
}

func projectionGrainKey(shedID, breedKey string) string {
	return strings.ToLower(strings.TrimSpace(shedID)) + ":" + strings.ToLower(strings.TrimSpace(breedKey))
}

func projectionRowHash(row domain.ProjectionRow) string {
	return hashStrings([]string{
		row.ParkID, row.ShedID, row.GrainKey, row.BaseCountAnchorID, row.IncludedShiftingEventIDsHash,
		row.BreedKey, fmt.Sprintf("%d", row.HeadCount), fmt.Sprintf("%d", row.PregnantCount),
		fmt.Sprintf("%d", row.LactatingCount), fmt.Sprintf("%d", row.WarmupCount),
		row.RationContextResolutionState, ptrValue(row.RationContextRef), ptrValue(row.BlockerReason),
	})
}

func rowHashSet(rows []domain.ProjectionRow) string {
	hashes := make([]string, 0, len(rows))
	for _, row := range rows {
		hashes = append(hashes, row.SourceRowHash)
	}
	return hashStrings(hashes)
}

func exceptionHashSet(exceptions []domain.ProjectionException) string {
	hashes := make([]string, 0, len(exceptions))
	for _, exception := range exceptions {
		hashes = append(hashes, strings.Join([]string{
			exception.ExceptionType,
			exception.SourceKey,
			exception.GrainKey,
			exception.Severity,
			exception.BlockerReason,
			ptrValue(exception.BreedKey),
			ptrValue(exception.StageTag),
		}, "|"))
	}
	return hashStrings(hashes)
}

func hashStrings(parts []string) string {
	if len(parts) == 0 {
		return "none"
	}
	clean := append([]string(nil), parts...)
	sort.Strings(clean)
	h := sha256.New()
	for _, part := range clean {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func normalizeProjectionRequest(req domain.CountProjectionRequest, asOf bool) (domain.CountProjectionRequest, error) {
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ParkID) == "" {
		return domain.CountProjectionRequest{}, ErrMissingRequiredField
	}
	if asOf {
		if req.AsOf.IsZero() {
			return domain.CountProjectionRequest{}, ErrMissingRequiredField
		}
		req.TargetDate = req.AsOf
	} else if req.TargetDate.IsZero() {
		return domain.CountProjectionRequest{}, ErrMissingRequiredField
	}
	if req.Limit == 0 {
		req.Limit = defaultProjectionLimit
	}
	if req.Limit < 0 || req.Limit > maxProjectionLimit {
		return domain.CountProjectionRequest{}, ErrInvalidLimit
	}
	return req, nil
}

func normalizeProjectionExceptionQuery(req domain.ProjectionExceptionQuery) (domain.ProjectionExceptionQuery, error) {
	req.TenantID = strings.TrimSpace(req.TenantID)
	if req.TenantID == "" {
		return domain.ProjectionExceptionQuery{}, ErrMissingRequiredField
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.Status == "" {
		req.Status = "open"
	}
	if !oneOf(req.Status, "open", "resolved", "dismissed") {
		return domain.ProjectionExceptionQuery{}, ErrInvalidExceptionFilter
	}
	req.ParkID = trimOptional(req.ParkID)
	req.ShedID = trimOptional(req.ShedID)
	req.ExceptionType = trimOptionalLower(req.ExceptionType)
	req.Severity = trimOptionalLower(req.Severity)
	req.OwnerRef = trimOptional(req.OwnerRef)
	req.WorkState = trimOptionalLower(req.WorkState)
	if req.ExceptionType != nil && !oneOf(*req.ExceptionType, "missing_base_count", "missing_structured_impact", "unreported_shifting", "count_mismatch", "alias_conflict", "ration_context_unresolved", "destination_shortage", "unsafe_surplus", "query_plan_unproven") {
		return domain.ProjectionExceptionQuery{}, ErrInvalidExceptionFilter
	}
	if req.Severity != nil && !oneOf(*req.Severity, "warning", "blocking", "critical") {
		return domain.ProjectionExceptionQuery{}, ErrInvalidExceptionFilter
	}
	if req.WorkState != nil && !oneOf(*req.WorkState, "blocked", "resolved", "dismissed") {
		return domain.ProjectionExceptionQuery{}, ErrInvalidExceptionFilter
	}
	if req.Limit == 0 {
		req.Limit = defaultProjectionExceptionLimit
	}
	if req.Limit < 0 || req.Limit > maxProjectionExceptionLimit {
		return domain.ProjectionExceptionQuery{}, ErrInvalidLimit
	}
	return req, nil
}

func dateOnly(t time.Time) time.Time {
	return biztime.BusinessDayStart(t)
}

func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func ptrIfNotEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func trimOptional(v *string) *string {
	if v == nil {
		return nil
	}
	return ptrIfNotEmpty(strings.TrimSpace(*v))
}

func trimOptionalLower(v *string) *string {
	if v == nil {
		return nil
	}
	return ptrIfNotEmpty(strings.ToLower(strings.TrimSpace(*v)))
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func appendBlockerReason(existing *string, reason string) *string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return existing
	}
	if existing == nil || strings.TrimSpace(*existing) == "" {
		return &reason
	}
	combined := combineBlockerReasons(*existing, reason)
	return &combined
}

func combineBlockerReasons(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	return left + "; " + right
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// ProjectedShedCountsForFeed returns the live-herd feed projection: what each shed grain will
// hold on the requested feed day, once the movements that are approved but not yet executed are
// applied.
//
// See Repository.ProjectedShedCountsForFeed for why this is a parallel path to ProjectedCountFor
// rather than a replacement for it.
func (s *Service) ProjectedShedCountsForFeed(ctx context.Context, req domain.FeedProjectedCountQuery) (domain.FeedProjectedCounts, error) {
	req, err := normalizeFeedProjectedCountQuery(req)
	if err != nil {
		return domain.FeedProjectedCounts{}, err
	}
	return s.repo.ProjectedShedCountsForFeed(ctx, req)
}

func normalizeFeedProjectedCountQuery(req domain.FeedProjectedCountQuery) (domain.FeedProjectedCountQuery, error) {
	req.TenantID = strings.TrimSpace(req.TenantID)
	if req.TenantID == "" {
		return domain.FeedProjectedCountQuery{}, ErrMissingRequiredField
	}
	if req.TargetDate.IsZero() {
		return domain.FeedProjectedCountQuery{}, ErrInvalidTargetDate
	}
	// Normalize to a business-day start in Asia/Kolkata here, at the boundary, so a caller that
	// passed a UTC instant late in the day cannot push the projection onto the wrong feed day.
	req.TargetDate = biztime.BusinessDayStart(req.TargetDate)

	req.LifecycleStatus = trimOptionalLower(req.LifecycleStatus)
	req.ParkID = trimOptional(req.ParkID)
	req.ShedID = trimOptional(req.ShedID)

	// The shed-SET filter is a batch read for a caller that already holds a bounded
	// page of shed ids. Blanks are dropped rather than passed through as empty
	// strings (which would fail the uuid cast), and the set is capped so this can
	// never become an unbounded IN-list smuggled past the paging limits.
	if len(req.ShedIDs) > 0 {
		shedIDs := make([]string, 0, len(req.ShedIDs))
		for _, id := range req.ShedIDs {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				shedIDs = append(shedIDs, trimmed)
			}
		}
		if len(shedIDs) > maxFeedProjectedCountShedIDs {
			return domain.FeedProjectedCountQuery{}, ErrInvalidLimit
		}
		req.ShedIDs = shedIDs
	}

	if req.Limit == 0 {
		req.Limit = defaultFeedProjectedCountLimit
	}
	if req.Limit < 0 || req.Limit > maxFeedProjectedCountLimit {
		return domain.FeedProjectedCountQuery{}, ErrInvalidLimit
	}
	if req.Offset < 0 || req.Offset > maxFeedProjectedCountOffset {
		return domain.FeedProjectedCountQuery{}, ErrInvalidOffset
	}
	return req, nil
}

func isJSONObject(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	var obj map[string]any
	return json.Unmarshal(raw, &obj) == nil
}
