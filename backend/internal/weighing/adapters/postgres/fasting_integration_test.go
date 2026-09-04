package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// Fasting (feed & water removal) precondition integration proofs (maintainer
// decision 2026-09-03; migrations 000243/000245, domain/fasting.go). The list
// serves ONE CARD PER SHED and the submit is PER SHED (maintainer correction
// #2, same day); the round's midnight gate reads only the PARENT's
// submitted_at, stamped when the last shed goes in.

const (
	fastingFeedProof   = "00000000-0000-4000-8000-000000009501"
	fastingWaterProof  = "00000000-0000-4000-8000-000000009502"
	fastingFeedProofB  = "00000000-0000-4000-8000-000000009505"
	fastingWaterProofB = "00000000-0000-4000-8000-000000009506"
	fastingOperator    = "00000000-0000-4000-8000-000000000305"
)

func fastingSubmitShedA(fastingID, key string) domain.SubmitFastingShed {
	return domain.SubmitFastingShed{
		TenantID: repoTenant, FastingTaskID: fastingID, CampaignShedID: repoAnimalScope,
		FeedProofRef: fastingFeedProof, WaterProofRef: fastingWaterProof,
		IdempotencyKey: key, SubmittedBy: fastingOperator,
	}
}

func fastingSubmitShedB(fastingID, key string) domain.SubmitFastingShed {
	return domain.SubmitFastingShed{
		TenantID: repoTenant, FastingTaskID: fastingID, CampaignShedID: repoShedScope,
		FeedProofRef: fastingFeedProofB, WaterProofRef: fastingWaterProofB,
		IdempotencyKey: key, SubmittedBy: fastingOperator,
	}
}

// insertLiveCameraProof seeds a COMPLETED in-app-camera video artifact — the
// shape the Android proof pipeline actually uploads (capture_source metadata is
// what the submit-path validator pins on).
func insertLiveCameraProof(t *testing.T, ctx context.Context, pool *pgxpool.Pool, proofID string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at, metadata)
VALUES ($1::uuid, $2::uuid, 'local', 'weighing-fasting-test/' || $1, 'video/mp4', 'completed', 'park', $3::uuid, 'shed', $3::uuid, 'video', $4::uuid, now(), '{"capture_source":"in_app_camera"}'::jsonb)
ON CONFLICT (proof_id) DO NOTHING`,
		proofID, repoTenant, repoPark, fastingOperator)
}

func seedFastingFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, weighDate string) string {
	t.Helper()
	seedWeighingObservationFixture(t, ctx, pool)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, fastingOperator, repoPark)
	var fastingID string
	if err := pool.QueryRow(ctx, `
INSERT INTO weighing_fasting_tasks (tenant_id, campaign_id, park_id, operator_user_id, planned_weigh_date, weigh_business_date, idempotency_key, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::date, $5::date, 'fasting-fixture:' || $5, $4::uuid)
ON CONFLICT (tenant_id, campaign_id) DO UPDATE SET
  operator_user_id = EXCLUDED.operator_user_id,
  planned_weigh_date = EXCLUDED.planned_weigh_date,
  weigh_business_date = EXCLUDED.weigh_business_date,
  status = 'open', feed_proof_ref = NULL, water_proof_ref = NULL,
  submitted_by = NULL, submitted_at = NULL
