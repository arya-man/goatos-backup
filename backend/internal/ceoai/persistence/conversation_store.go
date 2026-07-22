package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultQueryTimeout = 3 * time.Second

// convCols is the canonical column list returned for a Conversation, in scan order.
const convCols = `id::text, tenant_id::text, actor_id::text, title,
       created_at, updated_at, archived_at, retention_expires_at`

// livePredicate treats a thread as deleted once its retention boundary has passed. A
// NULL or future retention_expires_at is still live (a future value is a scheduled purge
// under the retention policy, not a delete).
const livePredicate = `(retention_expires_at IS NULL OR retention_expires_at > now())`

// PostgresConversationStore implements ConversationStore over ceo_ai_conversations and
// ceo_ai_messages. It uses keyset pagination (never OFFSET) and casts every scalar to
// its column type ($n::uuid) so the query plan stays SARGable.
type PostgresConversationStore struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

var _ ConversationStore = (*PostgresConversationStore)(nil)

// NewPostgresConversationStore binds a store to a pgx pool.
func NewPostgresConversationStore(pool *pgxpool.Pool, queryTimeout time.Duration) *PostgresConversationStore {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &PostgresConversationStore{pool: pool, queryTimeout: queryTimeout}
}

func (s *PostgresConversationStore) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.queryTimeout)
}

// Create opens a thread. When IdempotencyKey is set the insert is ON CONFLICT DO
// NOTHING against the partial unique (tenant, actor, idempotency_key); a replay then
// re-selects the original row so retries never fork a thread.
func (s *PostgresConversationStore) Create(ctx context.Context, in NewConversation) (Conversation, bool, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ActorID) == "" {
		return Conversation{}, false, fmt.Errorf("%w: tenant and actor required", ErrInvalidArgument)
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	var retention any
	if in.RetentionExpiresAt != nil {
		retention = in.RetentionExpiresAt.UTC()
	}

	// nullif('' -> NULL) keeps the partial unique index from treating every keyless
	// create as a collision (NULLs are distinct); a keyed create is replay-safe.
	row := s.pool.QueryRow(ctx, `
INSERT INTO ceo_ai_conversations (tenant_id, actor_id, title, idempotency_key, retention_expires_at)
VALUES ($1::uuid, $2::uuid, $3, nullif($4, ''), $5::timestamptz)
ON CONFLICT (tenant_id, actor_id, idempotency_key) WHERE idempotency_key IS NOT NULL
DO NOTHING
RETURNING `+convCols,
		in.TenantID, in.ActorID, titleArg(in.Title), in.IdempotencyKey, retention)

	conv, err := scanConversation(row)
	if err == nil {
		return conv, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, false, fmt.Errorf("ceoai: create conversation: %w", err)
	}
	// Conflict path: an existing keyed thread. Re-select it (created=false).
	existing, selErr := s.getByIdempotencyKey(ctx, in.TenantID, in.ActorID, in.IdempotencyKey)
	if selErr != nil {
		return Conversation{}, false, selErr
	}
	return existing, false, nil
}

func (s *PostgresConversationStore) getByIdempotencyKey(ctx context.Context, tenantID, actorID, key string) (Conversation, error) {
	row := s.pool.QueryRow(ctx, `
SELECT `+convCols+`
FROM ceo_ai_conversations
WHERE tenant_id = $1::uuid AND actor_id = $2::uuid AND idempotency_key = $3
  AND `+livePredicate, tenantID, actorID, key)
	conv, err := scanConversation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("ceoai: reselect conversation: %w", err)
	}
	return conv, nil
}

// Get loads one live, actor-scoped thread.
func (s *PostgresConversationStore) Get(ctx context.Context, tenantID, actorID, conversationID string) (Conversation, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	row := s.pool.QueryRow(ctx, `
SELECT `+convCols+`
FROM ceo_ai_conversations
WHERE tenant_id = $1::uuid AND actor_id = $2::uuid AND id = $3::uuid
  AND `+livePredicate, tenantID, actorID, conversationID)
	conv, err := scanConversation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("ceoai: get conversation: %w", err)
	}
	return conv, nil
}

