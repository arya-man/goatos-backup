package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// WEIGHING SOP service tests (maintainer decision 2026-09-15, docs/decisions/weighing-sop.md).
// Each was mutation-tested when written: deleting the matching branch of applyPlanningRules
// (the OFF drop, the OPTIONAL requested=false drop, the mode check) or the pin read in
// SubmitFastingShed turns the named case red.

// capturingRepo records the command the service hands the repository, so the stamped
// version, the normalized removal operator and the defaulted cap can be asserted.
type capturingRepo struct {
	fakeRepo
	created domain.CreateCampaign
	updated domain.CreateCampaign
}

func (r *capturingRepo) CreateCampaign(_ context.Context, cmd domain.CreateCampaign) (domain.Campaign, error) {
	r.created = cmd
	return domain.Campaign{CampaignID: "00000000-0000-4000-8000-000000000501", SOPVersion: cmd.SOPVersion}, nil
}

func (r *capturingRepo) UpdateCampaign(_ context.Context, _ string, cmd domain.UpdateCampaign) (domain.Campaign, error) {
	r.updated = cmd
	return domain.Campaign{CampaignID: "00000000-0000-4000-8000-000000000501"}, nil
}

const shedB = "00000000-0000-4000-8000-000000000802"

type pinnedVersion int

func (p pinnedVersion) CampaignSOPVersion(context.Context, string, string) (int, error) {
	return int(p), nil
}

func rulesWithMode(version int, mode string) ports.StaticRules {
	r := domain.SeededRules()
	r.Version = version
	r.FeedWaterRemoval.Mode = mode
	return ports.StaticRules{Rules: r}
}

func todayClock() func() time.Time {
	// 10:00 IST on the weigh date itself: tomorrow is still fastable, today is not.
	day, _ := time.ParseInLocation("2006-01-02", "2026-07-29", biztime.DefaultLocation())
	return func() time.Time { return day.Add(10 * time.Hour) }
}

func TestCreateCampaignStampsThePublishedVersionAndDefaultsTheCapFromTheSOP(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	rules := rulesWithMode(7, domain.RemovalModeRequired)
	rules.Rules.Planning.DefaultCapPerDay = 150
	repo := &capturingRepo{}
	service := NewService(repo).WithFeedWaterRemovalCutoff(eightPM).WithClock(beforeCutoffClock("2026-07-29")).WithSOPRules(rules, pinnedVersion(0))
	cmd := validCreate()
	cmd.PlannedCapPerDay = 0
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("create err = %v", err)
	}
	if repo.created.SOPVersion != 7 {
		t.Fatalf("stamped version = %d, want the PUBLISHED 7", repo.created.SOPVersion)
	}
	if repo.created.PlannedCapPerDay != 150 {
		t.Fatalf("cap = %d, want the SOP's default 150 (not the old literal 100)", repo.created.PlannedCapPerDay)
	}
	// Unwired: the seeded rules, version 0, cap 100 -- the pre-SOP behaviour.
	repo = &capturingRepo{}
	service = NewService(repo).WithFeedWaterRemovalCutoff(eightPM).WithClock(beforeCutoffClock("2026-07-29"))
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("unwired create err = %v", err)
	}
	if repo.created.SOPVersion != 0 || repo.created.PlannedCapPerDay != 100 {
		t.Fatalf("unwired stamp/cap = %d/%d, want 0/100", repo.created.SOPVersion, repo.created.PlannedCapPerDay)
	}
}

func TestCreateCampaignRefusesAModeTheSOPDoesNotOffer(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	rules := rulesWithMode(2, domain.RemovalModeRequired)
	rules.Rules.Planning.Modes = []string{domain.CategoryPerShedPartition}
	service := NewService(&capturingRepo{}).WithFeedWaterRemovalCutoff(eightPM).WithClock(beforeCutoffClock("2026-07-29")).WithSOPRules(rules, pinnedVersion(0))
	cmd := validCreate() // its one shed is individual_animal
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); !errors.Is(err, ports.ErrModeNotAllowed) {
		t.Fatalf("individual under a lump-sum-only SOP err = %v, want ErrModeNotAllowed", err)
	}
	cmd.Sheds[0].WeighingCategory = domain.CategoryPerShedPartition
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("offered mode err = %v", err)
	}
}

