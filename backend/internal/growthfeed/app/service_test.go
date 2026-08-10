package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthfeed/domain"
	"github.com/vgoats/goatos/backend/internal/growthfeed/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

const (
	tenantID = "11111111-1111-1111-1111-111111111111"
	parkA    = "22222222-2222-2222-2222-222222222222"
	parkB    = "33333333-3333-3333-3333-333333333333"
	penA     = "44444444-4444-4444-4444-444444444444"
	penB     = "55555555-5555-5555-5555-555555555555"
)

type fakeRepo struct {
	growth     []ports.PenGrowth
	cohorts    []ports.PenCohortRation
	parks      []string
	sawParkIDs []string
	sawAsOf    string
	sawLocIDs  []string
}

func (f *fakeRepo) ListPenGrowth(_ context.Context, _ string, parkIDs []string, _, _ time.Time) ([]ports.PenGrowth, error) {
	f.sawParkIDs = parkIDs
	return f.growth, nil
}

func (f *fakeRepo) ListPenCohortRation(_ context.Context, _ string, _, locationIDs []string, asOfDate string) ([]ports.PenCohortRation, error) {
	f.sawLocIDs, f.sawAsOf = locationIDs, asOfDate
	return f.cohorts, nil
}

func (f *fakeRepo) ListParkIDs(context.Context, string) ([]string, error) { return f.parks, nil }

func tenantCtx(role string) context.Context {
	return httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: role, ScopeType: "tenant", ScopeID: tenantID},
	})
}

func parkCtx(role, parkID string) context.Context {
	return httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: role, ScopeType: "park", ScopeID: parkID},
	})
}

// THE regression this test exists for. RolesAuthorize ANDs its list; using it
// here would demand weighing.monitor AND feed_config.read and lock out BOTH
// directors the screen is built for — each holds exactly one of them.
func TestBothDirectorsCanReadTheComparison(t *testing.T) {
	for _, role := range []string{
		permissions.RoleGrowthDirector, // weighing.monitor, no feed config read
		permissions.RoleFeedDirector,   // feed config read, no weighing.monitor
		permissions.RoleCEOInternal,
	} {
		repo := &fakeRepo{parks: []string{parkA}}
		svc := NewService(repo)

		_, err := svc.GetPenGrowthFeed(tenantCtx(role), Actor{TenantID: tenantID, Roles: []string{role}}, "", "", "")
		if err != nil {
			t.Fatalf("role %s was refused: %v", role, err)
		}
	}
}

