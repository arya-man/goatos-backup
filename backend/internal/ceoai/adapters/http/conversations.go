package http

// This file mounts the leadership assistant's thread surface, the companion to
// POST /ceo-ai/ask. The admin-web proxy (apps/admin-web/app/api/ceo-ai/*)
// forwards to exactly these backend paths:
//
//	GET    /ceo-ai/starters                       leadership capability probe + starter questions
//	GET    /ceo-ai/conversations                  keyset-paginated thread list
//	POST   /ceo-ai/conversations                  open a new (empty) thread
//	GET    /ceo-ai/conversations/{id}/messages    keyset message history for a thread
//	PATCH  /ceo-ai/conversations/{id}             rename a thread ({title})
//	DELETE /ceo-ai/conversations/{id}             soft-delete a thread
//
// Every route resolves the Actor from the SERVER SESSION (never user text) and
// is scoped to (tenant_id, actor_id). The starters route doubles as the
// leadership authorization probe the client uses to decide whether to render
// the assistant bubble: 200 => leadership (ceo_internal), 403 => not leadership.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/persistence"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// ConvStore is the subset of the durable conversation store this adapter drives.
// *persistence.PostgresConversationStore satisfies it.
type ConvStore interface {
	Create(ctx context.Context, in persistence.NewConversation) (persistence.Conversation, bool, error)
	Get(ctx context.Context, tenantID, actorID, conversationID string) (persistence.Conversation, error)
	List(ctx context.Context, q persistence.ListConversationsQuery) (persistence.ConversationPage, error)
	Rename(ctx context.Context, tenantID, actorID, conversationID, title string) (persistence.Conversation, error)
	SoftDelete(ctx context.Context, tenantID, actorID, conversationID string) error
	ListMessages(ctx context.Context, q persistence.ListMessagesQuery) (persistence.MessagePage, error)
}

// StartersProvider returns the tenant/role-aware starter questions the assistant
// can truthfully answer. A nil provider falls back to defaultStarters.
type StartersProvider func(ctx context.Context, actor Actor) []string

// Actor is the minimal session identity the conversation handler needs.
type Actor struct {
	TenantID string
	UserID   string
	Role     string
}

func (a Actor) leadership() bool { return a.Role == permissions.RoleCEOInternal }

// defaultStarters are backend-owned starter questions truthful to the governed
// Cube metrics + read tools the assistant currently routes to.
var defaultStarters = []string{
	"How many active goats and sheep do we have?",
	"What vaccinations are overdue by park?",
	"How many vaccinations are due today?",
	"Which sheds are behind on vaccination?",
	"Plot vaccination overdue by park",
	"Chart vaccinations due today by park",
}

// ConversationHandler serves the thread + starters surface.
type ConversationHandler struct {
	conv     ConvStore
	starters StartersProvider
	log      *slog.Logger
}

// NewConversationHandler builds the handler. A nil conv store leaves the
// corresponding routes returning 503 (never a panic); starters still work so
// the leadership probe/launcher is never blocked by an unwired store.
func NewConversationHandler(conv ConvStore, starters StartersProvider, log *slog.Logger) *ConversationHandler {
	if log == nil {
		log = slog.Default()
	}
	return &ConversationHandler{conv: conv, starters: starters, log: log}
}

// Register mounts the thread and starters routes on the protected mux.
func (h *ConversationHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /ceo-ai/starters", h.Starters)
	mux.HandleFunc("GET /ceo-ai/conversations", h.ListConversations)
	mux.HandleFunc("POST /ceo-ai/conversations", h.CreateConversation)
	mux.HandleFunc("GET /ceo-ai/conversations/{id}/messages", h.ListMessages)
	mux.HandleFunc("PATCH /ceo-ai/conversations/{id}", h.RenameConversation)
	mux.HandleFunc("DELETE /ceo-ai/conversations/{id}", h.DeleteConversation)
}

// actorFrom resolves the leadership actor from the server session.
func (h *ConversationHandler) actorFrom(r *http.Request) (Actor, bool) {
	ctx := r.Context()
	full := BuildActor(
		httpmiddleware.TenantIDFromContext(ctx),
		httpmiddleware.ActorIDFromContext(ctx),
		httpmiddleware.LocaleTagFromContext(ctx),
		httpmiddleware.AuthGrantsFromContext(ctx),
	)
	a := Actor{TenantID: full.TenantID, UserID: full.UserID, Role: full.Role}
	return a, a.TenantID != "" && a.UserID != ""
}

// requireLeadership enforces both a valid session and the ceo_internal role. It
// writes the correct status and returns ok=false when the caller must stop.
func (h *ConversationHandler) requireLeadership(w http.ResponseWriter, r *http.Request) (Actor, bool) {
	actor, ok := h.actorFrom(r)
	if !ok {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, map[string]string{"error": "unauthorized"}, nil)
		return Actor{}, false
	}
	if !actor.leadership() {
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden, map[string]string{"error": "leadership_required"}, nil)
		return Actor{}, false
	}
	return actor, true
}

// Starters is GET /ceo-ai/starters — the leadership capability probe. 200 with
// starter questions for ceo_internal; 403 otherwise; 401 without a session.
func (h *ConversationHandler) Starters(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireLeadership(w, r)
	if !ok {
		return
	}
	starters := defaultStarters
	if h.starters != nil {
		if custom := h.starters(r.Context(), actor); len(custom) > 0 {
			starters = custom
		}
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]any{"starters": starters})
}

