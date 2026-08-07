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
	gotParkIDs  []string
	gotStart    time.Time
	gotEnd      time.Time
	parks       []domain.WeighingPark
	shedWeights domain.ShedWeights
}

func (r *shedWeightsRepo) GetShedWeights(_ context.Context, _ string, parkIDs []string, start, end time.Time) (domain.ShedWeights, error) {
	r.gotParkIDs = append([]string(nil), parkIDs...)
	r.gotStart, r.gotEnd = start, end
	out := r.shedWeights
	// The real repository builds the park vocabulary from the scope it was handed.
	// Mirroring that here keeps this test honest: it still proves the caller's scope
	// is what reaches the read, rather than asserting on a list the fake invented.
	for _, park := range r.parks {
		for _, id := range parkIDs {
			if park.ParkID == id {
				out.Parks = append(out.Parks, domain.GrowthPark{ParkID: park.ParkID, Name: park.Name})
			}
		}
	}
	return out, nil
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

	if _, err := svc.GetShedWeights(ctx, swActor(), swParkB, "", ""); err == nil {
		t.Fatal("expected park B to be denied for a park-A scoped monitor, got nil error")
	}
	if repo.gotParkIDs != nil {
		t.Fatalf("repository must not be reached when scope is denied, got parkIDs=%v", repo.gotParkIDs)
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

	out, err := svc.GetShedWeights(ctx, swActor(), "", "", "")
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	if len(repo.gotParkIDs) != 1 || repo.gotParkIDs[0] != swParkA {
		t.Fatalf("expected repository scoped to park A only, got %v", repo.gotParkIDs)
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
	repo := &shedWeightsRepo{parks: []domain.WeighingPark{{ParkID: swParkA, Name: "Coimbatore"}}}
	svc := NewService(repo)
	ctx := swContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: swTenant,
	})

	if _, err := svc.GetShedWeights(ctx, swActor(), swParkA, "2026-07-01", "2026-07-28"); err != nil {
		t.Fatalf("GetShedWeights: %v", err)
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
			if _, err := svc.GetShedWeights(ctx, swActor(), tc.park, tc.from, tc.to); err != ports.ErrInvalidArgument {
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

	if _, err := svc.GetShedWeights(ctx, actor, "", "", ""); err != ports.ErrForbidden {
		t.Fatalf("want ErrForbidden for a non-monitor role, got %v", err)
	}
}
