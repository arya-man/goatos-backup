package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Shifting EXECUTION endpoints -- the operator's half of a movement.
//
//	GET  /app/counts/shifting-events/pending-execution   -- authorized, waiting to be walked
//	POST /app/counts/shifting-events/{shifting_event_id}/complete
//	POST /app/counts/shifting-events/{shifting_event_id}/cancel
//
// All three are gated on CountsWrite, NOT on the approval permissions: executing a movement is the
// operator's job, and the approver who authorized it is usually not the person in the park. Any
// operator holding CountsWrite may complete or cancel any movement in their tenant (maintainer
// decision, 2026-07-19) -- there is deliberately no "only the raiser" check, because the person who
// witnesses the animals move is not reliably the person who typed the request.

const (
	appShiftingPendingExecutionRoute = "/app/counts/shifting-events/pending-execution"
	appShiftingCompleteRoute         = "/app/counts/shifting-events/{shifting_event_id}/complete"
	appShiftingCancelRoute           = "/app/counts/shifting-events/{shifting_event_id}/cancel"

	appShiftingCompleteCommand = "counts.app.shifting_complete"
	appShiftingCancelCommand   = "counts.app.shifting_cancel"
)

// ShiftingExecutionWorkflow is the slice of counts/app.ShiftingExecutionService this handler needs.
type ShiftingExecutionWorkflow interface {
	Complete(ctx context.Context, in countsapp.CompleteShiftingInput) (domain.ShiftingExecutionResult, bool, error)
	Cancel(ctx context.Context, in countsapp.CancelShiftingInput) (domain.ShiftingExecutionResult, bool, error)
	ListPendingExecution(ctx context.Context, tenantID, sourceParkID string, pageSize int, cursor string) (domain.ShiftingExecutionPage, error)
}

// WithShiftingExecutionWorkflow injects the execution service. A handler without it answers 501
// rather than silently pretending a movement completed.
func (h *AppWriteHandler) WithShiftingExecutionWorkflow(execution ShiftingExecutionWorkflow) *AppWriteHandler {
	h.execution = execution
	return h
}

// RegisterShiftingExecution wires the execution surface.
func RegisterShiftingExecution(mux *http.ServeMux, h *AppWriteHandler) {
	// Registered BEFORE the {shifting_event_id} patterns would matter under a router that resolves
	// by registration order. net/http's ServeMux resolves by specificity, so the literal
	// "pending-execution" segment already wins over a wildcard, but the ordering is kept explicit
	// so a future router swap cannot turn the queue into a lookup for an event whose id is the
	// literal string "pending-execution".
	mux.HandleFunc("GET "+appShiftingPendingExecutionRoute, h.ListShiftingPendingExecution)
	mux.HandleFunc("POST "+appShiftingCompleteRoute, h.CompleteShiftingEvent)
	mux.HandleFunc("POST "+appShiftingCancelRoute, h.CancelShiftingEvent)
}

// ---------------------------------------------------------------------------
// Complete / Cancel
// ---------------------------------------------------------------------------

type appShiftingCancelRequest struct {
	Reason string `json:"reason"`
}

type appShiftingExecutionResponse struct {
	ShiftingEventID string `json:"shifting_event_id"`
	EventStatus     string `json:"event_status"`

	DestinationParkID string `json:"destination_park_id"`
	DestinationShedID string `json:"destination_shed_id"`

	// MovedGoatIDs is exactly which animals the completion relocated. Empty for a cancellation,
	// which moves nobody.
	MovedGoatIDs []string `json:"moved_goat_ids"`
	MovedCount   int      `json:"moved_count"`

	AppliedAt    *time.Time `json:"applied_at,omitempty"`
	AppliedAtIST *string    `json:"applied_at_ist,omitempty"`
	AppliedBy    *string    `json:"applied_by,omitempty"`

	CanceledAt    *time.Time `json:"canceled_at,omitempty"`
	CanceledAtIST *string    `json:"canceled_at_ist,omitempty"`
	CanceledBy    *string    `json:"canceled_by,omitempty"`
	CancelReason  *string    `json:"cancel_reason,omitempty"`

	IdempotentReplay bool `json:"idempotent_replay"`
}

