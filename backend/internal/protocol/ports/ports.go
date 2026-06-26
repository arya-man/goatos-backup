// Package ports declares the protocol domain's repository boundary.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// ErrNotFound is returned when a requested protocol row does not exist.
var ErrNotFound = errors.New("protocol: not found")

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
	// PublishVersion flips a draft version to published. The source-backed approval gate is
	// enforced by the app layer before calling this.
	PublishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string) error

	CreateRule(ctx context.Context, in domain.NewRule) (ruleID string, err error)
	ListRules(ctx context.Context, tenantID, versionID string) ([]domain.Rule, error)

	// ListActiveAnimalStages returns the tenant's active animal_stage_lookup rows (display order)
	// so Config authoring picks stage bands from reference data, not hardcoded frontend literals.
	ListActiveAnimalStages(ctx context.Context, tenantID string) ([]domain.AnimalStage, error)

	CreateTrigger(ctx context.Context, in domain.NewTrigger) (triggerID string, err error)
}
