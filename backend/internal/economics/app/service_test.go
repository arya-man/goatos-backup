package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/economics/domain"
	"github.com/vgoats/goatos/backend/internal/economics/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

type fakeRepo struct {
	parks      []domain.Park
	gotParkIDs []string
	gotStart   time.Time
	gotEnd     time.Time
	calls      int
}

func (f *fakeRepo) ListParks(ctx context.Context, tenantID string) ([]domain.Park, error) {
	return f.parks, nil
}

func (f *fakeRepo) GetBusinessEconomics(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.BusinessEconomics, error) {
	f.calls++
	f.gotParkIDs = parkIDs
	f.gotStart = periodStart
	f.gotEnd = periodEnd
	return domain.BusinessEconomics{Estimate: true}, nil
}

// TestEconomicsGateIsTheDedicatedPermission pins the capability gate: only a
// role holding SalesEconomicsRead reaches the repository. sales director /
// procurement director / growth director are the mutation test — each holds an
// adjacent capability (SalesRead, WeighingMonitor) a lazier gate might reuse.
func TestEconomicsGateIsTheDedicatedPermission(t *testing.T) {
	repo := &fakeRepo{parks: []domain.Park{{ParkID: "00000000-0000-4000-8000-000000000010", Name: "CPT"}}}
	svc := NewService(repo)

	salesDirector := permissions.RoleKey(permissions.TierDirector, permissions.VerticalSales)
	for _, role := range []string{permissions.RoleOperator, permissions.RoleProcurementDirector, permissions.RoleGrowthDirector, salesDirector} {
		_, err := svc.GetBusinessEconomics(context.Background(), domain.Actor{TenantID: "t", Roles: []string{role}}, "", "", "")
		if !errors.Is(err, ports.ErrForbidden) {
			t.Fatalf("%s: err = %v, want ErrForbidden", role, err)
		}
	}
	if repo.calls != 0 {
		t.Fatalf("repository reached %d times through a forbidden gate", repo.calls)
	}

	// ceo_internal with no grants in context = internal/unrestricted: resolves
	// every park from the vocabulary.
	if _, err := svc.GetBusinessEconomics(context.Background(), domain.Actor{TenantID: "t", Roles: []string{permissions.RoleCEOInternal}}, "", "", ""); err != nil {
		t.Fatalf("ceo_internal: %v", err)
	}
	if repo.calls != 1 || len(repo.gotParkIDs) != 1 || repo.gotParkIDs[0] != "00000000-0000-4000-8000-000000000010" {
		t.Fatalf("repo calls/parks = %d/%v", repo.calls, repo.gotParkIDs)
	}
}

// TestEconomicsWindowIsBusinessDayGrain pins the window contract: inclusive
// business dates only (anything finer rejected), end before start rejected,
// and an explicit window resolves to the half-open [start, end+1d).
func TestEconomicsWindowIsBusinessDayGrain(t *testing.T) {
	repo := &fakeRepo{parks: []domain.Park{{ParkID: "00000000-0000-4000-8000-000000000010", Name: "CPT"}}}
	svc := NewService(repo)
	actor := domain.Actor{TenantID: "t", Roles: []string{permissions.RoleCEOInternal}}

	for _, tc := range [][2]string{
		{"2026-08-01T00:00:00", "2026-08-10"}, // finer than a date
		{"not-a-date", "2026-08-10"},
		{"2026-08-10", "2026-08-01"}, // end before start
	} {
		if _, err := svc.GetBusinessEconomics(context.Background(), actor, "", tc[0], tc[1]); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatalf("window %v: err = %v, want ErrInvalidArgument", tc, err)
		}
	}
	if _, err := svc.GetBusinessEconomics(context.Background(), actor, "not-a-uuid", "", ""); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("a non-uuid park_id must be rejected")
	}

	if _, err := svc.GetBusinessEconomics(context.Background(), actor, "", "2026-06-01", "2026-08-24"); err != nil {
		t.Fatalf("explicit window: %v", err)
	}
	if got := repo.gotStart.Format("2006-01-02"); got != "2026-06-01" {
		t.Fatalf("period start = %s", got)
	}
	// The caller's last day is inclusive, so the exclusive boundary is the day after.
	if got := repo.gotEnd.Format("2006-01-02"); got != "2026-08-25" {
		t.Fatalf("period end (exclusive) = %s", got)
	}
}
