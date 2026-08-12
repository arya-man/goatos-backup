package http

import (
	nethttp "net/http"

	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/verification/app"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// reviewEventRequest is the wire shape for one client-emitted review-analytics event. See
// migrations/postgres/000116_verification_review_events.sql and domain/review_events.go for the
// closed event_type vocabulary and payload contract.
type reviewEventRequest struct {
	ItemID        string                    `json:"item_id"`
	ProofID       string                    `json:"proof_id,omitempty"`
	SessionID     string                    `json:"session_id"`
	EventType     string                    `json:"event_type"`
	OccurredAt    string                    `json:"occurred_at"`
	Payload       domain.ReviewEventPayload `json:"payload,omitempty"`
	ClientEventID string                    `json:"client_event_id"`
}

type reviewEventBatchRequest struct {
	Events []reviewEventRequest `json:"events"`
}

type reviewEventBatchResponse struct {
	Inserted int    `json:"inserted"`
	TraceID  string `json:"trace_id"`
}

// RecordReviewEvents is the browser's periodic/on-unload flush of verifier video-review telemetry
// (queue_opened, item_opened, video_play/pause/seek_attempt/ended, proof_switched,
// fullscreen_toggled, verdict_recorded). Gated on verification.verdict -- the VERIFIER's own
// authority, not the leadership-visible verification.review that gates seeing the queue. Leadership
// (CEO/CxO) deliberately holds review and never verdict, so gating ingest on review would let a
// read-only principal write rows into the very stream that audits the verifier. Reading the derived
// facts (GET .../review-facts) stays on verification.review.
func (h *Handler) RecordReviewEvents(w nethttp.ResponseWriter, r *nethttp.Request) {
	if h.reviewEvent == nil {
		h.respondError(w, r, app.NotFound("review_events_not_configured", "review-event ingest is not wired"))
		return
	}
	var body reviewEventBatchRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Events) == 0 {
		h.respondError(w, r, app.BadRequest("empty_batch", "at least one event is required"))
		return
	}
	authorizedCategories, ok := h.resolveVerifierCategories(w, r, "")
	if !ok {
		return
	}
	inputs := make([]app.ReviewEventBatchInput, len(body.Events))
	for i, e := range body.Events {
		inputs[i] = app.ReviewEventBatchInput{
			ItemID:        e.ItemID,
			ProofID:       e.ProofID,
			SessionID:     e.SessionID,
			EventType:     e.EventType,
			OccurredAt:    e.OccurredAt,
			Payload:       e.Payload,
			ClientEventID: e.ClientEventID,
		}
	}
	inserted, err := h.service.RecordReviewEvents(r.Context(), h.reviewEvent, tenantID(r), actorID(r), authorizedCategories, inputs)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, reviewEventBatchResponse{Inserted: inserted, TraceID: traceID(r)})
}

type itemReviewFactsResponse struct {
	Facts   []itemReviewFactsEntry `json:"facts"`
	TraceID string                 `json:"trace_id"`
}

type itemReviewFactsEntry struct {
	ActorID              string   `json:"actor_id"`
	ProofDurationMs      int64    `json:"proof_duration_ms"`
	WatchedDistinctMs    int64    `json:"watched_distinct_ms"`
	WatchFraction        float64  `json:"watch_fraction"`
	PlayCount            int      `json:"play_count"`
	PauseCount           int      `json:"pause_count"`
	SeekAttemptCount     int      `json:"seek_attempt_count"`
	TimeToVerdictSeconds *float64 `json:"time_to_verdict_seconds,omitempty"`
	WatchedFull          bool     `json:"watched_full"`
}

// GetItemReviewFacts serves the derived per-actor watch/timing integrity facts for one item -- the
// CEO-facing "did the verifier actually watch this" proof for a single video, read-time computed
// from the raw event stream (bounded by that item's own event count).
func (h *Handler) GetItemReviewFacts(w nethttp.ResponseWriter, r *nethttp.Request) {
	if h.reviewEvent == nil {
		h.respondError(w, r, app.NotFound("review_events_not_configured", "review-event ingest is not wired"))
		return
	}
	authorizedCategories, ok := h.resolveVerifierCategories(w, r, "")
	if !ok {
		return
	}
	facts, err := h.service.ItemReviewFacts(r.Context(), h.reviewEvent, tenantID(r), actorID(r), authorizedCategories, r.PathValue("item_id"))
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	entries := make([]itemReviewFactsEntry, len(facts))
	for i, f := range facts {
		entries[i] = itemReviewFactsEntry{
			ActorID:              f.ActorID,
			ProofDurationMs:      f.ProofDurationMs,
			WatchedDistinctMs:    f.WatchedDistinctMs,
			WatchFraction:        f.WatchFraction,
			PlayCount:            f.PlayCount,
			PauseCount:           f.PauseCount,
			SeekAttemptCount:     f.SeekAttemptCount,
			TimeToVerdictSeconds: f.TimeToVerdictSeconds,
			WatchedFull:          f.WatchedFull,
		}
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, itemReviewFactsResponse{Facts: entries, TraceID: traceID(r)})
}