// OPTIONAL: the planner's word decides. Declining drops the operator, skips the evening
// cutoff (today is plannable, no cutoff need even be configured) and creates no round;
// asking keeps the 2026-09-03 rule whole; saying nothing reads as the operator's presence.
func TestCreateCampaignUnderOptionalRemovalHonoursThePlannerChoice(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	rules := rulesWithMode(3, domain.RemovalModeOptional)
	no, yes := false, true

	// Declined: no operator needed, no cutoff needed, today allowed.
	repo := &capturingRepo{}
	service := NewService(repo).WithClock(todayClock()).WithSOPRules(rules, pinnedVersion(0))
	cmd := validCreate()
	cmd.FastingOperatorUserID = ""
	cmd.FeedWaterRemovalRequested = &no
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("declined removal, today, no cutoff wired: err = %v, want allowed", err)
	}
	if repo.created.FastingOperatorUserID != "" {
		t.Fatal("a declined removal must reach the repository with NO removal operator (no round is created)")
	}
	// Declined but an operator was sent anyway (a stale wizard state): dropped, not honoured.
	cmd.FastingOperatorUserID = testOp
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("declined with operator err = %v", err)
	}
	if repo.created.FastingOperatorUserID != "" {
		t.Fatal("declined removal must drop a stray operator")
	}
	// Declined but the date is yesterday: refused as a past date, never as a cutoff.
	cmd.StartBusinessDate, cmd.PeriodStartDate, cmd.PeriodEndDate = "2026-07-28", "2026-07-28", "2026-07-28"
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); !errors.Is(err, ports.ErrWeighDateInPast) {
		t.Fatalf("declined removal on a past date err = %v, want ErrWeighDateInPast", err)
	}

	// Asked: the whole 2026-09-03 rule -- operator mandatory, today refused by the cutoff.
	cmd = validCreate()
	cmd.FeedWaterRemovalRequested = &yes
	cmd.FastingOperatorUserID = ""
	service = NewService(repo).WithFeedWaterRemovalCutoff(eightPM).WithClock(todayClock()).WithSOPRules(rules, pinnedVersion(0))
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); !errors.Is(err, ports.ErrFastingOperatorRequired) {
		t.Fatalf("asked without operator err = %v, want ErrFastingOperatorRequired", err)
	}
	cmd.FastingOperatorUserID = testOp
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); !errors.Is(err, ports.ErrFastingWindowClosed) {
		t.Fatalf("asked for today err = %v, want ErrFastingWindowClosed", err)
	}

	// Not said + operator present (an older APK): ON, exactly as before.
	cmd = validCreate()
	service = NewService(repo).WithFeedWaterRemovalCutoff(eightPM).WithClock(beforeCutoffClock("2026-07-29")).WithSOPRules(rules, pinnedVersion(0))
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("not-said with operator err = %v", err)
	}
	if repo.created.FastingOperatorUserID != testOp {
		t.Fatal("not-said with operator must keep the round")
	}
	// Not said + no operator: OFF for this task.
	cmd.FastingOperatorUserID = ""
	service = NewService(repo).WithClock(beforeCutoffClock("2026-07-29")).WithSOPRules(rules, pinnedVersion(0))
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("not-said without operator err = %v, want allowed (no round)", err)
	}
}

// OFF: no task carries the precondition. A stray operator (older APK) is dropped; an explicit
// ask is refused by name; the evening cutoff never applies.
func TestCreateCampaignUnderOffDropsTheOperatorAndRefusesAnExplicitAsk(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	rules := rulesWithMode(4, domain.RemovalModeOff)
	repo := &capturingRepo{}
	service := NewService(repo).WithClock(todayClock()).WithSOPRules(rules, pinnedVersion(0))
	cmd := validCreate() // sends the operator, weigh date = today
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("off, today, operator sent: err = %v, want allowed", err)
	}
	if repo.created.FastingOperatorUserID != "" {
		t.Fatal("under OFF the operator is not an assignment and must be dropped")
	}
	yes := true
	cmd.FeedWaterRemovalRequested = &yes
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); !errors.Is(err, ports.ErrRemovalNotOffered) {
		t.Fatalf("explicit ask under OFF err = %v, want ErrRemovalNotOffered", err)
	}
}

// REQUIRED is the 2026-09-03 rule unchanged, and the planner's "no" is not a word.
func TestCreateCampaignUnderRequiredIgnoresADecline(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	no := false
	service := NewService(&capturingRepo{}).WithFeedWaterRemovalCutoff(eightPM).WithClock(beforeCutoffClock("2026-07-29")).WithSOPRules(rulesWithMode(1, domain.RemovalModeRequired), pinnedVersion(0))
	cmd := validCreate()
	cmd.FeedWaterRemovalRequested = &no
	cmd.FastingOperatorUserID = ""
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); !errors.Is(err, ports.ErrFastingOperatorRequired) {
		t.Fatalf("decline under REQUIRED err = %v, want ErrFastingOperatorRequired", err)
	}
}

// An EDIT runs on the task's PINNED version, not the latest publish: the pin says required
// while the published version says off, and the edit still demands the operator.
func TestUpdateCampaignRunsOnThePinnedVersionNotTheLatestPublish(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	campaignID := "00000000-0000-4000-8000-000000000501"
	published := rulesWithMode(5, domain.RemovalModeOff)
	// The source answers version 2 (pinned) as REQUIRED and version 5 (published) as OFF.
	src := versionedRules{5: published.Rules, 2: rulesWithMode(2, domain.RemovalModeRequired).Rules}
	store := &fakeFastingStore{startDate: "2026-07-29", hasFasting: true}
	service := NewService(&capturingRepo{}).WithFeedWaterRemovalCutoff(eightPM).WithFastingStore(store).WithClock(beforeCutoffClock("2026-07-29")).WithSOPRules(src, pinnedVersion(2))
	cmd := validCreate()
	cmd.FastingOperatorUserID = ""
	if _, err := service.UpdateCampaign(context.Background(), ceo, campaignID, cmd); !errors.Is(err, ports.ErrFastingOperatorRequired) {
		t.Fatalf("edit on a REQUIRED pin err = %v, want ErrFastingOperatorRequired even though the latest publish is OFF", err)
	}
}

