package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// The trimming-planner carve-out (maintainer decision 2026-09-04): a holder of
// pc_care.plan_trimming -- the Breeding Director -- plans hoof and hair trimming and is refused
// every other category on create, cancel and the pen pager, while a pc_care.plan holder (CEO) is
// unchanged. These tests drive the SERVICE, which is where the category is decided; the route
// table only admits the holder.

const (
	trimmingPark  = "9c000000-0000-4000-8000-00000000a001"
	otherPark     = "9c000000-0000-4000-8000-00000000a002"
	trimmingShed  = "9c000000-0000-4000-8000-00000000b001"
	trimmingUser  = "9c000000-0000-4000-8000-00000000c001"
	trimmingTask  = "9c000000-0000-4000-8000-00000000d001"
	dewormingTask = "9c000000-0000-4000-8000-00000000d002"
)

// plannerFakeStore records planner writes and serves two tasks: a hoof-trimming one and a
// deworming one, both in trimmingPark.
type plannerFakeStore struct {
	fakeStore
	created       []ports.CreateTaskParams
	canceled      []string
	shedPages     []string
	catalogParks  []ports.PlannerPark
	catalogCalled int
}

func (f *plannerFakeStore) CreateTask(_ context.Context, p ports.CreateTaskParams) (ports.TaskRow, error) {
	f.created = append(f.created, p)
	return ports.TaskRow{TaskID: trimmingTask, Category: p.Category, ParkID: p.ParkID}, nil
}

func (f *plannerFakeStore) CancelTask(_ context.Context, _, taskID, _, _ string) error {
	f.canceled = append(f.canceled, taskID)
	return nil
}

func (f *plannerFakeStore) GetTask(_ context.Context, _, taskID string, _ []string, _ bool) (ports.TaskRow, error) {
	switch taskID {
	case trimmingTask:
		return ports.TaskRow{TaskID: trimmingTask, Category: domain.CategoryHoofTrimming, ParkID: trimmingPark}, nil
	case dewormingTask:
		return ports.TaskRow{TaskID: dewormingTask, Category: domain.CategoryDeworming, ParkID: trimmingPark}, nil
	}
	return ports.TaskRow{}, ports.ErrNotFound
}

func (f *plannerFakeStore) PlannerCatalog(_ context.Context, _ string) (ports.PlannerCatalog, error) {
	f.catalogCalled++
	return ports.PlannerCatalog{Parks: f.catalogParks}, nil
}

func (f *plannerFakeStore) PlannerParkSheds(_ context.Context, _, _, category, _, _ string, _ int) (ports.PlannerParkSheds, error) {
	f.shedPages = append(f.shedPages, category)
	return ports.PlannerParkSheds{}, nil
}

func breedingDirectorActor() domain.Actor {
	return domain.Actor{TenantID: testTenant, UserID: trimmingUser, Roles: []string{permissions.RoleBreedingDirector}}
}

func ceoActor() domain.Actor {
	return domain.Actor{TenantID: testTenant, UserID: trimmingUser, Roles: []string{permissions.RoleCEOInternal}}
}

func createInput(category string) CreateTaskInput {
	return CreateTaskInput{
		Category:            category,
		ParkID:              trimmingPark,
		ShedID:              trimmingShed,
		PlannedBusinessDate: "2026-09-10",
		AssigneeUserIDs:     []string{testAssignee},
		IdempotencyKey:      "plan-" + category,
	}
}

// TestCreateTaskHonoursTheTrimmingPlannerCarveOut: the Breeding Director creates hoof and hair
// trimming, and is refused deworming and ticks removal with ErrForbidden -- the same answer a
// non-planner gets, because to him those categories are not his to plan. The CEO still plans
// all four.
func TestCreateTaskHonoursTheTrimmingPlannerCarveOut(t *testing.T) {
	store := &plannerFakeStore{}
	svc := NewService(store)
	ctx := context.Background()

	for _, category := range domain.TrimmingCategories {
		if _, err := svc.CreateTask(ctx, breedingDirectorActor(), createInput(category)); err != nil {
			t.Fatalf("breeding_director create %s: %v, want success", category, err)
		}
	}
	for _, category := range []string{domain.CategoryDeworming, domain.CategoryTicksRemoval} {
		_, err := svc.CreateTask(ctx, breedingDirectorActor(), createInput(category))
		if !errors.Is(err, ports.ErrForbidden) {
			t.Fatalf("breeding_director create %s: err=%v, want ErrForbidden", category, err)
		}
	}
	if len(store.created) != len(domain.TrimmingCategories) {
		t.Fatalf("store saw %d creates, want exactly the %d trimming ones", len(store.created), len(domain.TrimmingCategories))
	}

	// The whole-module planner is unchanged by the carve-out.
	store.created = nil
	for _, category := range domain.PlannerCategories {
		if _, err := svc.CreateTask(ctx, ceoActor(), createInput(category)); err != nil {
			t.Fatalf("ceo create %s: %v, want success", category, err)
		}
	}
	if len(store.created) != len(domain.PlannerCategories) {
		t.Fatalf("ceo created %d tasks, want %d", len(store.created), len(domain.PlannerCategories))
	}

	// A refused category is refused as NOT YOURS, not as unknown: an invalid category is
	// still the invalid-category error, so the two failures stay distinguishable.
	if _, err := svc.CreateTask(ctx, breedingDirectorActor(), createInput("shearing")); !errors.Is(err, domain.ErrInvalidCategory) {
		t.Fatalf("unknown category err=%v, want ErrInvalidCategory", err)
	}
}

