package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The kid stage ladder auto-raiser (maintainer decision 2026-08-20): K0->K1 at 2 days,
// K1->K2 at 7 days, business-day grain; exactly one candidate pen auto-raises, zero or several
// become the operator card.

const (
	ladderTenant  = "00000000-0000-4000-8000-000000000001"
	ladderParkID  = "44444444-4444-4444-8444-444444444444"
	ladderShedK1  = "55555555-5555-4555-8555-555555555551"
	ladderShedK1B = "55555555-5555-4555-8555-555555555552"
	ladderKidA    = "10000000-0000-4000-8000-000000000101"
	ladderKidB    = "10000000-0000-4000-8000-000000000102"
)

// fakeKidStageRepo layers the ladder read port over the shared fakeRepo and captures what the
// raiser asked for and wrote.
type fakeKidStageRepo struct {
	*fakeRepo
	dueByStage map[string][]domain.KidStageDueGoat
	cutoffs    map[string]time.Time

	approvals []domain.ApprovalRequestSubmission
	events    []domain.ShiftingEvent
}

func (f *fakeKidStageRepo) ListKidStageDueGoats(_ context.Context, _ string, fromStage string, bornOnOrBefore time.Time) ([]domain.KidStageDueGoat, error) {
	if f.cutoffs == nil {
		f.cutoffs = map[string]time.Time{}
	}
	f.cutoffs[strings.ToUpper(fromStage)] = bornOnOrBefore
	return f.dueByStage[strings.ToUpper(fromStage)], nil
}

func (f *fakeKidStageRepo) RecordShiftingEvent(ctx context.Context, in domain.ShiftingEvent) (string, bool, error) {
	f.events = append(f.events, in)
	return f.fakeRepo.RecordShiftingEvent(ctx, in)
}

func (f *fakeKidStageRepo) CreateApprovalRequest(_ context.Context, in domain.ApprovalRequestSubmission) (domain.ApprovalRequest, bool, error) {
	f.approvals = append(f.approvals, in)
	return domain.ApprovalRequest{ApprovalRequestID: "approval-1", Status: "pending"}, false, nil
}

func ladderCatalog(k1Pens int) domain.ShiftingDestinationCatalog {
	sheds := []domain.ShiftingDestinationShed{
		{ShedID: "66666666-6666-4666-8666-666666666666", Name: "Adults", Display: "Adults", ConfiguredStage: "Non-Pregnant"},
	}
	if k1Pens >= 1 {
		sheds = append(sheds, domain.ShiftingDestinationShed{ShedID: ladderShedK1, Name: "Kids 1", Display: "Kids 1", ConfiguredStage: "K1"})
	}
	if k1Pens >= 2 {
		sheds = append(sheds, domain.ShiftingDestinationShed{ShedID: ladderShedK1B, Name: "Kids 1B", Display: "Kids 1B", ConfiguredStage: "K1"})
	}
	return domain.ShiftingDestinationCatalog{
		ManagementStages: []string{"K0", "K1", "K2", "Non-Pregnant"},
		Parks: []domain.ShiftingDestinationPark{{
			ParkID: ladderParkID,
			Name:   "Coimbatore",
			Sheds:  sheds,
		}},
	}
}

func dueKid(goatID string) domain.KidStageDueGoat {
	return domain.KidStageDueGoat{
		GoatID:          goatID,
		DisplayID:       "G-000900",
		Tag:             "TEMP-CBE-KID-001",
		ParkID:          ladderParkID,
		ShedID:          "77777777-7777-4777-8777-777777777771",
		ManagementStage: "K0",
		BornOn:          time.Date(2026, 8, 15, 0, 0, 0, 0, biztime.DefaultLocation()),
	}
}

func kidFact(goatID string) domain.GoatShiftingFact {
	park, shed := ladderParkID, "77777777-7777-4777-8777-777777777771"
	return domain.GoatShiftingFact{
		GoatID: goatID, LifecycleStatus: "alive",
		BreedKey: "sirohi", BreedLabel: "Sirohi", ParkID: &park, ShedID: &shed,
	}
}

