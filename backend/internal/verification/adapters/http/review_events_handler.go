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
// fullscreen_toggled, verdict_recorded). Gated on verification.review -- the same permission that
// gates seeing the queue at all, since this is metadata ABOUT that same review activity.
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
