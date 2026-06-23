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
	// PublishVersion flips a draft version to published. The source-backed approval gate is
	// enforced by the app layer before calling this.
	PublishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string) error

	CreateRule(ctx context.Context, in domain.NewRule) (ruleID string, err error)
	ListRules(ctx context.Context, tenantID, versionID string) ([]domain.Rule, error)

	CreateTrigger(ctx context.Context, in domain.NewTrigger) (triggerID string, err error)
}