// CompleteShiftingEvent records that an authorized movement physically happened. THIS is where the
// animals relocate.
func (h *AppWriteHandler) CompleteShiftingEvent(w http.ResponseWriter, r *http.Request) {
	tenantID, actorID, eventID, clientKey, ok := h.executionPreamble(w, r)
	if !ok {
		return
	}

	// Completion has no body: the movement's animals, destination, and authorization are all
	// already recorded. Accepting an animal set here would let the operator's phone relocate a
	// herd the approver never signed off on.
	if raw, bodyOK := h.readBody(w, r); !bodyOK {
		return
	} else if len(strings.TrimSpace(string(raw))) > 0 {
		var body struct{}
		if err := decodeStrictJSON(raw, &body, "ShiftingCompleteRequest"); err != nil {
			h.writeAppError(w, r, err)
			return
		}
	}

	canonical, err := canonicalRequestBytes(tenantID, appShiftingCompleteCommand, appShiftingCompleteRoute, struct {
		ShiftingEventID string `json:"shifting_event_id"`
	}{ShiftingEventID: eventID})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}

	result, replay, err := h.execution.Complete(r.Context(), countsapp.CompleteShiftingInput{
		TenantID:           tenantID,
		ShiftingEventID:    eventID,
		CompletedByUserID:  actorID,
		TraceID:            appTraceID(r),
		IdempotencyKey:     "counts-shifting-completion:" + clientKey,
		RequestFingerprint: stableHash("counts-app-shifting-completion", canonical),
	})
	if err != nil {
		h.writeShiftingExecutionError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, executionResponse(result, replay))
}

// CancelShiftingEvent retires an authorized movement that will never be executed. NOTHING moves.
func (h *AppWriteHandler) CancelShiftingEvent(w http.ResponseWriter, r *http.Request) {
	tenantID, actorID, eventID, clientKey, ok := h.executionPreamble(w, r)
	if !ok {
		return
	}

	raw, bodyOK := h.readBody(w, r)
	if !bodyOK {
		return
	}
	var body appShiftingCancelRequest
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := decodeStrictJSON(raw, &body, "ShiftingCancelRequest"); err != nil {
			h.writeAppError(w, r, err)
			return
		}
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_reason",
			"reason is required to cancel a shifting", nil)
		return
	}

	// The fingerprint covers the cancellation's MEANING (which movement, what reason), so replaying
	// the same key with a different reason is a conflict rather than a silent overwrite of the
	// record of why a movement was abandoned.
	canonical, err := canonicalRequestBytes(tenantID, appShiftingCancelCommand, appShiftingCancelRoute, struct {
		ShiftingEventID string `json:"shifting_event_id"`
		Reason          string `json:"reason"`
	}{ShiftingEventID: eventID, Reason: reason})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}

	result, replay, err := h.execution.Cancel(r.Context(), countsapp.CancelShiftingInput{
		TenantID:           tenantID,
		ShiftingEventID:    eventID,
		CanceledByUserID:   actorID,
		Reason:             reason,
		IdempotencyKey:     "counts-shifting-cancel:" + clientKey,
		RequestFingerprint: stableHash("counts-app-shifting-cancel", canonical),
	})
	if err != nil {
		h.writeShiftingExecutionError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, executionResponse(result, replay))
}

// executionPreamble runs the checks both transitions share and returns the resolved identifiers.
func (h *AppWriteHandler) executionPreamble(w http.ResponseWriter, r *http.Request) (tenantID, actorID, eventID, clientKey string, ok bool) {
	tenantID = httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return "", "", "", "", false
	}
	if h.execution == nil {
		h.writeError(w, r, http.StatusNotImplemented, "shifting_execution_unavailable",
			"shifting execution workflow is not configured", nil)
		return "", "", "", "", false
	}
	actorID = httpmiddleware.ActorIDFromContext(r.Context())
	if actorID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_actor", "missing actor context", nil)
		return "", "", "", "", false
	}
	key, err := appIdempotencyKey(r)
	if err != nil {
		h.writeAppError(w, r, err)
		return "", "", "", "", false
	}
	eventID = strings.TrimSpace(r.PathValue("shifting_event_id"))
	if eventID == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_shifting_event_id",
			"shifting_event_id is required", nil)
		return "", "", "", "", false
	}
	return tenantID, actorID, eventID, key, true
}

