package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// fakeRepo records the park scope the service resolved, so a test can assert
// on the ARGUMENT the repository was called with rather than on a response the
// fake made up. A scope regression is invisible if the fake ignores its input.
// This is the module's service fake: a widened ports.Repository gets its stub
// here, or every app test breaks on the interface.
type fakeRepo struct {
	gotParkIDs []string
	gotStart   time.Time
	gotEnd     time.Time
	parks      []domain.Park
	result     domain.GrowthDirectorWeights
}

func (r *fakeRepo) ListParks(context.Context, string) ([]domain.Park, error) {
	return r.parks, nil
}

func (r *fakeRepo) GetGrowthDirectorWeights(_ context.Context, _ string, parkIDs []string, start, end time.Time, _, _ string) (domain.GrowthDirectorWeights, error) {
	r.gotParkIDs = append([]string(nil), parkIDs...)
	r.gotStart, r.gotEnd = start, end
	out := r.result
	// The real repository builds the park vocabulary from the scope it was
	// handed. Mirroring that keeps this test honest: it proves the caller's
	// scope is what reaches the read, not a list the fake invented.
	for _, park := range r.parks {
		for _, id := range parkIDs {
			if park.ParkID == id {
				out.Parks = append(out.Parks, park)
			}
		}
	}
	return out, nil
}

const (
	gdTenant = "00000000-0000-0000-0000-000000000001"
	gdParkA  = "00000000-0000-4000-8000-000000000201"
	gdParkB  = "00000000-0000-4000-8000-000000000202"
)

func gdActor() domain.Actor {
	return domain.Actor{TenantID: gdTenant, Roles: []string{permissions.RoleGrowthDirector}}
}

func gdContext(grants ...permissions.ActiveGrant) context.Context {
	ctx := httpmiddleware.WithAuthGrants(context.Background(), grants)
	return httpmiddleware.WithTenantID(ctx, gdTenant)
}

// A park-scoped monitor must not be able to read another park by naming its
// id. WeighingMonitor is a CAPABILITY, not a scope, so the flat role gate
// alone lets a park-A director through — the scope resolution is the only
// thing standing between them and park B's herd.
func TestGetGrowthDirectorWeightsRejectsUnauthorizedParkID(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: gdParkA,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), gdParkB, "", "", "", ""); err == nil {
		t.Fatal("expected park B to be denied for a park-A scoped monitor, got nil error")
	}
	if repo.gotParkIDs != nil {
		t.Fatalf("repository must not be reached when scope is denied, got parkIDs=%v", repo.gotParkIDs)
	}
}

// Omitting park_id means "every park I may monitor" — never every park that
// exists. A park-A monitor asking for the section gets park A only.
func TestGetGrowthDirectorWeightsOmittedParkIDUsesOnlyAuthorizedParks(t *testing.T) {
	repo := &fakeRepo{parks: []domain.Park{
		{ParkID: gdParkA, Name: "Coimbatore"},
		{ParkID: gdParkB, Name: "Channapatna"},
	}}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: gdParkA,
	})

	out, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), "", "", "", "", "")
	if err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}
	if len(repo.gotParkIDs) != 1 || repo.gotParkIDs[0] != gdParkA {
		t.Fatalf("expected repository scoped to park A only, got %v", repo.gotParkIDs)
	}
	if got := int(repo.gotEnd.Sub(repo.gotStart).Hours() / 24); got != domain.DefaultPeriodDays {
		t.Fatalf("omitted from/to default window=%d days, want %d", got, domain.DefaultPeriodDays)
	}
	// The park vocabulary is backend-owned AND scoped: offering park B here
	// would advertise a park this caller cannot read.
	if len(out.Parks) != 1 || out.Parks[0].ParkID != gdParkA {
		t.Fatalf("park vocabulary must be limited to authorized parks, got %+v", out.Parks)
	}
}

// A tenant-wide monitor omitting park_id covers every park in the tenant,
// resolved through ListParks rather than through the (empty) grant park list.
func TestGetGrowthDirectorWeightsTenantWideResolvesAllParks(t *testing.T) {
	repo := &fakeRepo{parks: []domain.Park{
		{ParkID: gdParkA, Name: "Coimbatore"},
		{ParkID: gdParkB, Name: "Channapatna"},
	}}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: gdTenant,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), "", "", "", "", ""); err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}
	if len(repo.gotParkIDs) != 2 {
		t.Fatalf("tenant-wide monitor must aggregate every park in the tenant, got %v", repo.gotParkIDs)
	}
}

// The window is a BUSINESS-DAY range in Asia/Kolkata, passed down half-open. A
// caller's inclusive last day must become an exclusive midnight boundary the
// day after, or the final day's weighs are silently dropped.
func TestGetGrowthDirectorWeightsPassesHalfOpenBusinessDayWindow(t *testing.T) {
	repo := &fakeRepo{parks: []domain.Park{{ParkID: gdParkA, Name: "Coimbatore"}}}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: gdTenant,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), gdParkA, "2026-07-01", "2026-07-28", "", ""); err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}
	if got := repo.gotStart.Format("2006-01-02"); got != "2026-07-01" {
		t.Fatalf("period start: want 2026-07-01, got %s", got)
	}
	// Exclusive boundary is the day AFTER the caller's inclusive last day.
	if got := repo.gotEnd.Format("2006-01-02"); got != "2026-07-29" {
		t.Fatalf("period end must be exclusive midnight after the last day: want 2026-07-29, got %s", got)
	}
}

func TestGetGrowthDirectorWeightsRejectsMalformedInput(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: gdTenant,
	})

	for _, tc := range []struct{ name, park, from, to string }{
		{"non-uuid park", "not-a-uuid", "", ""},
		{"non-date from", "", "01/07/2026", ""},
		{"non-date to", "", "", "2026-7-1"},
		{"inverted window", "", "2026-07-28", "2026-07-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), tc.park, tc.from, tc.to, "", ""); err != ports.ErrInvalidArgument {
				t.Fatalf("want ErrInvalidArgument, got %v", err)
			}
		})
	}
}

// A role without WeighingMonitor is refused before any scope work happens.
func TestGetGrowthDirectorWeightsRequiresMonitorCapability(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	ctx := gdContext()
	actor := domain.Actor{TenantID: gdTenant, Roles: []string{permissions.RoleOperator}}

	if _, err := svc.GetGrowthDirectorWeights(ctx, actor, "", "", "", "", ""); err != ports.ErrForbidden {
		t.Fatalf("want ErrForbidden for a non-monitor role, got %v", err)
	}
}

// Park-scoped grants that carry no monitor capability anywhere: the actor
// passed the flat role gate but owns no park here. An unrestricted read would
// be the escalation this path exists to prevent.
func TestGetGrowthDirectorWeightsNoAuthorizedParksIsNotFound(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	// A grant whose role does not carry WeighingMonitor: the actor claims the
	// role flatly but holds no park with the capability.
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleOperator, ScopeType: "park", ScopeID: gdParkA,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), "", "", "", "", ""); err != ports.ErrNotFound {
		t.Fatalf("want ErrNotFound for a monitor with no authorized park, got %v", err)
	}
	if repo.gotParkIDs != nil {
		t.Fatalf("repository must not be reached with an empty scope, got %v", repo.gotParkIDs)
	}
}