// Switching the removal OFF on an edit (optional mode) deletes an untouched round and is
// refused once a pen was submitted.
func TestUpdateCampaignSwitchingRemovalOffIsLockedByASubmittedPen(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	campaignID := "00000000-0000-4000-8000-000000000501"
	no := false
	cmd := validCreate()
	cmd.FeedWaterRemovalRequested = &no

	repo := &capturingRepo{}
	untouched := &fakeFastingStore{startDate: "2026-07-29", hasFasting: true}
	service := NewService(repo).WithFastingStore(untouched).WithClock(beforeCutoffClock("2026-07-29")).WithSOPRules(rulesWithMode(3, domain.RemovalModeOptional), pinnedVersion(3))
	if _, err := service.UpdateCampaign(context.Background(), ceo, campaignID, cmd); err != nil {
		t.Fatalf("switch off on an untouched round err = %v", err)
	}
	if !repo.updated.RemoveFasting || repo.updated.FastingOperatorUserID != "" {
		t.Fatalf("updated cmd = remove:%v op:%q, want the round removed", repo.updated.RemoveFasting, repo.updated.FastingOperatorUserID)
	}

	submitted := &fakeFastingStore{startDate: "2026-07-29", hasFasting: true, fastingSubbed: true}
	service = NewService(repo).WithFastingStore(submitted).WithClock(beforeCutoffClock("2026-07-29")).WithSOPRules(rulesWithMode(3, domain.RemovalModeOptional), pinnedVersion(3))
	if _, err := service.UpdateCampaign(context.Background(), ceo, campaignID, cmd); !errors.Is(err, ports.ErrRemovalChangeLocked) {
		t.Fatalf("switch off after a submit err = %v, want ErrRemovalChangeLocked", err)
	}
}

func TestPlannerCatalogCarriesThePublishedRules(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	service := NewService(&capturingRepo{}).WithSOPRules(rulesWithMode(9, domain.RemovalModeOptional), pinnedVersion(0))
	catalog, err := service.PlannerCatalog(context.Background(), ceo, "2026-07-29")
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SOP.Version != 9 || catalog.SOP.FeedWaterRemoval.Mode != domain.RemovalModeOptional {
		t.Fatalf("catalog sop = v%d/%s, want v9/optional", catalog.SOP.Version, catalog.SOP.FeedWaterRemoval.Mode)
	}
}

// The lump-sum video window is the PINNED version's, inside the proof policy's ceiling.
func TestRecordShedObservationHonoursThePinnedLumpSumVideoWindow(t *testing.T) {
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}
	rules := rulesWithMode(2, domain.RemovalModeRequired)
	rules.Rules.Capture.LumpSum.VideoMin, rules.Rules.Capture.LumpSum.VideoMax = 2, 2
	service := NewService(&shedObservationRepo{}).WithVerificationEnqueuer(&captureVerificationEnqueuer{}).WithSOPRules(rules, pinnedVersion(2))
	cmd := domain.RecordShedObservation{
		CampaignID: "00000000-0000-4000-8000-000000000501", CampaignShedID: "00000000-0000-4000-8000-000000000801",
		WeightKg: 250, ProofArtifactIDs: []string{proofOne}, IdempotencyKey: "shed-1",
	}
	if _, err := service.RecordShedObservation(context.Background(), operator, cmd); !errors.Is(err, ports.ErrLumpSumVideoCount) {
		t.Fatalf("one video under a 2..2 window err = %v, want ErrLumpSumVideoCount", err)
	}
	cmd.ProofArtifactIDs = []string{proofOne, proofTwo}
	if _, err := service.RecordShedObservation(context.Background(), operator, cmd); err != nil {
		t.Fatalf("two videos err = %v", err)
	}
}

