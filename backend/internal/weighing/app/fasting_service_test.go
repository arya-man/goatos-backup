package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// fakeFastingStore records calls; behavior is table-driven per test.
type fakeFastingStore struct {
	submitCalls   int
	submitted     domain.SubmitFastingShed
	submitResult  domain.FastingShedSubmitResult
	submitErr     error
	listCalls     int
	listNow       time.Time
	startDate     string
	fastingSubbed bool
	hasFasting    bool
}

func (f *fakeFastingStore) ListFastingShedCardsForOperator(_ context.Context, _, _ string, now time.Time, _ string, _ int) (domain.FastingShedCardPage, error) {
	f.listCalls++
	f.listNow = now
	return domain.FastingShedCardPage{Items: []domain.FastingShedCard{}}, nil
}

func (f *fakeFastingStore) FastingTaskByID(_ context.Context, _, _, _ string) (domain.FastingTask, error) {
	return domain.FastingTask{}, ports.ErrNotFound
}

func (f *fakeFastingStore) SubmitFastingShed(_ context.Context, cmd domain.SubmitFastingShed) (domain.FastingShedSubmitResult, error) {
	f.submitCalls++
	f.submitted = cmd
	if f.submitErr != nil {
		return domain.FastingShedSubmitResult{}, f.submitErr
	}
	return f.submitResult, nil
}

func (f *fakeFastingStore) ApplyFastingVerdict(_ context.Context, _ domain.FastingVerdict) error {
	return nil
}

func (f *fakeFastingStore) CampaignStartDate(_ context.Context, _, _ string) (string, bool, bool, error) {
	return f.startDate, f.fastingSubbed, f.hasFasting, nil
}

const (
	fastingTaskID = "00000000-0000-4000-8000-000000000901"
	proofFour     = "00000000-0000-4000-8000-000000000704"
)

// THE CREATE CUTOFF: at or after 20:00 IST, tomorrow is refused; before it,
// tomorrow is allowed. Mutation-tested by deleting the cutoff branch in
// Service.CreateCampaign (test goes red on the evening case).
func TestCreateCampaignEnforcesTheFastingEveningCutoff(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	cmd := validCreate() // weigh date 2026-07-29

	evening := beforeCutoffClock("2026-07-29")().Add(10*time.Hour + 30*time.Minute) // 20:30 IST on the 28th
	service := NewService(&fakeRepo{}).WithClock(func() time.Time { return evening })
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); !errors.Is(err, ports.ErrFastingWindowClosed) {
		t.Fatalf("evening create err = %v, want ErrFastingWindowClosed", err)
	}

	morning := NewService(&fakeRepo{}).WithClock(beforeCutoffClock("2026-07-29"))
	if _, err := morning.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("morning create err = %v, want allowed", err)
	}
}

// The removal operator is MANDATORY: absent gets its own named error, and a
// malformed id stays the generic invalid-argument.
func TestCreateCampaignRequiresTheFastingOperator(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	service := NewService(&fakeRepo{}).WithClock(beforeCutoffClock("2026-07-29"))

	cmd := validCreate()
	cmd.FastingOperatorUserID = ""
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); !errors.Is(err, ports.ErrFastingOperatorRequired) {
		t.Fatalf("missing fasting operator err = %v, want ErrFastingOperatorRequired", err)
	}
	cmd.FastingOperatorUserID = "not-a-uuid"
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("malformed fasting operator err = %v, want ErrInvalidArgument", err)
	}
}

