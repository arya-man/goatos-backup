package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// The ROUND create at the app gate (maintainer decision 2026-09-05): the planner ticks
// several pens and ONE create carries all of them, with the feed & water removal still
// deworming-only and still cut off at 20:00 IST — the cutoff is now a property of the
// round (one evening, one crew), not of a pen.

const fastingShed2 = "9c000000-0000-4000-8000-000000001003"

// fakeRoundStore captures the CreateRoundParams the service hands the store.
type fakeRoundStore struct {
	createCalls    int
	lastCreate     ports.CreateRoundParams
	closed         []ports.CloseRoundParams
	lastCardsQuery ports.ListRoundCardsQuery
}

func (f *fakeRoundStore) CreateRound(_ context.Context, p ports.CreateRoundParams) (ports.RoundRow, error) {
	f.createCalls++
	f.lastCreate = p
	pens := make([]ports.TaskRow, len(p.Pens))
	return ports.RoundRow{RoundID: "round-1", Category: p.Category, Pens: pens, PenCount: int32(len(pens))}, nil
}

func (f *fakeRoundStore) GetRound(_ context.Context, _, roundID string, _ []string, _ bool) (ports.RoundRow, error) {
	return ports.RoundRow{RoundID: roundID}, nil
}

func (f *fakeRoundStore) ListRoundCards(_ context.Context, q ports.ListRoundCardsQuery) (ports.RoundCardPage, error) {
	f.lastCardsQuery = q
	return ports.RoundCardPage{}, nil
}

func (f *fakeRoundStore) CloseRound(_ context.Context, p ports.CloseRoundParams) error {
	f.closed = append(f.closed, p)
	return nil
}

func (f *fakeRoundStore) ListRemovalPenProofs(_ context.Context, _, _ string) ([]ports.RemovalPenProofRow, error) {
	return nil, nil
}

func (f *fakeRoundStore) RegisterRemovalPenProof(_ context.Context, _ ports.RegisterRemovalPenProofParams) error {
	return nil
}

func (f *fakeRoundStore) ApplyVerifiedRemovalPen(_ context.Context, _ ports.ApplyRemovalPenVerdictParams) (bool, error) {
	return true, nil
}

func (f *fakeRoundStore) BounceRemovalPenForRework(_ context.Context, _ ports.ApplyRemovalPenVerdictParams) (bool, error) {
	return true, nil
}

func roundSvc(now time.Time) (*Service, *fakeRoundStore) {
	rounds := &fakeRoundStore{}
	svc := NewService(&fakeCreateStore{}).WithRoundStore(rounds).WithNow(func() time.Time { return now })
	return svc, rounds
}

func dewormingRoundInput(planned string, pens ...RoundPenInput) CreateRoundInput {
	if len(pens) == 0 {
		pens = []RoundPenInput{{ShedID: fastingShed}}
	}
	return CreateRoundInput{
		Category:            domain.CategoryDeworming,
		ParkID:              fastingPark,
		Pens:                pens,
		PlannedBusinessDate: planned,
		AssigneeUserIDs:     []string{testAssignee},
		IdempotencyKey:      "pc-deworming-round-create",
		ActorID:             "ceo-1",
		ActorType:           "human",
	}
}

// The whole point of the change: several pens reach the store as ONE round, not as N
// creates. Before this, the phone looped and fired one create per pen.
func TestCreateRoundCarriesEveryTickedPenInOneWrite(t *testing.T) {
	svc, rounds := roundSvc(pinnedIST(10, 9, 0))
	in := dewormingRoundInput("2026-09-11",
		RoundPenInput{ShedID: fastingShed, PartitionLabel: "Part 1"},
		RoundPenInput{ShedID: fastingShed, PartitionLabel: "Part 2"},
		RoundPenInput{ShedID: fastingShed2},
	)

	round, err := svc.CreateRound(plannerCtx(), plannerActor(), in)
	if err != nil {
		t.Fatalf("CreateRound err = %v, want nil", err)
	}
	if rounds.createCalls != 1 {
		t.Fatalf("store create calls = %d, want exactly 1 for a three-pen round", rounds.createCalls)
	}
	if got := len(rounds.lastCreate.Pens); got != 3 {
		t.Fatalf("pens reaching the store = %d, want 3", got)
	}
	if round.PenCount != 3 {
		t.Fatalf("round.PenCount = %d, want 3", round.PenCount)
	}
	// The labels are NOT taken from the client; only identity crosses the boundary.
	for _, pen := range rounds.lastCreate.Pens {
		if pen.ShedID == "" {
			t.Fatal("a pen reached the store without its shed id")
		}
	}
}

