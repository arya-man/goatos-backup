// Package app holds the feed-direction application service.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	countsports "github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/feed/domain"
	"github.com/vgoats/goatos/backend/internal/feed/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

var (
	ErrMissingRequiredField        = errors.New("feed: missing required field")
	ErrInvalidResolutionAction     = errors.New("feed: invalid counts projection exception resolution action")
	ErrInvalidIdempotencyKey       = errors.New("feed: invalid idempotency key")
	ErrInvalidResolutionReason     = errors.New("feed: invalid resolution reason")
	ErrInvalidResolutionRef        = errors.New("feed: invalid resolution ref")
	ErrInvalidLimit                = errors.New("feed: invalid limit")
	ErrInvalidCursor               = errors.New("feed: invalid cursor")
	ErrInvalidExceptionFilter      = errors.New("feed: invalid counts projection exception filter")
	ErrInvalidPreviewFilter        = errors.New("feed: invalid generation preview filter")
	ErrCountsResolverUnavailable   = errors.New("feed: counts projection exception resolver unavailable")
	ErrCountsListerUnavailable     = errors.New("feed: counts projection exception lister unavailable")
	ErrCountsProjectionUnavailable = errors.New("feed: counts projection provider unavailable")
	ErrIdempotencyConflict         = errors.New("feed: idempotency key reused with different payload")
	ErrProjectionExceptionNotFound = errors.New("feed: counts projection exception not found")
	ErrProjectionExceptionClosed   = errors.New("feed: counts projection exception already closed")
)

const (
	minIdempotencyKeyLen   = 8
	maxIdempotencyKeyLen   = 200
	maxResolutionReasonLen = 2000
	maxResolutionRefLen    = 500
	defaultExceptionLimit  = int32(50)
	maxExceptionLimit      = int32(200)
	defaultPreviewLimit    = int32(50)
	maxPreviewLimit        = int32(200)
)

type countsProjectionExceptionResolver interface {
	ResolveProjectionException(ctx context.Context, in countsdomain.ProjectionExceptionResolutionRequest) (countsdomain.ProjectionExceptionResolution, error)
}

type countsProjectionExceptionLister interface {
	ListProjectionExceptions(ctx context.Context, in countsdomain.ProjectionExceptionQuery) (countsdomain.ProjectionExceptionList, error)
}

type countsProjectionProvider interface {
	ProjectedCountFor(ctx context.Context, req countsdomain.CountProjectionRequest) (countsdomain.CountProjection, error)
}

// feedProjectedCountsProvider is the LIVE-HERD feed projection, deliberately separate from
// countsProjectionProvider above.
//
// The two answer the same question from different sources and both are wired. countsProjection
// replays movements over a physically-counted anchor (count_base_anchors); this one starts from
// the live goats table and applies only approved-but-unexecuted shiftings. Per the maintainer
// decision of 2026-07-19 this farm runs no physical counting workflow, so the live herd is the
// count -- but the anchor path stays for the counts-source import and parity tooling, so the
// seams stay separate rather than one quietly shadowing the other.
type feedProjectedCountsProvider interface {
	ProjectedShedCountsForFeed(ctx context.Context, req countsdomain.FeedProjectedCountQuery) (countsdomain.FeedProjectedCounts, error)
}

// Service coordinates feed-direction use-cases over the repository boundary.
type Service struct {
	repo                      ports.Repository
	countsExceptionResolution countsProjectionExceptionResolver
	countsExceptionList       countsProjectionExceptionLister
	countsProjection          countsProjectionProvider
	feedProjectedCounts       feedProjectedCountsProvider
	now                       func() time.Time
}

// NewService constructs a Service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// WithClock overrides the service clock for deterministic tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Service) WithCountsProjectionExceptionResolver(resolver countsProjectionExceptionResolver) *Service {
	s.countsExceptionResolution = resolver
	return s
}

func (s *Service) WithCountsProjectionExceptionLister(lister countsProjectionExceptionLister) *Service {
	s.countsExceptionList = lister
	return s
}