type oversightAnalyticsResponse struct {
	KPIs             oversightKPIsResponse          `json:"kpis"`
	PendingByModule  []modulePendingBacklogResponse `json:"pending_by_module"`
	VerifierActivity []verifierActivityResponse     `json:"verifier_activity"`
	TraceID          string                         `json:"trace_id"`
}

type oversightKPIsResponse struct {
	VideosWaiting                     int                     `json:"videos_waiting"`
	OldestPendingAgeHours             *float64                `json:"oldest_pending_age_hours,omitempty"`
	VerdictsPerActiveDayLast7d        float64                 `json:"verdicts_per_active_day_last_7d"`
	EstDaysToClearBacklog             *float64                `json:"est_days_to_clear_backlog,omitempty"`
	PerModuleMedianReviewLatencyHours []moduleLatencyResponse `json:"per_module_median_review_latency_hours"`
	RejectRateLast30d                 *float64                `json:"reject_rate_last_30d,omitempty"`
}

type moduleLatencyResponse struct {
	Module      string  `json:"module"`
	MedianHours float64 `json:"median_hours"`
}

type modulePendingBacklogResponse struct {
	Module string `json:"module"`
	Count  int    `json:"count"`
}

type verifierActivityResponse struct {
	VerifierID         string `json:"verifier_id"`
	VerifierName       string `json:"verifier_name,omitempty"`
	Verdicts           int    `json:"verdicts"`
	Approved           int    `json:"approved"`
	Rejected           int    `json:"rejected"`
	BusiestDay         string `json:"busiest_day,omitempty"`
	ItemsTracked       int    `json:"items_tracked"`
	WatchedToEndCount  int    `json:"watched_to_end_count"`
	VerdictWithoutPlay int    `json:"verdict_without_play_count"`
}

// GetOversightAnalytics serves the CEO/PC-Director-only aggregate analytics rendered above the
// /verify queue table. Authorization is enforced entirely at the route-permission layer
// (permissions.VerificationOversee on this route in routes.go) -- there is no in-handler role
// check, matching every other route in this module.
func (h *Handler) GetOversightAnalytics(w nethttp.ResponseWriter, r *nethttp.Request) {
	result, err := h.service.OversightAnalytics(r.Context(), tenantID(r))
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	modules := make([]moduleLatencyResponse, len(result.KPIs.PerModuleMedianReviewLatencyHours))
	for i, m := range result.KPIs.PerModuleMedianReviewLatencyHours {
		modules[i] = moduleLatencyResponse{Module: m.Module, MedianHours: m.MedianHours}
	}
	backlog := make([]modulePendingBacklogResponse, len(result.PendingByModule))
	for i, b := range result.PendingByModule {
		backlog[i] = modulePendingBacklogResponse{Module: b.Module, Count: b.Count}
	}
	activity := make([]verifierActivityResponse, len(result.VerifierActivity))
	for i, a := range result.VerifierActivity {
		activity[i] = verifierActivityResponse{
			VerifierID:         a.VerifierID,
			VerifierName:       a.VerifierName,
			Verdicts:           a.Verdicts,
			Approved:           a.Approved,
			Rejected:           a.Rejected,
			BusiestDay:         a.BusiestDay,
			ItemsTracked:       a.ItemsTracked,
			WatchedToEndCount:  a.WatchedToEndCount,
			VerdictWithoutPlay: a.VerdictWithoutPlay,
		}
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, oversightAnalyticsResponse{
		KPIs: oversightKPIsResponse{
			VideosWaiting:                     result.KPIs.VideosWaiting,
			OldestPendingAgeHours:             result.KPIs.OldestPendingAgeHours,
			VerdictsPerActiveDayLast7d:        result.KPIs.VerdictsPerActiveDayLast7d,
			EstDaysToClearBacklog:             result.KPIs.EstDaysToClearBacklog,
			PerModuleMedianReviewLatencyHours: modules,
			RejectRateLast30d:                 result.KPIs.RejectRateLast30d,
		},
		PendingByModule:  backlog,
		VerifierActivity: activity,
		TraceID:          traceID(r),
	})
}
