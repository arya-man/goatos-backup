package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// The deworming feed & water removal precondition, at the app gate (maintainer decision
// 2026-09-03): validate-or-reject on the removal fields, the 20:00 IST planning cutoff on the
// service's injectable clock, and faithful pass-through of the honored fields to the store.

const (
	fastingPark    = "9c000000-0000-4000-8000-000000001001"
	fastingShed    = "9c000000-0000-4000-8000-000000001002"
	fastingRemover = "9c000000-0000-4000-8000-00000000ffff"
)

// fakeCreateStore captures the CreateTaskParams the service hands the store.
type fakeCreateStore struct {
	ports.TaskStore
	createCalls int
	lastCreate  ports.CreateTaskParams
	lastList    ports.ListTasksQuery
}

func (f *fakeCreateStore) CreateTask(_ context.Context, p ports.CreateTaskParams) (ports.TaskRow, error) {
	f.createCalls++
	f.lastCreate = p
	return ports.TaskRow{TaskID: testTask, Category: p.Category}, nil
}

func (f *fakeCreateStore) ListTasks(_ context.Context, q ports.ListTasksQuery) (ports.TaskPage, error) {
	f.lastList = q
	return ports.TaskPage{}, nil
}

func plannerCtx() context.Context {
	return httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{
		Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: testTenant,
	}})
}

func plannerActor() domain.Actor {
	return domain.Actor{TenantID: testTenant, UserID: "ceo-1", Roles: []string{permissions.RoleCEOInternal}}
}

func dewormingCreateInput(planned string) CreateTaskInput {
	return CreateTaskInput{
		Category:            domain.CategoryDeworming,
		ParkID:              fastingPark,
		ShedID:              fastingShed,
		PlannedBusinessDate: planned,
		AssigneeUserIDs:     []string{testAssignee},
		IdempotencyKey:      "pc-deworming-fasting-create",
		ActorID:             "ceo-1",
		ActorType:           "human",
	}
}

// pinnedIST returns a deterministic wall-clock instant in the Goat OS business calendar.
func pinnedIST(day, hour, minute int) time.Time {
	return time.Date(2026, time.September, day, hour, minute, 0, 0, biztime.DefaultLocation())
}

func TestCreateTaskFeedRemovalRequiresRemovalOperators(t *testing.T) {
	store := &fakeCreateStore{}
	svc := NewService(store).WithNow(func() time.Time { return pinnedIST(10, 9, 0) })
	in := dewormingCreateInput("2026-09-11")
	in.FeedRemovalRequired = true // and NO removal operators

	_, err := svc.CreateTask(plannerCtx(), plannerActor(), in)
	if !errors.Is(err, domain.ErrRemovalOperatorsRequired) {
		t.Fatalf("err = %v, want ErrRemovalOperatorsRequired", err)
	}
	if store.createCalls != 0 {
		t.Fatal("store.CreateTask must not run on a rejected removal request")
	}
}

func TestCreateTaskFeedRemovalOnNonDewormingIsRejected(t *testing.T) {
	store := &fakeCreateStore{}
	svc := NewService(store).WithNow(func() time.Time { return pinnedIST(10, 9, 0) })

	in := dewormingCreateInput("2026-09-11")
	in.Category = domain.CategoryTicksRemoval
	in.FeedRemovalRequired = true
	in.RemovalOperatorUserIDs = []string{fastingRemover}
	if _, err := svc.CreateTask(plannerCtx(), plannerActor(), in); !errors.Is(err, domain.ErrFeedRemovalNotApplicable) {
		t.Fatalf("toggle on ticks_removal err = %v, want ErrFeedRemovalNotApplicable", err)
	}

	// Removal operators with the toggle OFF are rejected too — silently dropping a field the
	// planner filled is banned (validate-or-reject).
	in2 := dewormingCreateInput("2026-09-11")
	in2.RemovalOperatorUserIDs = []string{fastingRemover}
	if _, err := svc.CreateTask(plannerCtx(), plannerActor(), in2); !errors.Is(err, domain.ErrFeedRemovalNotApplicable) {
		t.Fatalf("operators without toggle err = %v, want ErrFeedRemovalNotApplicable", err)
	}
	if store.createCalls != 0 {
		t.Fatal("store.CreateTask must not run on rejected removal fields")
	}
}

