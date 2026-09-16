package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	identitydomain "github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// SOP CAPTURE CARD on the Add birth / Add death forms (maintainer decisions 4 and 7, 2026-09-16).
//
// The request carries `sop_capture {sop_version_id, proofs{slot: ref}, answers}` beside the
// form's fixed fields. It is STRIPPED before identity's strict decoder and before the stored
// payload (identity would 400 on the unknown key; the capture has its own columns), judged by
// the capture card service, and stored beside the request as the snapshot the approver and the
// verifier read. A request that carries NO sop_capture is an OLDER APP: accepted, judged against
// the published card as "not captured", and its fingerprint is byte-identical to before the
// feature so an outbox retry still collapses.

const (
	appCaptureCardsRoute = "/app/counts/capture-cards/{kind}"
	sopCaptureField      = "sop_capture"
)

// CaptureCardWorkflow is the slice of counts/app.CaptureCardService this handler needs.
type CaptureCardWorkflow interface {
	Card(ctx context.Context, tenantID, kind string) (countsapp.CaptureCardView, error)
	Judge(ctx context.Context, tenantID, kind string, sub *domain.CaptureSubmission) (*domain.ApprovalCapture, error)
}

// WithCaptureCards wires the capture card seam. Without it every request is an older app
// against the empty card: nothing is judged and nothing is stored.
func (h *AppWriteHandler) WithCaptureCards(cards CaptureCardWorkflow) *AppWriteHandler {
	h.captureCards = cards
	return h
}

type sopCaptureRequest struct {
	SOPVersionID string             `json:"sop_version_id"`
	Proofs       authored.ProofRefs `json:"proofs"`
	Answers      authored.Answers   `json:"answers"`
}

// takeSopCapture removes sop_capture from the decoded fields and returns it typed; nil when the
// request did not send it.
func takeSopCapture(fields map[string]json.RawMessage) (*domain.CaptureSubmission, error) {
	raw, present := fields[sopCaptureField]
	delete(fields, sopCaptureField)
	if !present || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var req sopCaptureRequest
	if err := decodeStrictJSON(raw, &req, "SopCaptureSubmission"); err != nil {
		return nil, err
	}
	return &domain.CaptureSubmission{SOPVersionID: strings.TrimSpace(req.SOPVersionID), Proofs: req.Proofs, Answers: req.Answers}, nil
}

// judgeCapture runs the capture card service for the form kind.
func (h *AppWriteHandler) judgeCapture(ctx context.Context, tenantID, kind string, sub *domain.CaptureSubmission) (*domain.ApprovalCapture, error) {
	if h.captureCards == nil {
		return nil, nil
	}
	return h.captureCards.Judge(ctx, tenantID, kind, sub)
}

// captureFingerprintBody is what the request fingerprint covers when the app SENT a capture:
// the form payload plus the capture. A request without one is fingerprinted on the payload
// alone, byte-identical to before the feature.
func captureFingerprintBody(payload json.RawMessage, sub *domain.CaptureSubmission) any {
	if sub == nil {
		return payload
	}
	return map[string]any{
		"payload": payload,
		"capture": sopCaptureRequest{SOPVersionID: sub.SOPVersionID, Proofs: authored.NormalizeProofRefs(sub.Proofs), Answers: sub.Answers},
	}
}

// writeCaptureError maps a capture judgement error onto the wire (422 naming the slot / question,
// 409 for an unknown pin); anything else is not a capture error and returns false.
func (h *AppWriteHandler) writeCaptureError(w http.ResponseWriter, r *http.Request, err error) bool {
	var slotErr *authored.ProofError
	var answerErr *authored.AnswerError
	switch {
	case errors.Is(err, countsapp.ErrCaptureSOPVersionUnknown):
		h.writeError(w, r, http.StatusConflict, "capture_sop_version_unknown",
			"the form has changed since it was opened; refresh and record again", err)
	case errors.As(err, &slotErr):
		h.writeFieldError(w, r, http.StatusUnprocessableEntity, "capture_proof_slot_invalid", slotErr.Message, slotErr.SlotKey, err)
	case errors.As(err, &answerErr):
		h.writeFieldError(w, r, http.StatusUnprocessableEntity, "capture_answer_invalid", answerErr.Message, answerErr.QuestionID, err)
	case errors.Is(err, countsapp.ErrCaptureKindUnknown):
		h.writeError(w, r, http.StatusNotFound, "capture_kind_unknown", "no capture card for this form", err)
	default:
		return false
	}
	return true
}

