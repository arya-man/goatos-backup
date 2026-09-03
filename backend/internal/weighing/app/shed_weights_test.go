package app

import (
	"context"

	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// shedWeightsRepo records the park scope the service resolved, so a test can
// assert on the ARGUMENT the repository was called with rather than on a response
// the fake made up. A scope regression is invisible if the fake ignores its input.
type shedWeightsRepo struct {
	fakeRepo
	gotScopeParkIDs []string
	gotSelectedPark string
	gotStart        time.Time
	gotEnd          time.Time
	gotToleranceKg  float64
	parks           []domain.WeighingPark
	shedWeights     domain.ShedWeights
}

func (r *shedWeightsRepo) GetShedWeights(_ context.Context, _ string, scopeParkIDs []string, selectedParkID string, start, end time.Time, _, _, _ string, toleranceKg float64) (domain.ShedWeights, error) {
	r.gotScopeParkIDs = append([]string(nil), scopeParkIDs...)
	r.gotSelectedPark = selectedParkID
	r.gotStart, r.gotEnd = start, end
	r.gotToleranceKg = toleranceKg
	out := r.shedWeights
	// The real repository builds the park vocabulary from the scope it was handed.
	// Mirroring that here keeps this test honest: it still proves the caller's scope
	// is what reaches the read, rather than asserting on a list the fake invented.
	for _, park := range r.parks {
		for _, id := range scopeParkIDs {
			if park.ParkID == id {
				out.Parks = append(out.Parks, domain.GrowthPark{ParkID: park.ParkID, Name: park.Name})
			}
		}
	}
	return out, nil
}

// GetWeighingDates is a stub: the narrow landing-window read is not exercised by this fake.
func (r *shedWeightsRepo) GetWeighingDates(context.Context, string, []string, time.Time, time.Time, string, string, string) (domain.WeighingDates, error) {
	return domain.WeighingDates{}, nil
}

func (r *shedWeightsRepo) ListParks(context.Context, string) ([]domain.WeighingPark, error) {
	return r.parks, nil
}

const (
	swTenant = "00000000-0000-0000-0000-000000000001"
	swParkA  = "00000000-0000-4000-8000-000000000201"
	swParkB  = "00000000-0000-4000-8000-000000000202"
)

func swActor() domain.Actor {
	return domain.Actor{TenantID: swTenant, Roles: []string{permissions.RoleGrowthDirector}}
}

func swContext(grants ...permissions.ActiveGrant) context.Context {
	ctx := httpmiddleware.WithAuthGrants(context.Background(), grants)
	return httpmiddleware.WithTenantID(ctx, swTenant)
}

// A park-scoped monitor must not be able to read another park by naming its id.
// WeighingMonitor is a CAPABILITY, not a scope, so the flat role gate alone lets a
// park-A director through — the scope resolution is the only thing standing
// between them and park B's herd.
func TestGetShedWeightsRejectsUnauthorizedParkID(t *testing.T) {
	repo := &shedWeightsRepo{}
	svc := NewService(repo)
	ctx := swContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: swParkA,
	})

	if _, err := svc.GetShedWeights(ctx, swActor(), swParkB, "", "", "", "", "", ""); err == nil {
		t.Fatal("expected park B to be denied for a park-A scoped monitor, got nil error")
	}
	if repo.gotScopeParkIDs != nil {
		t.Fatalf("repository must not be reached when scope is denied, got parkIDs=%v", repo.gotScopeParkIDs)
	}
}

// Omitting park_id means "every park I may monitor" — never every park that
// exists. A park-A monitor asking for the herd-wide view gets park A only.
func TestGetShedWeightsOmittedParkIDUsesOnlyAuthorizedParks(t *testing.T) {
	repo := &shedWeightsRepo{parks: []domain.WeighingPark{
		{ParkID: swParkA, Name: "Coimbatore"},
		{ParkID: swParkB, Name: "Channapatna"},
	}}
	svc := NewService(repo)
	ctx := swContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: swParkA,
	})

	out, err := svc.GetShedWeights(ctx, swActor(), "", "", "", "", "", "", "")
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	if len(repo.gotScopeParkIDs) != 1 || repo.gotScopeParkIDs[0] != swParkA {
		t.Fatalf("expected repository scoped to park A only, got %v", repo.gotScopeParkIDs)
	}
	if repo.gotSelectedPark != "" {
		t.Fatalf("expected no selected park for omitted park filter, got %q", repo.gotSelectedPark)
	}
	if got := int(repo.gotEnd.Sub(repo.gotStart).Hours() / 24); got != domain.ShedWeightsDefaultPeriodDays {
		t.Fatalf("omitted from/to default window=%d days, want %d", got, domain.ShedWeightsDefaultPeriodDays)
	}
	// The park filter vocabulary is backend-owned AND scoped: offering park B here
	// would advertise a park this caller cannot read. The repository builds it from
	// the scope, so this asserts the scope that reached it.
	if len(out.Parks) != 1 || out.Parks[0].ParkID != swParkA {
		t.Fatalf("park vocabulary must be limited to authorized parks, got %+v", out.Parks)
	}
	if out.Parks[0].Name != "Coimbatore" {
		t.Fatalf("park label must come from the backend, got %q", out.Parks[0].Name)
	}
}

