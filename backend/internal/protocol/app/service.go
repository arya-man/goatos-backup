// Package app holds the protocol application service. Phase 0 keeps it thin; publish readiness and
// rule_dsl -> protocol_rules expansion (SM-1 generation) live in publish.go.
package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

// Service coordinates protocol config use-cases over the repository boundary.
type Service struct {
	repo ports.Repository
}

// NewService constructs a Service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

// CreateDefinition registers a protocol definition.
func (s *Service) CreateDefinition(ctx context.Context, in domain.NewDefinition) (string, error) {
	return s.repo.CreateDefinition(ctx, in)
}

// CreateVersion drafts a protocol version (always created as a draft by the caller).
func (s *Service) CreateVersion(ctx context.Context, in domain.NewVersion) (string, error) {
	if err := ValidateRuleDSL(in.RuleDsl); err != nil {
		return "", err
	}
	return s.repo.CreateVersion(ctx, in)
}

// GetVersion returns a stored version.
func (s *Service) GetVersion(ctx context.Context, tenantID, versionID string) (domain.Version, error) {
	return s.repo.GetVersion(ctx, tenantID, versionID)
}

// AddRule appends a dose/phase rule to a draft version.
func (s *Service) AddRule(ctx context.Context, in domain.NewRule) (string, error) {
	repeat, err := normalizeRepeatPolicy(in.Repeat, in.MinGapDays)
	if err != nil {
		return "", err
	}
	in.Repeat = repeat
	return s.repo.CreateRule(ctx, in)
}

// ListConfigs returns the Config authority list for a category (all versions, any status).
func (s *Service) ListConfigs(ctx context.Context, tenantID, category string) ([]domain.ConfigListItem, error) {
	return s.repo.ListConfigs(ctx, tenantID, category)
}

// ListAnimalStages returns the tenant's active animal-stage reference data for Config authoring.
func (s *Service) ListAnimalStages(ctx context.Context, tenantID string) ([]domain.AnimalStage, error) {
	return s.repo.ListActiveAnimalStages(ctx, tenantID)
}
