package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

func newTestItem(t *testing.T, ctx context.Context, repo *Repository, tenantID, idemKey string) domain.Item {
	t.Helper()
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: idemKey,
	})
	if err != nil {
		t.Fatalf("create test item: %v", err)
	}
	return created.Item
}

func TestInsertReviewEventsIsIdempotentOnReplay_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	itemRepo := NewRepository(pool, 5*time.Second)
	reviewRepo := NewReviewEventRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	item := newTestItem(t, ctx, itemRepo, tenantID, "vaccination:submission:review-1")
	actorID := uuid.NewString()

	batch := domain.ReviewEventBatch{
		TenantID: tenantID,
		ActorID:  actorID,
		Events: []domain.ReviewEvent{
			{
				TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-1",
				EventType: domain.ReviewEventItemOpened, OccurredAt: time.Now(), ClientEventID: uuid.NewString(),
			},
			{
				TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-1",
				EventType: domain.ReviewEventVideoPlay, OccurredAt: time.Now(), ClientEventID: uuid.NewString(),
			},
		},
	}

	inserted, err := reviewRepo.InsertReviewEvents(ctx, batch)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("first insert count = %d, want 2", inserted)
	}

	// Replay the EXACT same batch (same client_event_id per event) -- must insert nothing new.
	replayed, err := reviewRepo.InsertReviewEvents(ctx, batch)
	if err != nil {
		t.Fatalf("replay insert: %v", err)
	}
	if replayed != 0 {
		t.Fatalf("replay insert count = %d, want 0 (idempotent)", replayed)
	}

	var rowCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM verification_review_events WHERE tenant_id = $1::uuid", tenantID).Scan(&rowCount); err != nil {
		t.Fatalf("count verification_review_events: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("verification_review_events rows = %d, want 2 (replay must not double-count)", rowCount)
	}
}