func (s *Service) WithCountsProjectionProvider(provider countsProjectionProvider) *Service {
	s.countsProjection = provider
	return s
}

// WithFeedProjectedCounts wires the live-herd feed projection.
func (s *Service) WithFeedProjectedCounts(provider feedProjectedCountsProvider) *Service {
	s.feedProjectedCounts = provider
	return s
}

// ProjectedShedCountsForFeed returns the live-herd projected shed counts for a feed day.
//
// No HTTP endpoint is wired to this yet -- it is exposed on the feed service so the feed
// generation path can consume it, and so the provider seam is registered rather than left for a
// later change to invent a second one.
func (s *Service) ProjectedShedCountsForFeed(ctx context.Context, req countsdomain.FeedProjectedCountQuery) (countsdomain.FeedProjectedCounts, error) {
	if s.feedProjectedCounts == nil {
		return countsdomain.FeedProjectedCounts{}, ErrCountsProjectionUnavailable
	}
	return s.feedProjectedCounts.ProjectedShedCountsForFeed(ctx, req)
}

// RecordDirection records a shed feed-direction execution (idempotent).
func (s *Service) RecordDirection(ctx context.Context, in domain.NewDirection) (string, bool, error) {
	return s.repo.RecordDirection(ctx, in)
}

// AcceptDirection accepts a recorded direction on verification, returning its verification context
// (idempotent: applied is false on replay).
func (s *Service) AcceptDirection(ctx context.Context, tenantID, completionID string, verifiedBy *string) (domain.AcceptedDirection, bool, error) {
	return s.repo.AcceptDirection(ctx, tenantID, completionID, verifiedBy)
}

