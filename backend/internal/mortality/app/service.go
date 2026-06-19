package app

import (
	"context"
	"errors"
	"strings"

	locationsapp "github.com/vgoats/goatos/backend/internal/locations/app"
	locationsdomain "github.com/vgoats/goatos/backend/internal/locations/domain"
	"github.com/vgoats/goatos/backend/internal/mortality/domain"
	"github.com/vgoats/goatos/backend/internal/mortality/ports"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

type Service struct {
	repo     ports.Repository
	resolver LocationResolver
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

type LocationResolver interface {
	ResolveSourceLabel(ctx context.Context, input locationsapp.ResolveSourceLabelInput) (*locationsdomain.SourceLabelResolution, error)
}

func NewServiceWithResolver(repo ports.Repository, resolver LocationResolver) *Service {
	return &Service{repo: repo, resolver: resolver}
}

func (s *Service) GetDashboard(ctx context.Context, params ports.DashboardParams, traceID string) (*domain.DashboardResponse, error) {
	params.TenantID = strings.TrimSpace(params.TenantID)
	if !uuidutil.IsUUIDString(params.TenantID) {
		return nil, Unauthorized("missing_tenant_scope", "tenant scope is required")
	}
	params.Period = normalizePeriod(params.Period)
	if !validPeriod(params.Period) {
		return nil, BadRequest("invalid_period", "period must be overall, this-month, or month-wise")
	}
	result, err := s.repo.GetDashboard(ctx, params, traceID)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidFilter) {
			return nil, BadRequest("invalid_filter", "one or more Mortality filters are invalid")
		}
		return nil, Internal("mortality dashboard read failed")
	}
	return result, nil
}

func normalizePeriod(period string) string {
	period = strings.ToLower(strings.TrimSpace(period))
	if period == "" {
		return "overall"
	}
	if period == "this_month" {
		return "this-month"
	}
	if period == "monthwise" || period == "month_wise" {
		return "month-wise"
	}
	return period
}

func validPeriod(period string) bool {
	switch period {
	case "overall", "this-month", "month-wise":
		return true
	default:
		return false
	}
}