// The removal card's answers are judged by the task's PINNED version: a question the pinned
// document asks is required; one it does not ask is refused; the stored answers are the
// normalized set. The served card carries the pinned copy.
func TestSubmitFastingShedValidatesAnswersAgainstThePinnedVersion(t *testing.T) {
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}
	rules := rulesWithMode(6, domain.RemovalModeRequired)
	rules.Rules.FeedWaterRemoval.Instruction = "Empty every trough."
	rules.Rules.FeedWaterRemoval.Questions = []domain.SOPQuestion{
		{ID: "all_pens", Kind: domain.SOPQuestionChoice, Title: "Every pen emptied?", Required: true, Options: []domain.SOPOption{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}},
		{ID: "why_not", Kind: domain.SOPQuestionText, Title: "Why not", Required: true, OnlyIf: &domain.SOPCondition{QuestionID: "all_pens", Value: "no"}},
	}
	submittedAt := time.Date(2026, 7, 28, 15, 0, 0, 0, time.UTC)
	store := &fakeFastingStore{submitResult: domain.FastingShedSubmitResult{
		Card:     domain.FastingShedCard{FastingTaskID: fastingTaskID, CampaignShedID: shedB, FastingShedID: "00000000-0000-4000-8000-000000000902", ShedLabel: "Yashoda 1", Status: domain.FastingStatusPendingVerification, RowVersion: 1},
		Evidence: domain.FastingShedProof{FastingShedID: "00000000-0000-4000-8000-000000000902", CampaignShedID: shedB, FeedProofRef: proofThree, WaterProofRef: proofFour, RowVersion: 1},
		Task:     domain.FastingTask{TenantID: testTenant, FastingTaskID: fastingTaskID, CampaignID: "00000000-0000-4000-8000-000000000501", OperatorUserID: testOp, SubmittedAt: &submittedAt},
	}}
	service := NewService(&fakeRepo{}).WithFastingStore(store).WithVerificationEnqueuer(&captureVerificationEnqueuer{}).WithSOPRules(rules, pinnedVersion(6))
	cmd := domain.SubmitFastingShed{FastingTaskID: fastingTaskID, CampaignShedID: shedB, FeedProofRef: proofThree, WaterProofRef: proofFour, IdempotencyKey: "fast-1"}

	if _, err := service.SubmitFastingShed(context.Background(), operator, cmd); !errors.Is(err, domain.ErrSOPAnswerInvalid) {
		t.Fatalf("no answers on a required question err = %v, want ErrSOPAnswerInvalid", err)
	}
	if store.submitCalls != 0 {
		t.Fatal("store must not be reached by an invalid submit")
	}
	cmd.Answers = domain.SOPAnswers{"all_pens": json.RawMessage(`"yes"`), "why_not": json.RawMessage(`"stale"`)}
	card, err := service.SubmitFastingShed(context.Background(), operator, cmd)
	if err != nil {
		t.Fatalf("submit err = %v", err)
	}
	if _, kept := store.submitted.Answers["why_not"]; kept || len(store.submitted.Answers) != 1 {
		t.Fatalf("stored answers = %v, want only the applicable all_pens", store.submitted.Answers)
	}
	if card.Instruction != "Empty every trough." || len(card.Proofs) != 2 || len(card.Questions) != 2 {
		t.Fatalf("returned card copy = %q/%d/%d, want the pinned document's", card.Instruction, len(card.Proofs), len(card.Questions))
	}

	// Pinned to the SEED (version 0): the seed asks nothing, so an answer is unknown.
	service = NewService(&fakeRepo{}).WithFastingStore(store).WithVerificationEnqueuer(&captureVerificationEnqueuer{}).WithSOPRules(rules, pinnedVersion(0))
	if _, err := service.SubmitFastingShed(context.Background(), operator, cmd); !errors.Is(err, domain.ErrSOPAnswerInvalid) {
		t.Fatalf("answers under the seed err = %v, want ErrSOPAnswerInvalid (unknown question)", err)
	}
}

// The operator's card list is decorated per card from each task's pinned version.
func TestListMyFastingShedCardsCarriesEachTasksPinnedCopy(t *testing.T) {
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}
	v2 := rulesWithMode(2, domain.RemovalModeRequired).Rules
	v2.FeedWaterRemoval.Instruction = "v2 says"
	v3 := rulesWithMode(3, domain.RemovalModeRequired).Rules
	v3.FeedWaterRemoval.Instruction = "v3 says"
	v3.FeedWaterRemoval.Proofs[0].Title = "Feed away"
	store := &twoCardStore{}
	service := NewService(&fakeRepo{}).WithFeedWaterRemovalCutoff(eightPM).WithFastingStore(store).WithSOPRules(versionedRules{2: v2, 3: v3}, pinnedVersion(0))
	page, err := service.ListMyFastingShedCards(context.Background(), operator, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].Instruction != "v2 says" || page.Items[1].Instruction != "v3 says" {
		t.Fatalf("instructions = %q / %q, want each card's own pinned copy", page.Items[0].Instruction, page.Items[1].Instruction)
	}
	if page.Items[1].Proofs[0].Title != "Feed away" || page.Items[0].Proofs[0].Title == "Feed away" {
		t.Fatalf("slot titles leaked across versions: %q / %q", page.Items[0].Proofs[0].Title, page.Items[1].Proofs[0].Title)
	}
}

// versionedRules answers RulesVersion per version and PublishedRules with the highest.
type versionedRules map[int]domain.Rules

func (v versionedRules) PublishedRules(context.Context, string) (domain.Rules, error) {
	best := domain.Rules{}
	for _, r := range v {
		if r.Version > best.Version {
			best = r
		}
	}
	return best, nil
}

func (v versionedRules) RulesVersion(_ context.Context, _ string, version int) (domain.Rules, error) {
	if version == 0 {
		return domain.SeededRules(), nil
	}
	r, ok := v[version]
	if !ok {
		return domain.Rules{}, ports.ErrSOPVersionUnknown
	}
	return r, nil
}

type twoCardStore struct{ fakeFastingStore }

