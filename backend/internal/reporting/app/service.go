package app

import (
	"context"
	"errors"
	"strings"

	"github.com/vgoats/goatos/backend/internal/reporting/domain"
	"github.com/vgoats/goatos/backend/internal/reporting/ports"
)

type Service struct {
	repo ports.Repository
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetIdentityCounts(ctx context.Context, params ports.CountParams, traceID string) (*domain.IdentityCountsResult, error) {
	if err := requireTenant(params.TenantID); err != nil {
		return nil, err
	}
	if !validGrain(params.Grain) {
		return nil, BadRequest("invalid_grain", "grain is required and must be a Phase 1 identity counter grain")
	}
	if params.Limit < 1 || params.Limit > ports.MaxIdentityCountsLimit {
		return nil, BadRequest("invalid_limit", "limit is required and must be between 1 and 500")
	}
	page, err := s.repo.ListIdentityCounts(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if len(page.Items) == 0 {
		msg := "Counters are empty until reporting rebuild or projection jobs populate goat_identity_counters."
		page.Freshness.Warning = &msg
	}
	return &domain.IdentityCountsResult{
		Grain:      params.Grain,
		Items:      page.Items,
		NextCursor: page.NextCursor,
		HasMore:    page.HasMore,
		Freshness:  page.Freshness,
		TraceID:    traceID,
	}, nil
}

func (s *Service) RebuildIdentityCounters(ctx context.Context, params ports.RebuildIdentityCountersParams) (*domain.IdentityCounterRebuildResult, error) {
	if err := requireTenant(params.TenantID); err != nil {
		return nil, err
	}
	if len(params.Grains) == 0 {
		params.Grains = domain.AllIdentityCounterGrains
	}
	seen := map[string]struct{}{}
	for _, grain := range params.Grains {
		if !validGrain(grain) {
			return nil, BadRequest("invalid_grain", "rebuild requested an unsupported Phase 1 identity counter grain")
		}
		if _, ok := seen[grain]; ok {
			return nil, BadRequest("duplicate_grain", "rebuild grains must be unique")
		}
		seen[grain] = struct{}{}
	}
	result, err := s.repo.RebuildIdentityCounters(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return result, nil
}

func requireTenant(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return Unauthorized("missing_tenant_scope", "tenant scope is required")
	}
	return nil
}

func validGrain(grain string) bool {
	for _, allowed := range domain.AllIdentityCounterGrains {
		if grain == allowed {
			return true
		}
	}
	return false
}

func mapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ports.ErrNotFound) {
		return BadRequest("not_found_or_not_allowed", "reporting scope is missing or outside tenant scope")
	}
	if errors.Is(err, ports.ErrInvalidFilter) {
		return BadRequest("invalid_filter", "one or more reporting filters are invalid")
	}
	if errors.Is(err, ports.ErrInvalidCursor) {
		return BadRequest("invalid_cursor", "analytics counts cursor is invalid")
	}
	return Internal("reporting repository operation failed")
}