// The 20:00 IST cutoff on a pinned clock: before 20:00 tomorrow is plannable; from 20:00 the
// earliest is the day after tomorrow. Business-DAY comparisons, fixed dates, one pinned anchor.
func TestCreateTaskFeedRemovalEveningCutoff(t *testing.T) {
	cases := []struct {
		name    string
		now     time.Time
		planned string
		wantErr bool
	}{
		{"19:00 IST, tomorrow", pinnedIST(10, 19, 0), "2026-09-11", false},
		{"19:00 IST, today", pinnedIST(10, 19, 0), "2026-09-10", true},
		{"20:00 IST, tomorrow", pinnedIST(10, 20, 0), "2026-09-11", true},
		{"20:00 IST, day after tomorrow", pinnedIST(10, 20, 0), "2026-09-12", false},
	}
	for _, tc := range cases {
		store := &fakeCreateStore{}
		svc := NewService(store).WithNow(func() time.Time { return tc.now })
		in := dewormingCreateInput(tc.planned)
		in.FeedRemovalRequired = true
		in.RemovalOperatorUserIDs = []string{fastingRemover}

		_, err := svc.CreateTask(plannerCtx(), plannerActor(), in)
		if tc.wantErr {
			if !errors.Is(err, domain.ErrFastingWindowClosed) {
				t.Fatalf("%s: err = %v, want ErrFastingWindowClosed", tc.name, err)
			}
			if store.createCalls != 0 {
				t.Fatalf("%s: store.CreateTask must not run past the cutoff", tc.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected err %v", tc.name, err)
		}
		if store.createCalls != 1 {
			t.Fatalf("%s: store.CreateTask calls = %d, want 1", tc.name, store.createCalls)
		}
		if !store.lastCreate.FeedRemovalRequired {
			t.Fatalf("%s: FeedRemovalRequired must reach the store", tc.name)
		}
		if len(store.lastCreate.RemovalOperatorUserIDs) != 1 || store.lastCreate.RemovalOperatorUserIDs[0] != fastingRemover {
			t.Fatalf("%s: removal operators = %v, want [%s]", tc.name, store.lastCreate.RemovalOperatorUserIDs, fastingRemover)
		}
	}
}

// A deworming WITHOUT the toggle is untouched by the cutoff — planning today stays legal.
func TestCreateTaskWithoutFeedRemovalIgnoresTheEveningCutoff(t *testing.T) {
	store := &fakeCreateStore{}
	svc := NewService(store).WithNow(func() time.Time { return pinnedIST(10, 21, 0) })
	if _, err := svc.CreateTask(plannerCtx(), plannerActor(), dewormingCreateInput("2026-09-10")); err != nil {
		t.Fatalf("plain deworming create err = %v, want nil", err)
	}
	if store.lastCreate.FeedRemovalRequired || len(store.lastCreate.RemovalOperatorUserIDs) != 0 {
		t.Fatalf("plain create must carry no removal fields, got %+v", store.lastCreate)
	}
}

// feed_water_removal is system-owned: a planner naming it directly is refused.
func TestCreateTaskPlannerCannotNameFeedWaterRemoval(t *testing.T) {
	store := &fakeCreateStore{}
	svc := NewService(store)
	in := dewormingCreateInput("2026-09-11")
	in.Category = domain.CategoryFeedWaterRemoval
	if _, err := svc.CreateTask(plannerCtx(), plannerActor(), in); !errors.Is(err, domain.ErrKernelOwnedCategory) {
		t.Fatalf("err = %v, want ErrKernelOwnedCategory", err)
	}
}

// The list reads carry the SERVICE's clock into the store query, so the 20:00 IST visibility
// predicate is deterministic and can never drift to a DB-side now().
func TestListReadsCarryTheCallersClock(t *testing.T) {
	pinned := pinnedIST(10, 20, 30)
	store := &fakeCreateStore{}
	svc := NewService(store).WithNow(func() time.Time { return pinned })

	if _, err := svc.ListTasks(plannerCtx(), plannerActor(), "", "", "2026-09-10", "", 25, false); err != nil {
		t.Fatalf("ListTasks err = %v", err)
	}
	if !store.lastList.Now.Equal(pinned) {
		t.Fatalf("ListTasks Now = %v, want the pinned service clock %v", store.lastList.Now, pinned)
	}

	opCtx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{
		Role: permissions.RoleOperator, ScopeType: "park", ScopeID: fastingPark,
	}})
	if _, err := svc.Worklist(opCtx, operatorActor(testAssignee), domain.CategoryFeedWaterRemoval, "2026-09-10", "", 25); err != nil {
		t.Fatalf("Worklist err = %v", err)
	}
	if !store.lastList.Now.Equal(pinned) {
		t.Fatalf("Worklist Now = %v, want the pinned service clock %v", store.lastList.Now, pinned)
	}
}

// The removal videos are validated as live-camera VIDEOS on the task-proof register path.
func TestRegisterTaskProofRequiresVideoKindForRemovalSlots(t *testing.T) {
	store := &fakeStore{assignees: map[string]bool{testAssignee: true}}
	validator := &fakeProofValidator{}
	svc := NewService(store).WithProofValidator(validator)

	err := svc.RegisterTaskProof(context.Background(), operatorActor(testAssignee), RegisterTaskProofInput{
		TaskID:         testTask,
		SlotKey:        domain.SlotFeedVideo,
		ProofRef:       "proof-feed-1",
		IdempotencyKey: "pc-removal-feed-proof-1",
	})
	if err != nil {
		t.Fatalf("RegisterTaskProof err = %v", err)
	}
	if validator.kindCalls != 1 || validator.lastKind != "video" {
		t.Fatalf("kindCalls=%d lastKind=%q, want one 'video' kind validation", validator.kindCalls, validator.lastKind)
	}

	err = svc.RegisterTaskProof(context.Background(), operatorActor(testAssignee), RegisterTaskProofInput{
		TaskID:         testTask,
		SlotKey:        domain.SlotWaterVideo,
		ProofRef:       "proof-water-1",
		IdempotencyKey: "pc-removal-water-proof-1",
	})
	if err != nil {
		t.Fatalf("RegisterTaskProof water err = %v", err)
	}
	if validator.kindCalls != 2 || validator.lastKind != "video" {
		t.Fatalf("kindCalls=%d lastKind=%q, want 'video' for the water slot too", validator.kindCalls, validator.lastKind)
	}
}