func (s *twoCardStore) ListFastingShedCardsForOperator(context.Context, string, string, time.Time, ports.RemovalCutoffs, string, int) (domain.FastingShedCardPage, error) {
	return domain.FastingShedCardPage{Items: []domain.FastingShedCard{
		{FastingTaskID: fastingTaskID, CampaignShedID: shedB, SOPVersion: 2},
		{FastingTaskID: "00000000-0000-4000-8000-000000000903", CampaignShedID: shedB, SOPVersion: 3},
	}}, nil
}

// The removal card's captures are the pinned document's slots: a compulsory photo missing is
// refused by name before the store, a full submit hands the store every slot's kind and the
// captures in slot order (what the verifier item carries), and an older phone's legacy pair
// maps onto the seeded slots. Mutation-tested by dropping the legacy-pair mapping (the last
// case goes red).
func TestSubmitFastingShedJudgesCapturesByThePinnedSlots(t *testing.T) {
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}
	rules := rulesWithMode(7, domain.RemovalModeRequired)
	rules.Rules.FeedWaterRemoval.Proofs = []domain.RemovalProofSlot{
		{Key: "feed_video", Title: "Feed removed", Kind: domain.RemovalProofKindVideo, Required: true},
		{Key: "trough_photo", Title: "Empty trough", Kind: domain.RemovalProofKindPhoto, Required: true},
		{Key: "gate", Title: "Gate closed", Kind: domain.RemovalProofKindEither, Required: false},
	}
	rules.Rules.FeedWaterRemoval.Questions = []domain.SOPQuestion{
		{ID: "all_pens", Kind: domain.SOPQuestionChoice, Title: "Every pen emptied?", Required: false, Options: []domain.SOPOption{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}},
		{ID: "why", Kind: domain.SOPQuestionText, Title: "Why not?", Required: false},
	}
	submittedAt := time.Date(2026, 7, 28, 15, 0, 0, 0, time.UTC)
	newStore := func() *fakeFastingStore {
		return &fakeFastingStore{submitResult: domain.FastingShedSubmitResult{
			Card:     domain.FastingShedCard{FastingTaskID: fastingTaskID, CampaignShedID: shedB, FastingShedID: "00000000-0000-4000-8000-000000000902", Status: domain.FastingStatusPendingVerification, RowVersion: 1},
			Evidence: domain.FastingShedProof{FastingShedID: "00000000-0000-4000-8000-000000000902", CampaignShedID: shedB, Proofs: domain.RemovalProofRefs{"feed_video": proofThree, "trough_photo": proofFour, "gate": proofOne}, ProofKinds: map[string]string{proofOne: "photo"}, Answers: domain.SOPAnswers{"all_pens": json.RawMessage(`"no"`), "why": json.RawMessage(`"gate stuck"`)}, RowVersion: 1},
			Task:     domain.FastingTask{TenantID: testTenant, FastingTaskID: fastingTaskID, CampaignID: "00000000-0000-4000-8000-000000000501", OperatorUserID: testOp, SubmittedAt: &submittedAt},
		}}
	}
	store := newStore()
	enqueuer := &captureVerificationEnqueuer{}
	service := NewService(&fakeRepo{}).WithFastingStore(store).WithVerificationEnqueuer(enqueuer).WithSOPRules(rules, pinnedVersion(7))

	cmd := domain.SubmitFastingShed{FastingTaskID: fastingTaskID, CampaignShedID: shedB, IdempotencyKey: "fast-slots", Proofs: domain.RemovalProofRefs{"feed_video": proofThree}}
	_, err := service.SubmitFastingShed(ctxBg(), operator, cmd)
	var pe *domain.ProofError
	if !errors.Is(err, domain.ErrSOPProofInvalid) || !errors.As(err, &pe) || pe.SlotKey != "trough_photo" {
		t.Fatalf("missing compulsory photo err = %v, want ErrSOPProofInvalid naming trough_photo", err)
	}
	if store.submitCalls != 0 {
		t.Fatal("store must not be reached by a submit missing a compulsory capture")
	}

	cmd.Proofs = domain.RemovalProofRefs{"gate": proofOne, "trough_photo": proofFour, "feed_video": proofThree}
	if _, err := service.SubmitFastingShed(ctxBg(), operator, cmd); err != nil {
		t.Fatalf("full submit err = %v", err)
	}
	if store.submitted.SlotKinds["trough_photo"] != domain.RemovalProofKindPhoto || store.submitted.SlotKinds["gate"] != domain.RemovalProofKindEither {
		t.Fatalf("store slot kinds = %v, want the document's kinds", store.submitted.SlotKinds)
	}
	if got := store.submitted.OrderedRefs; len(got) != 3 || got[0] != proofThree || got[1] != proofFour || got[2] != proofOne {
		t.Fatalf("ordered refs = %v, want slot order [feed trough gate]", got)
	}
	if store.submitted.FeedProofRef != proofThree || store.submitted.WaterProofRef != "" {
		t.Fatalf("legacy mirrors = feed %q water %q, want feed_video mirrored and no water slot", store.submitted.FeedProofRef, store.submitted.WaterProofRef)
	}
	if got := enqueuer.received.MediaRefs; len(got) != 3 || got[1] != proofFour {
		t.Fatalf("verifier media refs = %v, want every capture in slot order", got)
	}
	// Each proof names itself for the verifier: the slot's title, and the kind the register judged
	// the capture to be -- the `either` gate was answered with a photo.
	meta := enqueuer.received.MediaMeta
	if len(meta) != 3 || meta[0] != (VerificationMediaMeta{Label: "Feed removed", Kind: "video"}) || meta[1] != (VerificationMediaMeta{Label: "Empty trough", Kind: "photo"}) || meta[2] != (VerificationMediaMeta{Label: "Gate closed", Kind: "photo"}) {
		t.Fatalf("verifier media meta = %+v, want the slot titles with the captured kinds", meta)
	}
	// The operator's answers reach the verifier as context rows, in farm words (review round 3).
	ctxRows := enqueuer.received.ContextRows
	if len(ctxRows) != 2 || ctxRows[0] != (VerificationContextRow{Label: "Every pen emptied?", Value: "No"}) || ctxRows[1] != (VerificationContextRow{Label: "Why not?", Value: "gate stuck"}) {
		t.Fatalf("verifier context rows = %+v, want the recorded answers under the question titles", ctxRows)
	}

	// An older phone: the legacy pair under the SEEDED document maps onto feed_video / water_video.
	store = newStore()
	service = NewService(&fakeRepo{}).WithFastingStore(store).WithVerificationEnqueuer(&captureVerificationEnqueuer{}).WithSOPRules(rules, pinnedVersion(0))
	legacy := domain.SubmitFastingShed{FastingTaskID: fastingTaskID, CampaignShedID: shedB, IdempotencyKey: "fast-legacy", FeedProofRef: proofThree, WaterProofRef: proofFour}
	if _, err := service.SubmitFastingShed(ctxBg(), operator, legacy); err != nil {
		t.Fatalf("legacy pair under the seed err = %v", err)
	}
	if store.submitted.Proofs["feed_video"] != proofThree || store.submitted.Proofs["water_video"] != proofFour {
		t.Fatalf("legacy pair mapped to %v, want the seeded slots", store.submitted.Proofs)
	}
	// ...but under the document above (no water_video slot) that same phone is refused by name.
	service = NewService(&fakeRepo{}).WithFastingStore(newStore()).WithSOPRules(rules, pinnedVersion(7))
	if _, err := service.SubmitFastingShed(ctxBg(), operator, legacy); !errors.Is(err, domain.ErrSOPProofInvalid) {
		t.Fatalf("legacy pair under a re-authored document err = %v, want ErrSOPProofInvalid", err)
	}
}

