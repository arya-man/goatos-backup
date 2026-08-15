package http

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/app"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// capturedSlotDTO is ONE already-recorded proof slot of a pen-session.
//
// It carries NO media url. The operator's need is "this slot is already done, and here is the
// reference I can submit with"; the media itself stays a verifier surface, so this route adds no new
// way to view another operator's footage. The uploader's display name is included so the UI can show
// "Captured by <name>" for teammate proofs.
type capturedSlotDTO struct {
	FieldKey       string `json:"field_key"`
	ProofRef       string `json:"proof_ref"`
	CapturedAt     string `json:"captured_at"`
	MimeType       string `json:"mime_type,omitempty"`
	CapturedByName string `json:"captured_by_name,omitempty"`
}

type distributionCapturesResponse struct {
	Items []capturedSlotDTO `json:"items"`
	// SessionStatus is the pen-session's completion status ("pending_verification", "completed",
	// "rework"), empty when nothing was submitted yet. Travels WITH the slots so the mobile
	// proof screen paints its read-only gate and the slot list from one consistent answer
	// (field bug 2026-08-15: a stale list-row hint opened a submitted session editable).
	SessionStatus string `json:"session_status,omitempty"`
}

// GetDistributionCaptures serves the pen-session's already-recorded proof slots.
//
// This is the read half of multi-operator feed distribution (maintainer decision 2026-08-14): three
// operators may split a pen-session's three proofs, so a phone must be able to learn that a slot it
// did not shoot is already done, and to name that proof when it submits. Read-only -- it changes no
// completion state, and a caller that ignores it behaves exactly as before.
func (h *Handler) GetDistributionCaptures(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	q := r.URL.Query()
	targetDate, err := businessDateFromString(strings.TrimSpace(q.Get("target_date")))
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	sessionNo, err := strconv.Atoi(strings.TrimSpace(q.Get("session_no")))
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "session_no must be an integer", nil)
		return
	}

	// CLAMP park_id to the caller's own grant before reading anything. This is the precondition for
	// admitting a park-scoped operator in routeAllowsScopedGrants: a CPT operator naming a CBE park
	// must be REFUSED, not merely shown nothing.
	scope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(), tenantID, strings.TrimSpace(q.Get("park_id")), permissions.FeedDirectionRead,
	)
	if !scope.Allowed {
		httpresponse.WriteError(w, r, h.log, scope.Status, codedError{Code: scope.Code, Message: scope.Message}, nil)
		return
	}

	result, err := h.service.ListPenSessionCaptures(r.Context(), app.PenSessionCapturesInput{
		TenantID: tenantID,
		ParkID:   scope.ParkID,
		ShedID:   strings.TrimSpace(q.Get("shed_id")),
		// The PEN. Omitting it would answer for the shed as a whole and tell an operator standing in
		// Castro - 2 that Castro - 1's work is theirs.
		PartitionLabel:    strings.TrimSpace(q.Get("partition_label")),
		SessionNo:         int32(sessionNo),
		TargetDate:        targetDate,
		Workflow:          strings.TrimSpace(q.Get("workflow")),
		AuthorizedParkIDs: scope.ParkIDs,
	})
	if err != nil {
		h.writeServiceError(w, r, "feed distribution captures", err)
		return
	}

	items := make([]capturedSlotDTO, 0, len(result.Slots))
	for _, slot := range result.Slots {
		items = append(items, capturedSlotDTO{
			FieldKey:       slot.FieldKey,
			ProofRef:       slot.ProofID,
			CapturedAt:     slot.CapturedAt.UTC().Format(time.RFC3339),
			MimeType:       slot.MimeType,
			CapturedByName: slot.CapturedByName,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, distributionCapturesResponse{Items: items, SessionStatus: result.SessionStatus})
}