func newLadderRaiser(repo *fakeKidStageRepo) *KidStageLadderRaiser {
	return NewKidStageLadderRaiser(repo, NewService(repo), NewApprovalService(repo, nil, nil))
}

func TestKidStageLadderCutoffsAreBusinessDayGrain(t *testing.T) {
	repo := &fakeKidStageRepo{fakeRepo: &fakeRepo{destinations: ladderCatalog(1)}}
	raiser := newLadderRaiser(repo)

	// 2026-08-20 15:41 IST: the business day started at 2026-08-20 00:00 IST, so K0 kids born on
	// or before 2026-08-18 00:00 IST are due K1, and K1 kids born on or before 2026-08-13 are due
	// K2. Hour-of-day must not move either cutoff.
	asOf := time.Date(2026, 8, 20, 15, 41, 0, 0, biztime.DefaultLocation())
	if _, err := raiser.DueGroups(context.Background(), ladderTenant, asOf); err != nil {
		t.Fatalf("DueGroups: %v", err)
	}
	dayStart := time.Date(2026, 8, 20, 0, 0, 0, 0, biztime.DefaultLocation())
	if got := repo.cutoffs["K0"]; !got.Equal(dayStart.AddDate(0, 0, -2)) {
		t.Fatalf("K0 cutoff = %v, want %v (2 days)", got, dayStart.AddDate(0, 0, -2))
	}
	if got := repo.cutoffs["K1"]; !got.Equal(dayStart.AddDate(0, 0, -7)) {
		t.Fatalf("K1 cutoff = %v, want %v (7 days)", got, dayStart.AddDate(0, 0, -7))
	}
}

func TestKidStageLadderAutoRaisesWhenExactlyOnePenCarriesTheTag(t *testing.T) {
	repo := &fakeKidStageRepo{
		fakeRepo: &fakeRepo{
			destinations: ladderCatalog(1),
			goatFacts:    []domain.GoatShiftingFact{kidFact(ladderKidA), kidFact(ladderKidB)},
		},
		dueByStage: map[string][]domain.KidStageDueGoat{
			"K0": {dueKid(ladderKidA), dueKid(ladderKidB)},
		},
	}
	raiser := newLadderRaiser(repo)

	asOf := time.Date(2026, 8, 20, 6, 0, 0, 0, biztime.DefaultLocation())
	result, err := raiser.AutoRaise(context.Background(), ladderTenant, asOf)
	if err != nil {
		t.Fatalf("AutoRaise: %v", err)
	}
	if len(result.Raised) != 1 || len(result.CardGroups) != 0 {
		t.Fatalf("raised=%d cards=%d, want 1 raise and no cards", len(result.Raised), len(result.CardGroups))
	}
	if result.Raised[0].GoatCount != 2 || result.Raised[0].ToStage != "K1" {
		t.Fatalf("raised = %+v, want both kids toward K1", result.Raised[0])
	}
	if len(repo.events) != 1 {
		t.Fatalf("events recorded = %d, want 1", len(repo.events))
	}
	event := repo.events[0]
	if event.DestinationShedID != ladderShedK1 || event.TargetManagementStage != "K1" ||
		event.ManagementStageMode != "destination_stage" {
		t.Fatalf("event = dest %q stage %q mode %q, want the K1 pen with destination_stage",
			event.DestinationShedID, event.TargetManagementStage, event.ManagementStageMode)
	}
	// The raise is a REAL movement: it must carry derived impact rows or the count projection
	// would record a movement of nothing.
	if len(event.Impacts) == 0 {
		t.Fatal("event.Impacts is empty — the auto-raise must derive impacts like any other raise")
	}
	if len(repo.approvals) != 1 {
		t.Fatalf("approvals submitted = %d, want 1", len(repo.approvals))
	}
	approval := repo.approvals[0]
	if approval.RaisedByUserID != KidStageLadderActorID {
		t.Fatalf("raised_by = %q, want the system ladder actor", approval.RaisedByUserID)
	}
	var payload struct {
		GoatIDs               []string `json:"goat_ids"`
		TargetManagementStage string   `json:"target_management_stage"`
	}
	if err := json.Unmarshal(approval.Payload, &payload); err != nil {
		t.Fatalf("approval payload: %v", err)
	}
	if len(payload.GoatIDs) != 2 || payload.TargetManagementStage != "K1" {
		t.Fatalf("approval payload = %+v, want both kids and K1", payload)
	}
	// Deterministic identity: the same sweep replays onto the same key.
	if !strings.HasPrefix(event.IdempotencyKey, "app-counts-shifting:auto-kid-stage:") ||
		!strings.Contains(event.IdempotencyKey, "2026-08-20") {
		t.Fatalf("idempotency key = %q, want the deterministic ladder key carrying the business date", event.IdempotencyKey)
	}
}

