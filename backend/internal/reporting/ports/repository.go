package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/reporting/domain"
)

var (
	ErrNotFound      = errors.New("reporting record not found")
	ErrInvalidFilter = errors.New("invalid reporting filter")
	ErrInvalidCursor = errors.New("invalid reporting cursor")
)

const MaxIdentityCountsLimit = 500

type CountParams struct {
	Grain              string
	TenantID           string
	Limit              int
	Cursor             *string
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

type CountPage struct {
	Items      []domain.IdentityCount
	NextCursor *string
	HasMore    bool
	Freshness  domain.Freshness
}

type RebuildIdentityCountersParams struct {
	TenantID          string
	Grains            []string
	SourceImportRunID *string
}

type Repository interface {
	ListIdentityCounts(ctx context.Context, params CountParams) (*CountPage, error)
	RebuildIdentityCounters(ctx context.Context, params RebuildIdentityCountersParams) (*domain.IdentityCounterRebuildResult, error)
	Ping(ctx context.Context) error
}
