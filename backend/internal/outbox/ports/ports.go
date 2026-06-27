package ports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/outbox/domain"
)

var (
	ErrPublishPermanent  = errors.New("permanent publish failure")
	ErrDLQActionConflict = errors.New("dlq action idempotency key reused with different request")
	ErrDLQActionPending  = errors.New("dlq action is still running")
)

type ClaimParams struct {
	Limit       int
	MaxAttempts int
	Now         time.Time
}

type ClaimResult struct {
	Messages        []domain.Message
	DeadLetterCount int
}

type DeadLetterQuery struct {
	TenantID  string
	Status    string
	EventType string
	Topic     string
	Limit     int
}

type ReplayDeadLettersParams struct {
	TenantID       string
	OutboxIDs      []string
	Reason         string
	Now            time.Time
	IdempotencyKey string
	RequestHash    string
}

type DiscardDeadLettersParams struct {
	TenantID       string
	OutboxIDs      []string
	Reason         string
	Now            time.Time
	IdempotencyKey string
	RequestHash    string
}

type Repository interface {
	ReclaimStalePublishing(ctx context.Context, now time.Time, leaseTimeout time.Duration) (int64, error)
	ClaimPending(ctx context.Context, params ClaimParams) (*ClaimResult, error)
	MarkPublished(ctx context.Context, outboxID string, now time.Time) error
	MarkRetry(ctx context.Context, outboxID string, nextAttemptAt time.Time, lastError string, now time.Time) error
	MarkFailed(ctx context.Context, outboxID string, lastError string, now time.Time) error
	MarkDeadLetter(ctx context.Context, outboxID string, lastError string, now time.Time) error
	ListDeadLetters(ctx context.Context, q DeadLetterQuery) ([]domain.DeadLetterMessage, error)
	Health(ctx context.Context, tenantID string, now time.Time) (domain.Health, error)
	ReplayDeadLetters(ctx context.Context, params ReplayDeadLettersParams) (int64, error)
	DiscardDeadLetters(ctx context.Context, params DiscardDeadLettersParams) (int64, error)
	Ping(ctx context.Context) error
}

type PublishMessage struct {
	OutboxID  string
	TenantID  string
	EventID   string
	EventType string
	Topic     string
	Headers   json.RawMessage
	Payload   json.RawMessage
	TraceID   *string
}

type Publisher interface {
	Publish(ctx context.Context, message PublishMessage) error
}

type PublishError struct {
	Err       error
	Retryable bool
}

func (e *PublishError) Error() string {
	if e == nil || e.Err == nil {
		return "publish error"
	}
	return e.Err.Error()
}

func (e *PublishError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func RetryablePublishError(err error) error {
	if err == nil {
		err = errors.New("publish failed")
	}
	return &PublishError{Err: err, Retryable: true}
}

func PermanentPublishError(err error) error {
	if err == nil {
		err = ErrPublishPermanent
	}
	return &PublishError{Err: fmt.Errorf("%w: %v", ErrPublishPermanent, err), Retryable: false}
}

func IsRetryablePublishFailure(err error) bool {
	if err == nil {
		return false
	}
	var publishErr *PublishError
	if errors.As(err, &publishErr) {
		return publishErr.Retryable
	}
	return true
}
