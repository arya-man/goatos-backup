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
)

var (
	ErrMissingRequiredField = errors.New("counts: missing required field")
	ErrInvalidCount         = errors.New("counts: invalid count")
	ErrInvalidJSON          = errors.New("counts: invalid json object")
	ErrInvalidLimit         = errors.New("counts: invalid limit")
	ErrInvalidHorizon       = errors.New("counts: invalid projection horizon")
	ErrMissingImpact        = errors.New("counts: shifting event requires structured impact")
)

const (
	defaultProjectionLimit = int32(100)
	maxProjectionLimit     = int32(500)
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
	for _, exception := range in.Exceptions {
		if strings.TrimSpace(exception.ExceptionType) == "" || strings.TrimSpace(exception.SourceKey) == "" ||
			strings.TrimSpace(exception.GrainKey) == "" || strings.TrimSpace(exception.BlockerReason) == "" {
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
	inputs, err := s.repo.ProjectionInputs(ctx, req)
	if err != nil {
		return domain.ProjectionRecomputeResult{}, err
	}
	snapshot := buildProjectionSnapshot(req, inputs)
	id, err := s.CreateProjectionSnapshot(ctx, snapshot)
	if err != nil {
		return domain.ProjectionRecomputeResult{}, err
	}
	return domain.ProjectionRecomputeResult{
		SnapshotID:       id,
		Horizon:          snapshot.Horizon,
		TargetDate:       snapshot.TargetDate,
		AsOf:             snapshot.AsOf,
		ProjectionStatus: snapshot.ProjectionStatus,
		RowCount:         len(snapshot.Rows),
		ExceptionCount:   len(snapshot.Exceptions),
	}, nil
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
			exceptions = append(exceptions, projectionException("alias_conflict", movement.LogicalShiftingEventKey, projectionGrainKey(movement.DestinationShedID, movement.BreedKey), req.ParkID, movement.DestinationShedID, movement.BreedKey, movement.StageTag, "blocking", aliasBlocker))
		}
		if movement.SourceShedID != nil {
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

func projectionException(exceptionType, sourceKey, grainKey, parkID, shedID, breedKey string, stageTag *string, severity, reason string) domain.ProjectionException {
	return domain.ProjectionException{
		ExceptionType: exceptionType, SourceKey: sourceKey, GrainKey: grainKey,
		ParkID: &parkID, ShedID: &shedID, BreedKey: &breedKey, StageTag: stageTag,
		Severity: severity, BlockerReason: reason,
		EvidenceJSON: []byte(fmt.Sprintf(`{"source":"counts_projection_recompute","reason":%q}`, reason)),
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

func dateOnly(t time.Time) time.Time {
	return time.Date(t.UTC().Year(), t.UTC().Month(), t.UTC().Day(), 0, 0, 0, 0, time.UTC)
}

func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
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

func isJSONObject(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	var obj map[string]any
	return json.Unmarshal(raw, &obj) == nil
}
