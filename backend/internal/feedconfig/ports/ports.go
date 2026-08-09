// Package ports defines the feed-config repository boundary.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/feedconfig/domain"
)

var (
	// ErrIdempotencyConflict is returned when a client idempotency key is reused with a DIFFERENT
	// request payload. It is not a retry, it is two different edits claiming one identity, and
	// guessing which one the author meant would silently write the wrong ration.
	ErrIdempotencyConflict = errors.New("feedconfig: idempotency key reused with different payload")

	// ErrParkNotFound is returned when the addressed park does not resolve to a locations row in the
	// caller's tenant. Fails the write CLOSED: a rate authored against a park that does not exist is
	// configuration nobody will ever read, and a silent success would look to the author like the
	// grid was updated.
	ErrParkNotFound = errors.New("feedconfig: park not found")

	// ErrShedNotFound is the shed equivalent, for shed-factor writes.
	ErrShedNotFound = errors.New("feedconfig: shed not found")

	// ErrPartitionNotFound is returned when the addressed pen is not in the shed's own
	// shed_partitions catalog, or when a label is supplied for a shed that has no pens at all.
	//
	// Fails CLOSED for the same reason as the two above, one level finer: the write path used to
	// validate the park and the shed and then take the pen on trust, so a typo authored quantities
	// against a pen that exists nowhere. No operational location resolves to it, so the direction
	// generator never reads those rows -- the author sees a saved kg that will never be fed.
	ErrPartitionNotFound = errors.New("feedconfig: partition not found in shed")

	// ErrPartitionRequired is returned when a SUBDIVIDED shed is addressed without naming a pen.
	//
	// Distinct from ErrPartitionNotFound because the fix is different and the caller should say so:
	// this is a missing choice, not a wrong one. A subdivided shed has no whole-shed operational
	// location, so a blank label there is the caller failing to say which pen -- and it would
	// otherwise author a phantom 'whole' row sitting beside the real pens, invisible to every
	// screen that lists them.
	ErrPartitionRequired = errors.New("feedconfig: partition required for a subdivided shed")

	// ErrExperimentPenAlreadyConfigured prevents the enrollment endpoint from acting as a partial
	// update when a stale or concurrent form addresses a pen that has already been enrolled.
	ErrExperimentPenAlreadyConfigured = errors.New("feedconfig: experiment pen is already configured")

	// ErrExperimentPenNotConfigured keeps the single-cell editor from becoming a non-atomic first
	// enrollment path. A new pen must be enrolled with its complete item set through the batch write.
	ErrExperimentPenNotConfigured = errors.New("feedconfig: experiment pen is not configured")

	// ErrFeedItemExists is returned when a catalog entry with the same normalized label is already
	// present in the tenant.
	//
	// The add fails CLOSED rather than updating the existing row, and that is the point rather than
	// a missing feature: this is an ADD action, so silently rewriting an item's authored energy or
	// wastage values under it would change data the author never opened. It is also not a silent
	// success -- "Dry Masoor Bhusa" and "dry masoor bhusa " are ONE item by feed_config_norm, so
	// pretending the second one was created would leave the author believing the catalog holds two
	// entries while every rate keyed on the label resolves to one.
	ErrFeedItemExists = errors.New("feedconfig: feed item already exists in this tenant")

	// ErrFutureDatedRow is returned when the currently-open row takes effect AFTER the business date
	// this edit would apply on. Neither effective-dating branch is correct for it: closing that row
	// with today's date would violate valid_to > valid_from, and correcting it in place would rewrite
	// a future authoring using today's intent. So the write fails closed and a human decides.
	ErrFutureDatedRow = errors.New("feedconfig: currently-open row is future-dated")
)