func executionResponse(result domain.ShiftingExecutionResult, replay bool) appShiftingExecutionResponse {
	moved := result.MovedGoatIDs
	if moved == nil {
		moved = []string{}
	}
	out := appShiftingExecutionResponse{
		ShiftingEventID:   result.ShiftingEventID,
		EventStatus:       result.EventStatus,
		DestinationParkID: result.DestinationParkID,
		DestinationShedID: result.DestinationShedID,
		MovedGoatIDs:      moved,
		MovedCount:        len(moved),
		AppliedAt:         result.AppliedAt,
		AppliedBy:         result.AppliedBy,
		CanceledAt:        result.CanceledAt,
		CanceledBy:        result.CanceledBy,
		CancelReason:      result.CancelReason,
		IdempotentReplay:  replay,
	}
	out.AppliedAtIST = istLabel(result.AppliedAt)
	out.CanceledAtIST = istLabel(result.CanceledAt)
	return out
}

// istLabel renders a stored UTC instant in the Goat OS business calendar.
//
// Storage stays absolute (timestamptz), but every BUSINESS meaning an operator reads is an India
// business-calendar meaning: a movement completed at 02:00 IST belongs to that IST date, and
// showing the caller a UTC instant would put it on the previous day.
func istLabel(t *time.Time) *string {
	if t == nil {
		return nil
	}
	label := t.In(biztime.DefaultLocation()).Format(time.RFC3339)
	return &label
}

// ---------------------------------------------------------------------------
// Pending-execution queue
// ---------------------------------------------------------------------------

type appShiftingPendingExecutionResponse struct {
	Items      []appShiftingPendingExecutionItem `json:"items"`
	NextCursor string                            `json:"next_cursor,omitempty"`
}

type appShiftingPendingExecutionItem struct {
	ShiftingEventID string `json:"shifting_event_id"`
	EventStatus     string `json:"event_status"`

	Priority string `json:"priority"`
	Category string `json:"category"`

	SourceParkID   *string `json:"source_park_id,omitempty"`
	SourceParkName *string `json:"source_park_name,omitempty"`
	SourceShedID   *string `json:"source_shed_id,omitempty"`
	SourceShedName *string `json:"source_shed_name,omitempty"`

	DestinationParkID   string `json:"destination_park_id"`
	DestinationParkName string `json:"destination_park_name"`
	DestinationShedID   string `json:"destination_shed_id"`
	DestinationShedName string `json:"destination_shed_name"`

	ApprovedByUserID *string    `json:"approved_by_user_id,omitempty"`
	ApprovedAt       *time.Time `json:"approved_at,omitempty"`
	ApprovedAtIST    *string    `json:"approved_at_ist,omitempty"`

	RaisedByUserID string    `json:"raised_by_user_id,omitempty"`
	RaisedAt       time.Time `json:"raised_at"`
	RaisedAtIST    string    `json:"raised_at_ist"`
	EffectiveAt    time.Time `json:"effective_at"`

	// AnimalCount is the FULL size of the movement. Animals is a bounded preview of at most
	// domain.MaxShiftingExecutionAnimalPreview of them -- a movement may name up to 500 animals,
	// and shipping every one of them inside a 20-row page would be a 10,000-row read to draw one
	// phone screen.
	AnimalCount      int                                 `json:"animal_count"`
	AnimalsTruncated bool                                `json:"animals_truncated"`
	Animals          []appShiftingPendingExecutionAnimal `json:"animals"`
}

type appShiftingPendingExecutionAnimal struct {
	GoatID    string  `json:"goat_id"`
	DisplayID string  `json:"display_id"`
	Tag       *string `json:"tag,omitempty"`
}

