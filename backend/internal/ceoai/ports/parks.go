package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// ParkDirectory answers which parks the tenant has, so the planners match a park the user names
// against the live list (Configuration > Items & settings > Parks) rather than a constant pair.
type ParkDirectory interface {
	ActiveParks(ctx context.Context, tenantID string) ([]domain.ParkRef, error)
}
