// Package http exposes the leadership assistant read endpoint. It is a thin
// adapter: it resolves the Actor from the SERVER SESSION (never user text),
// delegates to the app.Assistant, and writes the answer/source/mode/request_id/
// conversation_id/citations envelope — nothing else (no step trace / CoT).
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/app"
	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Asker is the app port the handler drives (*app.Assistant satisfies it).
type Asker interface {
	Ask(ctx context.Context, q domain.Question) (domain.Answer, error)
}

// Handler serves POST /api/ceo-ai/ask.
type Handler struct {
	assistant Asker
	log       *slog.Logger
}

// NewHandler builds the handler.
func NewHandler(assistant Asker, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{assistant: assistant, log: log}
}

type askRequest struct {
	Question       string `json:"question"`
	ConversationID string `json:"conversation_id"`
	// Stream selects the SSE progressive-render transport. The admin-web proxy
	// sends stream:true by default and stream:false to force a single JSON
	// answer. nil is treated as true (streaming is the default UX). The router
	// dispatches on this field (see Register); path + permission are identical.
	Stream *bool `json:"stream"`
}

// Ask handles the leadership question endpoint.
func (h *Handler) Ask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	actor := BuildActor(
		httpmiddleware.TenantIDFromContext(ctx),
		httpmiddleware.ActorIDFromContext(ctx),
		httpmiddleware.LocaleTagFromContext(ctx),
		httpmiddleware.AuthGrantsFromContext(ctx),
	)
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, map[string]string{"error": "unauthorized"}, nil)
		return
	}

	var req askRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"error": "invalid_json"}, err)
		return
	}
	question := strings.TrimSpace(req.Question)
	if len(question) > 1200 {
		question = question[:1200]
	}

	q := domain.Question{
		Actor:          actor,
		ConversationID: strings.TrimSpace(req.ConversationID),
		Text:           question,
		AsOf:           biztime.BusinessDayStart(time.Now()),
	}

	ans, err := h.assistant.Ask(ctx, q)
	if err != nil {
		switch {
		case errors.Is(err, app.ErrForbidden):
			httpresponse.WriteError(w, r, h.log, http.StatusForbidden, map[string]string{"error": "leadership_required"}, err)
		case errors.Is(err, app.ErrEmptyQuestion):
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"error": "question_required"}, err)
		default:
			httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, map[string]string{"error": "assistant_error"}, err)
		}
		return
	}
	// Only the allowed user-facing fields. Never emit step traces / CoT.
	httpresponse.WriteJSON(w, http.StatusOK, ans)
}

// BuildActor composes the leadership Actor from session context values. Role,
// tenant, and permission grants come from the auth middleware — never the body.
func BuildActor(tenantID, actorID, locale string, grants []permissions.ActiveGrant) domain.Actor {
	role := ""
	perms := make([]string, 0, len(grants))
	for _, g := range grants {
		if g.Role == permissions.RoleCEOInternal {
			role = permissions.RoleCEOInternal
		}
		perms = append(perms, g.Role)
	}
	return domain.Actor{TenantID: tenantID, UserID: actorID, Role: role, Perms: perms, Locale: locale}
}
