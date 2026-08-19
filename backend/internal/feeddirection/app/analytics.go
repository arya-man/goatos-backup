package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// Feed Analytics: the windowed DIRECTED rollup. Serving-only — no generation,
// no lifecycle writes, no completion overlay. See the grain contract on
// domain.DirectedAnalytics and the SQL notes in adapters/postgres/analytics.go.

// DirectedAnalyticsInput is one authorized analytics read.
type DirectedAnalyticsInput struct {
	TenantID string
	// ParkID is the selected park; empty means every authorized park.
	ParkID string
	// AuthorizedParkIDs is the caller's park capability set; empty means
	// tenant-wide. Resolved by the HTTP layer's park-scope middleware, applied
	// here so a park-scoped caller can never widen the rollup past their grant.
	AuthorizedParkIDs []string
	DateFrom          time.Time
	DateTo            time.Time
	// WastageDay is the single business day the experiment read's per-pen
	// wastage table describes; zero lets the adapter default it. Ignored by
	// the directed/execution/stock reads.
	WastageDay time.Time
}

// WithAnalyticsReader wires the directed-analytics rollup read. Optional: a pure
// generation unit test never touches it.
func (s *Service) WithAnalyticsReader(reader ports.DirectedAnalyticsReader) *Service {
	s.analytics = reader
	return s
}

// DirectedAnalytics serves the day and per-item directed series for one window.
func (s *Service) DirectedAnalytics(ctx context.Context, in DirectedAnalyticsInput) (domain.DirectedAnalytics, error) {
	if s.analytics == nil {
		return domain.DirectedAnalytics{}, fmt.Errorf("feeddirection: analytics reader is not wired")
	}
	parkIDs, err := analyticsParkFilter(in.ParkID, in.AuthorizedParkIDs)
	if err != nil {
		return domain.DirectedAnalytics{}, err
	}
	return s.analytics.DirectedAnalytics(ctx, in.TenantID, domain.DirectedAnalyticsQuery{
		ParkIDs:  parkIDs,
		DateFrom: in.DateFrom,
		DateTo:   in.DateTo,
	})
}

// analyticsParkFilter narrows to the selected park when one is chosen, otherwise
// to the caller's authorized set. A malformed id is rejected, never ignored — an
// ignored filter would silently widen the read.
func analyticsParkFilter(selected string, authorized []string) ([]uuid.UUID, error) {
	if selected != "" {
		id, err := uuid.Parse(selected)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ports.ErrParkNotFound, err)
		}
		return []uuid.UUID{id}, nil
	}
	out := make([]uuid.UUID, 0, len(authorized))
	for _, raw := range authorized {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ports.ErrParkNotFound, err)
		}
		out = append(out, id)
	}
	return out, nil
}

// ExecutionAnalytics serves the per-day completion-status counts and verify
// latency for one window.
func (s *Service) ExecutionAnalytics(ctx context.Context, in DirectedAnalyticsInput) (domain.ExecutionAnalytics, error) {
	if s.analytics == nil {
		return domain.ExecutionAnalytics{}, fmt.Errorf("feeddirection: analytics reader is not wired")
	}
	parkIDs, err := analyticsParkFilter(in.ParkID, in.AuthorizedParkIDs)
	if err != nil {
		return domain.ExecutionAnalytics{}, err
	}
	return s.analytics.ExecutionAnalytics(ctx, in.TenantID, domain.DirectedAnalyticsQuery{
		ParkIDs: parkIDs, DateFrom: in.DateFrom, DateTo: in.DateTo,
	})
}

// ExperimentAnalytics serves the trial arms' authored absolute-kg series.
func (s *Service) ExperimentAnalytics(ctx context.Context, in DirectedAnalyticsInput) (domain.ExperimentAnalytics, error) {
	if s.analytics == nil {
		return domain.ExperimentAnalytics{}, fmt.Errorf("feeddirection: analytics reader is not wired")
	}
	parkIDs, err := analyticsParkFilter(in.ParkID, in.AuthorizedParkIDs)
	if err != nil {
		return domain.ExperimentAnalytics{}, err
	}
	return s.analytics.ExperimentAnalytics(ctx, in.TenantID, domain.DirectedAnalyticsQuery{
		ParkIDs: parkIDs, DateFrom: in.DateFrom, DateTo: in.DateTo, WastageDay: in.WastageDay,
	})
}

// StockAnalytics serves the purchase-ledger stock cards and expenditure series.
func (s *Service) StockAnalytics(ctx context.Context, in DirectedAnalyticsInput) (domain.StockAnalytics, error) {
	if s.analytics == nil {
		return domain.StockAnalytics{}, fmt.Errorf("feeddirection: analytics reader is not wired")
	}
	parkIDs, err := analyticsParkFilter(in.ParkID, in.AuthorizedParkIDs)
	if err != nil {
		return domain.StockAnalytics{}, err
	}
	return s.analytics.StockAnalytics(ctx, in.TenantID, domain.DirectedAnalyticsQuery{
		ParkIDs: parkIDs, DateFrom: in.DateFrom, DateTo: in.DateTo,
	})
}
