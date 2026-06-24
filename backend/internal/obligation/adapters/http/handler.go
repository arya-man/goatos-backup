// Package http exposes the obligation module's read API (Action Center).
package http

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// DueLister is the slice of the obligation service the Action Center needs.
type DueLister interface {
	ListDue(ctx context.Context, tenantID, status string, dueBefore time.Time, limit int32) ([]domain.DueObligation, error)
}

// Handler serves the Action Center endpoints.
type Handler struct {
	due DueLister
	log *slog.Logger
}

// NewHandler constructs the handler with an optional logger.
func NewHandler(due DueLister, log ...*slog.Logger) *Handler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Handler{due: due, log: l}
}

// Register mounts the Action Center routes.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /action-center/obligations", h.ListDue)
}

const (
	defaultDueLimit = 100
	maxDueLimit     = 500
)

var allowedWorkStates = map[string]bool{
	"scheduled":            true,
	"due":                  true,
	"overdue":              true,
	"in_progress":          true,
	"proof_pending":        true,
	"verification_pending": true,
	"rejected":             true,
	"deferred":             true,
	"owner_missing":        true,
	"blocked":              true,
	"missed":               true,
	"completed":            true,
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

type dueItem struct {
	ObligationID      string    `json:"obligation_id"`
	ProtocolVersionID string    `json:"protocol_version_id"`
	RuleID            string    `json:"rule_id"`
	TargetType        string    `json:"target_type"`
	TargetID          string    `json:"target_id"`
	ScopeType         string    `json:"scope_type"`
	ScopeID           string    `json:"scope_id"`
	DueAt             time.Time `json:"due_at"`
	Status            string    `json:"status"`
	WorkState         string    `json:"work_state"`
}

type dueResponse struct {
	Items []dueItem `json:"items"`
}

// ListDue returns the obligations due as of due_before (default: now), for a status (default: due),
// earliest due first. Bounded by limit (default 100, max 500).
func (h *Handler) ListDue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	workState := q.Get("work_state")
	if workState == "" {
		workState = q.Get("status")
	}
	if workState == "" {
		workState = "due"
	}
	if !allowedWorkStates[workState] {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			errorEnvelope{Code: "invalid_status", Message: "status must be one of the Action Center work states", TraceID: traceID(r)}, nil)
		return
	}

	dueBefore := time.Now()
	if v := q.Get("due_before"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
				errorEnvelope{Code: "invalid_due_before", Message: "due_before must be RFC3339", TraceID: traceID(r)}, nil)
			return
		}
		dueBefore = parsed
	}

	limit := int32(defaultDueLimit)
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
				errorEnvelope{Code: "invalid_limit", Message: "limit must be a positive integer", TraceID: traceID(r)}, nil)
			return
		}
		if n > maxDueLimit {
			n = maxDueLimit
		}
		limit = int32(n)
	}

	repoStatus, postFilter := repoStatusForWorkState(workState)
	if repoStatus == "" {
		httpresponse.WriteJSON(w, http.StatusOK, dueResponse{Items: []dueItem{}})
		return
	}

	asOf := time.Now()
	rows, err := h.due.ListDue(r.Context(), tenantID(r), repoStatus, dueBefore, limit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
		return
	}
	items := make([]dueItem, 0, len(rows))
	for _, o := range rows {
		rowWorkState := workStateForObligation(o.Status, o.DueAt, asOf)
		if postFilter != "" && rowWorkState != postFilter {
			continue
		}
		items = append(items, dueItem{
			ObligationID:      o.ObligationID,
			ProtocolVersionID: o.ProtocolVersionID,
			RuleID:            o.RuleID,
			TargetType:        o.TargetType,
			TargetID:          o.TargetID,
			ScopeType:         o.ScopeType,
			ScopeID:           o.ScopeID,
			DueAt:             o.DueAt,
			Status:            o.Status,
			WorkState:         rowWorkState,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, dueResponse{Items: items})
}

func repoStatusForWorkState(workState string) (string, string) {
	switch workState {
	case "overdue":
		return "due", "overdue"
	case "proof_pending":
		return "in_progress", "proof_pending"
	case "deferred":
		return "waived", "deferred"
	case "missed":
		return "missed", "missed"
	case "scheduled", "due", "in_progress":
		return workState, ""
	default:
		return "", ""
	}
}

func workStateForObligation(status string, dueAt, asOf time.Time) string {
	switch status {
	case "scheduled", "due":
		if dueAt.Before(asOf) {
			return "overdue"
		}
		return "due"
	case "in_progress":
		return "proof_pending"
	case "waived":
		return "deferred"
	case "missed":
		return "missed"
	default:
		return status
	}
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}