RETURNING fasting_task_id::text`,
		repoTenant, repoCampaign, repoPark, fastingOperator, weighDate).Scan(&fastingID); err != nil {
		t.Fatalf("seed fasting row: %v", err)
	}
	insertLiveCameraProof(t, ctx, pool, fastingFeedProof)
	insertLiveCameraProof(t, ctx, pool, fastingWaterProof)
	insertLiveCameraProof(t, ctx, pool, fastingFeedProofB)
	insertLiveCameraProof(t, ctx, pool, fastingWaterProofB)
	execWeighingTestSQL(t, ctx, pool, `DELETE FROM weighing_fasting_shed_proofs WHERE tenant_id=$1::uuid AND fasting_task_id=$2::uuid`, repoTenant, fastingID)
	return fastingID
}

func cardByShed(items []domain.FastingShedCard, campaignShedID string) (domain.FastingShedCard, bool) {
	for _, it := range items {
		if it.CampaignShedID == campaignShedID {
			return it, true
		}
	}
	return domain.FastingShedCard{}, false
}

// THE 20:00 IST VISIBILITY WINDOW, proved on a database round trip: at 19:59 on
// the removal evening the operator's list is empty; at 20:00 ONE CARD PER SHED
// is served, each NAMING its shed. The clock is the CALLER's, bound as a
// parameter.
func TestFastingListVisibilityOpensAtEightPMIST(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fastingID := seedFastingFixture(t, ctx, pool, "2026-09-04")
	repo := NewRepository(pool, 5*time.Second)

	ist := biztime.DefaultLocation()
	before := time.Date(2026, 9, 3, 19, 59, 0, 0, ist)
	page, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, before, "", 20)
	if err != nil {
		t.Fatalf("list before window: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("cards visible at 19:59 IST; the removal evening opens at 20:00")
	}

	atOpen := time.Date(2026, 9, 3, 20, 0, 0, 0, ist)
	page, err = repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, atOpen, "", 20)
	if err != nil {
		t.Fatalf("list at window open: %v", err)
	}
	// ONE CARD PER SHED (maintainer correction #2): the two-bucket round serves
	// exactly two cards — never one umbrella card the sheds hide inside.
	if len(page.Items) != 2 {
		t.Fatalf("cards at 20:00 IST = %d, want ONE PER SHED (2): %+v", len(page.Items), page.Items)
	}
	for _, card := range page.Items {
		if card.FastingTaskID != fastingID {
			t.Fatalf("card round = %s, want %s", card.FastingTaskID, fastingID)
		}
		if card.ShedLabel == "" || card.SubjectLabel != domain.FastingShedSubjectLabel(card.ShedLabel) {
			t.Fatalf("card must NAME its shed in its own title, got %+v", card)
		}
		if card.RemovalBusinessDate != "2026-09-03" || card.WeighBusinessDate != "2026-09-04" {
			t.Fatalf("dates removal=%s weigh=%s, want 2026-09-03 / 2026-09-04", card.RemovalBusinessDate, card.WeighBusinessDate)
		}
		if card.Status != domain.FastingStatusOpen {
			t.Fatalf("unrecorded card status = %s, want open", card.Status)
		}
		if card.SubmittedAt != nil {
			t.Fatalf("round submitted_at set before any submit: %+v", card)
		}
	}
	if _, ok := cardByShed(page.Items, repoAnimalScope); !ok {
		t.Fatal("bucket A has no card of its own")
	}
	if _, ok := cardByShed(page.Items, repoShedScope); !ok {
		t.Fatal("bucket B has no card of its own")
	}

	// Another operator sees nothing: the cards belong to their assignee.
	page, err = repo.ListFastingShedCardsForOperator(ctx, repoTenant, repoOperator, atOpen, "", 20)
	if err != nil {
		t.Fatalf("other-operator list: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatal("another operator was served someone else's removal cards")
	}
}

// Per-shed submit: wrong operator refused, a shed outside the round refused,
// the same clip in both slots refused, a clip already on a SIBLING shed
// refused; the FIRST shed's submit does NOT stamp the round; the LAST shed's
// submit stamps it in the same transaction; exact replay returns the original;
// same key different payload conflicts; a second real submit of a submitted
// shed is refused by state.
func TestSubmitFastingShedWritesOnceAndStampsTheRoundOnTheLastShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fastingID := seedFastingFixture(t, ctx, pool, "2026-09-04")
	repo := NewRepository(pool, 5*time.Second)

	// Wrong operator first: refused, nothing written.
	wrong := fastingSubmitShedA(fastingID, "fasting-submit-wrong")
	wrong.SubmittedBy = repoOperator
	if _, err := repo.SubmitFastingShed(ctx, wrong); !errors.Is(err, ports.ErrFastingNotAssigned) {
		t.Fatalf("wrong-operator submit err = %v, want ErrFastingNotAssigned", err)
	}

	// A shed the round does not hold: refused, leaking nothing.
	unknown := fastingSubmitShedA(fastingID, "fasting-submit-unknown")
	unknown.CampaignShedID = "00000000-0000-4000-8000-000000009999"
	if _, err := repo.SubmitFastingShed(ctx, unknown); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("unknown-shed submit err = %v, want ErrNotFound", err)
	}

	// The same clip in both slots: one clip cannot prove two removals.
	sameClip := fastingSubmitShedA(fastingID, "fasting-submit-same")
	sameClip.WaterProofRef = sameClip.FeedProofRef
	if _, err := repo.SubmitFastingShed(ctx, sameClip); !errors.Is(err, ports.ErrFastingProofInvalid) {
		t.Fatalf("same-clip-both-slots err = %v, want ErrFastingProofInvalid", err)
	}

	// FIRST shed in: the shed card is pending, the ROUND is NOT yet stamped.
	first, err := repo.SubmitFastingShed(ctx, fastingSubmitShedA(fastingID, "fasting-submit-a"))
	if err != nil {
		t.Fatalf("shed A submit: %v", err)
	}
	if first.Replayed {
		t.Fatal("fresh submit reported as replay")
	}
	if first.Card.Status != domain.FastingStatusPendingVerification || first.Evidence.FastingShedID == "" {
		t.Fatalf("shed A card = %+v, want pending_verification with an evidence row", first.Card)
	}
	if first.Task.SubmittedAt != nil {
		t.Fatal("round stamped after ONE of two sheds — the midnight gate would open on half the work")
	}

	// A clip already on the SIBLING shed refuses this shed's submit.
	reuse := fastingSubmitShedB(fastingID, "fasting-submit-sib")
	reuse.FeedProofRef = fastingFeedProof
	if _, err := repo.SubmitFastingShed(ctx, reuse); !errors.Is(err, ports.ErrFastingProofInvalid) {
		t.Fatalf("sibling clip reuse err = %v, want ErrFastingProofInvalid", err)
	}

	// LAST shed in: the round is stamped in the SAME transaction.
	second, err := repo.SubmitFastingShed(ctx, fastingSubmitShedB(fastingID, "fasting-submit-b"))
	if err != nil {
		t.Fatalf("shed B submit: %v", err)
	}
	if second.Task.SubmittedAt == nil || second.Task.Status != domain.FastingStatusPendingVerification {
		t.Fatalf("round after last shed = %+v, want submitted_at stamped + pending_verification", second.Task)
	}
	if second.Card.SubmittedAt == nil {
		t.Fatalf("last shed's card must echo the round stamp, got %+v", second.Card)
	}

	// Exact replay: the ORIGINAL card comes back and NO new round is minted.
	replay, err := repo.SubmitFastingShed(ctx, fastingSubmitShedA(fastingID, "fasting-submit-a"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replay.Replayed || replay.Card.RowVersion != first.Card.RowVersion {
		t.Fatalf("replay = %+v, want the original snapshot flagged Replayed", replay.Card)
	}

	// Same key, different payload: conflict.
	conflict := fastingSubmitShedA(fastingID, "fasting-submit-a")
	conflict.FeedProofRef = fastingFeedProofB
	if _, err := repo.SubmitFastingShed(ctx, conflict); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same-key different-payload err = %v, want ErrIdempotencyConflict", err)
	}

	// A second real submit of an already-submitted shed: refused by state.
	if _, err := repo.SubmitFastingShed(ctx, fastingSubmitShedA(fastingID, "fasting-submit-a2")); !errors.Is(err, ports.ErrFastingAlreadySubmitted) {
		t.Fatalf("second submit err = %v, want ErrFastingAlreadySubmitted", err)
	}
}

// A REWORK re-submit means NEW videos FOR THAT SHED: reusing the rejected
// shed's clip refuses the submit; fresh clips re-mint a round for the rejected
// shed ONLY while the approved sibling keeps its verdict; approving the redone
// shed completes the ROUND.
func TestReworkResubmitRefusesTheRejectedClipAndAcceptsFreshOnes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fastingID := seedFastingFixture(t, ctx, pool, "2026-09-04")
	repo := NewRepository(pool, 5*time.Second)

	a, err := repo.SubmitFastingShed(ctx, fastingSubmitShedA(fastingID, "fasting-rework-a"))
	if err != nil {
		t.Fatalf("shed A submit: %v", err)
	}
	b, err := repo.SubmitFastingShed(ctx, fastingSubmitShedB(fastingID, "fasting-rework-b"))
	if err != nil {
		t.Fatalf("shed B submit: %v", err)
	}

	// Verifier: reject shed A, approve shed B.
	if err := repo.ApplyFastingVerdict(ctx, domain.FastingVerdict{
		TenantID: repoTenant, FastingShedID: a.Evidence.FastingShedID,
		Status: domain.VerificationStatusRework, VerifiedBy: repoVerifier,
		Reason: "Feed still in the trough at Gandhi 1 - Part 1. Redo it.", EventID: "00000000-0000-4000-8000-000000009701",
	}); err != nil {
		t.Fatalf("rework verdict: %v", err)
	}
	if err := repo.ApplyFastingVerdict(ctx, domain.FastingVerdict{
		TenantID: repoTenant, FastingShedID: b.Evidence.FastingShedID,
		Status: domain.VerificationStatusVerified, VerifiedBy: repoVerifier,
		EventID: "00000000-0000-4000-8000-000000009702",
	}); err != nil {
		t.Fatalf("approve verdict: %v", err)
	}
	atOpen := time.Date(2026, 9, 3, 20, 0, 0, 0, biztime.DefaultLocation())
	page, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, atOpen, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	cardA, _ := cardByShed(page.Items, repoAnimalScope)
	cardB, _ := cardByShed(page.Items, repoShedScope)
	if cardA.Status != domain.FastingStatusRework || cardA.ReworkReason == "" {
		t.Fatalf("rejected shed's card = %+v, want rework with the verifier's reason verbatim", cardA)
	}
	if cardB.Status != domain.FastingStatusCompleted {
		t.Fatalf("approved shed's card = %+v, want completed untouched by the sibling's rework", cardB)
	}
	if cardA.SubmittedAt == nil {
		t.Fatal("rework cleared the round's submitted_at — the midnight gate would re-block a weighing that already ran")
	}

	// The approved sibling cannot be re-submitted: its verdict stands.
	if _, err := repo.SubmitFastingShed(ctx, fastingSubmitShedB(fastingID, "fasting-rework-b2")); !errors.Is(err, ports.ErrFastingAlreadySubmitted) {
		t.Fatalf("approved-shed resubmit err = %v, want ErrFastingAlreadySubmitted", err)
	}

	// Re-using either REJECTED clip refuses the resubmit by name.
	freshFeed := "00000000-0000-4000-8000-000000009503"
	insertLiveCameraProof(t, ctx, pool, freshFeed)
	reuse := fastingSubmitShedA(fastingID, "fasting-rework-reuse")
	reuse.FeedProofRef = freshFeed // water still the rejected clip
	if _, err := repo.SubmitFastingShed(ctx, reuse); !errors.Is(err, ports.ErrRejectedProofReuse) {
		t.Fatalf("rejected-clip reuse err = %v, want ErrRejectedProofReuse", err)
	}

	// Fresh pair: a fresh round for THIS shed only, and the round returns to
	// pending review without touching its original submission stamp.
	freshWater := "00000000-0000-4000-8000-000000009504"
	insertLiveCameraProof(t, ctx, pool, freshWater)
	resub := fastingSubmitShedA(fastingID, "fasting-rework-fresh")
	resub.FeedProofRef = freshFeed
	resub.WaterProofRef = freshWater
	second, err := repo.SubmitFastingShed(ctx, resub)
	if err != nil {
		t.Fatalf("fresh resubmit: %v", err)
	}
	if second.Card.Status != domain.FastingStatusPendingVerification || second.Card.RowVersion <= a.Card.RowVersion {
		t.Fatalf("resubmitted card = %+v, want pending_verification past row_version %d", second.Card, a.Card.RowVersion)
	}
	if second.Task.SubmittedAt == nil {
		t.Fatal("resubmit lost the round's submitted_at")
	}

	// Approving the redone shed completes the ROUND.
	if err := repo.ApplyFastingVerdict(ctx, domain.FastingVerdict{
		TenantID: repoTenant, FastingShedID: second.Evidence.FastingShedID,
		Status: domain.VerificationStatusVerified, VerifiedBy: repoVerifier,
		EventID: "00000000-0000-4000-8000-000000009703",
	}); err != nil {
		t.Fatalf("final approve: %v", err)
	}
	final, err := repo.FastingTaskByID(ctx, repoTenant, fastingID, "")
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != domain.FastingStatusCompleted {
		t.Fatalf("round status = %s, want completed once every shed is approved", final.Status)
	}
}

// THE MIDNIGHT GATE, all three halves, proved through the real sweep: with NO
// shed submitted the round rolls; with HALF the sheds submitted it STILL rolls
// (the gate is the ROUND's stamp, which only the last shed writes); with every
// shed submitted nothing moves.
func TestMidnightGateRollsUnlessEveryShedWasSubmitted(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	// Weigh date = the sweep's business date ("today"): the deadline (00:00 of
	// the weigh date) has passed.
	asOf := time.Date(2026, 9, 4, 0, 10, 0, 0, biztime.DefaultLocation())
	today := biztime.BusinessDate(asOf)
	fastingID := seedFastingFixture(t, ctx, pool, today)
	repo := NewRepository(pool, 5*time.Second)

	seedWorkItems := func() {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_work_items (tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id, weighing_category, shed_label, shed_location_id, planned_business_date, due_business_date, work_state)
SELECT cs.tenant_id, cs.campaign_id, cs.campaign_shed_id, $3::uuid, cs.operator_user_id, cs.weighing_category, cs.display_name, cs.location_id, $2::date, $2::date, 'scheduled'
FROM weighing_campaign_sheds cs
WHERE cs.tenant_id = $1::uuid AND cs.campaign_id = $4::uuid
ON CONFLICT (tenant_id, campaign_shed_id) DO UPDATE SET due_business_date=EXCLUDED.due_business_date, work_state='scheduled', terminal_at=NULL`,
			repoTenant, today, repoPark, repoCampaign)
	}
	seedWorkItems()

	// NOTHING submitted: the round rolls and pushes the work to tomorrow.
	result, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{TenantID: repoTenant, AsOf: asOf})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if result.FastingGatedWorkItems != 2 || result.FastingTasksRolled != 1 {
		t.Fatalf("gated=%d rolled=%d, want 2/1", result.FastingGatedWorkItems, result.FastingTasksRolled)
	}
	tomorrow := biztime.BusinessDate(asOf.AddDate(0, 0, 1))
	var dueCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM weighing_work_items
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND due_business_date=$3::date`,
		repoTenant, repoCampaign, tomorrow).Scan(&dueCount); err != nil {
		t.Fatal(err)
	}
	if dueCount != 2 {
		t.Fatalf("work items due tomorrow = %d, want 2 (the gate must push, not hold)", dueCount)
	}
	var ftDate string
	var rolledCount int
	if err := pool.QueryRow(ctx, `