func TestCreateRoundRefusesEmptyOversizeAndDuplicatePenLists(t *testing.T) {
	svc, rounds := roundSvc(pinnedIST(10, 9, 0))

	empty := dewormingRoundInput("2026-09-11")
	empty.Pens = nil
	if _, err := svc.CreateRound(plannerCtx(), plannerActor(), empty); !errors.Is(err, domain.ErrNoPens) {
		t.Fatalf("no pens err = %v, want ErrNoPens", err)
	}

	// The duplicate is spelled differently — the case a naive equality check would miss and
	// the database would then refuse halfway through the write.
	dup := dewormingRoundInput("2026-09-11",
		RoundPenInput{ShedID: fastingShed, PartitionLabel: "Part 1"},
		RoundPenInput{ShedID: fastingShed, PartitionLabel: " part 1 "},
	)
	if _, err := svc.CreateRound(plannerCtx(), plannerActor(), dup); !errors.Is(err, domain.ErrDuplicatePen) {
		t.Fatalf("duplicate pen err = %v, want ErrDuplicatePen", err)
	}

	tooMany := dewormingRoundInput("2026-09-11")
	tooMany.Pens = make([]RoundPenInput, 0, domain.MaxPensPerRound+1)
	for i := 0; i <= domain.MaxPensPerRound; i++ {
		tooMany.Pens = append(tooMany.Pens, RoundPenInput{ShedID: fastingShed, PartitionLabel: "Part " + itoa(i)})
	}
	if _, err := svc.CreateRound(plannerCtx(), plannerActor(), tooMany); !errors.Is(err, domain.ErrTooManyPens) {
		t.Fatalf("oversize err = %v, want ErrTooManyPens", err)
	}

	if rounds.createCalls != 0 {
		t.Fatalf("a refused round reached the store %d times, want 0", rounds.createCalls)
	}
}

// A kernel-owned category has no pens to multiply and must never be planned as a round.
func TestCreateRoundRefusesCategoriesThePlannerDoesNotOwn(t *testing.T) {
	svc, rounds := roundSvc(pinnedIST(10, 9, 0))
	for _, tc := range []struct {
		category string
		want     error
	}{
		{domain.CategoryInventoryVaccine, domain.ErrKernelOwnedCategory},
		{domain.CategoryFeedWaterRemoval, domain.ErrKernelOwnedCategory},
		{"nonsense", domain.ErrInvalidCategory},
	} {
		in := dewormingRoundInput("2026-09-11")
		in.Category = tc.category
		if _, err := svc.CreateRound(plannerCtx(), plannerActor(), in); !errors.Is(err, tc.want) {
			t.Fatalf("category %q err = %v, want %v", tc.category, err, tc.want)
		}
	}
	if rounds.createCalls != 0 {
		t.Fatalf("a refused round reached the store %d times, want 0", rounds.createCalls)
	}
}

// Feed & water removal stays DEWORMING-ONLY at round grain. A dropped toggle would read to
// the planner as accepted while creating no removal card at all.
func TestCreateRoundFeedRemovalIsDewormingOnly(t *testing.T) {
	svc, rounds := roundSvc(pinnedIST(10, 9, 0))

	wrongCategory := dewormingRoundInput("2026-09-11")
	wrongCategory.Category = domain.CategoryTicksRemoval
	wrongCategory.FeedRemovalRequired = true
	wrongCategory.RemovalOperatorUserIDs = []string{fastingRemover}
	if _, err := svc.CreateRound(plannerCtx(), plannerActor(), wrongCategory); !errors.Is(err, domain.ErrFeedRemovalNotApplicable) {
		t.Fatalf("ticks removal with the toggle err = %v, want ErrFeedRemovalNotApplicable", err)
	}

	operatorsWithoutToggle := dewormingRoundInput("2026-09-11")
	operatorsWithoutToggle.RemovalOperatorUserIDs = []string{fastingRemover}
	if _, err := svc.CreateRound(plannerCtx(), plannerActor(), operatorsWithoutToggle); !errors.Is(err, domain.ErrFeedRemovalNotApplicable) {
		t.Fatalf("operators without the toggle err = %v, want ErrFeedRemovalNotApplicable", err)
	}

	toggleWithoutOperators := dewormingRoundInput("2026-09-11")
	toggleWithoutOperators.FeedRemovalRequired = true
	if _, err := svc.CreateRound(plannerCtx(), plannerActor(), toggleWithoutOperators); !errors.Is(err, domain.ErrRemovalOperatorsRequired) {
		t.Fatalf("toggle without operators err = %v, want ErrRemovalOperatorsRequired", err)
	}

	if rounds.createCalls != 0 {
		t.Fatalf("a refused round reached the store %d times, want 0", rounds.createCalls)
	}
}