func ctxBg() context.Context { return context.Background() }

// The removal evening is the SOP's when its document sets one, else the farm's: at 20:30 IST a
// farm evening of 20:00 refuses tomorrow while a SOP evening of 21:00 still allows it, and the
// served rule set carries the EFFECTIVE value so the phone never resolves it. Mutation-tested by
// making removalCutoff ignore rules.FeedWaterRemoval.CutoffTime (the first case goes red).
func TestTheSOPCutoffOverridesTheFarmEvening(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	evening := beforeCutoffClock("2026-07-29")().Add(10*time.Hour + 30*time.Minute) // 20:30 IST on the 28th
	cmd := validCreate()                                                            // weigh date 2026-07-29

	sopEvening := rulesWithMode(3, domain.RemovalModeRequired)
	sopEvening.Rules.FeedWaterRemoval.CutoffTime = "21:00"
	service := NewService(&capturingRepo{}).WithFeedWaterRemovalCutoff(eightPM).WithClock(func() time.Time { return evening }).WithSOPRules(sopEvening, pinnedVersion(0))
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("20:30 under a 21:00 SOP evening err = %v, want allowed", err)
	}
	catalog, err := service.PlannerCatalog(context.Background(), ceo, "2026-07-29")
	if err != nil || catalog.SOP.FeedWaterRemoval.CutoffTime != "21:00" {
		t.Fatalf("served cutoff = %q err %v, want the SOP's 21:00", catalog.SOP.FeedWaterRemoval.CutoffTime, err)
	}

	farmEvening := rulesWithMode(3, domain.RemovalModeRequired) // no cutoff of its own
	service = NewService(&capturingRepo{}).WithFeedWaterRemovalCutoff(eightPM).WithClock(func() time.Time { return evening }).WithSOPRules(farmEvening, pinnedVersion(0))
	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); !errors.Is(err, ports.ErrFastingWindowClosed) {
		t.Fatalf("20:30 under the farm's 20:00 err = %v, want ErrFastingWindowClosed", err)
	}
	catalog, err = service.PlannerCatalog(context.Background(), ceo, "2026-07-29")
	if err != nil || catalog.SOP.FeedWaterRemoval.CutoffTime != "20:00" {
		t.Fatalf("served cutoff = %q err %v, want the farm's 20:00 filled in", catalog.SOP.FeedWaterRemoval.CutoffTime, err)
	}
}

