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
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// PEN RECONCILIATION endpoints — the Herd Operations Reconcile tab (maintainer decision
// 2026-09-02).
//
//	GET  /app/counts/pen-reconciliation/cards                     -- the wrong-pen card queue
//	POST /app/counts/pen-reconciliation/cards/{card_id}/complete  -- return video submitted
//
// Both are gated on CountsWrite: returning a strayed animal to its registered pen is ordinary
// herd-operations field work. There is NO approval route on purpose — the verifier's evidence
// review is the only gate, and the register is never rewritten by either endpoint.

const (
	appPenReconciliationListRoute     = "/app/counts/pen-reconciliation/cards"
	appPenReconciliationCompleteRoute = "/app/counts/pen-reconciliation/cards/{card_id}/complete"

	appPenReconciliationCompleteCommand = "counts.app.pen_reconciliation_complete"
)

// PenReconciliationWorkflow is the slice of counts/app.PenReconciliationService this handler
// needs.
type PenReconciliationWorkflow interface {
	Complete(ctx context.Context, in countsapp.CompletePenReconciliationInput) (domain.PenReconciliationCompletionResult, bool, error)
	List(ctx context.Context, tenantID, status string, pageSize int, cursor string) (domain.PenReconciliationPage, error)
}

// WithPenReconciliationWorkflow injects the reconciliation service. A handler without it
// answers 501 rather than silently pretending a card completed.
func (h *AppWriteHandler) WithPenReconciliationWorkflow(reconciliation PenReconciliationWorkflow) *AppWriteHandler {
	h.reconciliation = reconciliation
	return h
}

