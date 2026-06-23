// Package app holds the obligation application service. Phase 0 keeps it thin; SM-1..SM-7
// generation/transition handlers arrive in Phase 1. The reserve-before-insert status-event
// path lives in the repository and is exercised by the retry-dedup test.
package app

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
)

// Service coordinates obligation use-cases over the repository boundary.
type Service struct {
	repo ports.Repository
}

// NewService constructs a Service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

// GenerateObligation creates one obligation (idempotent on its deterministic key).
func (s *Service) GenerateObligation(ctx context.Context, in domain.NewObligation) (string, bool, error) {
	return s.repo.InsertObligation(ctx, in)
}

// ListDue returns due-window obligations.
func (s *Service) ListDue(ctx context.Context, tenantID, status string, dueBefore time.Time, limit int32) ([]domain.DueObligation, error) {
	return s.repo.ListDue(ctx, tenantID, status, dueBefore, limit)
}

// RecordStatusEvent appends an idempotent status event (no duplicate on retry).
func (s *Service) RecordStatusEvent(ctx context.Context, ev domain.NewStatusEvent) (string, bool, error) {
	return s.repo.RecordStatusEvent(ctx, ev)
}
