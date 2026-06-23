// Package app holds the vaccination application service. Phase 1A keeps it thin; the SM-5
// verification flow and inventory consume/release wire through this service in later slices.
package app

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

// Service coordinates vaccination use-cases over the repository boundary.
type Service struct {
	repo ports.Repository
}

// NewService constructs a Service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

// RecordCompletion records an administered dose (idempotent).
func (s *Service) RecordCompletion(ctx context.Context, in domain.NewCompletion) (string, bool, error) {
	return s.repo.RecordCompletion(ctx, in)
}

// AcceptCompletion accepts a recorded completion on verification, returning its verification context
// (idempotent: applied is false on replay).
func (s *Service) AcceptCompletion(ctx context.Context, tenantID, completionID string, verifiedBy *string, withdrawalUntil *time.Time) (domain.AcceptedCompletion, bool, error) {
	return s.repo.AcceptCompletion(ctx, tenantID, completionID, verifiedBy, withdrawalUntil)
}

// RejectCompletion rejects a recorded completion (rework). applied is false on replay.
func (s *Service) RejectCompletion(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (bool, error) {
	return s.repo.RejectCompletion(ctx, tenantID, completionID, reason, verifiedBy)
}

// GoatHistory returns a goat's vaccination history.
func (s *Service) GoatHistory(ctx context.Context, tenantID, goatID string, limit int32) ([]domain.CompletionHistoryItem, error) {
	return s.repo.ListCompletionsByGoat(ctx, tenantID, goatID, limit)
}