SELECT weigh_business_date::text, rolled_forward_count FROM weighing_fasting_tasks
WHERE tenant_id=$1::uuid AND fasting_task_id=$2::uuid`, repoTenant, fastingID).Scan(&ftDate, &rolledCount); err != nil {
		t.Fatal(err)
	}
	if ftDate != tomorrow || rolledCount != 1 {
		t.Fatalf("fasting row date=%s rolled=%d, want %s / 1 (re-armed for the next evening)", ftDate, rolledCount, tomorrow)
	}

	// A second sweep the SAME tick moves nothing further: the state converged.
	again, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{TenantID: repoTenant, AsOf: asOf})
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if again.FastingGatedWorkItems != 0 || again.FastingTasksRolled != 0 {
		t.Fatalf("second sweep gated=%d rolled=%d, want 0/0", again.FastingGatedWorkItems, again.FastingTasksRolled)
	}

	// HALF submitted (one card of two): the round is NOT stamped and STILL
	// rolls — a partially fasted round must not weigh tomorrow.
	fastingID = seedFastingFixture(t, ctx, pool, today)
	seedWorkItems()
	half, err := repo.SubmitFastingShed(ctx, fastingSubmitShedA(fastingID, "fasting-gate-half"))
	if err != nil {
		t.Fatalf("half submit: %v", err)
	}
	if half.Task.SubmittedAt != nil {
		t.Fatal("round stamped by one of two sheds")
	}
	partial, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{TenantID: repoTenant, AsOf: asOf})
	if err != nil {
		t.Fatalf("half sweep: %v", err)
	}
	if partial.FastingGatedWorkItems != 2 || partial.FastingTasksRolled != 1 {
		t.Fatalf("half-submitted sweep gated=%d rolled=%d, want 2/1 — one shed's card is not the round", partial.FastingGatedWorkItems, partial.FastingTasksRolled)
	}

	// EVERY shed submitted opens the gate: reset, submit both, sweep — nothing
	// rolls. (Fresh idempotency keys: the shed rows were reset by the reseed.)
	fastingID = seedFastingFixture(t, ctx, pool, today)
	seedWorkItems()
	if _, err := repo.SubmitFastingShed(ctx, fastingSubmitShedA(fastingID, "fasting-gate-full-a")); err != nil {
		t.Fatalf("full submit A: %v", err)
	}
	full, err := repo.SubmitFastingShed(ctx, fastingSubmitShedB(fastingID, "fasting-gate-full-b"))
	if err != nil {
		t.Fatalf("full submit B: %v", err)
	}
	if full.Task.SubmittedAt == nil {
		t.Fatal("round not stamped after its last shed")
	}
	opened, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{TenantID: repoTenant, AsOf: asOf})
	if err != nil {
		t.Fatalf("post-submit sweep: %v", err)
	}
	if opened.FastingGatedWorkItems != 0 || opened.FastingTasksRolled != 0 {
		t.Fatalf("post-submit sweep gated=%d rolled=%d, want 0/0 — submission satisfies the gate", opened.FastingGatedWorkItems, opened.FastingTasksRolled)
	}
	var stillToday int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM weighing_work_items
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND due_business_date=$3::date AND work_state IN ('scheduled','delayed')`,
		repoTenant, repoCampaign, today).Scan(&stillToday); err != nil {
		t.Fatal(err)
	}
	if stillToday != 2 {
		t.Fatalf("work items still due today = %d, want 2 (submitted fasting must not block)", stillToday)
	}
}

