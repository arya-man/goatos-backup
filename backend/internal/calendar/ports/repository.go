// Package ports defines Calendar application dependencies.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
)

var (
	ErrNotFound              = errors.New("calendar: not found")
	ErrForbidden             = errors.New("calendar: forbidden")
	ErrIdempotencyConflict   = errors.New("calendar: idempotency conflict")
	ErrIdempotencyInProgress = errors.New("calendar: idempotency request in progress")
	ErrActiveSnoozeExists    = errors.New("calendar: active snooze exists")
	ErrEventNotActionable    = errors.New("calendar: event is not actionable")
	ErrInvalidReference      = errors.New("calendar: invalid reference")
)

type Repository interface {
	ListEvents(ctx context.Context, q domain.Query) (domain.CalendarEventListResponse, error)
	GetEventDetail(ctx context.Context, q domain.EventQuery) (domain.CalendarEventDetail, error)
	ListDriveTargets(ctx context.Context, q domain.DriveTargetQuery) (domain.CalendarDriveTargetListResponse, error)
	History(ctx context.Context, q domain.HistoryQuery) (domain.CalendarHistoryResponse, error)
	SendNudge(ctx context.Context, in SendNudge) (domain.CalendarActionResponse, error)
	Snooze(ctx context.Context, in Snooze) (domain.CalendarActionResponse, error)
	AcknowledgeEscalation(ctx context.Context, in AcknowledgeEscalation) (domain.CalendarActionResponse, error)
	ResolveEscalation(ctx context.Context, in ResolveEscalation) (domain.CalendarActionResponse, error)
	SweepDueReminders(ctx context.Context, tenantID string, limit int) (int, error)
	SweepEscalations(ctx context.Context, in SweepEscalations) (int, error)
	RefreshVaccinationProjection(ctx context.Context, in RefreshVaccinationProjection) (int, error)
	PruneClosedVaccinationProjection(ctx context.Context, tenantID string, cutoff time.Time, limit int) (int, error)
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
	Scope          domain.ScopeFilter
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
	Scope           domain.ScopeFilter
}

type AcknowledgeEscalation struct {
	TenantID       string
	EventID        string
	ActorID        string
	TraceID        string
	IdempotencyKey string
	Reason         string
	Scope          domain.ScopeFilter
	ActorGrants    []ActorGrant
}

type ResolveEscalation struct {
	TenantID       string
	EventID        string
	ActorID        string
	TraceID        string
	IdempotencyKey string
	Reason         string
	Scope          domain.ScopeFilter
	ActorGrants    []ActorGrant
}

type ActorGrant struct {
	Role      string
	ScopeType string
	ScopeID   string
}

type RefreshVaccinationProjection struct {
	TenantID string
	DateFrom time.Time
	DateTo   time.Time
	Limit    int
}

type SweepEscalations struct {
	TenantID     string
	ObligationID string
	Limit        int
	Now          time.Time
	Level1After  time.Duration
	Level2After  time.Duration
	Level3After  time.Duration
	Level4After  time.Duration
}
