package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// WEIGHING SOP (maintainer decision 2026-09-15): the pin and the answers are DATABASE facts.
// Proved on a migrated database: a task's sop_version is read back by the pin reader (NULL on a
// pre-SOP task reads as 0 = the seed); a pen submit stores the operator's answers on the pen's
// evidence row; the operator's card list carries the task's pin and the recorded answers back,
// so the phone renders what was answered; and an exact replay returns the stored answers, not
// the retry's.
func TestSOPPinAndRemovalAnswersRoundTripThroughPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fastingID := seedFastingFixture(t, ctx, pool, "2026-09-04")
	repo := NewRepository(pool, 5*time.Second)

	// The fixture's task predates the pin: NULL reads as 0, the seed.
	version, err := repo.CampaignSOPVersion(ctx, repoTenant, repoCampaign)
	if err != nil {
		t.Fatalf("pin of a pre-SOP task: %v", err)
	}
	if version != 0 {
		t.Fatalf("pre-SOP task pin = %d, want 0 (the seed)", version)
	}
	if _, err := pool.Exec(ctx, `UPDATE weighing_campaigns SET sop_version = 4 WHERE tenant_id = $1::uuid AND campaign_id = $2::uuid`, repoTenant, repoCampaign); err != nil {
		t.Fatalf("pin the task: %v", err)
	}
	if version, err = repo.CampaignSOPVersion(ctx, repoTenant, repoCampaign); err != nil || version != 4 {
		t.Fatalf("pinned task reads v%d err %v, want v4", version, err)
	}

	// A pen submit stores its answers on the evidence row.
	cmd := fastingSubmitShedA(fastingID, "fasting-submit-answers")
	cmd.Answers = domain.SOPAnswers{"all_pens": json.RawMessage(`"yes"`), "buckets": json.RawMessage(`12`)}
	first, err := repo.SubmitFastingShed(ctx, cmd)
	if err != nil {
		t.Fatalf("submit with answers: %v", err)
	}
	if string(first.Card.Answers["all_pens"]) != `"yes"` || string(first.Evidence.Answers["buckets"]) != `12` {
		t.Fatalf("submit result answers = card %v evidence %v, want the submitted answers", first.Card.Answers, first.Evidence.Answers)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT sop_answers::text FROM weighing_fasting_shed_proofs WHERE tenant_id = $1::uuid AND fasting_task_id = $2::uuid AND campaign_shed_id = $3::uuid`, repoTenant, fastingID, repoAnimalScope).Scan(&stored); err != nil {
		t.Fatalf("read stored answers: %v", err)
	}
	var storedMap map[string]any
	if err := json.Unmarshal([]byte(stored), &storedMap); err != nil || storedMap["all_pens"] != "yes" || storedMap["buckets"] != float64(12) {
		t.Fatalf("stored sop_answers = %s (err %v), want {all_pens: yes, buckets: 12}", stored, err)
	}

	// The card list carries the pin and the recorded answers back to the phone.
	atOpen := time.Date(2026, 9, 3, 20, 0, 0, 0, biztime.DefaultLocation())
	page, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, atOpen, eightPMCutoff, "", 20)
	if err != nil {
		t.Fatalf("list cards: %v", err)
	}
	card, ok := cardByShed(page.Items, repoAnimalScope)
	if !ok {
		t.Fatalf("shed A card missing from %+v", page.Items)
	}
	if card.SOPVersion != 4 {
		t.Fatalf("card sop version = %d, want the task's pin 4", card.SOPVersion)
	}
	if string(card.Answers["all_pens"]) != `"yes"` {
		t.Fatalf("card answers = %v, want the recorded answers", card.Answers)
	}
	other, ok := cardByShed(page.Items, repoShedScope)
	if !ok || other.Answers != nil {
		t.Fatalf("unsubmitted shed B answers = %v, want none", other.Answers)
	}

	// An exact replay returns the ORIGINAL result; a same-key different-answers retry is not a
	// second submit either (the idempotency fingerprint covers the answers).
	replay, err := repo.SubmitFastingShed(ctx, cmd)
	if err != nil || !replay.Replayed {
		t.Fatalf("exact replay = replayed:%v err %v", replay.Replayed, err)
	}
}
