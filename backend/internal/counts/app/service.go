package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	locationsapp "github.com/vgoats/goatos/backend/internal/locations/app"
	locationsdomain "github.com/vgoats/goatos/backend/internal/locations/domain"
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
	params.View = normalizeView(params.View)
	if !validView(params.View) {
		return nil, BadRequest("invalid_view", "view must be overall, core-farms, cbe, cpt, or holdings")
	}
	if params.SnapshotDate != "" && params.SnapshotDate != "latest" {
		if _, err := time.Parse("2006-01-02", params.SnapshotDate); err != nil {
			return nil, BadRequest("invalid_snapshot_date", "snapshot_date must be latest or YYYY-MM-DD")
		}
	}
	result, err := s.repo.GetDashboard(ctx, params, traceID)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidFilter) {
			return nil, BadRequest("invalid_filter", "one or more Counts filters are invalid")
		}
		return nil, Internal("counts dashboard read failed")
	}
	return result, nil
}

func normalizeView(view string) string {
	view = strings.ToLower(strings.TrimSpace(view))
	if view == "" {
		return "overall"
	}
	if view == "core" {
		return "core-farms"
	}
	return view
}

func validView(view string) bool {
	switch view {
	case "overall", "core-farms", "cbe", "cpt", "holdings":
		return true
	default:
		return false
	}
}
