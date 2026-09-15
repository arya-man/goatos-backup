package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	fwrdomain "github.com/vgoats/goatos/backend/internal/feedwaterremoval/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
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
	page, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, atOpen, ports.RemovalCutoffs{Default: eightPMCutoff}, "", 20)
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
	// The window opens at the PINNED version's evening: with v4 set to 21:00 the card is
	// not yet listed at 20:00 even though the farm default has opened; an override for a
	// version the task is not pinned to changes nothing; the versions read names the pin.
	versions, err := repo.FastingCardSOPVersions(ctx, repoTenant, fastingOperator, atOpen)
	if err != nil || len(versions) != 1 || versions[0] != 4 {
		t.Fatalf("pinned versions = %v err %v, want [4]", versions, err)
	}
	ninePM := fwrdomain.MustCutoff(21, 0)
	held, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, atOpen, ports.RemovalCutoffs{Default: eightPMCutoff, ByVersion: map[int]fwrdomain.Cutoff{4: ninePM}}, "", 20)
	if err != nil {
		t.Fatalf("list cards under a later pinned evening: %v", err)
	}
	if len(held.Items) != 0 {
		t.Fatalf("cards listed at 20:00 under a 21:00 pinned evening: %d, want none", len(held.Items))
	}
	shown, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, atOpen, ports.RemovalCutoffs{Default: eightPMCutoff, ByVersion: map[int]fwrdomain.Cutoff{7: ninePM}}, "", 20)
	if err != nil || len(shown.Items) != len(page.Items) {
		t.Fatalf("an override for another version changed the list: %d vs %d (err %v)", len(shown.Items), len(page.Items), err)
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

// PR #274 review, finding 2: a create succeeds but its response is lost; a later publish moves
// the stamps (version, default cap); the identical client request is retried. The replay
// identity is the CLIENT's fingerprint the service fixed before the rules, so the retry returns
// the task it created rather than 409.
func TestCreateCampaignReplaysAfterALaterPublishMovedTheStamps(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	client := domain.CreateCampaign{
		TenantID: repoTenant, ParkID: repoPark,
		PeriodStartDate: "2026-11-02", PeriodEndDate: "2026-11-08", StartBusinessDate: "2026-11-04",
		OperatorUserID: repoOperator, CreatedBy: repoOperator, IdempotencyKey: "sop-replay-after-publish",
		Sheds: []domain.CreateCampaignShed{{LocationID: lcpShedOne, LocationType: "shed", DisplayName: "CBE Godel 1 - Part 8", WeighingCategory: domain.CategoryPerShedPartition}},
	}
	fingerprint := domain.RequestFingerprint(client)

	first := client
	first.RequestFingerprint, first.SOPVersion, first.PlannedCapPerDay = fingerprint, 3, 100
	created, err := repo.CreateCampaign(ctx, first)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	retry := client
	retry.RequestFingerprint, retry.SOPVersion, retry.PlannedCapPerDay = fingerprint, 4, 250
	replayed, err := repo.CreateCampaign(ctx, retry)
	if err != nil {
		t.Fatalf("retry after a later publish: err = %v, want the created task replayed", err)
	}
	if replayed.CampaignID != created.CampaignID || replayed.SOPVersion != 3 || replayed.PlannedCapPerDay != 100 {
		t.Fatalf("replay = %s v%d cap %d, want the FIRST task %s v3 cap 100", replayed.CampaignID, replayed.SOPVersion, replayed.PlannedCapPerDay, created.CampaignID)
	}
	// The pre-rules replay lookup the service asks first: exact fingerprint -> the task; a
	// different fingerprint or key -> nothing (never a conflict; the create decides that).
	if found, ok, err := repo.CampaignByIdempotencyKey(ctx, repoTenant, client.IdempotencyKey, fingerprint); err != nil || !ok || found.CampaignID != created.CampaignID {
		t.Fatalf("replay lookup = %v ok %v err %v, want the created task", found.CampaignID, ok, err)
	}
	if _, ok, err := repo.CampaignByIdempotencyKey(ctx, repoTenant, client.IdempotencyKey, "not-that-request"); err != nil || ok {
		t.Fatalf("replay lookup with another fingerprint: ok %v err %v, want not found", ok, err)
	}
	changed := client
	changed.PeriodEndDate = "2026-11-09"
	changed.RequestFingerprint, changed.SOPVersion, changed.PlannedCapPerDay = domain.RequestFingerprint(changed), 4, 250
	if _, err := repo.CreateCampaign(ctx, changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("a DIFFERENT request on the same key: err = %v, want idempotency conflict", err)
	}
}

// PR #274 review, findings 3 and 4: the kind the register judged an `either` capture to be
// rides every read -- the fresh submit, an exact replay (a retried verification enqueue) and
// the card list (the phone reopening the card with no local state) -- as {ref: kind} on the
// evidence and {slot key: kind} on the card.
func TestEitherSlotCapturedKindRidesEveryRead(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fastingID := seedFastingFixture(t, ctx, pool, "2026-09-04")
	repo := NewRepository(pool, 5*time.Second)
	const gatePhoto = "00000000-0000-4000-8000-0000000000e1"
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at, metadata)
VALUES ($1::uuid, $2::uuid, 'local', 'weighing-fasting-test/' || $1, 'image/jpeg', 'completed', 'park', $3::uuid, 'shed', $3::uuid, 'photo', $4::uuid, now(), '{"capture_source":"in_app_camera"}'::jsonb)
ON CONFLICT (proof_id) DO NOTHING`, gatePhoto, repoTenant, repoPark, fastingOperator)

	cmd := fastingSubmitShedA(fastingID, "either-kind-1")
	cmd.Answers = domain.SOPAnswers{"all_pens": json.RawMessage(`"yes"`)}
	cmd.Proofs = domain.RemovalProofRefs{"feed_video": fastingFeedProof, "water_video": fastingWaterProof, "gate": gatePhoto}
	cmd.SlotKinds = map[string]string{"feed_video": domain.RemovalProofKindVideo, "water_video": domain.RemovalProofKindVideo, "gate": domain.RemovalProofKindEither}
	cmd.OrderedRefs = []string{fastingFeedProof, fastingWaterProof, gatePhoto}
	first, err := repo.SubmitFastingShed(ctx, cmd)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if first.Evidence.ProofKinds[gatePhoto] != "photo" || first.Card.ProofKinds["gate"] != "photo" {
		t.Fatalf("fresh submit kinds = evidence %v card %v, want the gate as a photo", first.Evidence.ProofKinds, first.Card.ProofKinds)
	}
	replay, err := repo.SubmitFastingShed(ctx, cmd)
	if err != nil || !replay.Replayed {
		t.Fatalf("replay: err %v replayed %v", err, replay.Replayed)
	}
	if replay.Evidence.ProofKinds[gatePhoto] != "photo" {
		t.Fatalf("replay evidence kinds = %v, want the gate still a photo (a retried enqueue must not fall back to video)", replay.Evidence.ProofKinds)
	}
	// The replay evidence carries the recorded ANSWERS too, so a retried enqueue can hand the
	// verifier the same context rows the first one did (review round 3, finding 2).
	if string(replay.Evidence.Answers["all_pens"]) != `"yes"` {
		t.Fatalf("replay evidence answers = %v, want the submitted answers", replay.Evidence.Answers)
	}
	atOpen := time.Date(2026, 9, 3, 20, 0, 0, 0, biztime.DefaultLocation())
	page, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, atOpen, ports.RemovalCutoffs{Default: eightPMCutoff}, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	card, ok := cardByShed(page.Items, repoAnimalScope)
	if !ok || card.ProofKinds["gate"] != "photo" || card.ProofKinds["feed_video"] != "video" {
		t.Fatalf("card list kinds = %v (found %v), want gate photo / feed_video video", card.ProofKinds, ok)
	}
}

// History and future tasks must not displace tonight's oldest active pin.
func TestFastingCutoffVersionsKeepAllTonightPins(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFastingFixture(t, ctx, pool, "2026-09-04")
	repo := NewRepository(pool, 5*time.Second)
	for v := 1; v <= 52; v++ {
		date := "2026-09-04"
		if v == 51 {
			date = "2026-09-03"
		}
		if v == 52 {
			date = "2026-09-05"
		}
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", 20000+v)
		execWeighingTestSQL(t, ctx, pool, `INSERT INTO weighing_campaigns
   (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by, sop_version)
   VALUES ($1::uuid,$2::uuid,$3::uuid,$4::date,$4::date,$4::date,'published',100,$5::uuid,$5::uuid,$6)`, id, repoTenant, repoPark, date, repoOperator, v)
		execWeighingTestSQL(t, ctx, pool, `INSERT INTO weighing_fasting_tasks
   (tenant_id,campaign_id,park_id,operator_user_id,planned_weigh_date,weigh_business_date,idempotency_key,created_by)
   VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::date,$5::date,$6,$4::uuid)`, repoTenant, id, repoPark, fastingOperator, date, fmt.Sprintf("pin-%d", v))
	}
	at := time.Date(2026, 9, 3, 18, 0, 0, 0, biztime.DefaultLocation())
	versions, err := repo.FastingCardSOPVersions(ctx, repoTenant, fastingOperator, at)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, v := range versions {
		seen[v] = true
	}
	if len(versions) != 51 || !seen[1] || !seen[50] || seen[51] || seen[52] {
		t.Fatalf("tonight pins truncated or wrong date slice: %v", versions)
	}
	// Execute the old capped shape against the same fixture: the failing-before counterexample.
	oldRows, err := pool.Query(ctx, fastingCardSOPVersionsSQL+" LIMIT 20", repoTenant, fastingOperator, at)
	if err != nil {
		t.Fatal(err)
	}
	oldCount, oldHasFirst := 0, false
	for oldRows.Next() {
		var v int
		if err := oldRows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		oldCount++
		oldHasFirst = oldHasFirst || v == 1
	}
	oldRows.Close()
	if oldCount != 20 || oldHasFirst {
		t.Fatalf("old-shape premise failed: count=%d oldest=%v", oldCount, oldHasFirst)
	}
	plans, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+fastingCardSOPVersionsSQL, repoTenant, fastingOperator, at)
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for plans.Next() {
		var line string
		if err := plans.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	plans.Close()
	t.Logf("same-fixture coverage: old=%d, fixed=%d (includes v1); plan:\n%s", oldCount, len(versions), strings.Join(plan, "\n"))

	// Same business-day slice even when UTC and IST calendar dates differ.
	versions, err = repo.FastingCardSOPVersions(ctx, repoTenant, fastingOperator, time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC))
	if err != nil || len(versions) != 51 {
		t.Fatalf("IST day boundary: %v %v", versions, err)
	}
}

// No farm default is needed when tonight's pin owns its cutoff. Old evenings
// remain visible; future and unresolved tonight pins must never open early.
func TestSOPCardVisibilityWithoutFarmDefault(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFastingFixture(t, ctx, pool, "2026-09-04")
	if _, err := pool.Exec(ctx, `UPDATE weighing_campaigns SET sop_version=7 WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, repoTenant, repoCampaign); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(pool, 5*time.Second)
	for _, tc := range []struct {
		name, now string
		override  bool
		visible   bool
	}{
		{"future", "2026-09-02 23:59", true, false},
		{"before", "2026-09-03 21:29", true, false},
		{"at cutoff", "2026-09-03 21:30", true, true},
		{"unresolved tonight", "2026-09-03 23:59", false, false},
		{"past evening", "2026-09-04 00:00", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.ParseInLocation("2006-01-02 15:04", tc.now, biztime.DefaultLocation())
			if err != nil {
				t.Fatal(err)
			}
			cutoffs := ports.RemovalCutoffs{}
			if tc.override {
				cutoffs.ByVersion = map[int]fwrdomain.Cutoff{7: fwrdomain.MustCutoff(21, 30)}
			}
			page, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, now, cutoffs, "", 20)
			if err != nil {
				t.Fatal(err)
			}
			if (len(page.Items) > 0) != tc.visible {
				t.Fatalf("got %d cards; visible=%v", len(page.Items), tc.visible)
			}
		})
	}
}
