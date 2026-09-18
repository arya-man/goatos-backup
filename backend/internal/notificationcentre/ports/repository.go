// Package ports states what the notification centre needs from storage.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/domain"
)

// Errors storage may return.
var (
	// ErrInvalidArgument is a malformed cursor or id reaching the adapter.
	ErrInvalidArgument = errors.New("notificationcentre: invalid argument")
	// ErrIdempotencyConflict is the same Idempotency-Key re-presented with a DIFFERENT id
	// set. The first call's effect stands; the second is refused rather than applied.
	ErrIdempotencyConflict = errors.New("notificationcentre: idempotency conflict")
)

// ListParams is one page request.
//
// MemberOrUserID is whoever is asking, as the caller's authenticated actor id. The adapter
// resolves it to a canonical ACTIVE workforce_member_id (either the member id itself or the
// linked user_id) and filters the feed on that. There is deliberately no way to ask for
// someone else's feed: no parameter names another person.
type ListParams struct {
	TenantID       string
	MemberOrUserID string
	Cursor         string
	Limit          int
}

// MarkReadParams is one mark-as-read batch. Ids that do not belong to MemberOrUserID are
// IGNORED, never updated, and never counted.
type MarkReadParams struct {
	TenantID       string
	MemberOrUserID string
	IDs            []string
	IdempotencyKey string
}

// Repository is the storage contract.
type Repository interface {
	ListNotifications(ctx context.Context, p ListParams) (domain.Page, error)
	MarkRead(ctx context.Context, p MarkReadParams) (int, error)
}