// The verdict applier NEVER touches submitted_at, and a redelivery of the
// SAME event applies once.
func TestApplyFastingVerdictNeverUnsubmits(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fastingID := seedFastingFixture(t, ctx, pool, "2026-09-04")
	repo := NewRepository(pool, 5*time.Second)

	a, err := repo.SubmitFastingShed(ctx, fastingSubmitShedA(fastingID, "fasting-submit-verdict-a"))
	if err != nil {
		t.Fatalf("submit A: %v", err)
	}
	if _, err := repo.SubmitFastingShed(ctx, fastingSubmitShedB(fastingID, "fasting-submit-verdict-b")); err != nil {
		t.Fatalf("submit B: %v", err)
	}

	if err := repo.ApplyFastingVerdict(ctx, domain.FastingVerdict{
		TenantID: repoTenant, FastingShedID: a.Evidence.FastingShedID,
		Status: domain.VerificationStatusRework, VerifiedBy: repoVerifier,
		Reason: "Water trough still full in the video.", EventID: "00000000-0000-4000-8000-000000009601",
	}); err != nil {
		t.Fatalf("rework verdict: %v", err)
	}
	task, err := repo.FastingTaskByID(ctx, repoTenant, fastingID, "")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.FastingStatusRework || task.ReworkReason == "" {
		t.Fatalf("after rework: %+v", task)
	}
	if task.SubmittedAt == nil {
		t.Fatal("rework cleared submitted_at — the midnight gate would re-block a weighing that already ran")
	}

	// Redelivery of the SAME event applies once (idempotent no-op).
	if err := repo.ApplyFastingVerdict(ctx, domain.FastingVerdict{
		TenantID: repoTenant, FastingShedID: a.Evidence.FastingShedID,
		Status: domain.VerificationStatusRework, VerifiedBy: repoVerifier,
		Reason: "Water trough still full in the video.", EventID: "00000000-0000-4000-8000-000000009601",
	}); err != nil {
		t.Fatalf("verdict redelivery: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Aggregate/projection adversarial cases for the per-shed card read (the
// evidence LEFT JOIN is UNIQUE per (task, bucket); the list is keyset-paged;
// scope is the assignee; status is a disjoint enum).
// -----------------------------------------------------------------------------

// OneToMany: the bucket join serves EXACTLY one card per live bucket — the
// evidence row can never multiply a bucket's card, and a canceled bucket's
// card disappears.
func TestFastingCardsAreOnePerLiveBucket(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fastingID := seedFastingFixture(t, ctx, pool, "2026-09-04") // fixture seeds 2 buckets
	repo := NewRepository(pool, 5*time.Second)
	atOpen := time.Date(2026, 9, 3, 20, 0, 0, 0, biztime.DefaultLocation())

	// With evidence recorded for one bucket, the round still serves exactly 2
	// cards (one per bucket) — never 3, never 1.
	if _, err := repo.SubmitFastingShed(ctx, fastingSubmitShedA(fastingID, "fasting-onetomany-a")); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, atOpen, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("cards = %d, want exactly one per live bucket (2)", len(page.Items))
	}
	cardA, _ := cardByShed(page.Items, repoAnimalScope)
	cardB, _ := cardByShed(page.Items, repoShedScope)
	if cardA.Status != domain.FastingStatusPendingVerification || cardB.Status != domain.FastingStatusOpen {
		t.Fatalf("cards A=%s B=%s, want the submitted bucket pending and the other open", cardA.Status, cardB.Status)
	}
	// Cancel one bucket: its card disappears; the sibling's stays.
	execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_campaign_sheds SET status='canceled' WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope)
	page, err = repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, atOpen, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].CampaignShedID != repoAnimalScope {
		t.Fatalf("after cancel: %+v, want only the live bucket's card", page.Items)
	}
}