// TestItemReviewFactsWatchFractionCountsDistinctCoveredRanges is the mandatory adversarial case:
// replaying the same 2 seconds of video must not inflate watch time. Two overlapping play spans
// covering [0,5000) and [2000,7000) must merge into one 7000ms covered span, not sum to 10000ms.
func TestItemReviewFactsWatchFractionCountsDistinctCoveredRanges_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	itemRepo := NewRepository(pool, 5*time.Second)
	reviewRepo := NewReviewEventRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	item := newTestItem(t, ctx, itemRepo, tenantID, "vaccination:submission:review-2")
	actorID := uuid.NewString()
	base := time.Now()
	durationMs := int64(10000)

	pos := func(ms int64) *int64 { return &ms }
	events := []domain.ReviewEvent{
		{TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventItemOpened, OccurredAt: base, ClientEventID: uuid.NewString()},
		// First play: watches [0,5000) of the video, over 5s of WALL CLOCK.
		//
		// The wall-clock gaps here are load-bearing, not decoration. clampSpanToElapsed caps a
		// claimed span at MaxPlausiblePlaybackRate (2x) * elapsed + 500ms slack, so a 5000ms span
		// claimed one second apart is cut to 2500ms -- which is the anti-fraud rule working, not a
		// bug. This fixture previously spaced the events 1s apart and asserted the unclamped 7000,
		// so it failed for a correct reason: it claimed 5s of video played in 1s of real time. Keep
		// each play->pause gap >= half the video span it claims, or the clamp will fire and the
		// union assertion below will measure clamping instead of interval merging.
		{TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVideoPlay, OccurredAt: base.Add(1 * time.Second), Payload: domain.ReviewEventPayload{VideoPositionMs: pos(0), VideoDurationMs: &durationMs}, ClientEventID: uuid.NewString()},
		{TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVideoPause, OccurredAt: base.Add(6 * time.Second), Payload: domain.ReviewEventPayload{VideoPositionMs: pos(5000)}, ClientEventID: uuid.NewString()},
		// Replay: watches [2000,7000) -- overlaps the first span by 3000ms -- again over 5s of wall clock.
		{TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVideoPlay, OccurredAt: base.Add(7 * time.Second), Payload: domain.ReviewEventPayload{VideoPositionMs: pos(2000), VideoDurationMs: &durationMs}, ClientEventID: uuid.NewString()},
		{TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVideoPause, OccurredAt: base.Add(12 * time.Second), Payload: domain.ReviewEventPayload{VideoPositionMs: pos(7000)}, ClientEventID: uuid.NewString()},
		{TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVerdictRecorded, OccurredAt: base.Add(13 * time.Second), ClientEventID: uuid.NewString()},
	}
	if _, err := reviewRepo.InsertReviewEvents(ctx, domain.ReviewEventBatch{TenantID: tenantID, ActorID: actorID, Events: events}); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	facts, err := reviewRepo.ItemReviewFacts(ctx, tenantID, item.ItemID)
	if err != nil {
		t.Fatalf("item review facts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts count = %d, want 1 actor", len(facts))
	}
	f := facts[0]
	if f.WatchedDistinctMs != 7000 {
		t.Fatalf("watched_distinct_ms = %d, want 7000 (union of [0,5000) and [2000,7000), NOT 10000 summed)", f.WatchedDistinctMs)
	}
	if f.ProofDurationMs != durationMs {
		t.Fatalf("proof_duration_ms = %d, want %d", f.ProofDurationMs, durationMs)
	}
	wantFraction := 0.7
	if f.WatchFraction < wantFraction-0.001 || f.WatchFraction > wantFraction+0.001 {
		t.Fatalf("watch_fraction = %f, want ~%f", f.WatchFraction, wantFraction)
	}
	if f.WatchedFull {
		t.Fatal("watched_full should be false at 70% < 90% threshold")
	}
	if f.PlayCount != 2 || f.PauseCount != 2 {
		t.Fatalf("play/pause counts = %d/%d, want 2/2", f.PlayCount, f.PauseCount)
	}
	if f.TimeToVerdictSeconds == nil {
		t.Fatal("time_to_verdict_seconds should be set (item_opened and verdict_recorded both present)")
	} else if *f.TimeToVerdictSeconds < 12.9 || *f.TimeToVerdictSeconds > 13.1 {
		// item_opened is at base, verdict_recorded at base+13s -- the plays above each span 5s of
		// wall clock so clampSpanToElapsed does not fire, which pushes the verdict out to +13s.
		t.Fatalf("time_to_verdict_seconds = %f, want ~13", *f.TimeToVerdictSeconds)
	}
}

// TestInsertReviewEventsAcceptsNullItemIDForQueueScopedEvent is the real-bug regression
// (2026-08-06): a queue_opened event has no item yet and its item_id column must accept NULL --
// migration 000119 dropped the NOT NULL and added the scope CHECK. A batch mixing a null-item
// queue_opened row with a real item-scoped row must insert BOTH.
func TestInsertReviewEventsAcceptsNullItemIDForQueueScopedEvent_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	itemRepo := NewRepository(pool, 5*time.Second)
	reviewRepo := NewReviewEventRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	item := newTestItem(t, ctx, itemRepo, tenantID, "vaccination:submission:review-null-item")
	actorID := uuid.NewString()

	batch := domain.ReviewEventBatch{
		TenantID: tenantID,
		ActorID:  actorID,
		Events: []domain.ReviewEvent{
			{
				TenantID: tenantID, ItemID: nil, ActorID: actorID, SessionID: "sess-queue",
				EventType: domain.ReviewEventQueueOpened, OccurredAt: time.Now(),
				Payload:       domain.ReviewEventPayload{Category: func() *string { s := "vaccination_proof"; return &s }()},
				ClientEventID: uuid.NewString(),
			},
			{
				TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-queue",
				EventType: domain.ReviewEventVideoPlay, OccurredAt: time.Now(), ClientEventID: uuid.NewString(),
			},
		},
	}

	inserted, err := reviewRepo.InsertReviewEvents(ctx, batch)
	if err != nil {
		t.Fatalf("insert mixed batch: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("inserted = %d, want 2 (null-item queue row + item-scoped row both land)", inserted)
	}

	var nullItemRows, realItemRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM verification_review_events WHERE tenant_id = $1::uuid AND item_id IS NULL AND event_type = 'queue_opened'`, tenantID).Scan(&nullItemRows); err != nil {
		t.Fatalf("count null-item rows: %v", err)
	}
	if nullItemRows != 1 {
		t.Fatalf("null-item queue_opened rows = %d, want 1", nullItemRows)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM verification_review_events WHERE tenant_id = $1::uuid AND item_id = $2::uuid`, tenantID, item.ItemID).Scan(&realItemRows); err != nil {
		t.Fatalf("count item-scoped rows: %v", err)
	}
	if realItemRows != 1 {
		t.Fatalf("item-scoped rows = %d, want 1", realItemRows)
	}
}