// RegisterPenReconciliation wires the Reconcile surface.
func RegisterPenReconciliation(mux *http.ServeMux, h *AppWriteHandler) {
	mux.HandleFunc("GET "+appPenReconciliationListRoute, h.ListPenReconciliationCards)
	mux.HandleFunc("POST "+appPenReconciliationCompleteRoute, h.CompletePenReconciliationCard)
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

type appPenReconciliationListResponse struct {
	Items        []appPenReconciliationCard           `json:"items"`
	NextCursor   string                               `json:"next_cursor,omitempty"`
	StatusCounts domain.PenReconciliationStatusCounts `json:"status_counts"`
}

type appPenReconciliationCard struct {
	CardID           string `json:"card_id"`
	Status           string `json:"status"`
	PrimaryActionKey string `json:"primary_action_key"`

	GoatID            string `json:"goat_id"`
	GoatDisplayID     string `json:"goat_display_id,omitempty"`
	ScannedIdentifier string `json:"scanned_identifier"`

	// FoundOperationalLocationDisplay is where the animal was actually scanned — the weighing
	// bucket's operator-facing pen label, snapshotted at raise.
	FoundLocationID                 string `json:"found_location_id"`
	FoundPartitionLabel             string `json:"found_partition_label,omitempty"`
	FoundOperationalLocationDisplay string `json:"found_operational_location_display"`

	// Registered* is the pen the register says the animal lives in — where the operator must
	// return it. Display is composed by the canonical oploc helper; clients render it
	// verbatim and never rebuild it from name + partition.
	RegisteredShedID                     string `json:"registered_shed_id"`
	RegisteredShedName                   string `json:"registered_shed_name"`
	RegisteredPartitionLabel             string `json:"registered_partition_label,omitempty"`
	RegisteredOperationalLocationDisplay string `json:"registered_operational_location_display"`

	ParkID   *string `json:"park_id,omitempty"`
	ParkName *string `json:"park_name,omitempty"`

	RaisedAt    time.Time `json:"raised_at"`
	RaisedAtIST string    `json:"raised_at_ist"`

	ProofRef     *string    `json:"proof_ref,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	VerifiedAt   *time.Time `json:"verified_at,omitempty"`
	ReworkReason *string    `json:"rework_reason,omitempty"`
}

// ListPenReconciliationCards returns one keyset page of the Reconcile queue.
func (h *AppWriteHandler) ListPenReconciliationCards(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	if h.reconciliation == nil {
		h.writeError(w, r, http.StatusNotImplemented, "pen_reconciliation_unavailable",
			"pen reconciliation workflow is not configured", nil)
		return
	}

	// Page size is capped server-side: this queue is read from a phone standing in a park, and
	// a client asking for 500 rows must get one screen of work.
	pageSize := domain.MaxPenReconciliationPageSize
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

	page, err := h.reconciliation.List(r.Context(), tenantID,
		strings.TrimSpace(r.URL.Query().Get("status")), pageSize,
		strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		h.writePenReconciliationError(w, r, err)
		return
	}

	items := make([]appPenReconciliationCard, 0, len(page.Items))
	for _, card := range page.Items {
		items = append(items, appPenReconciliationCard{
			CardID:                          card.CardID,
			Status:                          card.Status,
			PrimaryActionKey:                card.PrimaryActionKey,
			GoatID:                          card.GoatID,
			GoatDisplayID:                   card.GoatDisplayID,
			ScannedIdentifier:               card.ScannedIdentifier,
			FoundLocationID:                 card.FoundLocationID,
			FoundPartitionLabel:             card.FoundPartitionLabel,
			FoundOperationalLocationDisplay: card.FoundDisplayName,
			RegisteredShedID:                card.RegisteredShedID,
			RegisteredShedName:              card.RegisteredShedName,
			RegisteredPartitionLabel:        card.RegisteredPartitionLabel,
			RegisteredOperationalLocationDisplay: oploc.OperationalLocation{
				ShedName:       card.RegisteredShedName,
				PartitionLabel: card.RegisteredPartitionLabel,
			}.Display(),
			ParkID:       card.ParkID,
			ParkName:     card.ParkName,
			RaisedAt:     card.RaisedAt,
			RaisedAtIST:  card.RaisedAt.In(biztime.DefaultLocation()).Format(time.RFC3339),
			ProofRef:     card.ProofRef,
			CompletedAt:  card.CompletedAt,
			VerifiedAt:   card.VerifiedAt,
			ReworkReason: card.ReworkReason,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, appPenReconciliationListResponse{
		Items: items, NextCursor: page.NextCursor, StatusCounts: page.StatusCounts,
	})
}

// ---------------------------------------------------------------------------
// Complete
// ---------------------------------------------------------------------------

type appPenReconciliationCompleteResponse struct {
	CardID string `json:"card_id"`
	Status string `json:"status"`

	ScannedIdentifier                    string `json:"scanned_identifier"`
	RegisteredOperationalLocationDisplay string `json:"registered_operational_location_display"`

	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CompletedAtIST *string    `json:"completed_at_ist,omitempty"`

	IdempotentReplay bool `json:"idempotent_replay"`
}

// CompletePenReconciliationCard records the operator's return-video submission.
func (h *AppWriteHandler) CompletePenReconciliationCard(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	if h.reconciliation == nil {
		h.writeError(w, r, http.StatusNotImplemented, "pen_reconciliation_unavailable",
			"pen reconciliation workflow is not configured", nil)
		return
	}
	actorID := httpmiddleware.ActorIDFromContext(r.Context())
	if actorID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_actor", "missing actor context", nil)
		return
	}
	clientKey, err := appIdempotencyKey(r)
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	cardID := strings.TrimSpace(r.PathValue("card_id"))
	if cardID == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_card_id", "card_id is required", nil)
		return
	}

	var proofRef string
	if raw, bodyOK := h.readBody(w, r); !bodyOK {
		return
	} else if len(strings.TrimSpace(string(raw))) > 0 {
		var body struct {
			ProofRef string `json:"proof_ref"`
		}
		if err := decodeStrictJSON(raw, &body, "PenReconciliationCompleteRequest"); err != nil {
			h.writeAppError(w, r, err)
			return
		}
		proofRef = strings.TrimSpace(body.ProofRef)
	}
	if proofRef == "" {
		h.writeError(w, r, http.StatusUnprocessableEntity, "proof_required",
			"a video proof (proof_ref) is required to complete a pen reconciliation", nil)
		return
	}

	// The fingerprint covers the completion's MEANING (which card, which video), so a same-key
	// replay carrying a different video is a conflict rather than a silent override.
	canonical, err := canonicalRequestBytes(tenantID, appPenReconciliationCompleteCommand,
		appPenReconciliationCompleteRoute, struct {
			CardID   string `json:"card_id"`
			ProofRef string `json:"proof_ref"`
		}{CardID: cardID, ProofRef: proofRef})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}

	result, replay, err := h.reconciliation.Complete(r.Context(), countsapp.CompletePenReconciliationInput{
		TenantID:           tenantID,
		CardID:             cardID,
		CompletedByUserID:  actorID,
		TraceID:            appTraceID(r),
		ProofRef:           proofRef,
		IdempotencyKey:     "counts-pen-reconciliation-completion:" + clientKey,
		RequestFingerprint: stableHash("counts-app-pen-reconciliation-completion", canonical),
	})
	if err != nil {
		h.writePenReconciliationError(w, r, err)
		return
	}

	out := appPenReconciliationCompleteResponse{
		CardID:            result.CardID,
		Status:            result.Status,
		ScannedIdentifier: result.ScannedIdentifier,
		RegisteredOperationalLocationDisplay: oploc.OperationalLocation{
			ShedName:       result.RegisteredShedName,
			PartitionLabel: result.RegisteredPartitionLabel,
		}.Display(),
		CompletedAt:      result.CompletedAt,
		CompletedAtIST:   istLabel(result.CompletedAt),
		IdempotentReplay: replay,
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

func (h *AppWriteHandler) writePenReconciliationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrPenReconciliationCardNotFound):
		h.writeError(w, r, http.StatusNotFound, "pen_reconciliation_card_not_found",
			"pen reconciliation card not found", err)
	case errors.Is(err, ports.ErrPenReconciliationNotActionable):
		h.writeError(w, r, http.StatusBadRequest, "pen_reconciliation_not_actionable", err.Error(), err)
	case errors.Is(err, ports.ErrPenReconciliationProofRequired):
		// 422: the card is actionable, but the return must be proven on video. Actionable
		// input error.
		h.writeError(w, r, http.StatusUnprocessableEntity, "proof_required",
			"a video proof (proof_ref) is required to complete a pen reconciliation", err)
	case errors.Is(err, ports.ErrIdempotencyConflict):
		h.writeError(w, r, http.StatusConflict, "idempotency_conflict",
			"Idempotency-Key was reused with a different payload", err)
	case errors.Is(err, countsapp.ErrInvalidPenReconciliationFilter):
		h.writeError(w, r, http.StatusBadRequest, "invalid_cursor",
			"cursor or status is not a valid pen reconciliation filter", err)
	case errors.Is(err, countsapp.ErrMissingRequiredField), errors.Is(err, countsapp.ErrInvalidJSON):
		h.writeError(w, r, http.StatusBadRequest, "invalid_pen_reconciliation_request", err.Error(), err)
	default:
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
	}
}
