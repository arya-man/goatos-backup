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

	// GetFCR builds the Weighing FCR tab for one half-open window: feed directed to each pen
	// between its consecutive weighing rounds against the gain those rounds measured, rolled up
	// by breed, sex, weight band, park, origin and week. parkIDs must be non-empty and already
	// authorization-checked. sex/origin are applied at PEN grain (agree-or-neither), because feed
	// is directed to a whole pen and cannot be split between two cohorts.
	GetFCR(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string) (domain.FCRReport, error)

	// GetSalePrices returns the newest assumed live-weight sale price per species effective on or
	// before asOf (a business date). Maintainer-edited data, never a constant.
	GetSalePrices(ctx context.Context, tenantID string, asOf time.Time) (domain.SalePrices, error)

	// GetAssumptions returns the sale prices effective on asOf plus every keyed figure in
	// growth_assumptions (maintainer decision 2026-09-19). A tenant with no row for a key gets no
	// entry; consumers fall back to the figure the old constant carried.
	GetAssumptions(ctx context.Context, tenantID string, asOf time.Time) (domain.Assumptions, error)

	// GrowthSettings resolves the figures the reads are judged against (band edges, slow-growth
	// target, bad-scan cut-off, default period, sale lines), defaulted when the tenant has no row.
	GrowthSettings(ctx context.Context, tenantID string) (domain.GrowthSettings, error)

	// PutAssumptions applies a validated update in ONE transaction: a sale price is appended as
	// the row effective on asOf (a same-day re-set overwrites that day's row, earlier days keep
	// theirs), and a keyed figure is updated in place under its row_version fence.
	// ErrAssumptionConflict when a fence does not match. Idempotent: replaying the same update
	// re-lands the same rows.
	PutAssumptions(ctx context.Context, tenantID, setBy string, asOf time.Time, update domain.AssumptionsUpdate) (domain.Assumptions, error)

	// PutAssumptions applies a validated update in ONE transaction: a sale price is appended as
	// the row effective on asOf (a same-day re-set overwrites that day's row, earlier days keep
	// theirs), and a keyed figure is updated in place under its row_version fence.

	// GetFeedWeightBandSource reads the LATEST locked/amended feed direction per
	// park and workflow, rolled up per pen and cohort, each rollup carrying the
	// pen's weight evidence at the General tab's own grain (0 rows for an unweighed
	// pen, 1 for a pen-average pen, 1 per band for a per-animal pen), plus the
	// animals sold or dead inside the window. The window bounds the weighing side
	// only. Same scoping rule as above: parkIDs are already authorized.
	GetFeedWeightBandSource(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string, bandEdgesKg []float64) (FeedWeightBandSource, error)
}

// FeedWeightBandSource is the repository's raw answer for the feed-by-weight-band
// read: sheet-stage counts plus every rollup with its evidence rows. The service
// derives display values, applies the sex filter and builds the reconciliation.
type FeedWeightBandSource struct {
	// FeedDay is the newest sheet day among the selected parks; each rollup carries its own
	// park + workflow sheet day (FeedRollup.FeedDay), which may be older.
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
	// FeedDay is the sheet day (YYYY-MM-DD) of the latest locked/amended issue for this
	// rollup's park and workflow.
	FeedDay       string
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
	// AnimalsAll / AverageWeightKgAll / *CountAll are the same figures counting every weighed
	// animal including those since sold or dead; equal to the on-farm figures on a pen-average
	// row.
	AnimalsAll         int
	AverageWeightKgAll float64
	FemaleCountAll     int
	MaleCountAll       int
	// ExitedAnimals is how many of the weighed animals behind a per-animal band have since
	// exited (goats.exited_at set, whatever the reason). Outside Animals and AverageWeightKg,
	// inside AnimalsAll. Always 0 on a pen-average row.
	ExitedAnimals int
	// ExitedSold and ExitedDied are the sold and died buckets of ExitedAnimals
	// (domain.FeedExitBucket); the remainder is "other" (inactive, transferred, lost...).
	ExitedSold int
	ExitedDied int
}

// ErrAssumptionConflict is returned when a keyed figure was changed by someone else since the
// caller loaded it; the caller reloads and decides again.
var ErrAssumptionConflict = errors.New("growthdirector: assumption row_version conflict")
