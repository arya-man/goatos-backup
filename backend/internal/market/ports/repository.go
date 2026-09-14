// Package ports declares the market survey's storage contract.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/market/domain"
)

var (
	// ErrNotFound is a city or question id the tenant does not carry.
	ErrNotFound = errors.New("market: not found")
	// ErrDuplicateName is a second active city or question with the same name.
	ErrDuplicateName = errors.New("market: duplicate name")
	// ErrCityRetired is a day entry for a city that is not phoned any more.
	ErrCityRetired = errors.New("market: city retired")
	// ErrIdempotencyConflict is a replay of a used key with a different payload.
	ErrIdempotencyConflict = errors.New("market: idempotency key reused with a different payload")
	// ErrIdempotencyKeyRequired is a mutating call with no key.
	ErrIdempotencyKeyRequired = errors.New("market: idempotency key required")
	// ErrTooMany is a config that has reached its bound.
	ErrTooMany = errors.New("market: too many")
)

// RecordDayEntryParams is one city's answers for one business day.
type RecordDayEntryParams struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	Write          domain.DayEntryWrite
}

// Repository is the market survey's storage.
type Repository interface {
	// GetConfig reads every city and question, active and retired, in shown order, plus the
	// configured call time (blank when no row is stored).
	GetConfig(ctx context.Context, tenantID string) (domain.Config, error)
	// SetCallTime stores the "HH:MM" local time the day's calls open.
	SetCallTime(ctx context.Context, tenantID, actorID, callTime string) error
	CreateCity(ctx context.Context, tenantID, actorID, idempotencyKey string, write domain.CityWrite) (domain.City, error)
	UpdateCity(ctx context.Context, tenantID, actorID, cityID string, write domain.CityWrite) (domain.City, error)
	CreateQuestion(ctx context.Context, tenantID, actorID, idempotencyKey string, write domain.QuestionWrite) (domain.Question, error)
	UpdateQuestion(ctx context.Context, tenantID, actorID, questionID string, write domain.QuestionWrite) (domain.Question, error)

	// ListDayEntries reads every entry for one business day.
	ListDayEntries(ctx context.Context, tenantID, businessDate string) ([]domain.Entry, error)
	// RecordDayEntry upserts a city's answers for the day, snapshotting the live city name and
	// each question's label and unit inside the same transaction. Idempotent on the key.
	RecordDayEntry(ctx context.Context, p RecordDayEntryParams) ([]domain.Entry, error)
	// ListEntriesBetween reads the analytics window, oldest first.
	ListEntriesBetween(ctx context.Context, tenantID, from, to string) ([]domain.Entry, error)

	// ReporterUserIDs lists the users holding an active market_reporter grant -- the morning
	// push's audience. Per person by construction: it reads the grant, never a job.
	ReporterUserIDs(ctx context.Context, tenantID string) ([]string, error)
}
