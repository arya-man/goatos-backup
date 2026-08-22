package app

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

const (
	// maxActivityRangeDays bounds the window a single overlay read may span. The movement
	// history it overlays is itself bucket-capped at 2000 buckets, so a request for a year of
	// markers is asking for something no chart can draw.
	maxActivityRangeDays = 31
	// maxActivityEvents bounds the markers one response may carry. Beyond this the client is
	// told the window is truncated and must narrow the range -- an overlay that silently drops
	// half its markers is worse than one that says it did.
	maxActivityEvents = 500
)

// GetTagActivity returns the farm records that may be drawn beside one tag's movement history.
//
// THE BOUNDARY THIS FUNCTION EXISTS TO ENFORCE (migration 000196): an animal's history with a
// tag starts at the instant the tag was mapped to it. `from` is clamped UP to that instant
// before a single record is read, so no farm event from the tag's device-bench period can ever
// reach the response. A tag with no animal behind it returns an empty result with a reason --
// never an error, and never a zero pretending to be an observation.
//
// WHAT THE RESULT MEANS: co-occurrence in time. Nothing here explains a movement change, and no
// movement here confirms anything about a record. See domain.ActivityCorrelationNote, which is
// returned with every response so the boundary travels with the data.
func (s *Service) GetTagActivity(ctx context.Context, actor domain.Actor, tagID, from, to string) (domain.ActivityResponse, error) {
	if actor.TenantID == "" {
		return domain.ActivityResponse{}, fmt.Errorf("actor tenant_id required")
	}

	fromTime, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return domain.ActivityResponse{}, fmt.Errorf("invalid from timestamp: %w", domain.ErrValidation)
	}
	toTime, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return domain.ActivityResponse{}, fmt.Errorf("invalid to timestamp: %w", domain.ErrValidation)
	}
	if !toTime.After(fromTime) {
		return domain.ActivityResponse{}, fmt.Errorf("to must be after from: %w", domain.ErrValidation)
	}
	if toTime.Sub(fromTime) > maxActivityRangeDays*24*time.Hour {
		return domain.ActivityResponse{}, fmt.Errorf("range spans more than %d days: %w", maxActivityRangeDays, domain.ErrValidation)
	}

	scope, err := s.repo.GetTagActivityScope(ctx, actor.TenantID, tagID)
	if err != nil {
		s.log.Error("failed to resolve tag activity scope", "tag_id", tagID, "error", err)
		return domain.ActivityResponse{}, fmt.Errorf("resolve tag activity scope failed: %w", err)
	}
	if scope == nil {
		return domain.ActivityResponse{}, fmt.Errorf("tag %q: %w", tagID, domain.ErrTagNotFound)
	}

	resp := domain.ActivityResponse{
		TagID:            tagID,
		From:             fromTime,
		To:               toTime,
		MonitoringSince:  scope.MonitoringSince,
		Events:           []domain.ActivityEvent{},
		UnavailableKinds: []domain.UnavailableActivityKind{},
		CorrelationNote:  domain.ActivityCorrelationNote,
	}

	// No animal behind the tag: its packets are device telemetry, so there is no farm activity
	// to show -- not "zero events for this animal", but "there is no animal". Empty + reason.
	if scope.GoatID == "" {
		reason := domain.ActivityReasonTagNotMapped
		resp.Reason = &reason
		return resp, nil
	}
	// Mapped, but we cannot say WHEN the mapping happened. Fail closed: attributing a whole
	// record history to a boundary we cannot state is exactly what 000196 forbids.
	if scope.MonitoringSince == nil {
		reason := domain.ActivityReasonMonitoringBoundaryUnknown
		resp.Reason = &reason
		return resp, nil
	}

	// THE CLAMP. Everything before this instant is the tag's device period.
	effectiveFrom := fromTime
	if scope.MonitoringSince.After(effectiveFrom) {
		effectiveFrom = *scope.MonitoringSince
	}
	resp.From = effectiveFrom
	if !toTime.After(effectiveFrom) {
		reason := domain.ActivityReasonWindowBeforeMonitoring
		resp.Reason = &reason
		return resp, nil
	}

	events, err := s.repo.ListFarmActivity(ctx, actor.TenantID, *scope, effectiveFrom, toTime, maxActivityEvents)
	if err != nil {
		s.log.Error("failed to list farm activity", "tag_id", tagID, "error", err)
		return domain.ActivityResponse{}, fmt.Errorf("list farm activity failed: %w", err)
	}
	if len(events) > maxActivityEvents {
		events = events[:maxActivityEvents]
		resp.Truncated = true
	}
	if len(events) > 0 {
		resp.Events = events
	}
	return resp, nil
}