// TestCancelTaskHonoursTheTrimmingPlannerCarveOut: cancel is judged on the TASK's category,
// read from the store -- the caller never names it.
func TestCancelTaskHonoursTheTrimmingPlannerCarveOut(t *testing.T) {
	store := &plannerFakeStore{}
	svc := NewService(store)
	ctx := context.Background()

	if err := svc.CancelTask(ctx, breedingDirectorActor(), trimmingTask, "trace"); err != nil {
		t.Fatalf("breeding_director cancel hoof-trimming task: %v, want success", err)
	}
	if err := svc.CancelTask(ctx, breedingDirectorActor(), dewormingTask, "trace"); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("breeding_director cancel deworming task: err=%v, want ErrForbidden", err)
	}
	if len(store.canceled) != 1 || store.canceled[0] != trimmingTask {
		t.Fatalf("store canceled %v, want only the hoof-trimming task", store.canceled)
	}
	if err := svc.CancelTask(ctx, ceoActor(), dewormingTask, "trace"); err != nil {
		t.Fatalf("ceo cancel deworming task: %v, want success", err)
	}
}

// TestPlannerParkShedsHonoursTheTrimmingPlannerCarveOut: the pen pager is a planning surface, so
// a trimming planner may page pens for trimming and not for deworming; a read-only monitor, who
// plans nothing, keeps the look at every category it had before.
func TestPlannerParkShedsHonoursTheTrimmingPlannerCarveOut(t *testing.T) {
	store := &plannerFakeStore{}
	svc := NewService(store)
	ctx := context.Background()

	if _, err := svc.PlannerParkSheds(ctx, breedingDirectorActor(), trimmingPark, domain.CategoryHairTrimming, "2026-09-10", "", 25); err != nil {
		t.Fatalf("breeding_director pages hair-trimming pens: %v", err)
	}
	if _, err := svc.PlannerParkSheds(ctx, breedingDirectorActor(), trimmingPark, domain.CategoryDeworming, "2026-09-10", "", 25); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("breeding_director pages deworming pens: err=%v, want ErrForbidden", err)
	}
	monitor := domain.Actor{TenantID: testTenant, UserID: trimmingUser, Roles: []string{permissions.RolePCDirector}}
	if _, err := svc.PlannerParkSheds(ctx, monitor, trimmingPark, domain.CategoryDeworming, "2026-09-10", "", 25); err != nil {
		t.Fatalf("pc_director (monitor) pages deworming pens: %v, want the read it always had", err)
	}
	if len(store.shedPages) != 2 {
		t.Fatalf("store paged %v, want the two admitted reads only", store.shedPages)
	}
}

// TestPlannerCatalogNarrowsCategoriesToWhatTheCallerMayPlan: the wizard's category dropdown
// comes ONLY from this list, so narrowing it here is what keeps the phone from offering a
// category the create write would refuse. A monitor with no planning capability keeps the full
// planner list (the wizard is not offered to them; the list still labels the board's filter).
func TestPlannerCatalogNarrowsCategoriesToWhatTheCallerMayPlan(t *testing.T) {
	store := &plannerFakeStore{catalogParks: []ports.PlannerPark{{ParkID: trimmingPark, ParkName: "CPT"}}}
	svc := NewService(store)
	ctx := context.Background()

	got, err := svc.PlannerCatalog(ctx, breedingDirectorActor())
	if err != nil {
		t.Fatalf("breeding_director catalog: %v", err)
	}
	if len(got.Categories) != 2 || got.Categories[0] != domain.CategoryHoofTrimming || got.Categories[1] != domain.CategoryHairTrimming {
		t.Fatalf("breeding_director categories = %v, want exactly [hoof_trimming hair_trimming]", got.Categories)
	}

	got, err = svc.PlannerCatalog(ctx, ceoActor())
	if err != nil {
		t.Fatalf("ceo catalog: %v", err)
	}
	if len(got.Categories) != len(domain.PlannerCategories) {
		t.Fatalf("ceo categories = %v, want every planner category", got.Categories)
	}

	monitor := domain.Actor{TenantID: testTenant, UserID: trimmingUser, Roles: []string{permissions.RolePCDirector}}
	got, err = svc.PlannerCatalog(ctx, monitor)
	if err != nil {
		t.Fatalf("pc_director catalog: %v", err)
	}
	if len(got.Categories) != len(domain.PlannerCategories) {
		t.Fatalf("pc_director (monitor) categories = %v, want the full planner list it always had", got.Categories)
	}
}

// TestTrimmingPlannerIsParkScopedByItsOwnGrant: a park-scoped breeding_director grant plans
// trimming in its park and is told NOT FOUND (the existence-hiding answer) in another -- and a
// pc_care.plan grant elsewhere does not widen a trimming write, because the park check runs
// against the capabilities that authorize THIS category.
func TestTrimmingPlannerIsParkScopedByItsOwnGrant(t *testing.T) {
	store := &plannerFakeStore{}
	svc := NewService(store)
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{
		Role:      permissions.RoleBreedingDirector,
		ScopeType: "park",
		ScopeID:   trimmingPark,
	}})

	if _, err := svc.CreateTask(ctx, breedingDirectorActor(), createInput(domain.CategoryHoofTrimming)); err != nil {
		t.Fatalf("park-scoped breeding_director creates in own park: %v", err)
	}
	in := createInput(domain.CategoryHoofTrimming)
	in.ParkID = otherPark
	if _, err := svc.CreateTask(ctx, breedingDirectorActor(), in); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("park-scoped breeding_director creates in another park: err=%v, want ErrNotFound", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("store saw %d creates, want 1", len(store.created))
	}
}
