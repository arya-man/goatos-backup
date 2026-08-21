// Package ports declares the protocol domain's repository boundary.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// ErrNotFound is returned when a requested protocol row does not exist.
var ErrNotFound = errors.New("protocol: not found")

// ErrDraftAlreadyExists is returned when creating a draft would give a protocol scope a
// SECOND one. A plan being worked on is a single thing, so the caller should open the
// existing draft rather than making another. Enforced by a partial unique index, which is
// what makes it safe against two callers racing past a read-then-write check.
var ErrDraftAlreadyExists = errors.New("protocol: a draft already exists for this scope")

// ErrVersionNotDraft is returned when a caller tries to mutate or publish a
// protocol version that is no longer draft. Published config is immutable.
var ErrVersionNotDraft = errors.New("protocol: version is not draft")

// ErrIdempotencyConflict is returned when a caller reuses an idempotency key
// with a different semantic protocol-config payload.
var ErrIdempotencyConflict = errors.New("protocol: idempotency key reused with different payload")

// ErrActiveVersionOverlap is returned when a publish would create two active
// versions for the same logical ruleset family, scope, and effective window.
var ErrActiveVersionOverlap = errors.New("protocol: active version overlaps existing published version")

// ErrCapacityParityMismatch is returned by an atomic publish when the versioned rule_dsl.capacity does
// not equal the row the same transaction wrote into vaccination_capacity_config. The publish transaction
// is rolled back (the version stays draft); the app layer maps this to ErrNotPublishable.
var ErrCapacityParityMismatch = errors.New("protocol: capacity parity mismatch after publish")

// ErrVaccinationMatrixOwnershipConflict is returned when a seed-owned vaccination matrix publish
// targets, or would retire, a version that is not provably seed-owned (drafted_by != the seed actor,
// including an unstamped NULL author). The publish fails closed and mutates nothing.
var ErrVaccinationMatrixOwnershipConflict = errors.New("protocol: seed vaccination matrix publish would touch a non-seed-owned version")

// Repository is the persistence boundary for protocol config. Implementations wrap generated
// sqlc queries; no hand-written SQL leaks above this interface.
type Repository interface {
	Ping(ctx context.Context) error

	CreateDefinition(ctx context.Context, in domain.NewDefinition) (protocolID string, err error)
	GetDefinitionByCode(ctx context.Context, tenantID, code string) (domain.Definition, error)

	CreateVersion(ctx context.Context, in domain.NewVersion) (versionID string, err error)
	GetVersion(ctx context.Context, tenantID, versionID string) (domain.Version, error)
	ListPublishedVersions(ctx context.Context, tenantID, protocolID string) ([]domain.Version, error)
	// ListConfigs returns every version (draft/published/retired) of every protocol in a category
	// for a tenant, newest first, for the generic Config authority screen.
	ListConfigs(ctx context.Context, tenantID, category string) ([]domain.ConfigListItem, error)
	// PublishVersion flips a draft version to published. Executable-contract checks are enforced
	// by the app layer before calling this.
	PublishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string, idempotencyKey ...string) error
	// DiscardVersion permanently deletes a DRAFT version and its rules. Implementations
	// must refuse anything that is not a draft: a published or retired version is part of
	// the tenant's history and no caller may remove it.
	DiscardVersion(ctx context.Context, tenantID, versionID string) error

	CreateRule(ctx context.Context, in domain.NewRule) (ruleID string, err error)
	ListRules(ctx context.Context, tenantID, versionID string) ([]domain.Rule, error)

	// ListActiveAnimalStages returns the tenant's active animal_stage_lookup rows (display order)
	// so Config authoring picks stage bands from reference data, not hardcoded frontend literals.
	ListActiveAnimalStages(ctx context.Context, tenantID string) ([]domain.AnimalStage, error)

	CreateTrigger(ctx context.Context, in domain.NewTrigger) (triggerID string, err error)
}