// The card list's window opens at the evening of EACH card's PINNED version, not the
// evening the latest publish chose: v2 sets 21:30, v3 (the published one) sets 23:00, the
// seed sets nothing (farm 20:00), and a version the farm never published (9) falls back to
// the farm evening so tonight's card is still listed.
func TestListMyFastingShedCardsWindowsEachCardOnItsPinnedEvening(t *testing.T) {
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}
	v2 := rulesWithMode(2, domain.RemovalModeRequired).Rules
	v2.FeedWaterRemoval.CutoffTime = "21:30"
	v3 := rulesWithMode(3, domain.RemovalModeRequired).Rules
	v3.FeedWaterRemoval.CutoffTime = "23:00"
	store := &fakeFastingStore{cardVersions: []int{0, 2, 3, 9}}
	service := NewService(&fakeRepo{}).WithFeedWaterRemovalCutoff(eightPM).WithFastingStore(store).WithSOPRules(versionedRules{2: v2, 3: v3}, pinnedVersion(0))
	if _, err := service.ListMyFastingShedCards(context.Background(), operator, "", 20); err != nil {
		t.Fatal(err)
	}
	got := store.listCutoffs
	if got.Default.String() != "20:00" {
		t.Fatalf("default = %s, want the farm evening 20:00", got.Default)
	}
	if got.For(2).String() != "21:30" || got.For(3).String() != "23:00" {
		t.Fatalf("per-version = v2 %s / v3 %s, want 21:30 / 23:00", got.For(2), got.For(3))
	}
	if got.For(0).String() != "20:00" || got.For(9).String() != "20:00" {
		t.Fatalf("seed / unknown = %s / %s, want the farm evening", got.For(0), got.For(9))
	}
	if _, ok := got.ByVersion[9]; ok {
		t.Fatal("an unpublished version must not carry an override")
	}
}

// A person ticked Weighing "Set up" on /people holds weighing.plan through their OWN access rows
// while their job role (operator) does not. The route gate lets them in on the person set; the
// service must judge from the SAME set, or the tick is route-green / service-403 (found on the
// throwaway stack, 2026-09-15). And the reverse: rows that resolved WITHOUT weighing.plan refuse
// a role that would have had it.
func TestPlannerCatalogHonoursThePersonResolvedPermissionSet(t *testing.T) {
	service := NewService(&fakeRepo{}).WithSOPRules(rulesWithMode(1, domain.RemovalModeRequired), pinnedVersion(0))
	ticked := domain.Actor{
		TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator},
		Permissions: []string{permissions.WeighingMonitor, permissions.WeighingPlan}, PermissionsResolved: true,
	}
	if _, err := service.PlannerCatalog(context.Background(), ticked, "2026-09-16"); err != nil {
		t.Fatalf("person-granted planner refused: %v", err)
	}
	roleOnly := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}
	if _, err := service.PlannerCatalog(context.Background(), roleOnly, "2026-09-16"); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("operator role without the tick: err = %v, want forbidden", err)
	}
	narrowed := domain.Actor{
		TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleCEOInternal},
		Permissions: []string{permissions.WeighingExecute}, PermissionsResolved: true,
	}
	if _, err := service.PlannerCatalog(context.Background(), narrowed, "2026-09-16"); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("rows resolved without weighing.plan must refuse even a planning role: err = %v", err)
	}
}

// A retry of an IDENTICAL client request must replay the task it created even after a later
// publish moved the stamps (version, default cap, dropped operator): the replay identity is the
// client's request, fixed BEFORE the rules touch the command (PR #274 review, finding 2).
func TestCreateCampaignReplayIdentityIsTheClientsRequestNotTheStampedCommand(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	no := false
	raw := validCreate()
	raw.PlannedCapPerDay = 0 // the SOP default fills this in
	raw.FastingOperatorUserID = ""
	raw.FeedWaterRemovalRequested = &no

	v3 := rulesWithMode(3, domain.RemovalModeOptional)
	v3.Rules.Planning.DefaultCapPerDay = 100
	first := &capturingRepo{}
	if _, err := NewService(first).WithClock(todayClock()).WithSOPRules(v3, pinnedVersion(0)).CreateCampaign(context.Background(), ceo, raw); err != nil {
		t.Fatal(err)
	}
	v4 := rulesWithMode(4, domain.RemovalModeOff)
	v4.Rules.Planning.DefaultCapPerDay = 250
	retry := &capturingRepo{}
	if _, err := NewService(retry).WithClock(todayClock()).WithSOPRules(v4, pinnedVersion(0)).CreateCampaign(context.Background(), ceo, raw); err != nil {
		t.Fatal(err)
	}
	if first.created.SOPVersion == retry.created.SOPVersion || first.created.PlannedCapPerDay == retry.created.PlannedCapPerDay {
		t.Fatalf("test premise: the stamped commands must differ (v%d cap %d vs v%d cap %d)", first.created.SOPVersion, first.created.PlannedCapPerDay, retry.created.SOPVersion, retry.created.PlannedCapPerDay)
	}
	if first.created.RequestFingerprint == "" || first.created.RequestFingerprint != retry.created.RequestFingerprint {
		t.Fatalf("replay identity = %q vs %q, want the SAME client fingerprint on both stamped commands", first.created.RequestFingerprint, retry.created.RequestFingerprint)
	}
	changed := raw
	changed.PlannedCapPerDay = 9
	other := &capturingRepo{}
	if _, err := NewService(other).WithClock(todayClock()).WithSOPRules(v4, pinnedVersion(0)).CreateCampaign(context.Background(), ceo, changed); err != nil {
		t.Fatal(err)
	}
	if other.created.RequestFingerprint == first.created.RequestFingerprint {
		t.Fatal("a DIFFERENT client request must not share the replay identity")
	}
}