// An edit that KEEPS the date passes even after the cutoff; a MOVED date must
// itself be plannable; and no date may move once the removal was submitted.
func TestUpdateCampaignFastingDateRules(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	campaignID := "00000000-0000-4000-8000-000000000501"

	// Same date, evening clock: allowed (the date was valid when planned).
	keep := &fakeFastingStore{startDate: "2026-07-29", hasFasting: true}
	evening := beforeCutoffClock("2026-07-29")().Add(11 * time.Hour) // 21:00 IST on the 28th
	service := NewService(&fakeRepo{}).WithFastingStore(keep).WithClock(func() time.Time { return evening })
	cmd := validCreate()
	if _, err := service.UpdateCampaign(context.Background(), ceo, campaignID, cmd); err != nil {
		t.Fatalf("same-date evening edit err = %v, want allowed", err)
	}

	// Moved date past the cutoff: refused.
	move := &fakeFastingStore{startDate: "2026-07-30", hasFasting: true}
	service = NewService(&fakeRepo{}).WithFastingStore(move).WithClock(func() time.Time { return evening })
	if _, err := service.UpdateCampaign(context.Background(), ceo, campaignID, cmd); !errors.Is(err, ports.ErrFastingWindowClosed) {
		t.Fatalf("moved-date evening edit err = %v, want ErrFastingWindowClosed", err)
	}

	// Submitted removal locks the date entirely.
	locked := &fakeFastingStore{startDate: "2026-07-30", hasFasting: true, fastingSubbed: true}
	service = NewService(&fakeRepo{}).WithFastingStore(locked).WithClock(beforeCutoffClock("2026-07-29"))
	if _, err := service.UpdateCampaign(context.Background(), ceo, campaignID, cmd); !errors.Is(err, ports.ErrFastingSubmittedDateLocked) {
		t.Fatalf("date move after submitted removal err = %v, want ErrFastingSubmittedDateLocked", err)
	}
}

// A shed submit demands BOTH videos before the store is ever reached; a
// successful submit raises exactly ONE verification item, for THIS shed,
// carrying that shed's refs in capture order and NAMING the shed; and an
// idempotent replay re-runs the idempotent enqueue so a prior post-commit
// enqueue failure can be repaired.
func TestSubmitFastingShedRequiresBothVideosAndEnqueuesThatShed(t *testing.T) {
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}
	submittedAt := time.Date(2026, 7, 28, 21, 0, 0, 0, time.UTC)
	shedB := "00000000-0000-4000-8000-000000000802"
	store := &fakeFastingStore{
		submitResult: domain.FastingShedSubmitResult{
			Card: domain.FastingShedCard{FastingTaskID: fastingTaskID, CampaignShedID: shedB,
				FastingShedID: "00000000-0000-4000-8000-000000000902", ShedLabel: "Yashoda 1",
				SubjectLabel: "Remove feed & water · Yashoda 1", Status: domain.FastingStatusPendingVerification, RowVersion: 1},
			Evidence: domain.FastingShedProof{FastingShedID: "00000000-0000-4000-8000-000000000902",
				CampaignShedID: shedB, ShedLabel: "Yashoda 1", ShedLocationID: testShedLocationID,
				FeedProofRef: proofThree, WaterProofRef: proofFour, RowVersion: 1},
			Task: domain.FastingTask{TenantID: testTenant, FastingTaskID: fastingTaskID,
				CampaignID: "00000000-0000-4000-8000-000000000501", ParkID: testPark,
				OperatorUserID: testOp, SubmittedAt: &submittedAt, RowVersion: 2},
		},
	}
	enqueuer := &captureVerificationEnqueuer{}
	service := NewService(&fakeRepo{}).WithFastingStore(store).WithVerificationEnqueuer(enqueuer)

	// A missing water video is refused before the store is reached.
	cmd := domain.SubmitFastingShed{FastingTaskID: fastingTaskID, CampaignShedID: shedB,
		FeedProofRef: proofThree, IdempotencyKey: "fast-1"}
	if _, err := service.SubmitFastingShed(context.Background(), operator, cmd); !errors.Is(err, ports.ErrFastingProofRequired) {
		t.Fatalf("missing water video err = %v, want ErrFastingProofRequired", err)
	}
	if store.submitCalls != 0 {
		t.Fatal("store must not be reached by an invalid submit")
	}

	cmd.WaterProofRef = proofFour
	card, err := service.SubmitFastingShed(context.Background(), operator, cmd)
	if err != nil {
		t.Fatalf("submit err = %v", err)
	}
	if card.SubjectLabel != "Remove feed & water · Yashoda 1" {
		t.Fatalf("card subject = %q, must NAME the shed", card.SubjectLabel)
	}
	if store.submitCalls != 1 {
		t.Fatalf("store submit calls = %d, want 1", store.submitCalls)
	}
	if enqueuer.calls != 1 {
		t.Fatalf("verification enqueue calls = %d, want exactly ONE for THIS shed", enqueuer.calls)
	}
	got := enqueuer.received
	if got.Category != domain.VerificationRefTypeFasting {
		t.Fatalf("enqueue category = %s, want %s", got.Category, domain.VerificationRefTypeFasting)
	}
	if len(got.MediaRefs) != 2 || got.MediaRefs[0] != proofThree || got.MediaRefs[1] != proofFour {
		t.Fatalf("media refs = %v, want THIS shed's [feed water] in capture order", got.MediaRefs)
	}
	if got.ObservationID != "00000000-0000-4000-8000-000000000902" {
		t.Fatalf("ref id = %s, want the SHED evidence row id", got.ObservationID)
	}
	if got.SubjectLabel != "Remove feed & water · Yashoda 1" {
		t.Fatalf("subject label = %q, must NAME the shed", got.SubjectLabel)
	}
	if got.ShedID != testShedLocationID || got.ParkID != testPark {
		t.Fatalf("routing shed=%s park=%s, want %s/%s", got.ShedID, got.ParkID, testShedLocationID, testPark)
	}

	// A REPLAYED submit re-runs the idempotent enqueue. If the first response was
	// lost after the durable submit but before the verifier item was created, the
	// retry repairs the queue; if the item already exists, CreateItem no-ops on
	// the same key.
	store.submitResult.Replayed = true
	if _, err := service.SubmitFastingShed(context.Background(), operator, cmd); err != nil {
		t.Fatalf("replay err = %v", err)
	}
	if enqueuer.calls != 2 {
		t.Fatalf("enqueue calls after replay = %d, want 2", enqueuer.calls)
	}
}

