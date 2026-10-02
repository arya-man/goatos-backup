package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

type assumptionsRepo struct {
	fakeRepo
	values []domain.AssumptionValue
}

func (r *assumptionsRepo) GetAssumptions(context.Context, string, time.Time, bool) (domain.Assumptions, error) {
	return domain.Assumptions{Values: r.values, SalePrices: []domain.SalePrice{{}}}, nil
}

// TestTheSaleReadyLineFollowsTheTicksAndCarriesNothingElse (People / HRMS fixes, 2026-10-02):
// the Growth Director reads checked ROLE grants only, so a person given Weighing or Sales on
// People / HRMS without a matching role was refused. The Farm value card's sale-ready line is open
// to a Sales reader and returns the two figures alone.
func TestTheSaleReadyLineFollowsTheTicksAndCarriesNothingElse(t *testing.T) {
	repo := &assumptionsRepo{values: []domain.AssumptionValue{
		{Key: "sale_ready_threshold_kg", Value: 35},
		{Key: "sale_ready_lower_kg", Value: 30},
		{Key: "feed_cost_per_kg", Value: 99},
	}}
	svc := NewService(repo)
	salesByTick := domain.Actor{TenantID: "t", Roles: []string{permissions.RoleFeedDirector}, Permissions: []string{permissions.SalesRead}, PermissionsResolved: true}
	got, err := svc.GetSaleReadyLine(context.Background(), salesByTick)
	if err != nil {
		t.Fatalf("a Farm value reader (Sales ticked) was refused the sale-ready line: %v", err)
	}
	if got.ThresholdKg == nil || *got.ThresholdKg != 35 || got.LowerKg == nil || *got.LowerKg != 30 {
		t.Fatalf("line = %+v, want 35 / 30", got)
	}
	// The full assumptions (prices and every other figure) stay on Weighing.
	if _, err := svc.GetAssumptions(context.Background(), salesByTick, false); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("a sales-only reader must not get the full assumptions: err = %v", err)
	}
	// The ticks decide over the role: a Feed Director ROLE with Weighing ticked reads them.
	weighingByTick := domain.Actor{TenantID: "t", Roles: []string{permissions.RoleFeedDirector}, Permissions: []string{permissions.WeighingMonitor}, PermissionsResolved: true}
	if _, err := svc.GetAssumptions(context.Background(), weighingByTick, false); err != nil {
		t.Fatalf("a person ticked for Weighing was refused the assumptions: %v", err)
	}
	if _, err := svc.GetSaleReadyLine(context.Background(), domain.Actor{TenantID: "t", PermissionsResolved: true}); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("someone with neither: err = %v, want forbidden", err)
	}
}