// PR #274 review round 2, finding 2: the create succeeded, the response was lost, and a later
// publish WITHDREW the way of weighing the task used. The identical retry must replay the task
// it created; the current publish's rules must not judge a request that already succeeded.
func TestCreateCampaignReplaysBeforeTheCurrentPublishJudgesTheRetry(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	raw := validCreate()
	raw.IdempotencyKey = "lost-response-retry"
	created := domain.Campaign{CampaignID: "00000000-0000-4000-8000-000000000777", SOPVersion: 3}
	// The service fingerprints the request AFTER stamping the actor's tenant and identity.
	asSeen := raw
	asSeen.TenantID, asSeen.CreatedBy = ceo.TenantID, ceo.UserID
	repo := &capturingRepo{}
	repo.replays = map[string]domain.Campaign{raw.IdempotencyKey + "|" + domain.RequestFingerprint(asSeen): created}

	withdrawn := rulesWithMode(4, domain.RemovalModeRequired)
	withdrawn.Rules.Planning.Modes = []string{domain.CategoryPerShedPartition}
	for _, shed := range raw.Sheds {
		if withdrawn.Rules.ModeAllowed(shed.WeighingCategory) {
			t.Fatalf("test premise: the retry's mode %q must be one the later publish withdrew", shed.WeighingCategory)
		}
	}
	service := NewService(repo).WithClock(todayClock()).WithSOPRules(withdrawn, pinnedVersion(0))
	got, err := service.CreateCampaign(context.Background(), ceo, raw)
	if err != nil {
		t.Fatalf("identical retry after the mode was withdrawn: err = %v, want the created task replayed", err)
	}
	if got.CampaignID != created.CampaignID || repo.created.IdempotencyKey == raw.IdempotencyKey {
		t.Fatalf("replay = %+v (store reached: %v), want the task created earlier and no new create", got, repo.created.IdempotencyKey != "")
	}
	// A genuinely NEW request under the withdrawn mode is still refused.
	fresh := raw
	fresh.IdempotencyKey = "brand-new"
	if _, err := service.CreateCampaign(context.Background(), ceo, fresh); !errors.Is(err, ports.ErrModeNotAllowed) {
		t.Fatalf("new request under a withdrawn mode: err = %v, want ErrModeNotAllowed", err)
	}
}

// countingRules counts how many times a pinned version is read from the source.
type countingRules struct {
	versionedRules
	reads int
}

func (c *countingRules) RulesVersion(ctx context.Context, tenantID string, version int) (domain.Rules, error) {
	c.reads++
	return c.versionedRules.RulesVersion(ctx, tenantID, version)
}

type manyVersionsStore struct {
	fakeFastingStore
	cards []domain.FastingShedCard
}

func (s *manyVersionsStore) ListFastingShedCardsForOperator(context.Context, string, string, time.Time, ports.RemovalCutoffs, string, int) (domain.FastingShedCardPage, error) {
	return domain.FastingShedCardPage{Items: s.cards}, nil
}

// PR #274 review round 3, finding 1: a farm with a long publish history must not pay one SOP
// read per version on EVERY card refresh. A pinned version is immutable, so it is read ONCE per
// process: the first refresh reads each candidate version once, the second reads nothing.
func TestCardRefreshReadsEachPinnedVersionOnceNotPerRefresh(t *testing.T) {
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}
	source := &countingRules{versionedRules: versionedRules{}}
	versions := make([]int, 0, 50)
	cards := make([]domain.FastingShedCard, 0, 50)
	for v := 1; v <= 50; v++ {
		r := rulesWithMode(v, domain.RemovalModeRequired).Rules
		r.FeedWaterRemoval.CutoffTime = "21:30"
		source.versionedRules[v] = r
		versions = append(versions, v)
		cards = append(cards, domain.FastingShedCard{FastingTaskID: fastingTaskID, CampaignShedID: shedB, SOPVersion: v})
	}
	store := &manyVersionsStore{cards: cards}
	store.cardVersions = versions
	service := NewService(&fakeRepo{}).WithFeedWaterRemovalCutoff(eightPM).WithFastingStore(store).WithSOPRules(source, pinnedVersion(0))
	if _, err := service.ListMyFastingShedCards(context.Background(), operator, "", 1); err != nil {
		t.Fatal(err)
	}
	if source.reads > 50 {
		t.Fatalf("first refresh read the source %d times for 50 candidate versions; want at most one read per version", source.reads)
	}
	first := source.reads
	if _, err := service.ListMyFastingShedCards(context.Background(), operator, "", 1); err != nil {
		t.Fatal(err)
	}
	if source.reads != first {
		t.Fatalf("second refresh read the source %d more time(s); a pinned version is immutable and must be served from the cache", source.reads-first)
	}
}
