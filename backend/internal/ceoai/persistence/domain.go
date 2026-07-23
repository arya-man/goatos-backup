// Package persistence is the durable memory of the CEO/leadership assistant:
// conversation threads and per-turn message history. It is a thin, tenant+actor-scoped
// store layer over the app-owned ceo_ai_* tables (migration 000021 + 000022). Every read
// and write is scoped by (tenant_id, actor_id) taken from the server-side session --
// NEVER from user text -- so actor A can never observe actor B's threads even within the
// same tenant.
package persistence

import (
	"encoding/json"
	"errors"
	"time"
)

// Sentinel errors returned by the stores. Callers map these to HTTP status codes at
// the boundary; they never leak internal detail into a leadership answer.
var (
	// ErrNotFound is returned when a scoped lookup matches no live row (wrong id, wrong
	// tenant, wrong actor, archived-out, or already deleted/expired). It is deliberately
	// indistinguishable from a cross-tenant miss so a probing caller cannot tell
	// "exists but not yours" from "does not exist".
	ErrNotFound = errors.New("ceoai/persistence: not found")

	// ErrInvalidArgument is returned for malformed input (empty tenant/actor,
	// oversized page) before any query runs -- fail fast, never silent-default.
	ErrInvalidArgument = errors.New("ceoai/persistence: invalid argument")
)

// Role is the author of a message turn. The ceo_ai_messages CHECK allows these three.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Valid reports whether r is a role the store will persist.
func (r Role) Valid() bool { return r == RoleUser || r == RoleAssistant || r == RoleSystem }

// MaxPageSize bounds every keyset page so a leadership thread list can never dump an
// unbounded result set (scale rule: bounded rows + LIMIT on every hot path).
const MaxPageSize = 20

// MaxTitleLen bounds a stored thread title.
const MaxTitleLen = 200

// Conversation is one leadership assistant thread. Delete is modeled by
// RetentionExpiresAt: a soft-deleted thread has it set to now (hidden from every read,
// eligible for the purge sweep); an archived thread has ArchivedAt set but remains
// resumable.
type Conversation struct {
	ID                 string
	TenantID           string
	ActorID            string
	Title              string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ArchivedAt         *time.Time // set when the actor archives; still resumable, hidden from default list.
	RetentionExpiresAt *time.Time // hard-purge boundary; when <= now the thread is treated as deleted.
}

// Message is a single persisted turn within a thread. The ceo_ai_messages row is owned
// by its parent thread (no per-message actor column); scope is enforced through the
// parent conversation's actor_id. Assistant turns carry provenance (source/mode/
// request_id) plus structured tool_calls (INTERNAL) and citations (user-visible).
type Message struct {
	ID             string
	ConversationID string
	TenantID       string
	Role           Role
	Content        string
	ToolCalls      json.RawMessage // INTERNAL routing/exec detail; never rendered raw to leadership.
	Citations      json.RawMessage // user-visible provenance chips.
	Source         string          // assistant only: resolved read surface / tier label.
	Mode           string          // assistant only: governed|operational|exploratory.
	RequestID      string          // assistant only: links to ceo_ai_assistant_audit.request_id.
	CreatedAt      time.Time
}

// NewConversation is the idempotent create input. IdempotencyKey, when non-empty, makes
// create replay-safe: a retry with the same (tenant, actor, key) returns the original
// row instead of forking a second thread.
type NewConversation struct {
	TenantID       string
	ActorID        string
	Title          string
	IdempotencyKey string
	// RetentionExpiresAt optionally stamps the hard-purge boundary at create time per the
	// retention policy. Nil leaves it open (no scheduled purge until soft-delete).
	RetentionExpiresAt *time.Time
}

// NewMessage appends a turn. ActorID is used to authorize the write against the parent
// thread's owner; it is not stored on the message row. The store stamps id + created_at
// and bumps the parent thread's updated_at inside the same transaction.
type NewMessage struct {
	ConversationID string
	TenantID       string
	ActorID        string
	Role           Role
	Content        string
	ToolCalls      json.RawMessage
	Citations      json.RawMessage
	Source         string
	Mode           string
	RequestID      string
}

// ConversationCursor is the opaque keyset position for thread listing, ordered by
// (updated_at DESC, id DESC). It is a value, not an OFFSET, so pagination stays O(1)
// regardless of how deep the list runs.
type ConversationCursor struct {
	UpdatedAt time.Time
	ID        string
}

// ListConversationsQuery is a bounded, keyset-paginated thread listing scoped to one actor.
type ListConversationsQuery struct {
	TenantID        string
	ActorID         string
	PageSize        int
	Cursor          *ConversationCursor
	IncludeArchived bool // false => archived threads are hidden (deleted always hidden).
}

// ConversationPage is one keyset page of threads plus the cursor for the next page.
type ConversationPage struct {
	Items      []Conversation
	NextCursor *ConversationCursor
	HasMore    bool
}

// MessageCursor is the keyset position for message history, ordered by
// (created_at ASC, id ASC) so a resumed thread reads oldest-first.
type MessageCursor struct {
	CreatedAt time.Time
	ID        string
}

// ListMessagesQuery is a bounded window of a thread's history for planner memory.
type ListMessagesQuery struct {
	ConversationID string
	TenantID       string
	ActorID        string
	PageSize       int
	Cursor         *MessageCursor
}

// MessagePage is one keyset page of message history.
type MessagePage struct {
	Items      []Message
	NextCursor *MessageCursor
	HasMore    bool
}