func TestOperatorCannotReadTheComparison(t *testing.T) {
	repo := &fakeRepo{parks: []string{parkA}}
	svc := NewService(repo)

	_, err := svc.GetPenGrowthFeed(
		tenantCtx(permissions.RoleOperator),
		Actor{TenantID: tenantID, Roles: []string{permissions.RoleOperator}}, "", "", "")

	if !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

// A park-scoped caller must never have their scope widened to the tenant on the
// strength of the flat role check alone.
func TestParkScopedCallerIsNeverWidenedToEveryPark(t *testing.T) {
	repo := &fakeRepo{parks: []string{parkA, parkB}}
	svc := NewService(repo)

	_, err := svc.GetPenGrowthFeed(
		parkCtx(permissions.RoleGrowthDirector, parkA),
		Actor{TenantID: tenantID, Roles: []string{permissions.RoleGrowthDirector}}, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.sawParkIDs) != 1 || repo.sawParkIDs[0] != parkA {
		t.Fatalf("scope = %v, want only %s", repo.sawParkIDs, parkA)
	}
}

// Asking for a park outside the caller's grants is NOT FOUND, not forbidden:
// a 403 would confirm the park exists.
func TestParkOutsideScopeIsNotFound(t *testing.T) {
	repo := &fakeRepo{parks: []string{parkA, parkB}}
	svc := NewService(repo)

	_, err := svc.GetPenGrowthFeed(
		parkCtx(permissions.RoleGrowthDirector, parkA),
		Actor{TenantID: tenantID, Roles: []string{permissions.RoleGrowthDirector}}, parkB, "", "")

	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// The ration is read as of the INCLUSIVE last day of the window — the rate in
// force when those gains were being produced, not the day after it.
func TestRationIsResolvedAsOfTheWindowsLastDay(t *testing.T) {
	repo := &fakeRepo{
		parks:  []string{parkA},
		growth: []ports.PenGrowth{{ParkID: parkA, LocationID: penA, ShedName: "Castro"}},
	}
	svc := NewService(repo)

	out, err := svc.GetPenGrowthFeed(
		tenantCtx(permissions.RoleGrowthDirector),
		Actor{TenantID: tenantID, Roles: []string{permissions.RoleGrowthDirector}},
		"", "2026-07-01", "2026-07-28")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.sawAsOf != "2026-07-28" {
		t.Fatalf("ration as-of = %q, want 2026-07-28", repo.sawAsOf)
	}
	if out.PeriodStart != "2026-07-01" || out.PeriodEnd != "2026-07-28" {
		t.Fatalf("period = %s..%s, want 2026-07-01..2026-07-28", out.PeriodStart, out.PeriodEnd)
	}
}

// A pen that was weighed but resolves to no live animal stays in the table. It is
// a real pen with real weights; dropping it would misrepresent coverage.
func TestWeighedPenWithNoHerdCohortIsKeptAndFlagged(t *testing.T) {
	repo := &fakeRepo{
		parks: []string{parkA},
		growth: []ports.PenGrowth{
			{ParkID: parkA, LocationID: penA, ShedName: "Castro", AnimalsWeighed: 12},
		},
		cohorts: nil, // the herd register knows nothing about this pen
	}
	svc := NewService(repo)

	out, err := svc.GetPenGrowthFeed(
		tenantCtx(permissions.RoleGrowthDirector),
		Actor{TenantID: tenantID, Roles: []string{permissions.RoleGrowthDirector}}, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Rows) != 1 {
		t.Fatalf("rows = %d, want the weighed pen kept", len(out.Rows))
	}
	if out.Rows[0].FeedPlanStatus != domain.FeedPlanUnknownCohort {
		t.Fatalf("status = %q, want %q", out.Rows[0].FeedPlanStatus, domain.FeedPlanUnknownCohort)
	}
	if out.PensWithoutCohort != 1 {
		t.Fatalf("pens_without_cohort = %d, want 1", out.PensWithoutCohort)
	}
}

// The location display is composed by the backend through oploc. A renderer that
// joins the two halves itself produces "Godel 1 1" (OL-3).
func TestOperationalLocationDisplayIsComposedByTheBackend(t *testing.T) {
	part := "Part 3"
	repo := &fakeRepo{
		parks: []string{parkA},
		growth: []ports.PenGrowth{
			{ParkID: parkA, LocationID: penA, ShedName: "Godel 1", PartitionLabel: &part},
			{ParkID: parkA, LocationID: penB, ShedName: "Yashoda"},
		},
	}
	svc := NewService(repo)

	out, _ := svc.GetPenGrowthFeed(
		tenantCtx(permissions.RoleGrowthDirector),
		Actor{TenantID: tenantID, Roles: []string{permissions.RoleGrowthDirector}}, "", "", "")

	if got := out.Rows[0].OperationalLocationDisplay; got != "Godel 1 - Part 3" {
		t.Fatalf("partitioned display = %q, want %q", got, "Godel 1 - Part 3")
	}
	// An undivided shed keeps its bare name — never a dangling separator.
	if got := out.Rows[1].OperationalLocationDisplay; got != "Yashoda" {
		t.Fatalf("undivided display = %q, want %q", got, "Yashoda")
	}
}

// Two partitions of one physical shed must ask the cohort question ONCE, not
// twice, and both must still receive the answer.
func TestPartitionsOfOneShedResolveTheCohortOnce(t *testing.T) {
	p1, p2 := "1", "2"
	repo := &fakeRepo{
		parks: []string{parkA},
		growth: []ports.PenGrowth{
			{ParkID: parkA, LocationID: penA, ShedName: "Castro", PartitionLabel: &p1},
			{ParkID: parkA, LocationID: penA, ShedName: "Castro", PartitionLabel: &p2},
		},
		cohorts: []ports.PenCohortRation{{
			LocationID: penA, Breed: "Sirohi", Stage: "Grower", LiveAnimals: 60,
		}},
	}
	svc := NewService(repo)

	out, _ := svc.GetPenGrowthFeed(
		tenantCtx(permissions.RoleGrowthDirector),
		Actor{TenantID: tenantID, Roles: []string{permissions.RoleGrowthDirector}}, "", "", "")

	if len(repo.sawLocIDs) != 1 {
		t.Fatalf("cohort lookup asked for %v, want one deduplicated location", repo.sawLocIDs)
	}
	if len(out.Rows) != 2 {
		t.Fatalf("rows = %d, want both partitions", len(out.Rows))
	}
	for _, row := range out.Rows {
		if row.Breed == nil || *row.Breed != "Sirohi" {
			t.Fatalf("partition %v lost its cohort", row.PartitionLabel)
		}
	}
}

// An out-of-order window is rejected rather than quietly returning nothing.
func TestReversedWindowIsRejected(t *testing.T) {
	svc := NewService(&fakeRepo{parks: []string{parkA}})

	_, err := svc.GetPenGrowthFeed(
		tenantCtx(permissions.RoleGrowthDirector),
		Actor{TenantID: tenantID, Roles: []string{permissions.RoleGrowthDirector}},
		"", "2026-07-28", "2026-07-01")

	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}
}
