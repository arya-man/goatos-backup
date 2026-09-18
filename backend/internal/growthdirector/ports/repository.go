// Package ports declares the Growth Director module's outbound interfaces and
// error taxonomy, mirroring the weighing module's shape.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
)

var (
	ErrForbidden       = errors.New("growthdirector: forbidden")
	ErrInvalidArgument = errors.New("growthdirector: invalid argument")
	ErrNotFound        = errors.New("growthdirector: not found")
)

// Repository is the read-only data access the Growth Director service needs.
type Repository interface {
	// ListParks returns all active parks for a tenant, for resolving a
	// tenant-wide monitor's "all parks" scope.
	ListParks(ctx context.Context, tenantID string) ([]domain.Park, error)

	// GetGrowthDirectorWeights builds all six widgets for one half-open window
	// [periodStart, periodEnd). parkIDs must be non-empty and every id must
	// already be authorization-checked by the caller: this method does no
	// scoping of its own.
	//
	// Data rules it enforces (see domain docs):
	//   - identity = lower(btrim(scanned_identifier)); blank tags excluded
	//   - weighing_observations.animal_id / mismatch_status are NEVER referenced
	//     (both columns dropped)
	//   - tag -> goat via goat_identifiers.normalized_value = upper(tag_key)
	//     (normalized_value is UPPER; lifetime-unique per tenant, 0..1)
	//   - weighing_shed_observations reads filter withdrawn_at IS NULL
	//   - verification_status='rework' excluded from growth math, included in trust
	//   - feed quantity_kg NULL = blocked and is never COALESCEd to 0
	GetGrowthDirectorWeights(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory, sections string) (domain.GrowthDirectorWeights, error)

	// GetFeedWeightBandSource reads the LATEST locked/amended feed direction per
	// park and workflow, rolled up per pen and cohort, each rollup carrying the
	// pen's weight evidence at the General tab's own grain (0 rows for an unweighed
	// pen, 1 for a pen-average pen, 1 per band for a per-animal pen), plus the
	// animals sold or dead inside the window. The window bounds the weighing side
	// only. Same scoping rule as above: parkIDs are already authorized.
	GetFeedWeightBandSource(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string, includeExited bool) (FeedWeightBandSource, error)
}

// FeedWeightBandSource is the repository's raw answer for the feed-by-weight-band
// read: sheet-stage counts plus every rollup with its evidence rows. The service
// derives display values, applies the sex filter and builds the reconciliation.
type FeedWeightBandSource struct {
	FeedDay        string
	PositiveRows   int
	CollapsedItems int
	// IndividualAnimalsWeighed and LumpSumAnimalsWeighed are the weighing side's own
	// totals BEFORE any feed match, at the General tab's grain (weighed-twice animals
	// including any that have since exited; lump pens weighed on two dates, latest head
	// count), so the table can be reconciled to that tab's Individual / Lump sum figures.
	IndividualAnimalsWeighed int
	LumpSumAnimalsWeighed    int
	Rollups                  []FeedRollup
	// Exited are the animals sold or dead inside the window, newest exit first.
	Exited []FeedExitedAnimal
}

// FeedExitedAnimal is one animal that left the farm inside the window, with its last weigh in
// the window when it has one (LastWeighedAt nil otherwise).
type FeedExitedAnimal struct {
	GoatID          string
	ParkID          string
	Tag             string
	Pen             string
	ExitReason      string
	LifecycleStatus string
	// Sex is the register's sex ("male" / "female"), empty when unknown.
	Sex           string
	ExitedAt      time.Time
	LastWeighedAt *time.Time
	LastWeightKg  float64
}

// FeedRollup is one (park, pen, shed tag, ration group, arm, breed, workflow)
// rollup off the latest sheet. Items are the collapsed feed items in label
// order; Evidence is empty when the pen has no weigh on record.
type FeedRollup struct {
	ParkID        string
	ParkName      string
	Pen           string
	ShedTag       string
	RationGroup   string
	ExperimentArm string
	Breed         string
	Workflow      string
	KgPerDay      float64
	Items         []FeedRollupItem
	Evidence      []FeedWeightEvidence
}

type FeedRollupItem struct {
	Label        string
	GramsPerHead float64
}

// FeedWeightEvidence is one weight fact for a pen: the pen average (one row) or
// one per-animal band (one row per band). FemaleCount / MaleCount are the sexes
// of the animals behind it, read from the herd register: the scanned tags in
// the band for a per-animal row, the goats placed in the pen for a pen-average
// row. Both zero when no animal resolves.
type FeedWeightEvidence struct {
	Source          string
	Band            string
	Animals         int
	AverageWeightKg float64
	FemaleCount     int
	MaleCount       int
	// ExitedAnimals is how many of the weighed animals behind a per-animal band have since
	// exited (sold / dead). They are outside Animals and AverageWeightKg unless the caller
	// asked to include them. Always 0 on a pen-average row.
	ExitedAnimals int
	// ExitedSold is the part of ExitedAnimals that was sold (lifecycle sold / exit reason
	// sold); the rest died or otherwise left.
	ExitedSold int
}
