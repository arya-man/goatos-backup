package persistence

import (
	"context"
	"time"
)

// ConversationStore is the durable thread + message-history port. Implementations are
// tenant+actor scoped on every method; the (tenantID, actorID) arguments come from the
// server session and are enforced in the WHERE clause of every query.
type ConversationStore interface {
	// Create opens a thread. Idempotent when NewConversation.IdempotencyKey is set:
	// a replay returns the existing thread (created=false).
	Create(ctx context.Context, in NewConversation) (conv Conversation, created bool, err error)

	// Get loads one live thread scoped to the actor. ErrNotFound on miss/deleted/cross-tenant.
	Get(ctx context.Context, tenantID, actorID, conversationID string) (Conversation, error)

	// List returns a bounded keyset page of the actor's threads, newest activity first.
	List(ctx context.Context, q ListConversationsQuery) (ConversationPage, error)

	// Rename updates the thread title. ErrNotFound if not the actor's live thread.
	Rename(ctx context.Context, tenantID, actorID, conversationID, title string) (Conversation, error)

	// Archive / Unarchive toggle the archived tombstone without deleting history.
	Archive(ctx context.Context, tenantID, actorID, conversationID string) (Conversation, error)
	Unarchive(ctx context.Context, tenantID, actorID, conversationID string) (Conversation, error)

	// SoftDelete marks the thread deleted by stamping retention_expires_at=now(): it
	// disappears from every read immediately and is hard-purged by the retention sweep.
	SoftDelete(ctx context.Context, tenantID, actorID, conversationID string) error

	// AppendMessage persists a turn and bumps the parent thread's updated_at atomically.
	// ErrNotFound if the parent is not the actor's live thread.
	AppendMessage(ctx context.Context, in NewMessage) (Message, error)

	// ListMessages returns a bounded keyset window of a thread's history, oldest-first.
	ListMessages(ctx context.Context, q ListMessagesQuery) (MessagePage, error)

	// PurgeExpired hard-deletes threads and cascaded messages whose retention_expires_at
	// is at or before cutoff. Returns the number of threads purged.
	PurgeExpired(ctx context.Context, cutoff time.Time) (int64, error)
}
