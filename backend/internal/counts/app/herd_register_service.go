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

// GetSummary returns exact scoped summary counts.
//
// Despite the name of herd_register_summary_projection, this read is served from canonical
// goats, not from that projection table — see Repository.GetHerdRegisterSummary.
func (s *HerdRegisterService) GetSummary(ctx context.Context, req domain.HerdRegisterSummaryQuery) (domain.HerdRegisterSummary, error) {
	return s.repo.GetHerdRegisterSummary(ctx, req)
}

// GetBreakdown returns the Counts Breakdown census: one page of farm x stage x breed x sex x
// shed grain rows plus whole-result totals, chart series and filter facets.
func (s *HerdRegisterService) GetBreakdown(ctx context.Context, req domain.CountsBreakdownQuery) (domain.CountsBreakdown, error) {
	return s.repo.GetCountsBreakdown(ctx, req)
}
