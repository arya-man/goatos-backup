// Package app holds the inventory application service (thin orchestration over the repository).
// Phase 0 keeps it minimal: domain logic (FEFO reservation, balance updates inside a txn) lands
// in Phase 1 alongside the obligation/SOP execution flow.
package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/inventory/domain"
	"github.com/vgoats/goatos/backend/internal/inventory/ports"
)

// Service coordinates inventory use-cases over the repository boundary.
type Service struct {
	repo ports.Repository
}

// NewService constructs a Service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

// CreateItem registers an inventory item.
func (s *Service) CreateItem(ctx context.Context, in domain.NewItem) (string, error) {
	return s.repo.CreateItem(ctx, in)
}

// CreateStockLot registers a stock lot.
func (s *Service) CreateStockLot(ctx context.Context, in domain.NewStockLot) (string, error) {
	return s.repo.CreateStockLot(ctx, in)
}

// PickFEFOLot returns the earliest-expiring available lot for an (location, item).
func (s *Service) PickFEFOLot(ctx context.Context, tenantID, locationID, itemID string) (domain.FEFOPick, error) {
	return s.repo.PickFEFOLot(ctx, tenantID, locationID, itemID)
}

// RecordMovement appends an idempotent ledger movement.
func (s *Service) RecordMovement(ctx context.Context, m domain.Movement) (string, bool, error) {
	return s.repo.RecordMovement(ctx, m)
}