// Repository is the feed-config persistence boundary.
//
// READS are bounded pages over authored config tables. WRITES are effective-dated upserts that
// carry the idempotency contract end to end: the key and the request fingerprint are persisted in
// the SAME transaction as the close/insert they describe, an exact replay returns the original
// result without re-running side effects, and a same-key/different-payload replay is
// ErrIdempotencyConflict.
type Repository interface {
	ListRationRates(ctx context.Context, q domain.RationRateQuery) (domain.RationRatePage, error)
	ListRationGroups(ctx context.Context, tenantID string, page domain.Page) (domain.RationGroupPage, error)
	ListShedTags(ctx context.Context, q domain.ShedTagQuery) (domain.ShedTagPage, error)
	ListFeedItems(ctx context.Context, tenantID string, page domain.Page) (domain.FeedItemPage, error)
	ListSessionTemplates(ctx context.Context, q domain.SessionTemplateQuery) (domain.SessionTemplatePage, error)
	ListScheduleConfig(ctx context.Context, q domain.ScheduleConfigQuery) (domain.ScheduleConfigPage, error)
	ListShedFactors(ctx context.Context, q domain.ShedFactorQuery) (domain.ShedFactorPage, error)
	ListExperimentConfig(ctx context.Context, q domain.ExperimentConfigQuery) (domain.ExperimentConfigPage, error)

	// ListPens returns the park's operational locations -- every shed, and every pen of a subdivided
	// shed -- each flagged with whether it already carries experiment configuration.
	//
	// It is the enroller's candidate source. The experiment table cannot be that source (it only
	// knows locations already enrolled) and the SHED list cannot either (one enrolled pen makes the
	// whole building look enrolled, which is how Godel 1 - Part 8 became unreachable from the UI).
	// Reads the locations / shed_partitions catalog and NO per-animal table, so a pen holding zero
	// animals is still listed -- that is usually the pen about to be filled and configured.
	ListPens(ctx context.Context, q domain.PenQuery) (domain.PenPage, error)

	// UpsertRationRate authors one grid cell, effective-dated and never destructive:
	//
	//	no open row                     -> INSERT              (outcome 'inserted')
	//	open row, same value            -> no write            (outcome 'unchanged')
	//	open row from an EARLIER day     -> close it + INSERT   (outcome 'superseded')
	//	open row authored TODAY          -> correct in place    (outcome 'corrected')
	//
	// The same-day correction is not a shortcut: a window closed on the day it opened would violate
	// the schema's valid_to > valid_from, and a same-day re-author is a correction of today's
	// authoring rather than a historical change worth its own window. This is exactly the three-way
	// reconciliation seed-feed-ration performs, so the UI and the seed cannot disagree.
	UpsertRationRate(ctx context.Context, cmd domain.UpsertRationRateCommand) (domain.WriteResult, error)

	// UpsertShedFactor authors one shed multiplier on the same effective-dated terms.
	UpsertShedFactor(ctx context.Context, cmd domain.UpsertShedFactorCommand) (domain.WriteResult, error)

	// CreateFeedItem adds one entry to the tenant's feed-item catalog.
	//
	// CREATE, NOT UPSERT, and the vocabulary reflects it -- the only success outcome is 'inserted'.
	// A duplicate normalized label is ErrFeedItemExists rather than a correction of the existing
	// row: the catalog is not effective-dated, so an "update" here would overwrite authored
	// attributes in place on a screen whose control says Add.
	//
	// Adding an item authors NO quantity. The new label is selectable on the ration grid, the shed
	// factors and the experiment sheds from the moment it exists, and every one of those
	// combinations stays UNCONFIGURED -- and therefore blocking -- until someone authors it. This
	// write must never create a rate to go with the item, not even 0.
	CreateFeedItem(ctx context.Context, cmd domain.CreateFeedItemCommand) (domain.WriteResult, error)

	// UpsertScheduleConfig authors one park/workflow dispatch clock on the same effective-dated
	// terms. All three times are stored as LOCAL Asia/Kolkata wall-clock values with no offset.
	UpsertScheduleConfig(ctx context.Context, cmd domain.UpsertScheduleConfigCommand) (domain.WriteResult, error)

	// UpsertExperimentConfig authors one experiment shed's ABSOLUTE kg of one feed item.
	//
	// Outcomes are a two-way reconciliation, not the three-way one above, because
	// feed_experiment_config is NOT effective-dated (see migration 000006):
	//
	//	no row               -> INSERT           (outcome 'inserted')
	//	row, same values     -> no write         (outcome 'unchanged')
	//	row, different values -> UPDATE in place (outcome 'corrected')
	//
	// There is no 'superseded' outcome here, and its absence is the schema speaking rather than an
	// omission: an experiment quantity is a hand-entered figure for a running trial that is corrected
	// while the trial runs, not a standing rule whose past values must stay reconstructable to
	// explain an old feed sheet. The write ledger still records who changed what and when.
	// The pen must already be enrolled; its first complete set is created only by the batch method.
	UpsertExperimentConfig(ctx context.Context, cmd domain.UpsertExperimentConfigCommand) (domain.WriteResult, error)

	// UpsertExperimentConfigBatch ENROLLS every feed item of one unconfigured pen atomically.
	//
	// It is not a convenience wrapper around N single-cell writes. A pen's authored cells are the
	// COMPLETE list of what it is fed -- the planner does not fall back to the ration grid for a
	// missing item -- so a partly-applied enrolment silently underfeeds live animals on a sheet that
	// looks complete. The whole set commits or none of it does. An already-configured pen returns
	// ErrExperimentPenAlreadyConfigured and must be changed through explicit cell edits.
	UpsertExperimentConfigBatch(ctx context.Context, cmd domain.UpsertExperimentConfigBatchCommand) (domain.WriteResult, error)

	// SetExperimentShedStatus switches ONE PEN between the experiment workflow and the normal
	// per-head ration grid, by flipping every authored cell of that pen in one statement.
	//
	// This is a change to WHAT ANIMALS ARE FED, not a visibility toggle: the direction path reads
	// only active rows, and a shed with none falls through to NormalPlanner and is fed
	// head_count x grams_per_head x shed_factor. It is complete-pen because a half-enrolled pen has
	// no representable feed; sibling pens in the same shed remain independent.
	//
	// ErrShedNotFound when the shed has no rows at all: there is no experiment configuration to
	// switch, and creating empty rows to carry a status would author cells nobody entered.
	SetExperimentShedStatus(ctx context.Context, cmd domain.SetExperimentShedStatusCommand) (domain.WriteResult, error)
}
