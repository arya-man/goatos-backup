package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// GROWTH, END TO END ON THE PRODUCTION PATH (maintainer decisions 2026-09-23,
// docs/features/shifting/shifting-rewrite-tag-rules.md). Raise through the real route, approve the
// real request, complete through the real apply transaction, and read the farm's rows back. Only
// inputs a farm has before a movement exists are seeded: animals, their stage and sex, the stage
// vocabulary, and what a pen was left set to.

func setGoatSex(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, sex string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE goats SET sex = $3 WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		countsTenant, goatID, sex); err != nil {
		t.Fatalf("set goat sex: %v", err)
	}
}

// raiseAndApplyGrowth raises a growth move through the route, checks what was stored, then approves
// and completes it. It returns nothing: callers assert the rows the apply wrote.
func raiseAndApplyGrowth(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, mux *http.ServeMux, repo *Repository,
	key string, goatIDs []string, wantTarget, wantAdopt string,
) {
	t.Helper()
	res := raiseTypedShifting(t, mux, key, domain.ShiftTypeGrowth, goatIDs)
	if res.Code != http.StatusOK {
		t.Fatalf("raise status=%d body=%s, want 200", res.Code, res.Body.String())
	}
	var raised struct {
		ShiftingEventID string `json:"shifting_event_id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &raised); err != nil || raised.ShiftingEventID == "" {
		t.Fatalf("decode raise response %q: %v", res.Body.String(), err)
	}
	category, target, adopt := storedShiftingSnapshot(t, ctx, pool, raised.ShiftingEventID)
	if category != domain.ShiftTypeGrowth || target != wantTarget || adopt != wantAdopt {
		t.Fatalf("stored category=%q target=%q adopt=%q, want growth/%q/%q", category, target, adopt, wantTarget, wantAdopt)
	}
	approvalRequestID := pendingApprovalForShifting(t, ctx, pool, raised.ShiftingEventID)
	if _, _, err := approveShifting(repo, ctx, key, approvalRequestID, raised.ShiftingEventID, goatIDs); err != nil {
		t.Fatalf("park-head approval: %v", err)
	}
	completed, _, err := submitShiftingForVerification(repo, ctx, key, raised.ShiftingEventID, "")
	if err != nil {
		t.Fatalf("operator completion: %v", err)
	}
	if completed.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("event status=%q after completion, want applied", completed.EventStatus)
	}
}

// A K2 animal into a pen with NO stage set that holds one K3 and one sick-bay animal whose stage is
// the pen name ICU-Kid. ICU-Kid is a pen tag, not a clinical state, so it is not stripped from the
// residents and the pen used to read "This destination holds a mix of tags". Now it moves, becomes
// K3, and the pen's Stage is left alone (only an EMPTY pen is re-tagged).
func TestGrowthEndToEndJoinsAPenHoldingK3AndAnICUKidAnimal(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, repo := typedE2EStack(t, pool)

	mover := "00000000-0000-4000-8000-00000000f351"
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K2")
	seedApprovalGoatWithStage(t, ctx, pool, "00000000-0000-4000-8000-00000000f352", countsShedB, "K3")
	seedApprovalGoatWithStage(t, ctx, pool, "00000000-0000-4000-8000-00000000f353", countsShedB, "ICU-Kid")
	for _, stage := range []string{"K2", "K3", "ICU-Kid"} {
		seedStageVocabulary(t, ctx, pool, stage)
	}

	raiseAndApplyGrowth(t, ctx, pool, mux, repo, "e2e-growth-k3-icukid", []string{mover}, "K3", "")

	if got := goatStage(t, ctx, pool, mover); got != "K3" {
		t.Fatalf("mover stage=%q, want K3", got)
	}
	if got := shedProfileStage(t, ctx, pool, countsShedB); got != "" {
		t.Fatalf("occupied pen stage=%q after growth, want untouched (blank)", got)
	}
}

// A K2 animal into a pen with NO stage set holding one K3 and one ICU animal. A true clinical state
// is stripped from the residents, so this already worked before 2026-09-23; kept as a guard that
// the ICU resident never becomes the stamped tag.
func TestGrowthEndToEndJoinsAPenHoldingK3AndICU(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, repo := typedE2EStack(t, pool)

	mover := "00000000-0000-4000-8000-00000000f301"
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K2")
	seedApprovalGoatWithStage(t, ctx, pool, "00000000-0000-4000-8000-00000000f302", countsShedB, "K3")
	seedApprovalGoatWithStage(t, ctx, pool, "00000000-0000-4000-8000-00000000f303", countsShedB, "ICU")
	for _, stage := range []string{"K2", "K3", "ICU"} {
		seedStageVocabulary(t, ctx, pool, stage)
	}

	raiseAndApplyGrowth(t, ctx, pool, mux, repo, "e2e-growth-k3-icu", []string{mover}, "K3", "")

	if got := goatShed(t, ctx, pool, mover); got != countsShedB {
		t.Fatalf("mover shed=%s, want destination %s", got, countsShedB)
	}
	if got := goatStage(t, ctx, pool, mover); got != "K3" {
		t.Fatalf("mover stage=%q, want K3", got)
	}
	if got := shedProfileStage(t, ctx, pool, countsShedB); got != "" {
		t.Fatalf("occupied pen stage=%q after growth, want untouched (blank)", got)
	}
}

// A resident carrying the next stage wins over what the pen is SET to: the pen is set F2 but a K3
// still stands in it, so a K2 moving in becomes K3 (maintainer answer 2026-09-23).
func TestGrowthEndToEndResidentNextStageWinsOverThePensSetStage(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, repo := typedE2EStack(t, pool)

	mover := "00000000-0000-4000-8000-00000000f311"
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K2")
	seedApprovalGoatWithStage(t, ctx, pool, "00000000-0000-4000-8000-00000000f312", countsShedB, "K3")
	seedStageVocabulary(t, ctx, pool, "K2")
	seedStageVocabulary(t, ctx, pool, "K3")
	seedShedProfile(t, ctx, pool, countsShedB, "F2")

	raiseAndApplyGrowth(t, ctx, pool, mux, repo, "e2e-growth-set-f2", []string{mover}, "K3", "")

	if got := goatStage(t, ctx, pool, mover); got != "K3" {
		t.Fatalf("mover stage=%q, want K3", got)
	}
}

// Growth into an EMPTY pen never stops: the K2 takes its next stage and the pen's Stage, left set
// to F2 by its old residents, becomes K3.
func TestGrowthEndToEndIntoEmptyPenTakesNextStageAndRetagsThePen(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, repo := typedE2EStack(t, pool)

	mover := "00000000-0000-4000-8000-00000000f321"
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K2")
	seedStageVocabulary(t, ctx, pool, "K2")
	seedStageVocabulary(t, ctx, pool, "K3")
	seedShedProfile(t, ctx, pool, countsShedB, "F2")

	raiseAndApplyGrowth(t, ctx, pool, mux, repo, "e2e-growth-empty", []string{mover}, "K3", "K3")

	if got := goatStage(t, ctx, pool, mover); got != "K3" {
		t.Fatalf("mover stage=%q, want K3", got)
	}
	if got := shedProfileStage(t, ctx, pool, countsShedB); got != "K3" {
		t.Fatalf("empty pen stage=%q after growth, want K3 replacing F2", got)
	}
}

// A male K3 into an empty pen goes straight to F2-Male, by sex.
func TestGrowthEndToEndK3IntoEmptyPenSplitsBySex(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, repo := typedE2EStack(t, pool)

	mover := "00000000-0000-4000-8000-00000000f331"
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K3")
	setGoatSex(t, ctx, pool, mover, "male")
	seedStageVocabulary(t, ctx, pool, "K3")
	seedStageVocabulary(t, ctx, pool, "F2-Male")

	raiseAndApplyGrowth(t, ctx, pool, mux, repo, "e2e-growth-k3-male", []string{mover}, "F2-Male", "F2-Male")

	if got := goatStage(t, ctx, pool, mover); got != "F2-Male" {
		t.Fatalf("mover stage=%q, want F2-Male", got)
	}
	if got := shedProfileStage(t, ctx, pool, countsShedB); got != "F2-Male" {
		t.Fatalf("empty pen stage=%q, want F2-Male", got)
	}
}

// A male and a female K3 need different next stages; one raise stamps one tag, so the raise is
// refused with farm copy and NOTHING durable is written.
func TestGrowthEndToEndGroupNeedingTwoStagesIsRefusedAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, _ := typedE2EStack(t, pool)

	male := "00000000-0000-4000-8000-00000000f341"
	female := "00000000-0000-4000-8000-00000000f342"
	seedApprovalGoatWithStage(t, ctx, pool, male, countsShedA, "K3")
	seedApprovalGoatWithStage(t, ctx, pool, female, countsShedA, "K3")
	setGoatSex(t, ctx, pool, male, "male")
	for _, stage := range []string{"K3", "F2-Male", "F2-Female"} {
		seedStageVocabulary(t, ctx, pool, stage)
	}

	before := time.Now()
	res := raiseTypedShifting(t, mux, "e2e-growth-split", domain.ShiftTypeGrowth, []string{male, female})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("raise status=%d body=%s, want 400", res.Code, res.Body.String())
	}
	var failure struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &failure); err != nil {
		t.Fatalf("decode refusal: %v", err)
	}
	if failure.Code != "growth_group_needs_split" ||
		failure.Message != "These animals need different next stages. Move each stage in its own shifting" {
		t.Fatalf("refusal = %+v", failure)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM shifting_events WHERE tenant_id = $1::uuid AND created_at >= $2`,
		countsTenant, before); got != 0 {
		t.Fatalf("refused raise wrote %d shifting event(s), want 0", got)
	}
}
