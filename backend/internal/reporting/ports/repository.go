package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/reporting/domain"
)

var ErrNotFound = errors.New("reporting record not found")

type CountParams struct {
	Grain              string
	TenantID           string
	CustodianPartyID   *string
	FarmID             *string
	ParkID             *string
	ShedID             *string
	CohortID           *string
	LifecycleStatus    *string
	ReproductiveStatus *string
	GrowthCohortTag    *string
	ManagementStage    *string
	HealthStatus       *string
	IdentityState      *string
	BreedID            *string
	Sex                *string
}

type RebuildIdentityCountersParams struct {
	TenantID          string
	Grains            []string
	SourceImportRunID *string
}

type Repository interface {
	ListIdentityCounts(ctx context.Context, params CountParams) ([]domain.IdentityCount, domain.Freshness, error)
	RebuildIdentityCounters(ctx context.Context, params RebuildIdentityCountersParams) (*domain.IdentityCounterRebuildResult, error)
	Ping(ctx context.Context) error
}