// PageBoundary: three cards, page size two — the keyset cursor must hand over
// the third card exactly once, no duplicate and no gap across the boundary.
func TestFastingListPaginationPageBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFastingFixture(t, ctx, pool, "2026-09-04") // 2 cards (2 buckets)
	repo := NewRepository(pool, 5*time.Second)
	// One more campaign on a later date with ONE bucket of its own, assigned to
	// the same removal operator: a third card.
	extraCampaign := "00000000-0000-4000-8000-000000009107"
	extraBucket := "00000000-0000-4000-8000-000000009108"
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-09-05', '2026-09-05', '2026-09-05', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, extraCampaign, repoTenant, repoPark, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Boundary Shed', 'per_shed_partition', $5::uuid, 1)
ON CONFLICT (campaign_shed_id) DO NOTHING`, extraBucket, extraCampaign, repoTenant, repoPark, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_fasting_tasks (tenant_id, campaign_id, park_id, operator_user_id, planned_weigh_date, weigh_business_date, idempotency_key, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, '2026-09-05', '2026-09-05', 'fasting-page:2026-09-05', $4::uuid)
ON CONFLICT (tenant_id, campaign_id) DO NOTHING`, repoTenant, extraCampaign, repoPark, fastingOperator)
	afterAll := time.Date(2026, 9, 5, 20, 30, 0, 0, biztime.DefaultLocation())

	first, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, afterAll, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("page 1 = %d items cursor=%q, want 2 items + cursor", len(first.Items), first.NextCursor)
	}
	second, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, afterAll, first.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("page 2 = %d items cursor=%q, want exactly the 1 remaining card", len(second.Items), second.NextCursor)
	}
	seen := map[string]bool{}
	for _, it := range append(first.Items, second.Items...) {
		key := it.FastingTaskID + "/" + it.CampaignShedID
		if seen[key] {
			t.Fatalf("card %s served twice across the page boundary", key)
		}
		seen[key] = true
	}
	if len(seen) != 3 {
		t.Fatalf("distinct cards across pages = %d, want 3 (no gap at the boundary)", len(seen))
	}
}

