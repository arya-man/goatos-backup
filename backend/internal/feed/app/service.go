// Package app holds the feed-direction application service.
package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/feed/domain"
	"github.com/vgoats/goatos/backend/internal/feed/ports"
)

// Service coordinates feed-direction use-cases over the repository boundary.
type Service struct {
	repo ports.Repository
}

// NewService constructs a Service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

// RecordDirection records a shed feed-direction execution (idempotent).
func (s *Service) RecordDirection(ctx context.Context, in domain.NewDirection) (string, bool, error) {
	return s.repo.RecordDirection(ctx, in)
}

// AcceptDirection accepts a recorded direction on verification, returning its verification context
// (idempotent: applied is false on replay).
func (s *Service) AcceptDirection(ctx context.Context, tenantID, completionID string, verifiedBy *string) (domain.AcceptedDirection, bool, error) {
	return s.repo.AcceptDirection(ctx, tenantID, completionID, verifiedBy)
}

// RejectDirection rejects a recorded direction (rework). applied is false on replay.
func (s *Service) RejectDirection(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (bool, error) {
	return s.repo.RejectDirection(ctx, tenantID, completionID, reason, verifiedBy)
}

// ShedHistory returns a shed's feed history.
func (s *Service) ShedHistory(ctx context.Context, tenantID, shedID string, limit int32) ([]domain.DirectionHistoryItem, error) {
	return s.repo.ListDirectionsByShed(ctx, tenantID, shedID, limit)
}

// VerificationQueue returns directions awaiting review (earliest fed first).
func (s *Service) VerificationQueue(ctx context.Context, tenantID string, limit int32) ([]domain.RecordedDirection, error) {
	return s.repo.ListRecordedDirections(ctx, tenantID, limit)
}