// List returns a bounded keyset page of the actor's threads, newest activity first.
func (s *PostgresConversationStore) List(ctx context.Context, q ListConversationsQuery) (ConversationPage, error) {
	if strings.TrimSpace(q.TenantID) == "" || strings.TrimSpace(q.ActorID) == "" {
		return ConversationPage{}, fmt.Errorf("%w: tenant and actor required", ErrInvalidArgument)
	}
	pageSize := clampPageSize(q.PageSize)
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	var cursorUpdatedAt, cursorID any
	if q.Cursor != nil {
		cursorUpdatedAt = q.Cursor.UpdatedAt.UTC()
		cursorID = q.Cursor.ID
	}

	rows, err := s.pool.Query(ctx, `
SELECT `+convCols+`
FROM ceo_ai_conversations
WHERE tenant_id = $1::uuid AND actor_id = $2::uuid
  AND `+livePredicate+`
  AND ($3::bool OR archived_at IS NULL)
  AND ($4::timestamptz IS NULL OR (updated_at, id) < ($4::timestamptz, $5::uuid))
ORDER BY updated_at DESC, id DESC
LIMIT $6`, q.TenantID, q.ActorID, q.IncludeArchived, cursorUpdatedAt, cursorID, pageSize+1)
	if err != nil {
		return ConversationPage{}, fmt.Errorf("ceoai: list conversations: %w", err)
	}
	defer rows.Close()

	items := make([]Conversation, 0, pageSize)
	for rows.Next() {
		conv, scanErr := scanConversation(rows)
		if scanErr != nil {
			return ConversationPage{}, fmt.Errorf("ceoai: scan conversation: %w", scanErr)
		}
		items = append(items, conv)
	}
	if err := rows.Err(); err != nil {
		return ConversationPage{}, fmt.Errorf("ceoai: iterate conversations: %w", err)
	}
	return buildConversationPage(items, pageSize), nil
}

func buildConversationPage(items []Conversation, pageSize int) ConversationPage {
	page := ConversationPage{}
	if len(items) > pageSize {
		page.HasMore = true
		items = items[:pageSize]
	}
	page.Items = items
	if page.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		page.NextCursor = &ConversationCursor{UpdatedAt: last.UpdatedAt, ID: last.ID}
	}
	return page
}

// Rename updates the title and bumps updated_at.
func (s *PostgresConversationStore) Rename(ctx context.Context, tenantID, actorID, conversationID, title string) (Conversation, error) {
	return s.mutate(ctx, tenantID, actorID, conversationID, `
UPDATE ceo_ai_conversations
SET title = $4, updated_at = now()
WHERE tenant_id = $1::uuid AND actor_id = $2::uuid AND id = $3::uuid
  AND `+livePredicate+`
RETURNING `+convCols, titleArg(title))
}

// Archive sets the archived tombstone (idempotent: re-archiving is a no-op flip).
func (s *PostgresConversationStore) Archive(ctx context.Context, tenantID, actorID, conversationID string) (Conversation, error) {
	return s.mutate(ctx, tenantID, actorID, conversationID, `
UPDATE ceo_ai_conversations
SET archived_at = COALESCE(archived_at, now()), updated_at = now()
WHERE tenant_id = $1::uuid AND actor_id = $2::uuid AND id = $3::uuid
  AND `+livePredicate+`
RETURNING `+convCols)
}

// Unarchive clears the archived tombstone.
func (s *PostgresConversationStore) Unarchive(ctx context.Context, tenantID, actorID, conversationID string) (Conversation, error) {
	return s.mutate(ctx, tenantID, actorID, conversationID, `
UPDATE ceo_ai_conversations
SET archived_at = NULL, updated_at = now()
WHERE tenant_id = $1::uuid AND actor_id = $2::uuid AND id = $3::uuid
  AND `+livePredicate+`
RETURNING `+convCols)
}

