// Package domain holds the in-app notification centre's own vocabulary: one person's
// notification row, the page around it, and the limits the transport must respect.
//
// WHOSE NOTIFICATIONS THESE ARE (the module's whole reason to exist).
//
// notification_requests is a DELIVERY queue, not a per-person inbox, and its
// `recipient_ref` column is NOT an identity. QueueRoleNotifications writes the recipient
// DEVICE's FCM token into it (calendar/adapters/postgres/repository.go:1778), and the
// escalation sweeper writes a ROLE SLUG into it (same file, :1526). The notification
// repository documents at length that the column is a frozen snapshot that goes stale
// across a token rotation and must never be trusted as an address
// (notification/adapters/postgres/repository.go:320-352). Scoping an in-app feed on
// `recipient_ref` would therefore be both wrong (a role slug is shared by many people) and
// unstable (a reinstalled phone loses its own history).
//
// The authoritative per-person address every producer stamps is
// `context->>'member_id'`: the canonical workforce_member_id the consumer resolved BEFORE
// writing the row. That is the contract the four per-member alert indexes are built on
// (notification_requests_{weighing,vaccination,feed,counts}_alerts_idx) and the predicate
// every existing per-person alerts feed already filters by (weighing, vaccination, feed
// direction and counts adapters/postgres/alerts.go). This module reads the SAME field, for
// EVERY message key, and adds nothing to it.
package domain

import "errors"

// DefaultLimit and MaxLimit bound one page. The contract is limit=1..50, default 20.
const (
	DefaultLimit = 20
	MaxLimit     = 50
)

// Errors the transport maps onto stable HTTP codes.
var (
	// ErrInvalidLimit is a limit outside 1..50.
	ErrInvalidLimit = errors.New("notificationcentre: invalid limit")
	// ErrInvalidCursor is a cursor that is not a cursor this feed minted.
	ErrInvalidCursor = errors.New("notificationcentre: invalid cursor")
	// ErrNoIDs is a mark-as-read with an empty id list.
	ErrNoIDs = errors.New("notificationcentre: no notification ids")
	// ErrTooManyIDs is a mark-as-read over the batch cap.
	ErrTooManyIDs = errors.New("notificationcentre: too many notification ids")
	// ErrInvalidID is a mark-as-read id that is not a uuid.
	ErrInvalidID = errors.New("notificationcentre: invalid notification id")
	// ErrIdempotencyKeyRequired is a mark-as-read with no Idempotency-Key header.
	ErrIdempotencyKeyRequired = errors.New("notificationcentre: idempotency key required")
)

// MaxMarkReadIDs caps one mark-as-read batch. A page is at most 50 rows, and the UI marks
// at most a page (plus a little slack for a "mark all visible" over a stale page), so this
// is generous while still keeping the id array bounded and the statement planner-friendly.
const MaxMarkReadIDs = 200

// ClampLimit validates the requested page size. 0 (absent) means the default; anything
// outside 1..50 is refused rather than silently clamped, because a caller who asked for
// 200 rows and got 20 would page past rows it never saw.
func ClampLimit(requested int) (int, error) {
	switch {
	case requested == 0:
		return DefaultLimit, nil
	case requested < 1 || requested > MaxLimit:
		return 0, ErrInvalidLimit
	default:
		return requested, nil
	}
}

// Context is the routing envelope the producing consumer stamped on the row. Every field is
// a jsonb text field read with ->>, so every field here is a string; absent fields stay
// empty and the transport omits them.
type Context struct {
	TaskID     string
	TaskNo     string
	Screen     string
	GroupKey   string
	Priority   string
	MessageKey string
	Target     string
	Status     string
}

// Notification is one row of the caller's own feed.
type Notification struct {
	NotificationRequestID string
	NotificationType      string
	Title                 string
	Body                  string
	Status                string
	RequestedAt           string
	// ReadAt is empty until the caller marks the notification read.
	ReadAt string
	// ActorName is the display name of the person whose action raised this notification,
	// when the producer recorded one. Empty for system-raised rows (the sweepers).
	ActorName string
	Context   Context
}

// Page is one keyset page plus the caller's WHOLE-FEED unread total.
type Page struct {
	Items []Notification
	// UnreadCount is the caller's unread total across their entire feed, never the page's
	// own unread rows: the bell badge must not shrink just because the reader paged.
	UnreadCount int
	// NextCursor is empty on the last page.
	NextCursor string
}
