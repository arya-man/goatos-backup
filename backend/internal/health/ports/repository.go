package ports

import (
	"context"
	"errors"
	"github.com/vgoats/goatos/backend/internal/health/domain"
)

var (
	ErrNotFound              = errors.New("health: not found")
	ErrConflict              = errors.New("health: idempotency conflict")
	ErrGoatNotAlive          = errors.New("health: goat is not alive")
	ErrProtocolNotPublished  = errors.New("health: disease protocol is not published")
	ErrAgeBandMismatch       = errors.New("health: requested age band does not match goat")
	ErrCriticalActionGuarded = errors.New("health: critical action requires policy-pack handoff")
)

type Repository interface {
	OpenCase(context.Context, domain.OpenCaseInput) (domain.OpenCaseResult, error)
	ListWorkItems(context.Context, domain.ListFilter) (domain.WorkItemPage, error)
	GetWorkItem(context.Context, string, string) (domain.WorkItemDetail, error)
	CompleteWorkItem(context.Context, domain.CompleteInput) (domain.CompleteResult, error)
	HoldForDeathReview(context.Context, string, string) error
	ResumeAfterDeathRejected(context.Context, string, string) error
	CloseForApprovedDeath(context.Context, string, string) error
}
type ProtocolImporter interface {
	ReplacePublishedProtocols(context.Context, string, string, string, string, []domain.SourceProtocol) error
}
