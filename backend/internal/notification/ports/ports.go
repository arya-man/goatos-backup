// Package ports defines notification delivery dependencies.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/notification/domain"
)

var (
	ErrChannelNotConfigured = errors.New("notification channel is not configured")
	// ErrInvalidRecipient means the PROVIDER told us this device token is dead
	// (notregistered/unregistered). Suppressing the recipient's other queued pushes is correct:
	// the device really is gone.
	ErrInvalidRecipient = errors.New("notification recipient is invalid")
	// ErrRecipientUnusable means the identifier was never a device token in the first place --
	// a role name, an empty string, a malformed ref. It says nothing about any device.
	//
	// It must NOT share ErrInvalidRecipient's suppression: recipient_ref is a role name on the
	// calendar reminder path, so one badly-addressed reminder would mark every queued push for
	// that role 'suppressed' tenant-wide, and 'suppressed' is not re-eligible for delivery. That
	// turns a recoverable addressing bug into permanent, silent, tenant-wide notification loss --
	// worse than the burnt retries it replaced.
	ErrRecipientUnusable = errors.New("notification recipient is not a device token")
	// ErrRecipientNoActiveDevice means the request is addressed at a known workforce member
	// (context member_id) but that member currently has no active, reachable device (no row in
	// workforce_member_devices with status='active', a non-null fcm_token, and
	// notifications_enabled != false). Distinct from ErrRecipientUnusable: the identifier was a
	// real, well-formed member reference, not a malformed ref or role name -- the gap is that
	// nothing can be delivered to them right now (never installed, signed out, mid-reinstall). It
	// must never be conflated with a successful send, and must never be reported using
	// ErrRecipientUnusable's "not a device token" wording, which would misdescribe a resolvable
	// addressing gap as a malformed identifier.
	ErrRecipientNoActiveDevice = errors.New("notification recipient has no active reachable device")
)

type ClaimParams struct {
	TenantID     string
	Limit        int
	MaxAttempts  int
	Now          time.Time
	LeaseTimeout time.Duration
}

type Repository interface {
	ReclaimStaleSending(ctx context.Context, tenantID string, now time.Time, leaseTimeout time.Duration) (int, error)
	ClaimDue(ctx context.Context, params ClaimParams) ([]domain.Request, error)
	MarkSent(ctx context.Context, tenantID, notificationRequestID, leaseToken, deliveredBy string, now time.Time) error
	MarkFailed(ctx context.Context, tenantID, notificationRequestID, leaseToken, deliveredBy, failureReason string, nextAttemptAt *time.Time, now time.Time) error
	// OldestDueRequestedAt returns the requested_at of the GLOBALLY oldest
	// currently-due, still-undelivered request (status queued/failed with
	// COALESCE(next_attempt_at, requested_at) <= now) — not just within a claimed
	// batch, so a newer batch cannot hide an hour-old request whose retry just
	// became due. Returns (_, false, nil) for an empty backlog. Bounded to the
	// due rows via the notification_requests_queue_idx range (future-scheduled
	// retries are excluded, never scanned). This is the ADR-defined backlog-age
	// signal the kernel worker's 1-minute fast-lane stage exports.
	OldestDueRequestedAt(ctx context.Context, tenantID string, now time.Time) (time.Time, bool, error)
}

type Gateway interface {
	Name() string
	Send(ctx context.Context, request domain.Request) error
}

// DeliveryResult is the provider acknowledgement for a successful send. ProviderMessageID is
// optional because local/webhook adapters do not always return one, while FCM does.
type DeliveryResult struct {
	ProviderMessageID string
}

// ResultGateway is implemented by gateways that surface provider acknowledgements. The dispatcher
// keeps the smaller Gateway contract for replaceable adapters and progressively uses this richer
// contract when available.
type ResultGateway interface {
	Gateway
	SendWithResult(ctx context.Context, request domain.Request) (DeliveryResult, error)
}

// ResultRepository persists a provider acknowledgement atomically with the sent transition and
// immutable delivery-attempt ledger row.
type ResultRepository interface {
	Repository
	MarkSentWithResult(
		ctx context.Context,
		tenantID, notificationRequestID, leaseToken, deliveredBy, providerMessageID string,
		now time.Time,
	) error
}

// InvalidRecipientRepository is implemented by repositories that can retire provider-rejected
// recipient references, such as FCM registration tokens that Firebase reports as NotRegistered.
type InvalidRecipientRepository interface {
	Repository
	SuppressInvalidRecipient(ctx context.Context, tenantID, recipientRef, reason string, now time.Time) (int, error)
}

// ExhaustedNotification is one row of the operator-facing exhausted list — the "what is dead
// right now" view an on-call reaches via cmd/notification-requeue -mode list. It carries enough
// context to judge relevance (target/calendar event) without a second query.
type ExhaustedNotification struct {
	NotificationRequestID string
	TenantID              string
	CalendarEventID       string
	TargetType            string
	TargetID              string
	NotificationType      string
	Channel               string
	RecipientRef          string
	FailureReason         string
	DeliveryAttempts      int
	RequeueCount          int
	RequestedAt           time.Time
	UpdatedAt             time.Time
}

// ExhaustedQuery scopes ListExhausted. TenantID is always required; the remaining fields narrow
// the result set the same way RequeueParams narrows a requeue, so an operator can preview exactly
// what a subsequent requeue call would touch.
type ExhaustedQuery struct {
	TenantID         string
	NotificationType string
	Since            time.Time
	Until            time.Time
	Limit            int
}

// RequeueParams scopes an operator-driven requeue of exhausted notifications. TenantID and
// NotificationType are both required — there is no "requeue everything for a tenant" mode and no
// "requeue this type across all tenants" mode, by design (see RequeueExhausted doc). Since/Until
// bound the requested_at window; NotificationRequestIDs, if non-empty, further restricts to that
// explicit id set (still intersected with tenant/type/window).
type RequeueParams struct {
	TenantID               string
	NotificationType       string
	Since                  time.Time
	Until                  time.Time
	NotificationRequestIDs []string
	RequeuedBy             string
	Now                    time.Time
}

// RequeueResult reports what a requeue call actually touched.
type RequeueResult struct {
	Requeued int
	Skipped  int
}

// RequeueRepository is implemented by repositories that can list and recover exhausted
// notifications once the underlying cause (e.g. a missing channel config) is fixed.
type RequeueRepository interface {
	Repository
	// ListExhausted is the visibility half: it never mutates, so it is always safe for an
	// on-call to run before deciding whether/what to requeue.
	ListExhausted(ctx context.Context, query ExhaustedQuery) ([]ExhaustedNotification, error)
	// RequeueExhausted is the recovery half. See the implementation's doc comment
	// (adapters/postgres/repository.go) for the exact scoping and idempotency/relevance rules.
	RequeueExhausted(ctx context.Context, params RequeueParams) (RequeueResult, error)
}
