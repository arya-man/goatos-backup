package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	feeddirectioncounts "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/counts"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionverificationbridge "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/verificationbridge"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
)

// TestKernelStory_FeedWastageReplayAfterMeasurementKeepsOneItem is the production-path proof that an
// operator's exact replay of a wastage submit, arriving AFTER the verifier recorded the leftover
// weight, neither queues a second verifier card nor strands the pen-day.
//
// The defect (branch feat/sop-verifier-parity, 2026-09-17): the submit enqueue ran for every
// pending_verification result -- replays included -- keyed on the completion's CURRENT row_version.
// Recording a measurement bumps that row_version while the row stays pending, so the replay minted a
// second item under a new key, and the verdict round fence (newer item exists) then silently dropped
// the approve of the item the verifier was actually holding: a duplicate card and a pen-day stuck
// pending_verification.
//
// Only input facts are seeded (scope, experiment config, dispatch clock). The sheet, completion,
// items, measurement, verdict and the completed row all come from the production services and the
// real outbox -> domain consumer -> FeedWastageVerificationHandler path.
func TestKernelStory_FeedWastageReplayAfterMeasurementKeepsOneItem(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-feed-wastage-replay-after-measurement",
		"Feed wastage: a replayed submit after the verifier measured keeps one card",
		"An operator's phone retries its wastage submit after the verifier has already read the leftover "+
			"weight off the video. The retry must collapse onto the card she is holding, and her approve "+
			"must complete the pen-day with the weight she recorded.")
	defer story.Finish()
	story.Certify("backend kernel")
	ctx := fx.Ctx

	const (
		park       = "fa000000-0000-4000-8000-000000003001"
		shedCastro = "fa000000-0000-4000-8000-000000004001"
		shedTrial  = "fa000000-0000-4000-8000-000000004002"
		operator   = "fa000000-0000-4000-8000-000000005001"
		verifier   = "fa000000-0000-4000-8000-000000005002"
	)
	seedFeedCorrectionScope(t, ctx, fx.Pool, park, shedCastro, shedTrial)
	seedWastageTrialPen(t, ctx, fx.Pool, park, shedTrial)

	feedRepo := feeddirectionpg.NewRepository(fx.Pool, 10*time.Second)
	countsRepo := countspg.NewRepository(fx.Pool, 10*time.Second)
	verification := verificationapp.NewService(fx.VerifRepo, fx.VerifMedia)
	if err := verification.RegisterCategory(verificationcatalog.FeedWastage); err != nil {
		t.Fatalf("register feed wastage category: %v", err)
	}
	bridge := feeddirectionverificationbridge.NewWastage(verification)
	clock := &e2eClock{}
	feed := feeddirectionapp.NewService(feedRepo, feeddirectioncounts.NewReader(countsRepo)).
		WithIssueStore(feedRepo).
		WithScheduleReader(feedRepo).
		WithWastageStore(feedRepo).
		WithWastageVerificationEnqueuer(bridge).
		WithGeneratedBy("goatos-e2e")
	feed.WithClock(clock.Now)
	measurements := feeddirectionapp.NewWastageMeasurementService(feedRepo, nil).WithVerificationRelabeler(bridge)
	if err := verification.RegisterMeasurementApplier(feeddirectiondomain.VerificationCategoryWastage,
		feeddirectionverificationbridge.NewWastageMeasurementApplier(measurements, feedRepo)); err != nil {
		t.Fatalf("register wastage measurement applier: %v", err)
	}

	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, time.July, day, hour, minute, 0, 0, biztime.DefaultLocation())
	}

	story.Step("Day before, 14:00 — the experiment sheet is issued", "Tomorrow's experiment sheet covers the trial pen.")
	clock.Set(at(29, 14, 0))
	if _, err := feed.IssueDirection(ctx, feeddirectionapp.IssueRequest{
		TenantID: fxTenant, ParkID: park, Workflow: feeddirectiondomain.WorkflowExperiment, AsOf: at(29, 14, 0),
	}); err != nil {
		t.Fatalf("IssueDirection(experiment): %v", err)
	}

	story.Step("Feed day, 18:00 — the operator submits the leftover video", "One pen-day, one video, one verifier card.")
	clock.Set(at(30, 18, 0))
	submit := func() string {
		t.Helper()
		res, err := feed.CompleteWastage(ctx, feeddirectionapp.CompleteWastageInput{
			TenantID: fxTenant, ParkID: park, ShedID: shedTrial, TargetDate: feedDayTime(t),
			WastageProofRef: "proof-trial-wastage", CompletedBy: operator,
			IdempotencyKey: "feed-wastage-trial-1", ActorID: operator, ActorType: "operator",
		})
		if err != nil {
			t.Fatalf("CompleteWastage: %v", err)
		}
		return res.CompletionID
	}
	completionID := submit()
	items := listWastageItems(t, ctx, fx.Pool, completionID)
	if !story.Assert("the submit queued exactly one verifier card", len(items) == 1, "items=%d", len(items)) {
		t.FailNow()
	}
	firstItem := items[0].id

	story.Step("18:30 — the verifier records the leftover weight", "The standalone save an installed APK still uses.")
	if _, err := measurements.RecordWastageMeasurement(ctx, feeddirectionapp.WastageMeasurementCommand{
		TenantID: fxTenant, CompletionID: completionID, WastageKg: 2.5, RecordedBy: verifier,
		IdempotencyKey: "feed-wastage-measure-trial-1",
	}); err != nil {
		t.Fatalf("RecordWastageMeasurement: %v", err)
	}

	story.Step("18:35 — the operator's phone replays the same submit", "Exact replay: same key, same video.")
	if replayID := submit(); replayID != completionID {
		t.Fatalf("replay returned completion %s, want %s", replayID, completionID)
	}
	items = listWastageItems(t, ctx, fx.Pool, completionID)
	story.Assert("the replay collapsed onto the existing card instead of queuing a second one",
		len(items) == 1, "items=%d %+v", len(items), items)

	story.Step("18:40 — the verifier approves the card she is holding", "The approve travels the real outbox and consumer.")
	if _, err := verification.RecordVerdict(ctx, verificationdomain.Verdict{
		TenantID: fxTenant, ItemID: firstItem, Decision: verificationdomain.DecisionApproved,
		VerifierID: verifier, RowVersion: verificationItemRowVersion(t, ctx, fx.Pool, firstItem),
		IdempotencyKey: "feed-wastage-verdict-trial-1",
	}); err != nil {
		t.Fatalf("RecordVerdict(approved): %v", err)
	}
	fx.RelayOutboxEvents()

	status, kg := wastageCompletionState(t, ctx, fx.Pool, completionID)
	story.Assert("the approve completed the pen-day with the recorded weight",
		status == feeddirectiondomain.WastageStatusCompleted && kg == "2.500",
		"status=%s wastage_kg=%s", status, kg)
	items = listWastageItems(t, ctx, fx.Pool, completionID)
	story.Assert("no second card is left pending in the verifier's queue",
		countPending(items) == 0 && len(items) == 1, "items=%+v", items)
}

