// Package app holds the protocol application service. Phase 0 keeps it thin; the source-backed
// publish gate and rule_dsl -> protocol_rules expansion (SM-1 generation) arrive in Phase 1.
package app

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

// AfterPublishHook lets verticals react after a config version is actually
// published. The protocol layer remains generic; vaccination wires this to the
// idempotent SM-1 generator in bootstrap.
type AfterPublishHook interface {
	AfterProtocolVersionPublished(ctx context.Context, tenantID string, version domain.Version, publishedAt time.Time) error
}

// AfterPublishFunc adapts a function into an AfterPublishHook.
type AfterPublishFunc func(ctx context.Context, tenantID string, version domain.Version, publishedAt time.Time) error

func (f AfterPublishFunc) AfterProtocolVersionPublished(ctx context.Context, tenantID string, version domain.Version, publishedAt time.Time) error {
	return f(ctx, tenantID, version, publishedAt)
}

// Service coordinates protocol config use-cases over the repository boundary.
type Service struct {
	repo             ports.Repository
	afterPublishHook AfterPublishHook
}

// NewService constructs a Service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

// WithAfterPublishHook registers a generic post-publish hook. It is optional
// and must be idempotent because publish requests/retries can replay around the
// API boundary.
func (s *Service) WithAfterPublishHook(h AfterPublishHook) *Service {
	s.afterPublishHook = h
	return s
}

// CreateDefinition registers a protocol definition.
func (s *Service) CreateDefinition(ctx context.Context, in domain.NewDefinition) (string, error) {
	return s.repo.CreateDefinition(ctx, in)
}

// CreateVersion drafts a protocol version (always created as a draft by the caller).
func (s *Service) CreateVersion(ctx context.Context, in domain.NewVersion) (string, error) {
	return s.repo.CreateVersion(ctx, in)
}

// GetVersion returns a stored version.
func (s *Service) GetVersion(ctx context.Context, tenantID, versionID string) (domain.Version, error) {
	return s.repo.GetVersion(ctx, tenantID, versionID)
}

// AddRule appends a dose/phase rule to a draft version.
func (s *Service) AddRule(ctx context.Context, in domain.NewRule) (string, error) {
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