func (h *AppWriteHandler) writeFieldError(w http.ResponseWriter, r *http.Request, status int, code, message, field string, cause error) {
	httpresponse.WriteError(w, r, h.log, status, identitydomain.ErrorEnvelope{
		Code:        code,
		Message:     message,
		FieldErrors: []identitydomain.FieldError{{Field: field, Code: code, Message: message}},
		TraceID:     appTraceID(r),
		Retryable:   false,
	}, cause)
}

// --- GET /app/counts/capture-cards/{kind} --------------------------------------------------

type appCaptureCardResponse struct {
	Kind         string             `json:"kind"`
	SOPCode      string             `json:"sop_code"`
	SOPVersionID string             `json:"sop_version_id,omitempty"`
	VersionLabel string             `json:"version_label,omitempty"`
	Card         domain.CaptureCard `json:"card"`
}

// GetCaptureCard serves the published card for a form so the phone renders the SOP's extras
// from the backend (no app update on a publish; Room-cached and refreshed on open).
func (h *AppWriteHandler) GetCaptureCard(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.PathValue("kind")))
	if h.captureCards == nil {
		// Not wired: the empty card, so the phone renders the plain form.
		httpresponse.WriteJSON(w, http.StatusOK, appCaptureCardResponse{Kind: kind, Card: domain.CaptureCard{SchemaVersion: domain.CaptureSchemaVersion}})
		return
	}
	view, err := h.captureCards.Card(r.Context(), tenantID, kind)
	if err != nil {
		if !h.writeCaptureError(w, r, err) {
			h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
		}
		return
	}
	card := view.Card
	if card.SchemaVersion == "" {
		card.SchemaVersion = domain.CaptureSchemaVersion
	}
	httpresponse.WriteJSON(w, http.StatusOK, appCaptureCardResponse{
		Kind: view.Kind, SOPCode: view.SOPCode, SOPVersionID: view.VersionID, VersionLabel: view.VersionLabel, Card: card,
	})
}

// --- approver's row ---------------------------------------------------------------------------

// appApprovalCapture is CountsApprovalCapture on the wire, EXACTLY the shared contract
// (docs: SOP parity program decisions): {version_label, rows, media, missing_note}. Shifting's raise
// form populates the same shape. The verifier's verdict on the report proof rides on the list
// item itself (capture_review_status / capture_review_reason), not inside this shared schema.
type appApprovalCapture struct {
	VersionLabel string                    `json:"version_label,omitempty"`
	Rows         []appApprovalCaptureRow   `json:"rows"`
	Media        []appApprovalCaptureMedia `json:"media"`
	MissingNote  string                    `json:"missing_note,omitempty"`
}

type appApprovalCaptureRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Group string `json:"group,omitempty"`
}

type appApprovalCaptureMedia struct {
	ProofID string `json:"proof_id"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
}

// approvalCaptureDTO maps the stored snapshot; nil when the row carries none so the field is
// omitted rather than rendered empty.
func approvalCaptureDTO(item domain.ApprovalRequestSummary) *appApprovalCapture {
	e := item.Capture
	if e.IsEmpty() {
		return nil
	}
	out := &appApprovalCapture{
		VersionLabel: e.VersionLabel,
		Rows:         make([]appApprovalCaptureRow, 0, len(e.Rows)),
		Media:        make([]appApprovalCaptureMedia, 0, len(e.Media)),
		MissingNote:  e.MissingNote,
	}
	for _, r := range e.Rows {
		out.Rows = append(out.Rows, appApprovalCaptureRow{Label: r.Label, Value: r.Value, Group: r.Group})
	}
	for _, m := range e.Media {
		out.Media = append(out.Media, appApprovalCaptureMedia{ProofID: m.Ref, Label: m.Label, Kind: m.Kind})
	}
	return out
}
