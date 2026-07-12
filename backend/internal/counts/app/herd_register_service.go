// Package app provides the counts domain services.
package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

// HerdRegisterService exposes the herd register projection-backed read model.
type HerdRegisterService struct {
	repo ports.Repository
}

// NewHerdRegisterService creates a new herd register service.
func NewHerdRegisterService(repo ports.Repository) *HerdRegisterService {
	return &HerdRegisterService{repo: repo}
}

// GetSummary returns exact summary counts from the herd_register_summary_projection.
func (s *HerdRegisterService) GetSummary(ctx context.Context, req domain.HerdRegisterSummaryQuery) (domain.HerdRegisterSummary, error) {
	return s.repo.GetHerdRegisterSummary(ctx, req)
}