// RejectDirection rejects a recorded direction (rework). applied is false on replay.
func (s *Service) RejectDirection(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (bool, error) {
	return s.repo.RejectDirection(ctx, tenantID, completionID, reason, verifiedBy)
}

// ShedHistory returns a shed's feed history.
func (s *Service) ShedHistory(ctx context.Context, tenantID, shedID string, limit int32) ([]domain.DirectionHistoryItem, error) {
	return s.repo.ListDirectionsByShed(ctx, tenantID, shedID, limit)
}

// VerificationQueue returns directions awaiting review (earliest fed first).
func (s *Service) VerificationQueue(ctx context.Context, tenantID string, limit int32) ([]domain.RecordedDirection, error) {
	return s.repo.ListRecordedDirections(ctx, tenantID, limit)
}

func (s *Service) ListCountsProjectionExceptions(ctx context.Context, in domain.CountsProjectionExceptionQuery) (domain.CountsProjectionExceptionList, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	if in.Status == "" {
		in.Status = "open"
	}
	in.ParkID = trimOptional(in.ParkID)
	in.ShedID = trimOptional(in.ShedID)
	in.ExceptionType = trimOptionalLower(in.ExceptionType)
	in.Severity = trimOptionalLower(in.Severity)
	in.OwnerRef = trimOptional(in.OwnerRef)
	in.WorkState = trimOptionalLower(in.WorkState)
	if in.TenantID == "" {
		return domain.CountsProjectionExceptionList{}, ErrMissingRequiredField
	}
	if !oneOf(in.Status, "open", "resolved", "dismissed") ||
		(in.ParkID != nil && !countsdomain.IsUUIDString(*in.ParkID)) ||
		(in.ShedID != nil && !countsdomain.IsUUIDString(*in.ShedID)) ||
		(in.ExceptionType != nil && !oneOf(*in.ExceptionType, "missing_base_count", "missing_structured_impact", "unreported_shifting", "count_mismatch", "alias_conflict", "ration_context_unresolved", "destination_shortage", "unsafe_surplus", "query_plan_unproven")) ||
		(in.Severity != nil && !oneOf(*in.Severity, "warning", "blocking", "critical")) ||
		(in.WorkState != nil && !oneOf(*in.WorkState, "blocked", "resolved", "dismissed")) {
		return domain.CountsProjectionExceptionList{}, ErrInvalidExceptionFilter
	}
	if in.Limit == 0 {
		in.Limit = defaultExceptionLimit
	}
	if in.Limit < 0 || in.Limit > maxExceptionLimit {
		return domain.CountsProjectionExceptionList{}, ErrInvalidLimit
	}
	var cursor *countsdomain.ProjectionExceptionCursor
	if in.Cursor != nil {
		raw := strings.TrimSpace(*in.Cursor)
		if raw != "" {
			decoded, err := countsdomain.DecodeProjectionExceptionCursor(raw)
			if err != nil {
				return domain.CountsProjectionExceptionList{}, ErrInvalidCursor
			}
			cursor = &decoded
		}
	}
	if s.countsExceptionList == nil {
		return domain.CountsProjectionExceptionList{}, ErrCountsListerUnavailable
	}
	out, err := s.countsExceptionList.ListProjectionExceptions(ctx, countsdomain.ProjectionExceptionQuery{
		TenantID: in.TenantID, Status: in.Status, ParkID: in.ParkID, ShedID: in.ShedID,
		ExceptionType: in.ExceptionType, Severity: in.Severity, OwnerRef: in.OwnerRef,
		WorkState: in.WorkState, Cursor: cursor, Limit: in.Limit,
	})
	if err != nil {
		return domain.CountsProjectionExceptionList{}, err
	}
	items := make([]domain.CountsProjectionException, 0, len(out.Items))
	for _, item := range out.Items {
		items = append(items, mapCountsProjectionException(item))
	}
	return domain.CountsProjectionExceptionList{Items: items, NextCursor: out.NextCursor}, nil
}

func (s *Service) GenerationPreview(ctx context.Context, in domain.GenerationPreviewQuery) (domain.GenerationPreview, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ParkID = strings.TrimSpace(in.ParkID)
	in.ShedID = trimOptional(in.ShedID)
	in.BreedKey = trimOptionalLower(in.BreedKey)
	in.Cursor = trimOptional(in.Cursor)
	if in.TenantID == "" || in.ParkID == "" || in.TargetDate.IsZero() {
		return domain.GenerationPreview{}, ErrMissingRequiredField
	}
	if !countsdomain.IsUUIDString(in.ParkID) ||
		(in.ShedID != nil && !countsdomain.IsUUIDString(*in.ShedID)) {
		return domain.GenerationPreview{}, ErrInvalidPreviewFilter
	}
	if in.Limit == 0 {
		in.Limit = defaultPreviewLimit
	}
	if in.Limit < 0 || in.Limit > maxPreviewLimit {
		return domain.GenerationPreview{}, ErrInvalidLimit
	}
	if s.countsProjection == nil {
		return domain.GenerationPreview{}, ErrCountsProjectionUnavailable
	}
	projection, err := s.countsProjection.ProjectedCountFor(ctx, countsdomain.CountProjectionRequest{
		TenantID:   in.TenantID,
		ParkID:     in.ParkID,
		TargetDate: dateOnly(in.TargetDate),
		ShedID:     in.ShedID,
		BreedKey:   in.BreedKey,
		Cursor:     in.Cursor,
		Limit:      in.Limit,
	})
	if err != nil {
		return domain.GenerationPreview{}, err
	}
	return mapGenerationPreview(projection), nil
}

func (s *Service) ResolveCountsProjectionException(ctx context.Context, in domain.CountsProjectionExceptionResolutionCommand) (domain.CountsProjectionExceptionResolution, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ProjectionExceptionID = strings.TrimSpace(in.ProjectionExceptionID)
	in.Action = strings.ToLower(strings.TrimSpace(in.Action))
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.ResolutionReason = strings.TrimSpace(in.ResolutionReason)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.ResolutionRef != nil {
		ref := strings.TrimSpace(*in.ResolutionRef)
		in.ResolutionRef = ptrIfNotEmpty(ref)
	}
	if in.TenantID == "" || in.ProjectionExceptionID == "" || in.ActorID == "" ||
		in.ResolutionReason == "" || in.IdempotencyKey == "" {
		return domain.CountsProjectionExceptionResolution{}, ErrMissingRequiredField
	}
	if len(in.IdempotencyKey) < minIdempotencyKeyLen || len(in.IdempotencyKey) > maxIdempotencyKeyLen {
		return domain.CountsProjectionExceptionResolution{}, ErrInvalidIdempotencyKey
	}
	if len(in.ResolutionReason) > maxResolutionReasonLen {
		return domain.CountsProjectionExceptionResolution{}, ErrInvalidResolutionReason
	}
	if in.ResolutionRef != nil && len(*in.ResolutionRef) > maxResolutionRefLen {
		return domain.CountsProjectionExceptionResolution{}, ErrInvalidResolutionRef
	}
	if in.Action != "resolve" && in.Action != "dismiss" {
		return domain.CountsProjectionExceptionResolution{}, ErrInvalidResolutionAction
	}
	if s.countsExceptionResolution == nil {
		return domain.CountsProjectionExceptionResolution{}, ErrCountsResolverUnavailable
	}
	out, err := s.countsExceptionResolution.ResolveProjectionException(ctx, countsdomain.ProjectionExceptionResolutionRequest{
		TenantID:              in.TenantID,
		ProjectionExceptionID: in.ProjectionExceptionID,
		Action:                in.Action,
		ResolvedByRef:         in.ActorID,
		ResolutionReason:      in.ResolutionReason,
		ResolutionRef:         in.ResolutionRef,
		IdempotencyKey:        in.IdempotencyKey,
		RequestFingerprint:    countsProjectionExceptionResolutionFingerprint(in),
	})
	if err != nil {
		return domain.CountsProjectionExceptionResolution{}, mapCountsProjectionExceptionError(err)
	}
	return domain.CountsProjectionExceptionResolution{
		ResolutionID:          out.ProjectionExceptionResolutionID,
		ProjectionExceptionID: out.ProjectionExceptionID,
		Action:                out.Action,
		Status:                out.Status,
		WorkState:             out.WorkState,
		ResolvedByRef:         out.ResolvedByRef,
		ResolutionReason:      out.ResolutionReason,
		ResolutionRef:         out.ResolutionRef,
		ResolvedAt:            out.ResolvedAt,
		Replayed:              out.Replayed,
	}, nil
}

func mapGenerationPreview(in countsdomain.CountProjection) domain.GenerationPreview {
	blockers := generationPreviewBlockers(in)
	status := domain.ReadinessReady
	blockerReason := ""
	if len(blockers) > 0 {
		status = domain.ReadinessBlocked
		blockerReason = generationPreviewBlockerSummary(blockers)
	}
	return domain.GenerationPreview{
		TenantID:              in.TenantID,
		ParkID:                in.ParkID,
		TargetDate:            dateOnly(in.TargetDate),
		Status:                status,
		GenerationAllowed:     len(blockers) == 0,
		BlockerReason:         blockerReason,
		SnapshotID:            in.SnapshotID,
		ProjectionStatus:      in.ProjectionStatus,
		SourceContractVersion: in.SourceContractVersion,
		SourceHash:            in.SourceHash,
		BaseAnchorIDsHash:     in.BaseAnchorIDsHash,
		ShiftingEventIDsHash:  in.ShiftingEventIDsHash,
		ExceptionCount:        in.ExceptionCount,
		TotalRowCount:         in.TotalRowCount,
		Rows:                  generationPreviewRows(in.Rows),
		ShedBreedTotals:       generationPreviewTotals(in.ShedBreedTotals),
		Blockers:              blockers,
		NextCursor:            in.NextCursor,
	}
}

func generationPreviewRows(in []countsdomain.ProjectionRow) []domain.GenerationPreviewRow {
	if len(in) == 0 {
		return []domain.GenerationPreviewRow{}
	}
	out := make([]domain.GenerationPreviewRow, 0, len(in))
	for _, row := range in {
		out = append(out, domain.GenerationPreviewRow{
			ProjectionRowID:              row.ProjectionRowID,
			ParkID:                       row.ParkID,
			ShedID:                       row.ShedID,
			TargetDate:                   dateOnly(row.TargetDate),
			GrainKey:                     row.GrainKey,
			BaseCountAnchorID:            row.BaseCountAnchorID,
			IncludedShiftingEventIDsHash: row.IncludedShiftingEventIDsHash,
			BreedID:                      row.BreedID,
			BreedKey:                     row.BreedKey,
			BreedLabel:                   row.BreedLabel,
			StageTag:                     row.StageTag,
			AgeClass:                     row.AgeClass,
			Sex:                          row.Sex,
			HeadCount:                    row.HeadCount,
			PregnantCount:                row.PregnantCount,
			LactatingCount:               row.LactatingCount,
			WarmupCount:                  row.WarmupCount,
			RationContextResolutionState: row.RationContextResolutionState,
			RationContextRef:             row.RationContextRef,
			BlockerReason:                row.BlockerReason,
			SourceRowHash:                row.SourceRowHash,
		})
	}
	return out
}

func generationPreviewTotals(in []countsdomain.ProjectionShedBreedTotal) []domain.GenerationPreviewTotal {
	if len(in) == 0 {
		return []domain.GenerationPreviewTotal{}
	}
	out := make([]domain.GenerationPreviewTotal, 0, len(in))
	for _, total := range in {
		out = append(out, domain.GenerationPreviewTotal{
			ParkID: total.ParkID, ShedID: total.ShedID, BreedKey: total.BreedKey,
			BreedLabel: total.BreedLabel, HeadCount: total.HeadCount,
			PregnantCount: total.PregnantCount, LactatingCount: total.LactatingCount,
			WarmupCount: total.WarmupCount, RationContextResolutionState: total.RationContextResolutionState,
		})
	}
	return out
}

func generationPreviewBlockers(projection countsdomain.CountProjection) []domain.GenerationPreviewBlocker {
	blockers := make([]domain.GenerationPreviewBlocker, 0, len(projection.Blockers)+len(projection.Exceptions)+len(projection.Rows))
	if strings.ToLower(strings.TrimSpace(projection.ProjectionStatus)) != "ready" {
		blockers = append(blockers, domain.GenerationPreviewBlocker{
			Source:        "projection",
			Type:          "projection_not_ready",
			SourceKey:     projection.SnapshotID,
			GrainKey:      "park:" + projection.ParkID,
			Severity:      "blocking",
			BlockerReason: "Counts/Shifting projection status is not ready for Feed Direction generation preview.",
		})
	}
	for _, blocker := range projection.Blockers {
		blockers = append(blockers, domain.GenerationPreviewBlocker{
			Source:        "counts_projection",
			Type:          blocker.ExceptionType,
			SourceKey:     blocker.SourceKey,
			GrainKey:      blocker.GrainKey,
			Severity:      blocker.Severity,
			BlockerReason: blocker.BlockerReason,
		})
	}
	for _, exception := range projection.Exceptions {
		blockers = append(blockers, domain.GenerationPreviewBlocker{
			Source:        "counts_projection_exception",
			Type:          exception.ExceptionType,
			SourceKey:     exception.SourceKey,
			GrainKey:      exception.GrainKey,
			Severity:      exception.Severity,
			BlockerReason: exception.BlockerReason,
		})
	}
	if projection.ExceptionCount > 0 && len(projection.Exceptions) == 0 {
		blockers = append(blockers, domain.GenerationPreviewBlocker{
			Source:        "counts_projection_exception",
			Type:          "open_exception_count",
			SourceKey:     projection.SnapshotID,
			GrainKey:      "park:" + projection.ParkID,
			Severity:      "blocking",
			BlockerReason: "Counts/Shifting reports open projection exceptions outside this page.",
		})
	}
	for _, row := range projection.Rows {
		if rationContextResolved(row.RationContextResolutionState) && row.BlockerReason == nil {
			continue
		}
		reason := strings.TrimSpace(ptrValue(row.BlockerReason))
		if reason == "" {
			reason = "ration context is not resolved for this shed/breed/stage projection row"
		}
		blockers = append(blockers, domain.GenerationPreviewBlocker{
			Source:        "projection_row",
			Type:          "ration_context_unresolved",
			SourceKey:     row.ProjectionRowID,
			GrainKey:      row.GrainKey,
			Severity:      "blocking",
			BlockerReason: reason,
		})
	}
	for _, total := range projection.ShedBreedTotals {
		if rationContextResolved(total.RationContextResolutionState) {
			continue
		}
		blockers = append(blockers, domain.GenerationPreviewBlocker{
			Source:        "shed_breed_total",
			Type:          "ration_context_unresolved",
			SourceKey:     total.ShedID + ":" + total.BreedKey,
			GrainKey:      total.ShedID + ":" + total.BreedKey,
			Severity:      "blocking",
			BlockerReason: "aggregate shed/breed total has unresolved ration context",
		})
	}
	return blockers
}

func generationPreviewBlockerSummary(blockers []domain.GenerationPreviewBlocker) string {
	const maxDetails = 4
	details := make([]string, 0, maxDetails)
	for _, blocker := range blockers {
		detail := blocker.Type
		if blocker.GrainKey != "" {
			detail += "@" + blocker.GrainKey
		}
		if reason := strings.TrimSpace(blocker.BlockerReason); reason != "" {
			detail += " (" + reason + ")"
		}
		details = append(details, detail)
		if len(details) == maxDetails {
			break
		}
	}
	summary := "Counts/Shifting projection has Feed Direction blockers: " + strings.Join(details, "; ")
	if remaining := len(blockers) - len(details); remaining > 0 {
		summary += fmt.Sprintf("; +%d more", remaining)
	}
	return summary
}

func rationContextResolved(state string) bool {
	return strings.ToLower(strings.TrimSpace(state)) == "resolved"
}

func mapCountsProjectionException(in countsdomain.ProjectionException) domain.CountsProjectionException {
	evidence := json.RawMessage([]byte(`{}`))
	if len(in.EvidenceJSON) > 0 {
		evidence = append(json.RawMessage(nil), in.EvidenceJSON...)
	}
	return domain.CountsProjectionException{
		ProjectionExceptionID: in.ProjectionExceptionID,
		ProjectionSnapshotID:  in.ProjectionSnapshotID,
		ExceptionType:         in.ExceptionType,
		SourceKey:             in.SourceKey,
		GrainKey:              in.GrainKey,
		ParkID:                in.ParkID,
		ShedID:                in.ShedID,
		BreedKey:              in.BreedKey,
		StageTag:              in.StageTag,
		Severity:              in.Severity,
		Status:                in.Status,
		OwnerRef:              in.OwnerRef,
		WorkType:              in.WorkType,
		WorkState:             in.WorkState,
		DueAt:                 in.DueAt,
		NextAction:            in.NextAction,
		EvidenceLink:          in.EvidenceLink,
		BlockerReason:         in.BlockerReason,
		EvidenceJSON:          evidence,
		ResolutionID:          in.ResolutionID,
		ResolvedByRef:         in.ResolvedByRef,
		ResolutionReason:      in.ResolutionReason,
		ResolutionRef:         in.ResolutionRef,
		ResolvedAt:            in.ResolvedAt,
		CreatedAt:             in.CreatedAt,
		UpdatedAt:             in.UpdatedAt,
	}
}

func mapCountsProjectionExceptionError(err error) error {
	switch {
	case errors.Is(err, countsports.ErrIdempotencyConflict):
		return ErrIdempotencyConflict
	case errors.Is(err, countsports.ErrProjectionExceptionNotFound):
		return ErrProjectionExceptionNotFound
	case errors.Is(err, countsports.ErrProjectionExceptionClosed):
		return ErrProjectionExceptionClosed
	default:
		return err
	}
}

func countsProjectionExceptionResolutionFingerprint(in domain.CountsProjectionExceptionResolutionCommand) string {
	ref := ""
	if in.ResolutionRef != nil {
		ref = *in.ResolutionRef
	}
	parts := []string{in.TenantID, in.ProjectionExceptionID, in.Action, in.ActorID, in.ResolutionReason, ref}
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ptrIfNotEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
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

func (s *Service) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

func dateOnly(t time.Time) time.Time {
	return biztime.BusinessDayStart(t)
}
