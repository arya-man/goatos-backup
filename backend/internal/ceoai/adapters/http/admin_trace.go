// Package http also exposes the ADMIN-ONLY step-trace debug endpoint. This is
// the ONLY surface where the internal execution trace (sub-questions, tools,
// params, row counts, latency, review verdict) is visible — and only to the
// small ceo_internal/superadmin cohort. It exists so admins/engineers can "see
// each step" WITHOUT violating the committed Internal Tracking rule that bans
// the trace from the leadership chat answer.
//
// Hard gate: a non-admin (or unauthenticated) caller gets 403 and NEVER sees a
// trace, even for their own request. Tenant scope comes from the session, so an
// admin in tenant A can never read tenant B's trace.
package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	ceoobs "github.com/vgoats/goatos/backend/internal/ceoai/adapters/observability"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// TraceReader is the read port the admin debug handler drives
// (*observability.PostgresTraceStore and *MemoryTraceStore satisfy it).
type TraceReader interface {
	GetTrace(ctx context.Context, tenantID, requestID string) (ceoobs.TraceRecord, error)
}

// AdminTraceHandler serves GET /api/ceo-ai/admin/trace/{request_id}.
type AdminTraceHandler struct {
	traces TraceReader
	log    *slog.Logger
}

// NewAdminTraceHandler builds the admin trace handler.
func NewAdminTraceHandler(traces TraceReader, log *slog.Logger) *AdminTraceHandler {
	if log == nil {
		log = slog.Default()
	}
	return &AdminTraceHandler{traces: traces, log: log}
}

// Trace returns the internal step trace for one request_id, admin-gated.
func (h *AdminTraceHandler) Trace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := httpmiddleware.TenantIDFromContext(ctx)
	actorID := httpmiddleware.ActorIDFromContext(ctx)
	grants := httpmiddleware.AuthGrantsFromContext(ctx)

	if tenantID == "" || actorID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, map[string]string{"error": "unauthorized"}, nil)
		return
	}
	// HARD role gate: only the product-admin cohort may read internal traces.
	if !isTraceAdmin(grants) {
		// Same 403 shape whether the caller lacks the role or the trace does not
		// exist — a non-admin must not be able to probe request-id existence.
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden, map[string]string{"error": "admin_required"}, nil)
		return
	}

	requestID := strings.TrimSpace(r.PathValue("request_id"))
	if requestID == "" || len(requestID) > 200 {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"error": "request_id_required"}, nil)
		return
	}

	rec, err := h.traces.GetTrace(ctx, tenantID, requestID)
	if err != nil {
		if errors.Is(err, ceoobs.ErrTraceNotFound) {
			httpresponse.WriteError(w, r, h.log, http.StatusNotFound, map[string]string{"error": "trace_not_found"}, nil)
			return
		}
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, map[string]string{"error": "trace_error"}, err)
		return
	}

	// Sanitize again at the boundary: the internal trace must never carry a
	// secret out, even to an admin. Goat RFID/tags are allowed (not PII).
	httpresponse.WriteJSON(w, http.StatusOK, rec.Sanitize())
}

// isTraceAdmin reports whether any active grant is the product-admin role that
// may view internal assistant traces. Role comes from server-side grants only.
func isTraceAdmin(grants []permissions.ActiveGrant) bool {
	for _, g := range grants {
		if g.Role == permissions.RoleCEOInternal {
			return true
		}
	}
	return false
}
