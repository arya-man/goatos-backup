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
	"github.com/vgoats/goatos/backend/internal/permissions"
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
	// The SOP-driven questionnaire (maintainer decision 2026-09-13): opens (or finds) the card's
	// workflow so the phone renders the authored steps through /app/workflows/{workflow_id}.
	appPenReconciliationWorkflowRoute = "/app/counts/pen-reconciliation/cards/{card_id}/workflow"

	appPenReconciliationCompleteCommand = "counts.app.pen_reconciliation_complete"
)

// PenReconciliationWorkflow is the slice of counts/app.PenReconciliationService this handler
// needs.
type PenReconciliationWorkflow interface {
	Complete(ctx context.Context, in countsapp.CompletePenReconciliationInput) (domain.PenReconciliationCompletionResult, bool, error)
	List(ctx context.Context, in countsapp.ListPenReconciliationInput) (domain.PenReconciliationPage, error)
	EnsureWorkflow(ctx context.Context, tenantID, cardID string, authorizedParkIDs []string) (string, error)
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
	mux.HandleFunc("POST "+appPenReconciliationWorkflowRoute, h.EnsurePenReconciliationWorkflow)
}

// EnsurePenReconciliationWorkflow returns the card's SOP questionnaire workflow id, opening it
// from the published counts.reconcile SOP on first call. Idempotent; no body.
func (h *AppWriteHandler) EnsurePenReconciliationWorkflow(w http.ResponseWriter, r *http.Request) {
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
	cardID := strings.TrimSpace(r.PathValue("card_id"))
	if cardID == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_card_id", "card_id is required", nil)
		return
	}
	authorizedParkIDs, ok := h.authorizedPenReconciliationParkIDs(w, r, tenantID)
	if !ok {
		return
	}
	workflowID, err := h.reconciliation.EnsureWorkflow(r.Context(), tenantID, cardID, authorizedParkIDs)
	if err != nil {
		h.writePenReconciliationError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]string{"card_id": cardID, "workflow_id": workflowID})
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

type appPenReconciliationListResponse struct {
	Items        []appPenReconciliationCard           `json:"items"`
	NextCursor   string                               `json:"next_cursor,omitempty"`
	StatusCounts domain.PenReconciliationStatusCounts `json:"status_counts"`
	Filters      appPenReconciliationFilters          `json:"filters"`
}

// appPenReconciliationFilters is the backend-owned park filter bar. Parks lists exactly the
// parks the caller may choose (one for a park-scoped operator, every active park for a
// tenant-wide reader); SelectedParkID is the park this page is clamped to, empty when the page
// spans every offered park. The phone renders these verbatim and never derives a park list.
type appPenReconciliationFilters struct {
	Parks          []appPenReconciliationParkOption `json:"parks"`
	SelectedParkID string                           `json:"selected_park_id"`
}

type appPenReconciliationParkOption struct {
	ParkID string `json:"park_id"`
	Label  string `json:"label"`
	// Code is the park's short code (CBE, CPT): the small badge a card carries.
	Code     string `json:"code"`
	Selected bool   `json:"selected"`
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
	// ParkCode is the park's short code (CBE, CPT) shown small on the card so a reader who
	// sees both parks in one queue can tell them apart at a glance.
	ParkCode string `json:"park_code,omitempty"`

	RaisedAt    time.Time `json:"raised_at"`
	RaisedAtIST string    `json:"raised_at_ist"`

	ProofRef     *string    `json:"proof_ref,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	VerifiedAt   *time.Time `json:"verified_at,omitempty"`
	ReworkReason *string    `json:"rework_reason,omitempty"`
	// WorkflowID is the SOP questionnaire for this card once the phone opened it; the phone
	// executes the card through /app/workflows/{workflow_id}.
	WorkflowID *string  `json:"workflow_id"`
	ProofRefs  []string `json:"proof_refs"`
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

	// Park scope is resolved from the caller's grants, capability-aware: an operator's park
	// grant clamps the queue to that park (and refuses ?park_id for any other), a tenant-wide
	// counts grant sees the requested park or every park. A person covering several parks
	// without a tenant grant is offered all of them rather than refused -- this is a work queue,
	// and "choose a park" is the filter bar's job, not an error page's.
	requestedParkID := strings.TrimSpace(r.URL.Query().Get("park_id"))
	scope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(), tenantID, requestedParkID, permissions.CountsWrite)
	if !scope.Allowed && scope.Code != "park_selection_required" {
		h.writeError(w, r, scope.Status, scope.Code, scope.Message, nil)
		return
	}
	authorizedParkIDs := append([]string(nil), scope.ParkIDs...)
	if scope.Allowed && len(scope.ParkIDs) == 0 {
		// Tenant-wide: no clamp beyond the tenant; the requested park (if any) narrows below.
		authorizedParkIDs = nil
	}

	page, err := h.reconciliation.List(r.Context(), countsapp.ListPenReconciliationInput{
		TenantID:          tenantID,
		Status:            strings.TrimSpace(r.URL.Query().Get("status")),
		PageSize:          pageSize,
		Cursor:            strings.TrimSpace(r.URL.Query().Get("cursor")),
		AuthorizedParkIDs: authorizedParkIDs,
		RequestedParkID:   requestedParkID,
	})
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
			ParkCode:     stringOrEmpty(card.ParkCode),
			RaisedAt:     card.RaisedAt,
			RaisedAtIST:  card.RaisedAt.In(biztime.DefaultLocation()).Format(time.RFC3339),
			ProofRef:     card.ProofRef,
			CompletedAt:  card.CompletedAt,
			VerifiedAt:   card.VerifiedAt,
			ReworkReason: card.ReworkReason,
			WorkflowID:   card.WorkflowID,
			ProofRefs:    nonNilRefs(card.ProofRefs),
		})
	}
	filters := appPenReconciliationFilters{
		Parks:          make([]appPenReconciliationParkOption, 0, len(page.Parks)),
		SelectedParkID: page.SelectedParkID,
	}
	for _, park := range page.Parks {
		filters.Parks = append(filters.Parks, appPenReconciliationParkOption{
			ParkID:   park.ParkID,
			Label:    park.Name,
			Code:     park.Code,
			Selected: park.ParkID == page.SelectedParkID,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, appPenReconciliationListResponse{
		Items: items, NextCursor: page.NextCursor, StatusCounts: page.StatusCounts, Filters: filters,
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
	authorizedParkIDs, ok := h.authorizedPenReconciliationParkIDs(w, r, tenantID)
	if !ok {
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
		AuthorizedParkIDs:  authorizedParkIDs,
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

func (h *AppWriteHandler) authorizedPenReconciliationParkIDs(w http.ResponseWriter, r *http.Request, tenantID string) ([]string, bool) {
	scope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(), tenantID, "", permissions.CountsWrite)
	if !scope.Allowed && scope.Code != "park_selection_required" {
		h.writeError(w, r, scope.Status, scope.Code, scope.Message, nil)
		return nil, false
	}
	if scope.Allowed && len(scope.ParkIDs) == 0 {
		return nil, true
	}
	return append([]string(nil), scope.ParkIDs...), true
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

func nonNilRefs(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
