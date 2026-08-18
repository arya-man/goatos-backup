package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"encoding/json"
	"github.com/vgoats/goatos/backend/internal/feeddirection/app"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// FEED WASTAGE HTTP adapter (maintainer decision 2026-08-18). Three routes:
//
//	GET  /feed-wastage/worklist                                  the per-pen experiment worklist
//	POST /feed-direction/wastage/complete                        operator submit (ONE mandatory video)
//	POST /feed-direction/wastage/{completion_id}/measurement     verifier's recorded leftover (kg)
//
// The measurement route is gated on permissions.VerificationVerdict (routes.go): the person who
// judges the evidence is the person who records what it shows, and no director or CEO can reach it.

// WastageMeasurer is the narrow verifier write this handler drives. Deliberately NOT part of the
// Service interface — see Handler.wastageMeasurer.
type WastageMeasurer interface {
	RecordWastageMeasurement(ctx context.Context, cmd app.WastageMeasurementCommand) (ports.RecordWastageMeasurementResult, error)
}

// WithWastageMeasurer wires the verifier's measurement write.
func (h *Handler) WithWastageMeasurer(measurer WastageMeasurer) *Handler {
	h.wastageMeasurer = measurer
	return h
}

// GetWastageWorklist serves the per-pen Feed Wastage view for one park and one feed day.
func (h *Handler) GetWastageWorklist(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	query := r.URL.Query()

	targetDate, err := requiredBusinessDate(query, "target_date")
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	limit, err := boundedIntParam(query, "limit", app.DefaultShedPageLimit, 1, app.MaxShedPageLimit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	offset, err := boundedIntParam(query, "offset", 0, 0, app.MaxShedPageOffset)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	// Same status contract as the packing worklist: one bucket or empty for all; unknown rejected.
	status := strings.TrimSpace(query.Get("status"))
	if !domain.IsValidSessionStatusFilter(status) {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "status must be one of pending, pending_verification, completed", nil)
		return
	}
	parkScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(),
		tenantID,
		strings.TrimSpace(query.Get("park_id")),
		permissions.FeedWastageRead,
	)
	if !parkScope.Allowed {
		httpresponse.WriteError(w, r, h.log, parkScope.Status, parkScope.Message, nil)
		return
	}

	page, err := h.service.WastageWorklist(r.Context(), domain.WastageQuery{
		TenantID:          tenantID,
		ParkID:            parkScope.ParkID,
		AuthorizedParkIDs: parkScope.ParkIDs,
		TargetDate:        targetDate,
		ShedID:            strings.TrimSpace(query.Get("shed_id")),
		PartitionLabel:    strings.TrimSpace(query.Get("partition_label")),
		Status:            status,
		Limit:             limit,
		Offset:            offset,
	})
	if err != nil {
		h.writeServiceError(w, r, "feed wastage worklist", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

// completeWastageRequest is the verifier-gated wastage completion body: which pen, on which feed
// day, plus the ONE mandatory wastage video reference. The Idempotency-Key header carries the
// replay key. No session_no (the grain is the pen-day) and no workflow (experiment by definition).
type completeWastageRequest struct {
	ParkID          string `json:"park_id"`
	ShedID          string `json:"shed_id"`
	PartitionLabel  string `json:"partition_label"`
	TargetDate      string `json:"target_date"`
	WastageProofRef string `json:"wastage_proof_ref"`
}

type completeWastageResponse struct {
	CompletionID string `json:"completion_id"`
	Status       string `json:"status"`
	NewlyPending bool   `json:"newly_pending"`
}

// PostCompleteWastage records a pen-day's ONE mandatory wastage video and flips it to
// pending_verification. Nothing is completed here: the pen-day is completed only when a verifier
// approves the video. Idempotent on the Idempotency-Key header.
func (h *Handler) PostCompleteWastage(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	actorID := httpmiddleware.ActorIDFromContext(r.Context())
	if actorID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing actor context", nil)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "Idempotency-Key header is required", nil)
		return
	}
	if len(key) < 8 || len(key) > 200 {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "Idempotency-Key must be between 8 and 200 characters", nil)
		return
	}

	var body completeWastageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	targetDate, err := businessDateFromString(body.TargetDate)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	// The wastage video is mandatory. Reject a blank one with 422 proof_required BEFORE calling the
	// service, mirroring the packing route, so a proofless request never reaches the write path.
	if strings.TrimSpace(body.WastageProofRef) == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: "a wastage video proof (wastage_proof_ref) is required"}, nil)
		return
	}

	res, err := h.service.CompleteWastage(r.Context(), app.CompleteWastageInput{
		TenantID:        tenantID,
		ParkID:          strings.TrimSpace(body.ParkID),
		ShedID:          strings.TrimSpace(body.ShedID),
		PartitionLabel:  strings.TrimSpace(body.PartitionLabel),
		TargetDate:      targetDate,
		WastageProofRef: strings.TrimSpace(body.WastageProofRef),
		CompletedBy:     actorID,
		IdempotencyKey:  key,
		ActorID:         actorID,
		ActorType:       "operator",
		TraceID:         httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, r, "feed wastage complete", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, completeWastageResponse{
		CompletionID: res.CompletionID,
		Status:       res.Status,
		NewlyPending: res.NewlyPending,
	})
}