// The window is a BUSINESS-DAY range in Asia/Kolkata, passed down half-open. A
// caller's inclusive last day must become an exclusive midnight boundary the day
// after, or the final day's weighs are silently dropped.
func TestGetShedWeightsPassesHalfOpenBusinessDayWindow(t *testing.T) {
	repo := &shedWeightsRepo{parks: []domain.WeighingPark{
		{ParkID: swParkA, Name: "Coimbatore"},
		{ParkID: swParkB, Name: "Channapatna"},
	}}
	svc := NewService(repo)
	ctx := swContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: swTenant,
	})

	if _, err := svc.GetShedWeights(ctx, swActor(), swParkA, "2026-07-01", "2026-07-28", "", "", "", ""); err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	if len(repo.gotScopeParkIDs) != 2 || repo.gotScopeParkIDs[0] != swParkA || repo.gotScopeParkIDs[1] != swParkB {
		t.Fatalf("expected full tenant scope for vocabulary, got %v", repo.gotScopeParkIDs)
	}
	if repo.gotSelectedPark != swParkA {
		t.Fatalf("expected selected park %s for rows, got %q", swParkA, repo.gotSelectedPark)
	}
	if got := repo.gotStart.Format("2006-01-02"); got != "2026-07-01" {
		t.Fatalf("period start: want 2026-07-01, got %s", got)
	}
	// Exclusive boundary is the day AFTER the caller's inclusive last day.
	if got := repo.gotEnd.Format("2006-01-02"); got != "2026-07-29" {
		t.Fatalf("period end must be exclusive midnight after the last day: want 2026-07-29, got %s", got)
	}
}

func TestGetShedWeightsRejectsMalformedInput(t *testing.T) {
	repo := &shedWeightsRepo{}
	svc := NewService(repo)
	ctx := swContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: swTenant,
	})

	for _, tc := range []struct{ name, park, from, to string }{
		{"non-uuid park", "not-a-uuid", "", ""},
		{"non-date from", "", "01/07/2026", ""},
		{"non-date to", "", "", "2026-7-1"},
		{"inverted window", "", "2026-07-28", "2026-07-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.GetShedWeights(ctx, swActor(), tc.park, tc.from, tc.to, "", "", "", ""); err != ports.ErrInvalidArgument {
				t.Fatalf("want ErrInvalidArgument, got %v", err)
			}
		})
	}
}

func TestGetShedWeightsPassesSaleThresholdTolerance(t *testing.T) {
	repo := &shedWeightsRepo{parks: []domain.WeighingPark{{ParkID: swParkA, Name: "Coimbatore"}}}
	svc := NewService(repo)
	ctx := swContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: swParkA,
	})

	if _, err := svc.GetShedWeights(ctx, swActor(), "", "", "", "", "", "", "200"); err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	if repo.gotToleranceKg != 0.2 {
		t.Fatalf("tolerance kg = %.3f, want 0.200", repo.gotToleranceKg)
	}
}

func TestGetShedWeightsClampsSaleReadyWindowToReliableAnchor(t *testing.T) {
	repo := &shedWeightsRepo{parks: []domain.WeighingPark{{ParkID: swParkA, Name: "Coimbatore"}}}
	svc := NewService(repo)
	ctx := swContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: swParkA,
	})

	if _, err := svc.GetShedWeights(ctx, swActor(), "", "2026-07-23", "2026-09-03", "", "", "", "0"); err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	if got := repo.gotStart.Format("2006-01-02"); got != "2026-08-01" {
		t.Fatalf("sale-ready period start = %s, want 2026-08-01", got)
	}
	if got := repo.gotEnd.Format("2006-01-02"); got != "2026-09-04" {
		t.Fatalf("sale-ready period end = %s, want 2026-09-04", got)
	}
}

func TestGetShedWeightsRejectsInvalidSaleThresholdTolerance(t *testing.T) {
	repo := &shedWeightsRepo{}
	svc := NewService(repo)
	ctx := swContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: swTenant,
	})

	for _, raw := range []string{"-1", "1001", "2.5", "abc"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := svc.GetShedWeights(ctx, swActor(), "", "", "", "", "", "", raw); err != ports.ErrInvalidArgument {
				t.Fatalf("want ErrInvalidArgument, got %v", err)
			}
		})
	}
}

// A role without WeighingMonitor is refused before any scope work happens.
func TestGetShedWeightsRequiresMonitorCapability(t *testing.T) {
	repo := &shedWeightsRepo{}
	svc := NewService(repo)
	ctx := swContext()
	actor := domain.Actor{TenantID: swTenant, Roles: []string{permissions.RoleOperator}}

	if _, err := svc.GetShedWeights(ctx, actor, "", "", "", "", "", "", ""); err != ports.ErrForbidden {
		t.Fatalf("want ErrForbidden for a non-monitor role, got %v", err)
	}
}