func TestKidStageLadderTwoCandidatePensBecomeTheOperatorCard(t *testing.T) {
	repo := &fakeKidStageRepo{
		fakeRepo: &fakeRepo{
			destinations: ladderCatalog(2),
			goatFacts:    []domain.GoatShiftingFact{kidFact(ladderKidA)},
		},
		dueByStage: map[string][]domain.KidStageDueGoat{"K0": {dueKid(ladderKidA)}},
	}
	raiser := newLadderRaiser(repo)

	result, err := raiser.AutoRaise(context.Background(), ladderTenant,
		time.Date(2026, 8, 20, 6, 0, 0, 0, biztime.DefaultLocation()))
	if err != nil {
		t.Fatalf("AutoRaise: %v", err)
	}
	if len(result.Raised) != 0 || len(repo.events) != 0 {
		t.Fatalf("raised=%d events=%d, want none — two K1 pens means the operator chooses", len(result.Raised), len(repo.events))
	}
	if len(result.CardGroups) != 1 || len(result.CardGroups[0].Candidates) != 2 {
		t.Fatalf("cards = %+v, want one group carrying both candidate pens", result.CardGroups)
	}
}

func TestKidStageLadderNoCandidatePenBecomesTheOperatorCard(t *testing.T) {
	repo := &fakeKidStageRepo{
		fakeRepo:   &fakeRepo{destinations: ladderCatalog(0)},
		dueByStage: map[string][]domain.KidStageDueGoat{"K0": {dueKid(ladderKidA)}},
	}
	raiser := newLadderRaiser(repo)

	result, err := raiser.AutoRaise(context.Background(), ladderTenant,
		time.Date(2026, 8, 20, 6, 0, 0, 0, biztime.DefaultLocation()))
	if err != nil {
		t.Fatalf("AutoRaise: %v", err)
	}
	if len(result.Raised) != 0 {
		t.Fatalf("raised=%d, want none — no pen carries K1, so the operator card owns it", len(result.Raised))
	}
	if len(result.CardGroups) != 1 || len(result.CardGroups[0].Candidates) != 0 {
		t.Fatalf("cards = %+v, want one group with zero candidates", result.CardGroups)
	}
}

func TestKidStageLadderNothingDueDoesNothing(t *testing.T) {
	repo := &fakeKidStageRepo{fakeRepo: &fakeRepo{destinations: ladderCatalog(1)}}
	raiser := newLadderRaiser(repo)

	result, err := raiser.AutoRaise(context.Background(), ladderTenant,
		time.Date(2026, 8, 20, 6, 0, 0, 0, biztime.DefaultLocation()))
	if err != nil {
		t.Fatalf("AutoRaise: %v", err)
	}
	if len(result.Raised) != 0 || len(result.CardGroups) != 0 || len(repo.events) != 0 {
		t.Fatalf("result = %+v, want a clean no-op", result)
	}
}