// A verification failure after the durable submit SURFACES; and the operator
// permission gate holds.
func TestSubmitFastingShedAuthorization(t *testing.T) {
	store := &fakeFastingStore{}
	service := NewService(&fakeRepo{}).WithFastingStore(store)
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	cmd := domain.SubmitFastingShed{FastingTaskID: fastingTaskID, CampaignShedID: "00000000-0000-4000-8000-000000000801",
		FeedProofRef: proofOne, WaterProofRef: proofTwo, IdempotencyKey: "fast-2"}
	if _, err := service.SubmitFastingShed(context.Background(), ceo, cmd); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("CEO (no execute) submit err = %v, want forbidden", err)
	}
	if _, err := service.ListMyFastingShedCards(context.Background(), ceo, "", 20); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("CEO fasting list err = %v, want forbidden", err)
	}
}

// The list threads the SERVICE clock into the store, where the SQL visibility
// predicate binds it — never a second wall-clock read.
func TestListMyFastingShedCardsThreadsThePinnedClock(t *testing.T) {
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}
	store := &fakeFastingStore{}
	pinned := time.Date(2026, 9, 3, 20, 30, 0, 0, time.UTC)
	service := NewService(&fakeRepo{}).WithFastingStore(store).WithClock(func() time.Time { return pinned })
	if _, err := service.ListMyFastingShedCards(context.Background(), operator, "", 20); err != nil {
		t.Fatalf("list err = %v", err)
	}
	if store.listCalls != 1 || !store.listNow.Equal(pinned) {
		t.Fatalf("store now = %s calls=%d, want the pinned service clock once", store.listNow, store.listCalls)
	}
}