// ListConversations is GET /ceo-ai/conversations?cursor=&limit= — keyset page.
func (h *ConversationHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireLeadership(w, r)
	if !ok {
		return
	}
	if h.conv == nil {
		h.storeUnavailable(w, r)
		return
	}
	cursor, err := decodeConvCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"error": "invalid_cursor"}, err)
		return
	}
	page, err := h.conv.List(r.Context(), persistence.ListConversationsQuery{
		TenantID: actor.TenantID,
		ActorID:  actor.UserID,
		PageSize: parseLimit(r.URL.Query().Get("limit")),
		Cursor:   cursor,
	})
	if err != nil {
		h.writeStoreErr(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, c := range page.Items {
		items = append(items, conversationJSON(c))
	}
	body := map[string]any{"conversations": items}
	if page.NextCursor != nil {
		body["next_cursor"] = encodeConvCursor(page.NextCursor)
	}
	httpresponse.WriteJSON(w, http.StatusOK, body)
}

// CreateConversation is POST /ceo-ai/conversations — open a new empty thread.
func (h *ConversationHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireLeadership(w, r)
	if !ok {
		return
	}
	if h.conv == nil {
		h.storeUnavailable(w, r)
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	// Body is optional ("{}"); ignore decode errors for an empty/absent body.
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req)
	conv, _, err := h.conv.Create(r.Context(), persistence.NewConversation{
		TenantID: actor.TenantID,
		ActorID:  actor.UserID,
		Title:    strings.TrimSpace(req.Title),
	})
	if err != nil {
		h.writeStoreErr(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, conversationJSON(conv))
}

// ListMessages is GET /ceo-ai/conversations/{id}/messages — oldest-first history.
func (h *ConversationHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireLeadership(w, r)
	if !ok {
		return
	}
	if h.conv == nil {
		h.storeUnavailable(w, r)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	cursor, err := decodeMsgCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"error": "invalid_cursor"}, err)
		return
	}
	page, err := h.conv.ListMessages(r.Context(), persistence.ListMessagesQuery{
		ConversationID: id,
		TenantID:       actor.TenantID,
		ActorID:        actor.UserID,
		PageSize:       parseLimit(r.URL.Query().Get("limit")),
		Cursor:         cursor,
	})
	if err != nil {
		h.writeStoreErr(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, m := range page.Items {
		items = append(items, messageJSON(m))
	}
	body := map[string]any{"messages": items}
	if page.NextCursor != nil {
		body["next_cursor"] = encodeMsgCursor(page.NextCursor)
	}
	httpresponse.WriteJSON(w, http.StatusOK, body)
}

// RenameConversation is PATCH /ceo-ai/conversations/{id} — rename ({title}).
func (h *ConversationHandler) RenameConversation(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireLeadership(w, r)
	if !ok {
		return
	}
	if h.conv == nil {
		h.storeUnavailable(w, r)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"error": "invalid_json"}, err)
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"error": "title_required"}, nil)
		return
	}
	conv, err := h.conv.Rename(r.Context(), actor.TenantID, actor.UserID, id, title)
	if err != nil {
		h.writeStoreErr(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, conversationJSON(conv))
}

// DeleteConversation is DELETE /ceo-ai/conversations/{id} — soft-delete.
func (h *ConversationHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireLeadership(w, r)
	if !ok {
		return
	}
	if h.conv == nil {
		h.storeUnavailable(w, r)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if err := h.conv.SoftDelete(r.Context(), actor.TenantID, actor.UserID, id); err != nil {
		h.writeStoreErr(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// writeStoreErr maps store sentinels to HTTP status codes at the boundary.
func (h *ConversationHandler) writeStoreErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, persistence.ErrNotFound):
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound, map[string]string{"error": "not_found"}, err)
	case errors.Is(err, persistence.ErrInvalidArgument):
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"error": "invalid_argument"}, err)
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, map[string]string{"error": "assistant_error"}, err)
	}
}

func (h *ConversationHandler) storeUnavailable(w http.ResponseWriter, r *http.Request) {
	httpresponse.WriteError(w, r, h.log, http.StatusServiceUnavailable, map[string]string{"error": "assistant_unreachable", "mode": "degraded"}, nil)
}

func parseLimit(s string) int {
	if s == "" {
		return persistence.MaxPageSize
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return persistence.MaxPageSize
	}
	if n > persistence.MaxPageSize {
		return persistence.MaxPageSize
	}
	return n
}

func conversationJSON(c persistence.Conversation) map[string]any {
	return map[string]any{
		"id":         c.ID,
		"title":      c.Title,
		"created_at": c.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at": c.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func messageJSON(m persistence.Message) map[string]any {
	out := map[string]any{
		"id":         m.ID,
		"message_id": m.ID,
		"role":       string(m.Role),
		"content":    m.Content,
	}
	if m.Source != "" {
		out["source"] = m.Source
	}
	if m.Mode != "" {
		out["mode"] = m.Mode
	}
	if m.RequestID != "" {
		out["request_id"] = m.RequestID
	}
	if len(m.Citations) > 0 {
		out["citations"] = json.RawMessage(m.Citations)
	}
	return out
}

// --- opaque keyset cursor codecs (base64 "<rfc3339nano>|<id>") ---

func encodeConvCursor(c *persistence.ConversationCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.UpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID))
}

func decodeConvCursor(s string) (*persistence.ConversationCursor, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	ts, id, err := decodeCursor(s)
	if err != nil {
		return nil, err
	}
	return &persistence.ConversationCursor{UpdatedAt: ts, ID: id}, nil
}

func encodeMsgCursor(c *persistence.MessageCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID))
}

func decodeMsgCursor(s string) (*persistence.MessageCursor, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	ts, id, err := decodeCursor(s)
	if err != nil {
		return nil, err
	}
	return &persistence.MessageCursor{CreatedAt: ts, ID: id}, nil
}

func decodeCursor(s string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", errors.New("malformed cursor")
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", err
	}
	return ts, parts[1], nil
}