// mutate runs an actor-scoped RETURNING update and maps no-rows to ErrNotFound.
// extraArgs bind to $4.. after the (tenant, actor, conversation) scope triple.
func (s *PostgresConversationStore) mutate(ctx context.Context, tenantID, actorID, conversationID, sql string, extraArgs ...any) (Conversation, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	args := append([]any{tenantID, actorID, conversationID}, extraArgs...)
	row := s.pool.QueryRow(ctx, sql, args...)
	conv, err := scanConversation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("ceoai: mutate conversation: %w", err)
	}
	return conv, nil
}

// SoftDelete marks the thread deleted by stamping retention_expires_at=now(); it drops
// out of every live read immediately and is hard-removed by PurgeExpired.
func (s *PostgresConversationStore) SoftDelete(ctx context.Context, tenantID, actorID, conversationID string) error {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	tag, err := s.pool.Exec(ctx, `
UPDATE ceo_ai_conversations
SET retention_expires_at = now(), updated_at = now()
WHERE tenant_id = $1::uuid AND actor_id = $2::uuid AND id = $3::uuid
  AND `+livePredicate, tenantID, actorID, conversationID)
	if err != nil {
		return fmt.Errorf("ceoai: soft-delete conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AppendMessage inserts a turn and bumps the parent thread's updated_at in one
// transaction so the thread list re-sorts atomically with the new turn. The parent
// bump is the actor-scoped guard: a message for a thread that is not the actor's live
// thread finds no row to bump and the whole transaction is ErrNotFound.
func (s *PostgresConversationStore) AppendMessage(ctx context.Context, in NewMessage) (Message, error) {
	if !in.Role.Valid() {
		return Message{}, fmt.Errorf("%w: role %q", ErrInvalidArgument, in.Role)
	}
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ActorID) == "" || strings.TrimSpace(in.ConversationID) == "" {
		return Message{}, fmt.Errorf("%w: tenant, actor, conversation required", ErrInvalidArgument)
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Message{}, fmt.Errorf("ceoai: begin append: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	// Bump the parent first; RowsAffected==0 => not the actor's live thread.
	tag, err := tx.Exec(ctx, `
UPDATE ceo_ai_conversations
SET updated_at = now()
WHERE tenant_id = $1::uuid AND actor_id = $2::uuid AND id = $3::uuid
  AND `+livePredicate, in.TenantID, in.ActorID, in.ConversationID)
	if err != nil {
		return Message{}, fmt.Errorf("ceoai: bump conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Message{}, ErrNotFound
	}

	// tool_calls/citations are NOT NULL DEFAULT '[]'; COALESCE a nil bind to that default.
	row := tx.QueryRow(ctx, `
INSERT INTO ceo_ai_messages (
  conversation_id, tenant_id, role, content, tool_calls, citations, source, mode, request_id)
VALUES ($1::uuid, $2::uuid, $3, $4,
        COALESCE($5::jsonb, '[]'::jsonb), COALESCE($6::jsonb, '[]'::jsonb),
        nullif($7, ''), nullif($8, ''), nullif($9, ''))
RETURNING id::text, conversation_id::text, tenant_id::text, role, content,
          tool_calls, citations, source, mode, request_id, created_at`,
		in.ConversationID, in.TenantID, string(in.Role), in.Content,
		jsonbArg(in.ToolCalls), jsonbArg(in.Citations), in.Source, in.Mode, in.RequestID)
	msg, err := scanMessage(row)
	if err != nil {
		return Message{}, fmt.Errorf("ceoai: insert message: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, fmt.Errorf("ceoai: commit append: %w", err)
	}
	return msg, nil
}

// ListMessages returns a bounded keyset window of a thread's history, oldest-first.
// The parent live+scope check happens via the join predicate so a deleted or
// cross-actor thread yields an empty window rather than leaking rows.
func (s *PostgresConversationStore) ListMessages(ctx context.Context, q ListMessagesQuery) (MessagePage, error) {
	if strings.TrimSpace(q.TenantID) == "" || strings.TrimSpace(q.ActorID) == "" || strings.TrimSpace(q.ConversationID) == "" {
		return MessagePage{}, fmt.Errorf("%w: tenant, actor, conversation required", ErrInvalidArgument)
	}
	pageSize := clampPageSize(q.PageSize)
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	var cursorCreatedAt, cursorID any
	if q.Cursor != nil {
		cursorCreatedAt = q.Cursor.CreatedAt.UTC()
		cursorID = q.Cursor.ID
	}

	rows, err := s.pool.Query(ctx, `
SELECT m.id::text, m.conversation_id::text, m.tenant_id::text, m.role, m.content,
       m.tool_calls, m.citations, m.source, m.mode, m.request_id, m.created_at
FROM ceo_ai_messages m
JOIN ceo_ai_conversations c
  ON c.id = m.conversation_id
 AND (c.retention_expires_at IS NULL OR c.retention_expires_at > now())
WHERE m.conversation_id = $1::uuid
  AND m.tenant_id = $2::uuid
  AND c.actor_id = $3::uuid
  AND ($4::timestamptz IS NULL OR (m.created_at, m.id) > ($4::timestamptz, $5::uuid))
ORDER BY m.created_at ASC, m.id ASC
LIMIT $6`, q.ConversationID, q.TenantID, q.ActorID, cursorCreatedAt, cursorID, pageSize+1)
	if err != nil {
		return MessagePage{}, fmt.Errorf("ceoai: list messages: %w", err)
	}
	defer rows.Close()

	items := make([]Message, 0, pageSize)
	for rows.Next() {
		msg, scanErr := scanMessage(rows)
		if scanErr != nil {
			return MessagePage{}, fmt.Errorf("ceoai: scan message: %w", scanErr)
		}
		items = append(items, msg)
	}
	if err := rows.Err(); err != nil {
		return MessagePage{}, fmt.Errorf("ceoai: iterate messages: %w", err)
	}

	page := MessagePage{}
	if len(items) > pageSize {
		page.HasMore = true
		items = items[:pageSize]
	}
	page.Items = items
	if page.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		page.NextCursor = &MessageCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}

// purgeBatchSize bounds how many expired threads a single retention DELETE claims.
// The sweep loops in bounded batches so no tick issues one giant cross-tenant DELETE
// (with its cascade fan-out into ceo_ai_messages/ceo_ai_feedback) that would hold locks
// on every expired row in one long transaction and block concurrent create/append.
const purgeBatchSize = 500

// PurgeExpired hard-deletes threads past the retention boundary. Messages and feedback
// are removed by ON DELETE CASCADE on the child FKs.
//
// Scale: this is a worker sweep path, so it must not run one unbounded, un-chunked
// DELETE across all tenants (the banned "polling full scan / unbounded worker tick"
// anti-pattern). It claims expired rows in keyset-independent bounded batches via a
// LIMIT + FOR UPDATE SKIP LOCKED subquery — the same pattern the obligation/idempotency
// sweepers use — so each DELETE is small, concurrent purges never contend on the same
// rows, and every batch commits on its own (autocommit) so retention converges tick over
// tick even if a later batch is interrupted. Each batch gets its own queryTimeout budget
// so one slow batch cannot starve the rest of the sweep.
func (s *PostgresConversationStore) PurgeExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	var total int64
	cut := cutoff.UTC()
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := s.purgeExpiredBatch(ctx, cut)
		if err != nil {
			return total, err
		}
		total += n
		// A short batch means no more claimable expired rows this pass.
		if n < purgeBatchSize {
			return total, nil
		}
	}
}

// purgeExpiredBatch deletes up to purgeBatchSize expired threads in one bounded,
// self-contained (autocommit) statement and returns the rows removed.
func (s *PostgresConversationStore) purgeExpiredBatch(ctx context.Context, cutoff time.Time) (int64, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	tag, err := s.pool.Exec(ctx, `
DELETE FROM ceo_ai_conversations
WHERE id IN (
    SELECT id
    FROM ceo_ai_conversations
    WHERE retention_expires_at IS NOT NULL AND retention_expires_at <= $1::timestamptz
    ORDER BY retention_expires_at
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)`, cutoff, purgeBatchSize)
	if err != nil {
		return 0, fmt.Errorf("ceoai: purge expired conversations: %w", err)
	}
	return tag.RowsAffected(), nil
}
