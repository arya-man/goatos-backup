// Package ports defines Calendar application dependencies.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
)

var (
	ErrNotFound            = errors.New("calendar: not found")
	ErrIdempotencyConflict = errors.New("calendar: idempotency conflict")
	ErrActiveSnoozeExists  = errors.New("calendar: active snooze exists")
	ErrEventNotActionable  = errors.New("calendar: event is not actionable")
	ErrInvalidReference    = errors.New("calendar: invalid reference")
)

type Repository interface {
	ListEvents(ctx context.Context, q domain.Query) (domain.CalendarEventListResponse, error)
	GetEventDetail(ctx context.Context, tenantID, eventID string) (domain.CalendarEventDetail, error)
	History(ctx context.Context, q domain.HistoryQuery) (domain.CalendarHistoryResponse, error)
	SendNudge(ctx context.Context, in SendNudge) (domain.CalendarActionResponse, error)
	Snooze(ctx context.Context, in Snooze) (domain.CalendarActionResponse, error)
	SweepDueReminders(ctx context.Context, tenantID string, limit int) (int, error)
}

type SendNudge struct {
	TenantID       string
	EventID        string
	ActorID        string
	TraceID        string
	IdempotencyKey string
	Channel        string
	Message        string
	Reason         string
}

type Snooze struct {
	TenantID        string
	EventID         string
	ActorID         string
	TraceID         string
	IdempotencyKey  string
	SnoozeUntil     time.Time
	Reason          string
	ReplaceExisting bool
}