// TestItemReviewFactsUnaffectedByQueueScopedRows proves the per-item facts read path never sees
// (and cannot be skewed by) a queue-scoped row: ItemReviewFacts is always queried with a real
// item_id, and a NULL item_id row can never equal that filter.
func TestItemReviewFactsUnaffectedByQueueScopedRows_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	itemRepo := NewRepository(pool, 5*time.Second)
	reviewRepo := NewReviewEventRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	item := newTestItem(t, ctx, itemRepo, tenantID, "vaccination:submission:review-queue-noise")
	actorID := uuid.NewString()
	durationMs := int64(3000)
	pos := func(ms int64) *int64 { return &ms }
	category := "vaccination_proof"

	// A pile of queue-scoped noise for the SAME tenant/actor, before the real item facts.
	events := []domain.ReviewEvent{
		{TenantID: tenantID, ItemID: nil, ActorID: actorID, SessionID: "sess-queue", EventType: domain.ReviewEventQueueOpened, OccurredAt: time.Now(), Payload: domain.ReviewEventPayload{Category: &category}, ClientEventID: uuid.NewString()},
		{TenantID: tenantID, ItemID: nil, ActorID: actorID, SessionID: "sess-queue", EventType: domain.ReviewEventQueueOpened, OccurredAt: time.Now(), Payload: domain.ReviewEventPayload{Category: &category}, ClientEventID: uuid.NewString()},
		{TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventItemOpened, OccurredAt: time.Now(), ClientEventID: uuid.NewString()},
		{TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVideoPlay, OccurredAt: time.Now(), Payload: domain.ReviewEventPayload{VideoPositionMs: pos(0), VideoDurationMs: &durationMs}, ClientEventID: uuid.NewString()},
		{TenantID: tenantID, ItemID: &item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVideoEnded, OccurredAt: time.Now().Add(3 * time.Second), Payload: domain.ReviewEventPayload{VideoPositionMs: pos(3000)}, ClientEventID: uuid.NewString()},
	}
	if _, err := reviewRepo.InsertReviewEvents(ctx, domain.ReviewEventBatch{TenantID: tenantID, ActorID: actorID, Events: events}); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	facts, err := reviewRepo.ItemReviewFacts(ctx, tenantID, item.ItemID)
	if err != nil {
		t.Fatalf("item review facts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts count = %d, want exactly 1 actor (queue-scoped rows must not appear)", len(facts))
	}
	f := facts[0]
	if f.WatchedDistinctMs != 3000 || f.ProofDurationMs != 3000 || f.WatchFraction != 1 {
		t.Fatalf("facts skewed by queue-scoped noise: watched=%d duration=%d fraction=%f", f.WatchedDistinctMs, f.ProofDurationMs, f.WatchFraction)
	}
	if f.PlayCount != 1 {
		t.Fatalf("play_count = %d, want 1 (queue_opened rows must not be counted as plays)", f.PlayCount)
	}
}
