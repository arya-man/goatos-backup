package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

var (
	ErrForbidden           = errors.New("weighing: forbidden")
	ErrInvalidArgument     = errors.New("weighing: invalid argument")
	ErrNotFound            = errors.New("weighing: not found")
	ErrIdempotencyConflict = errors.New("weighing: idempotency conflict")
	ErrImmutable           = errors.New("weighing: immutable")
)

type Repository interface {
	CreateCampaign(ctx context.Context, cmd domain.CreateCampaign) (domain.Campaign, error)
	PublishCampaign(ctx context.Context, tenantID, campaignID, actorID, idempotencyKey string) (domain.Campaign, error)
	ListCampaigns(ctx context.Context, tenantID string) ([]domain.Campaign, error)
	ListScopeRoster(ctx context.Context, tenantID, campaignID, campaignShedID string, limit int) ([]domain.ExpectedAnimal, error)
	RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error)
	RecordShedObservation(ctx context.Context, cmd domain.RecordShedObservation) (domain.Observation, error)
	RefreshAvailability(ctx context.Context, tenantID, campaignID string) error
}
