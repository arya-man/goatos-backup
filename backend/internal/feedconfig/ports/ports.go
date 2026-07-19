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
	UpsertExperimentConfig(ctx context.Context, cmd domain.UpsertExperimentConfigCommand) (domain.WriteResult, error)

	// SetExperimentShedStatus switches a WHOLE SHED between the experiment workflow and the normal
	// per-head ration grid, by flipping the status of every one of that shed's rows in one statement.
	//
	// This is a change to WHAT ANIMALS ARE FED, not a visibility toggle: the direction path reads
	// only active rows, and a shed with none falls through to NormalPlanner and is fed
	// head_count x grams_per_head x shed_factor. It is whole-shed because a planner owns a shed, not
	// a cell -- a half-enrolled shed has no representable feed.
	//
	// ErrShedNotFound when the shed has no rows at all: there is no experiment configuration to
	// switch, and creating empty rows to carry a status would author cells nobody entered.
	SetExperimentShedStatus(ctx context.Context, cmd domain.SetExperimentShedStatusCommand) (domain.WriteResult, error)
}