// The 20:00 IST cutoff is a property of the ROUND — one evening, one crew — and binds a
// multi-pen round exactly as it bound a single-pen create.
func TestCreateRoundFeedRemovalEveningCutoff(t *testing.T) {
	pens := []RoundPenInput{{ShedID: fastingShed, PartitionLabel: "Part 1"}, {ShedID: fastingShed2}}
	for _, tc := range []struct {
		name    string
		now     time.Time
		planned string
		wantErr error
	}{
		{"before 20:00, tomorrow is plannable", pinnedIST(10, 19, 59), "2026-09-11", nil},
		{"before 20:00, today is too late", pinnedIST(10, 19, 59), "2026-09-10", domain.ErrFastingWindowClosed},
		{"at 20:00, tomorrow is too late", pinnedIST(10, 20, 0), "2026-09-11", domain.ErrFastingWindowClosed},
		{"at 20:00, the day after is plannable", pinnedIST(10, 20, 0), "2026-09-12", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, rounds := roundSvc(tc.now)
			in := dewormingRoundInput(tc.planned, pens...)
			in.FeedRemovalRequired = true
			in.RemovalOperatorUserIDs = []string{fastingRemover}

			_, err := svc.CreateRound(plannerCtx(), plannerActor(), in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if !rounds.lastCreate.FeedRemovalRequired {
				t.Fatal("FeedRemovalRequired must reach the store")
			}
			if len(rounds.lastCreate.RemovalOperatorUserIDs) != 1 {
				t.Fatalf("removal operators reaching the store = %v, want one", rounds.lastCreate.RemovalOperatorUserIDs)
			}
			if len(rounds.lastCreate.Pens) != len(pens) {
				t.Fatalf("pens reaching the store = %d, want %d", len(rounds.lastCreate.Pens), len(pens))
			}
		})
	}
}

// A round WITHOUT the removal is not held to the evening cutoff — there is no evening crew
// to staff, so a same-day trimming round stays plannable.
func TestCreateRoundWithoutFeedRemovalIgnoresTheEveningCutoff(t *testing.T) {
	svc, rounds := roundSvc(pinnedIST(10, 23, 0))
	in := dewormingRoundInput("2026-09-10")

	if _, err := svc.CreateRound(plannerCtx(), plannerActor(), in); err != nil {
		t.Fatalf("CreateRound err = %v, want nil", err)
	}
	if rounds.lastCreate.FeedRemovalRequired || len(rounds.lastCreate.RemovalOperatorUserIDs) != 0 {
		t.Fatal("a round that asked for no removal must carry none to the store")
	}
}

// A service with no round store refuses the write rather than pretending to plan one.
func TestCreateRoundWithoutAStoreIsUnavailableNotSilent(t *testing.T) {
	svc := NewService(&fakeCreateStore{}).WithNow(func() time.Time { return pinnedIST(10, 9, 0) })
	if _, err := svc.CreateRound(plannerCtx(), plannerActor(), dewormingRoundInput("2026-09-11")); !errors.Is(err, ports.ErrStoreUnavailable) {
		t.Fatalf("err = %v, want ErrStoreUnavailable", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}

// The planner's list mirrors weighing's: NO date axis. A dateless read is valid and carries
// the tab's filter instead; pinning a day stays possible for a caller that wants one.
func TestListRoundCardsAcceptsNoDateAndCarriesTheTab(t *testing.T) {
	svc, rounds := roundSvc(pinnedIST(10, 9, 0))

	if _, err := svc.ListRoundCards(plannerCtx(), plannerActor(), "", domain.CategoryDeworming, "", ports.RoundCardsFilterActive, "", 25, false); err != nil {
		t.Fatalf("dateless list err = %v, want nil", err)
	}
	if rounds.lastCardsQuery.DueBusinessDate != "" {
		t.Fatalf("date reaching the store = %q, want empty", rounds.lastCardsQuery.DueBusinessDate)
	}
	if rounds.lastCardsQuery.Filter != ports.RoundCardsFilterActive {
		t.Fatalf("filter reaching the store = %q, want active", rounds.lastCardsQuery.Filter)
	}

	// A malformed date is still refused — dropping the axis must not drop the validation.
	if _, err := svc.ListRoundCards(plannerCtx(), plannerActor(), "", domain.CategoryDeworming, "not-a-date", "", "", 25, false); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("malformed date err = %v, want ErrInvalidArgument", err)
	}
}