func seedWastageTrialPen(t *testing.T, ctx context.Context, pool *pgxpool.Pool, park, shedTrial string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_experiment_config (tenant_id, park_id, shed_id, feed_item_label, absolute_kg, head_count, experiment_category, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Concentrate', 12.000, 20, 'Trial A', 'active')`,
		fxTenant, park, shedTrial); err != nil {
		t.Fatalf("seed experiment config: %v", err)
	}
}

func listWastageItems(t *testing.T, ctx context.Context, pool *pgxpool.Pool, completionID string) []packingItem {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT item_id::text, coalesce(subject_label, ''), status
FROM verification_items
WHERE tenant_id = $1::uuid AND source_module = 'feed'
  AND source_ref_type = 'feed_wastage_completion' AND source_ref_id = $2
ORDER BY created_at`, fxTenant, completionID)
	if err != nil {
		t.Fatalf("list wastage verification items: %v", err)
	}
	defer rows.Close()
	out := []packingItem{}
	for rows.Next() {
		var it packingItem
		if err := rows.Scan(&it.id, &it.subject, &it.status); err != nil {
			t.Fatalf("scan wastage verification item: %v", err)
		}
		out = append(out, it)
	}
	return out
}

func wastageCompletionState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, completionID string) (string, string) {
	t.Helper()
	var status, kg string
	if err := pool.QueryRow(ctx, `
SELECT status, coalesce(wastage_kg::text, '')
FROM feed_wastage_completions WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`,
		fxTenant, completionID).Scan(&status, &kg); err != nil {
		t.Fatalf("read wastage completion: %v", err)
	}
	return status, kg
}