// wastageMeasurementRequest is the verifier's measurement body. WastageKg is a POINTER so an
// omitted field is distinguishable from an explicit 0 — zero is a VALID measurement (an empty
// trough), so blank-vs-zero must never be coerced.
type wastageMeasurementRequest struct {
	WastageKg      *float64 `json:"wastage_kg"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

// wastageMeasurementMessages maps each refusal code to the sentence the VERIFIER reads. Backend
// owns the copy; farm language only.
var wastageMeasurementMessages = map[string]string{
	"missing_completion":      "This video is not linked to a wastage record, so there is nothing to record against.",
	"missing_verifier":        "Sign in again, then record the wastage weight.",
	"missing_idempotency_key": "Something went wrong sending this value. Try again.",
	"missing_wastage":         "Enter the leftover feed weight in kg.",
	"wastage_out_of_range":    "Enter a leftover weight in kg between 0 and 10000.",
}

func wastageMeasurementMessage(code string) string {
	if msg, ok := wastageMeasurementMessages[code]; ok {
		return msg
	}
	return "That weight could not be saved. Check the value and try again."
}

// PostWastageMeasurement records the verifier's measured leftover weight on the completion the
// verification item points at. ONE route serves her phone and her admin-web drawer.
func (h *Handler) PostWastageMeasurement(w http.ResponseWriter, r *http.Request) {
	if h.wastageMeasurer == nil {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound, "feed wastage resource was not found", nil)
		return
	}
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	actorID := httpmiddleware.ActorIDFromContext(r.Context())
	if tenantID == "" || actorID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing actor context", nil)
		return
	}

	var body wastageMeasurementRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	// An absent value is its own refusal, kept apart from an out-of-range one so the verifier is
	// told to ENTER a weight rather than told the weight she never typed is wrong.
	if body.WastageKg == nil {
		h.wastageMeasurementRefusal(w, r, "missing_wastage")
		return
	}
	key := strings.TrimSpace(body.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}

	result, err := h.wastageMeasurer.RecordWastageMeasurement(r.Context(), app.WastageMeasurementCommand{
		TenantID:       tenantID,
		CompletionID:   r.PathValue("completion_id"),
		WastageKg:      *body.WastageKg,
		RecordedBy:     actorID,
		IdempotencyKey: key,
		TraceID:        httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		if code := app.WastageMeasurementCode(err); code != "" {
			h.wastageMeasurementRefusal(w, r, code)
			return
		}
		switch {
		case errors.Is(err, ports.ErrWastageCompletionNotFound):
			httpresponse.WriteError(w, r, h.log, http.StatusNotFound, "wastage completion was not found", nil)
		case errors.Is(err, ports.ErrWastageValueOutOfRange):
			h.wastageMeasurementRefusal(w, r, "wastage_out_of_range")
		case errors.Is(err, ports.ErrIdempotencyConflict):
			httpresponse.WriteError(w, r, h.log, http.StatusConflict, err.Error(), nil)
		default:
			httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "feed wastage measurement", err)
		}
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]any{
		"wastage_measurement": result,
	})
}

func (h *Handler) wastageMeasurementRefusal(w http.ResponseWriter, r *http.Request, code string) {
	httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
		codedError{Code: code, Message: wastageMeasurementMessage(code)}, nil)
}
