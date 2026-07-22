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

// PostgresFeedbackStore implements FeedbackStore over ceo_ai_feedback. Upsert is
// idempotent on the (message_id, actor_id) unique key so a leadership user re-rating the
// same answer updates their existing signal instead of forking duplicate rows that
// would double-count in the eval set.
type PostgresFeedbackStore struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

var _ FeedbackStore = (*PostgresFeedbackStore)(nil)

// NewPostgresFeedbackStore binds a feedback store to a pgx pool.
func NewPostgresFeedbackStore(pool *pgxpool.Pool, queryTimeout time.Duration) *PostgresFeedbackStore {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &PostgresFeedbackStore{pool: pool, queryTimeout: queryTimeout}
}

func (s *PostgresFeedbackStore) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.queryTimeout)
}

// Upsert records the actor's thumbs on one message. The tenant scope is resolved FROM
// the message row inside the insert (SELECT ... FROM ceo_ai_messages joined to the
// owning live conversation) so a caller cannot attach feedback to a message that is not
// in their own live thread: the guarded SELECT returns no row and the insert affects
// nothing, yielding ErrNotFound. ON CONFLICT makes a re-vote an in-place update.
func (s *PostgresFeedbackStore) Upsert(ctx context.Context, in NewFeedback) (Feedback, error) {
	if !in.Rating.Valid() {
		return Feedback{}, fmt.Errorf("%w: rating %d", ErrInvalidArgument, in.Rating)
	}
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ActorID) == "" || strings.TrimSpace(in.MessageID) == "" {
		return Feedback{}, fmt.Errorf("%w: tenant, actor, message required", ErrInvalidArgument)
	}
	reason := clampReason(in.Reason)
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	row := s.pool.QueryRow(ctx, `
INSERT INTO ceo_ai_feedback (message_id, tenant_id, actor_id, rating, reason)
SELECT m.id, m.tenant_id, $2::uuid, $4::smallint, nullif($5, '')
FROM ceo_ai_messages m
JOIN ceo_ai_conversations c
  ON c.id = m.conversation_id
 AND (c.retention_expires_at IS NULL OR c.retention_expires_at > now())
WHERE m.id = $1::uuid
  AND m.tenant_id = $3::uuid
  AND c.actor_id = $2::uuid
ON CONFLICT (message_id, actor_id)
DO UPDATE SET rating = EXCLUDED.rating, reason = EXCLUDED.reason
RETURNING id::text, message_id::text, tenant_id::text, actor_id::text, rating, reason, created_at`,
		in.MessageID, in.ActorID, in.TenantID, int16(in.Rating), reason)

	fb, err := scanFeedback(row)
	if errors.Is(err, pgx.ErrNoRows) {
		// No message matched the actor+tenant+live-thread guard.
		return Feedback{}, ErrNotFound
	}
	if err != nil {
		return Feedback{}, fmt.Errorf("ceoai: upsert feedback: %w", err)
	}
	return fb, nil
}

// Get returns the actor's feedback on a message, or ErrNotFound.
func (s *PostgresFeedbackStore) Get(ctx context.Context, tenantID, actorID, messageID string) (Feedback, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	row := s.pool.QueryRow(ctx, `
SELECT id::text, message_id::text, tenant_id::text, actor_id::text, rating, reason, created_at
FROM ceo_ai_feedback
WHERE message_id = $1::uuid AND tenant_id = $2::uuid AND actor_id = $3::uuid`,
		messageID, tenantID, actorID)
	fb, err := scanFeedback(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Feedback{}, ErrNotFound
	}
	if err != nil {
		return Feedback{}, fmt.Errorf("ceoai: get feedback: %w", err)
	}
	return fb, nil
}