// ListShiftingPendingExecution returns one keyset page of authorized movements awaiting execution.
func (h *AppWriteHandler) ListShiftingPendingExecution(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	if h.execution == nil {
		h.writeError(w, r, http.StatusNotImplemented, "shifting_execution_unavailable",
			"shifting execution workflow is not configured", nil)
		return
	}

	// Page size is capped server-side at MaxShiftingExecutionPageSize: this queue is read from a
	// phone standing in a park, and a client asking for 500 rows must get one screen of work.
	pageSize := domain.MaxShiftingExecutionPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_page_size",
				"page_size must be a positive integer", nil)
			return
		}
		if parsed < pageSize {
			pageSize = parsed
		}
	}

	page, err := h.execution.ListPendingExecution(r.Context(), tenantID,
		strings.TrimSpace(r.URL.Query().Get("park_id")), pageSize,
		strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		h.writeShiftingExecutionError(w, r, err)
		return
	}

	items := make([]appShiftingPendingExecutionItem, 0, len(page.Items))
	for _, row := range page.Items {
		animals := make([]appShiftingPendingExecutionAnimal, 0, len(row.Animals))
		for _, a := range row.Animals {
			animals = append(animals, appShiftingPendingExecutionAnimal{
				GoatID: a.GoatID, DisplayID: a.DisplayID, Tag: a.Tag,
			})
		}
		items = append(items, appShiftingPendingExecutionItem{
			ShiftingEventID: row.ShiftingEventID,
			// Every row on this queue is authorized by construction -- the read is filtered to it --
			// but naming the status keeps the client from inferring it from the route.
			EventStatus:         domain.ShiftingEventStatusAuthorized,
			Priority:            row.Priority,
			Category:            row.Category,
			SourceParkID:        row.SourceParkID,
			SourceParkName:      row.SourceParkName,
			SourceShedID:        row.SourceShedID,
			SourceShedName:      row.SourceShedName,
			DestinationParkID:   row.DestinationParkID,
			DestinationParkName: row.DestinationParkName,
			DestinationShedID:   row.DestinationShedID,
			DestinationShedName: row.DestinationShedName,
			ApprovedByUserID:    row.AuthorizedByUserID,
			ApprovedAt:          row.AuthorizedAt,
			ApprovedAtIST:       istLabel(row.AuthorizedAt),
			RaisedByUserID:      row.RaisedByUserID,
			RaisedAt:            row.RaisedAt,
			RaisedAtIST:         row.RaisedAt.In(biztime.DefaultLocation()).Format(time.RFC3339),
			EffectiveAt:         row.EffectiveAt,
			AnimalCount:         row.AnimalCount,
			AnimalsTruncated:    row.AnimalCount > len(animals),
			Animals:             animals,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK,
		appShiftingPendingExecutionResponse{Items: items, NextCursor: page.NextCursor})
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

func (h *AppWriteHandler) writeShiftingExecutionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrShiftingEventNotFound):
		h.writeError(w, r, http.StatusNotFound, "shifting_event_not_found", "shifting event not found", err)
	case errors.Is(err, ports.ErrShiftingNotAuthorized):
		// 400, not 409: the caller addressed a movement that is in the wrong state for this
		// transition (still pending approval, already rejected, already canceled). The message
		// names the actual state so an operator is told WHY rather than just refused.
		h.writeError(w, r, http.StatusBadRequest, "shifting_not_authorized", err.Error(), err)
	case errors.Is(err, ports.ErrShiftingExecutionIncomplete):
		h.writeError(w, r, http.StatusConflict, "shifting_execution_incomplete", err.Error(), err)
	case errors.Is(err, countsapp.ErrShiftingCancelReasonRequired):
		h.writeError(w, r, http.StatusBadRequest, "missing_reason",
			"reason is required to cancel a shifting", err)
	case errors.Is(err, ports.ErrIdempotencyConflict):
		h.writeError(w, r, http.StatusConflict, "idempotency_conflict",
			"Idempotency-Key was reused with a different payload", err)
	case errors.Is(err, countsapp.ErrInvalidShiftingExecutionFilter):
		h.writeError(w, r, http.StatusBadRequest, "invalid_cursor",
			"cursor is not a valid pending-execution cursor", err)
	case errors.Is(err, countsapp.ErrMissingRequiredField),
		errors.Is(err, countsapp.ErrInvalidJSON):
		h.writeError(w, r, http.StatusBadRequest, "invalid_shifting_execution_request", err.Error(), err)
	default:
		// Identity's app errors (a stale row_version on an animal, a merged goat) surface with
		// their own status/code taxonomy.
		var appErr *identityapp.Error
		if errors.As(err, &appErr) {
			h.writeError(w, r, appErr.HTTPStatus, appErr.Code, appErr.Message, err)
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
	}
}
