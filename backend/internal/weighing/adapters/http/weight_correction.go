package http

import (
	"context"
	"errors"
	nethttp "net/http"

	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/weighing/app"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// VERIFIER WEIGHT CORRECTION -- the HTTP adapter.
//
// ONE route serves both clients. The verifier's phone and her admin-web drawer are
// two renderings of the same act by the same person under the same capability, so a
// second route would be two places for one rule to drift.
//
// It is registered under /app/ because the phone is the primary surface; admin-web
// calls the identical path server-side. Authorization is permissions.VerificationVerdict
// (see permissions/routes.go), the verifier-exclusive capability that owns
// approve/reject -- so the person who judges the evidence is the person who may fix
// what it shows, and no director or CEO can reach it.

// WeightCorrector is the narrow write this handler drives. It is deliberately NOT
// part of the Service interface above: the correction is the verifier's act, and the
// handler that serves it must not be able to reach the planner/execution writes.
type WeightCorrector interface {
	CorrectObservationWeight(ctx context.Context, cmd domain.WeightCorrectionCommand) (domain.WeightCorrectionResult, error)
}

// WithWeightCorrector wires the verifier's correction write.
//
// Optional: a deployment that has not wired it answers 404 on the route rather than
// panicking, which is the honest answer for a capability this build does not serve.
func (h *Handler) WithWeightCorrector(corrector WeightCorrector) *Handler {
	h.weightCorrector = corrector
	return h
}

// weightCorrectionRequest is the body both clients send.
//
// WeightKg is a POINTER so an omitted field is distinguishable from an explicit 0.
// Without that, a client that failed to serialize the field would send what looks
// like a deliberate zero, and the range check would reject it with "out of range" --
// blaming the verifier for a value she never typed. Absent is its own refusal.
type weightCorrectionRequest struct {
	// RefType names the grain: weighing_observation (individual) or
	// weighing_shed_observation (lump-sum). It is the value the verification item
	// already carries in source.ref_type, so the client echoes it rather than
	// inferring it.
	RefType  string   `json:"ref_type"`
	WeightKg *float64 `json:"weight_kg"`
	// AnimalCount is kept on the wire ONLY so an installed APK that still offers
	// count editing gets an honest refusal: any non-zero value is rejected with
	// animal_count_not_applicable (maintainer decision 2026-08-24 — the lump-sum
	// head count is snapshotted from the herd register at submit and is frozen).
	AnimalCount    int    `json:"animal_count,omitempty"`
	Reason         string `json:"reason,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// weightCorrectionMessages maps each domain refusal code to the sentence the
// VERIFIER reads. Backend owns the copy (AGENTS.md: clients render, never compose),
// and it is farm language -- no field names, no types, no rule identifiers.
var weightCorrectionMessages = map[string]string{
	"missing_observation":         "This video is not linked to a weight record, so there is nothing to correct.",
	"missing_verifier":            "Sign in again, then correct the weight.",
	"missing_idempotency_key":     "Something went wrong sending this correction. Try again.",
	"invalid_ref_type":            "This video is not a weighing record, so there is no weight to correct.",
	"missing_weight":              "Enter the correct weight in kg.",
	"weight_out_of_range":         "Enter a weight in kg between 0.001 and 100000.",
	"animal_count_not_applicable": "The goat count is recorded automatically and can't be changed. Correct the weight only.",
}

func weightCorrectionMessage(code string) string {
	if msg, ok := weightCorrectionMessages[code]; ok {
		return msg
	}
	return "That weight could not be saved. Check the value and try again."
}

// CorrectObservationWeight applies the verifier's corrected weight to the
// observation the verification item points at.
func (h *Handler) CorrectObservationWeight(w nethttp.ResponseWriter, r *nethttp.Request) {
	if h.weightCorrector == nil {
		httpresponse.WriteError(w, r, h.log, nethttp.StatusNotFound, errorEnvelope{
			Code: "not_found", Message: "weighing resource was not found", TraceID: traceID(r),
		}, nil)
		return
	}
	var req weightCorrectionRequest
	if !h.decode(w, r, &req) {
		return
	}
	// An absent weight is its own refusal, kept apart from an out-of-range one so
	// the verifier is told to ENTER a weight rather than told the weight she never
	// typed is wrong.
	if req.WeightKg == nil {
		h.weightCorrectionRefusal(w, r, "missing_weight")
		return
	}

	a := actor(r)
	result, err := h.weightCorrector.CorrectObservationWeight(r.Context(), domain.WeightCorrectionCommand{
		TenantID:      a.TenantID,
		ObservationID: r.PathValue("observation_id"),
		RefType:       req.RefType,
		WeightKg:      *req.WeightKg,
		AnimalCount:   req.AnimalCount,
		Reason:        req.Reason,
		CorrectedBy:   a.UserID,
		// The client's own key when it sent one, the Idempotency-Key header otherwise
		// -- the same resolution every other weighing write uses.
		IdempotencyKey: h.idempotencyKey(r, req.IdempotencyKey),
	})
	if err != nil {
		h.respondWeightCorrection(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, map[string]any{
		"weight_correction": result,
		"trace_id":          traceID(r),
	})
}

// respondWeightCorrection maps the correction's own failures, then falls through to
// the shared weighing mapper for everything it does not own.
func (h *Handler) respondWeightCorrection(w nethttp.ResponseWriter, r *nethttp.Request, err error) {
	if code := app.CorrectionCode(err); code != "" {
		h.weightCorrectionRefusal(w, r, code)
		return
	}
	if errors.Is(err, ports.ErrCorrectionAfterClose) {
		// Named apart from the generic invalid_state so the verifier is told the real
		// remedy: this bucket is finished, and a leader has to reopen it. "Not
		// editable in its current state" would leave her with no next step.
		httpresponse.WriteError(w, r, h.log, nethttp.StatusConflict, errorEnvelope{
			Code:    "weighing_bucket_closed",
			Message: "This pen's weighing is already closed. Ask a manager to reopen it before correcting the weight.",
			TraceID: traceID(r),
		}, nil)
		return
	}
	h.respond(w, r, nil, err)
}

func (h *Handler) weightCorrectionRefusal(w nethttp.ResponseWriter, r *nethttp.Request, code string) {
	httpresponse.WriteError(w, r, h.log, nethttp.StatusUnprocessableEntity, errorEnvelope{
		Code:    code,
		Message: weightCorrectionMessage(code),
		TraceID: traceID(r),
	}, nil)
}