// Assignee scope: the list is the OPERATOR's own cards only; a different
// operator in the same tenant and park sees nothing.
func TestFastingListParkScopeIsAssigneeOnly(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFastingFixture(t, ctx, pool, "2026-09-04")
	repo := NewRepository(pool, 5*time.Second)
	atOpen := time.Date(2026, 9, 3, 20, 0, 0, 0, biztime.DefaultLocation())

	mine, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, atOpen, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, repoOperator, atOpen, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine.Items) != 2 || len(theirs.Items) != 0 {
		t.Fatalf("assignee sees %d, another operator sees %d — want 2 / 0", len(mine.Items), len(theirs.Items))
	}
}

// EveryStatus: each status a shed's evidence row can hold serves exactly its
// one card in the operator's list — the enum buckets are disjoint and none of
// them hides history.
func TestFastingListServesEveryShedStatusOnceVisible(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fastingID := seedFastingFixture(t, ctx, pool, "2026-09-04")
	repo := NewRepository(pool, 5*time.Second)
	atOpen := time.Date(2026, 9, 3, 20, 0, 0, 0, biztime.DefaultLocation())

	// Record evidence for bucket A so its row exists to move through statuses.
	if _, err := repo.SubmitFastingShed(ctx, fastingSubmitShedA(fastingID, "fasting-status-a")); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending_verification", "rework", "completed"} {
		execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_fasting_shed_proofs SET status=$3 WHERE tenant_id=$1::uuid AND fasting_task_id=$2::uuid AND campaign_shed_id=$4::uuid`, repoTenant, fastingID, status, repoAnimalScope)
		page, err := repo.ListFastingShedCardsForOperator(ctx, repoTenant, fastingOperator, atOpen, "", 20)
		if err != nil {
			t.Fatalf("status %s: %v", status, err)
		}
		cardA, ok := cardByShed(page.Items, repoAnimalScope)
		if !ok || cardA.Status != status {
			t.Fatalf("status %s: card = %+v — every status serves its one card", status, cardA)
		}
		cardB, ok := cardByShed(page.Items, repoShedScope)
		if !ok || cardB.Status != domain.FastingStatusOpen {
			t.Fatalf("status %s: unrecorded sibling = %+v, must stay its own open card", status, cardB)
		}
	}
}

// THE ROUND STAMP READS SHED STATE, NOT CLIP PRESENCE (review finding on PR
// 176). Verifier items are enqueued per shed as each lands, so shed A can be
// submitted and bounced to rework BEFORE shed B is submitted. A's row still
// carries its rejected refs; a ref-presence check counted it as covered and
// B's submit stamped the round, opening the midnight gate while A still owed
// fresh clips. The round is stamped only once A comes back with a fresh pair.
func TestRoundIsNotStampedWhileASiblingShedSitsInRework(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fastingID := seedFastingFixture(t, ctx, pool, "2026-09-04")
	repo := NewRepository(pool, 5*time.Second)

	a, err := repo.SubmitFastingShed(ctx, fastingSubmitShedA(fastingID, "fasting-early-rework-a"))
	if err != nil {
		t.Fatalf("shed A submit: %v", err)
	}
	if a.Task.SubmittedAt != nil {
		t.Fatal("round stamped after one of two sheds")
	}
	// Verifier bounces A while B is still open.
	if err := repo.ApplyFastingVerdict(ctx, domain.FastingVerdict{
		TenantID: repoTenant, FastingShedID: a.Evidence.FastingShedID,
		Status: domain.VerificationStatusRework, VerifiedBy: repoVerifier,
		Reason: "Water trough still full. Redo it.", EventID: "00000000-0000-4000-8000-000000009711",
	}); err != nil {
		t.Fatalf("rework verdict: %v", err)
	}

	// B lands: every shed row now holds refs, but A's are REJECTED. The round
	// must stay unstamped.
	b, err := repo.SubmitFastingShed(ctx, fastingSubmitShedB(fastingID, "fasting-early-rework-b"))
	if err != nil {
		t.Fatalf("shed B submit: %v", err)
	}
	if b.Task.SubmittedAt != nil {
		t.Fatalf("round stamped while shed A sits in rework: %+v — the midnight gate would open on rejected work", b.Task)
	}
	if b.Card.SubmittedAt != nil {
		t.Fatalf("shed B's card echoes a round stamp that must not exist: %+v", b.Card)
	}

	// A's fresh pair is the LAST submitted shed: THAT stamps the round.
	freshFeed := "00000000-0000-4000-8000-000000009513"
	freshWater := "00000000-0000-4000-8000-000000009514"
	insertLiveCameraProof(t, ctx, pool, freshFeed)
	insertLiveCameraProof(t, ctx, pool, freshWater)
	resub := fastingSubmitShedA(fastingID, "fasting-early-rework-a2")
	resub.FeedProofRef = freshFeed
	resub.WaterProofRef = freshWater
	second, err := repo.SubmitFastingShed(ctx, resub)
	if err != nil {
		t.Fatalf("fresh resubmit of A: %v", err)
	}
	if second.Task.SubmittedAt == nil || second.Task.Status != domain.FastingStatusPendingVerification {
		t.Fatalf("round after A's fresh pair = %+v, want submitted_at stamped + pending_verification", second.Task)
	}
}
