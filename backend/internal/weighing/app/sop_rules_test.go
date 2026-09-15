package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	fwrdomain "github.com/vgoats/goatos/backend/internal/feedwaterremoval/domain"
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

func (s *twoCardStore) ListFastingShedCardsForOperator(context.Context, string, string, time.Time, fwrdomain.Cutoff, string, int) (domain.FastingShedCardPage, error) {
	return domain.FastingShedCardPage{Items: []domain.FastingShedCard{
		{FastingTaskID: fastingTaskID, CampaignShedID: shedB, SOPVersion: 2},
		{FastingTaskID: "00000000-0000-4000-8000-000000000903", CampaignShedID: shedB, SOPVersion: 3},
	}}, nil
}
