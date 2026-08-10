package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthfeed/domain"
)

var (
	// ErrForbidden is returned when the caller holds neither of the capabilities
	// this read is offered to.
	ErrForbidden = errors.New("growthfeed: forbidden")
	// ErrInvalidArgument covers a malformed park id or business date.
	ErrInvalidArgument = errors.New("growthfeed: invalid argument")
	// ErrNotFound is the deliberate answer for a caller whose scope reaches no park
	// here, and for a park id outside that scope. The two are the same response on
	// purpose: distinguishing them would confirm the existence of a park the caller
	// may not see.
	ErrNotFound = errors.New("growthfeed: not found")
)

// PenGrowth is one pen's growth facts, read from weighing tables alone.
type PenGrowth struct {
	ParkID           string
	ParkName         string
	LocationID       string
	ShedName         string
	PartitionLabel   *string
	WeighingCategory string
	AnimalsWeighed   int
	AverageWeightKg  *float64
	ADGGPerDay       *float64
	ADGBasis         string
	ADGSampleCount   int
	ADGSpanDays      *int
}

// PenCohortRation is one pen's herd cohort and its authored ration, read from
// goats and feed config. Keyed by LocationID, which is the pen's own location row
// (a partition has its own row; an undivided shed is its own pen).
type PenCohortRation struct {
	LocationID string
	// Breed/Sex/Stage are empty when the pen does not agree on one value.
	Breed       string
	Sex         string
	Stage       string
	LiveAnimals int

	RationGroupLabel string
	ShedTagLabel     string
	Plan             domain.FeedPlanInput
}

// Repository is the read side. There is no write side: this module owns no table
// and mutates nothing.
type Repository interface {
	// ListPenGrowth returns one row per (park, location, partition) weighing bucket
	// in scope, over the half-open window [periodStart, periodEnd).
	ListPenGrowth(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) ([]PenGrowth, error)
	// ListPenCohortRation resolves the cohort and ration for the given pens as of
	// asOfDate (an Asia/Kolkata business date, YYYY-MM-DD): the ration in force at
	// the END of the weighing window, which is the one whose effect the gain
	// figures are being read against.
	ListPenCohortRation(ctx context.Context, tenantID string, parkIDs, locationIDs []string, asOfDate string) ([]PenCohortRation, error)
	// ListParkIDs lists every park in the tenant, for a tenant-wide caller.
	ListParkIDs(ctx context.Context, tenantID string) ([]string, error)
}
