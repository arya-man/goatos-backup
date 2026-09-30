package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// The fumigation-planner carve-out (maintainer instruction 2026-09-30): park heads, the Breeding
// Director and the Health Director plan FUMIGATION -- the pen disinfectant spray -- and nothing
// else of PC Care they did not already plan; the CEO plans it through pc_care.plan. Driven
// through the SERVICE, where the category is decided.

func parkHeadActor() domain.Actor {
	return domain.Actor{TenantID: testTenant, UserID: trimmingUser, Roles: []string{permissions.RoleParkHead}}
}

func healthDirectorActor() domain.Actor {
	return domain.Actor{TenantID: testTenant, UserID: trimmingUser, Roles: []string{permissions.RoleHealthDirector}}
}

func TestFumigationIsPlannedByParkHeadsAndTheBreedingAndHealthDirectors(t *testing.T) {
	for name, actor := range map[string]domain.Actor{
		"park_head":         parkHeadActor(),
		"health_director":   healthDirectorActor(),
		"breeding_director": breedingDirectorActor(),
		"ceo_internal":      ceoActor(),
	} {
		store := &plannerFakeStore{}
		svc := NewService(store).WithFeedWaterRemovalCutoff(eightPM)
		if _, err := svc.CreateTask(context.Background(), actor, createInput(domain.CategoryFumigation)); err != nil {
			t.Fatalf("%s plans fumigation: %v, want success", name, err)
		}
		if len(store.created) != 1 || store.created[0].FeedRemovalRequired {
			t.Fatalf("%s: store saw %+v, want one fumigation create with no feed & water removal", name, store.created)
		}
		// The create snapshots the pen card's two videos as the task's compulsory captures.
		if got := store.created[0].RequiredSlotKeys; len(got) != 2 || got[0] != domain.SlotMixingVideo || got[1] != domain.SlotSprayingVideo {
			t.Fatalf("%s: required slots %v, want [mixing_video spraying_video]", name, got)
		}
	}

	// The fumigation desks are refused every category that is not theirs -- the same answer a
	// non-planner gets.
	for name, actor := range map[string]domain.Actor{"park_head": parkHeadActor(), "health_director": healthDirectorActor()} {
		for _, category := range []string{domain.CategoryDeworming, domain.CategoryAntiProtozoan, domain.CategoryTicksRemoval, domain.CategoryHoofTrimming, domain.CategoryHairTrimming} {
			store := &plannerFakeStore{}
			svc := NewService(store).WithFeedWaterRemovalCutoff(eightPM)
			if _, err := svc.CreateTask(context.Background(), actor, createInput(category)); !errors.Is(err, ports.ErrForbidden) {
				t.Fatalf("%s plans %s: err=%v, want ErrForbidden", name, category, err)
			}
			if len(store.created) != 0 {
				t.Fatalf("%s %s reached the store", name, category)
			}
		}
	}
	// The PC Director still plans nothing -- fumigation included.
	store := &plannerFakeStore{}
	svc := NewService(store).WithFeedWaterRemovalCutoff(eightPM)
	pcDirector := domain.Actor{TenantID: testTenant, UserID: trimmingUser, Roles: []string{permissions.RolePCDirector}}
	if _, err := svc.CreateTask(context.Background(), pcDirector, createInput(domain.CategoryFumigation)); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("pc_director plans fumigation: err=%v, want ErrForbidden", err)
	}
}

// Fumigation needs no feed & water removal: asking for one is refused by name, never silently
// dropped, and the task may be planned for TODAY (there is no evening cutoff to miss).
func TestFumigationCarriesNoFeedRemovalAndMayBePlannedForToday(t *testing.T) {
	store := &plannerFakeStore{}
	svc := NewService(store).WithFeedWaterRemovalCutoff(eightPM)
	in := createInput(domain.CategoryFumigation)
	yes := true
	in.FeedRemovalRequested = &yes
	in.RemovalOperatorUserIDs = []string{testAssignee}
	if _, err := svc.CreateTask(context.Background(), parkHeadActor(), in); !errors.Is(err, domain.ErrFeedRemovalNotApplicable) {
		t.Fatalf("fumigation with removal: err=%v, want ErrFeedRemovalNotApplicable", err)
	}

	// 21:00 IST on the day itself -- past the 20:00 removal evening -- a fumigation for TODAY
	// is still planned.
	svc = svc.WithNow(func() time.Time { return time.Date(2026, 9, 10, 15, 30, 0, 0, time.UTC) })
	if _, err := svc.CreateTask(context.Background(), parkHeadActor(), createInput(domain.CategoryFumigation)); err != nil {
		t.Fatalf("fumigation planned for today after the evening cutoff: %v, want success", err)
	}
}

// A park head plans fumigation in their own park only: the grant's park scope decides, and
// another park is the existence-hiding NOT FOUND.
func TestFumigationPlannerIsParkScopedByItsOwnGrant(t *testing.T) {
	store := &plannerFakeStore{}
	svc := NewService(store).WithFeedWaterRemovalCutoff(eightPM)
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{
		Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: trimmingPark,
	}})
	if _, err := svc.CreateTask(ctx, parkHeadActor(), createInput(domain.CategoryFumigation)); err != nil {
		t.Fatalf("park head plans fumigation in own park: %v", err)
	}
	in := createInput(domain.CategoryFumigation)
	in.ParkID = otherPark
	if _, err := svc.CreateTask(ctx, parkHeadActor(), in); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("park head plans fumigation in another park: err=%v, want ErrNotFound", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("store saw %d creates, want 1", len(store.created))
	}
}

// The planner catalog offers each desk exactly what it may plan, so the phone's wizard never
// offers a category the create would refuse.
func TestPlannerCatalogOffersFumigationDesksOnlyFumigation(t *testing.T) {
	store := &plannerFakeStore{catalogParks: []ports.PlannerPark{{ParkID: trimmingPark, ParkName: "CPT"}}}
	svc := NewService(store).WithFeedWaterRemovalCutoff(eightPM)
	for name, actor := range map[string]domain.Actor{"park_head": parkHeadActor(), "health_director": healthDirectorActor()} {
		got, err := svc.PlannerCatalog(context.Background(), actor)
		if err != nil {
			t.Fatalf("%s catalog: %v", name, err)
		}
		if len(got.Categories) != 1 || got.Categories[0] != domain.CategoryFumigation {
			t.Fatalf("%s categories = %v, want exactly [fumigation]", name, got.Categories)
		}
	}
}
