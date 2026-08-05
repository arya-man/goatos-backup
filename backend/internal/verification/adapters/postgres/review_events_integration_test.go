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
				TenantID: tenantID, ItemID: item.ItemID, ActorID: actorID, SessionID: "sess-1",
				EventType: domain.ReviewEventItemOpened, OccurredAt: time.Now(), ClientEventID: uuid.NewString(),
			},
			{
				TenantID: tenantID, ItemID: item.ItemID, ActorID: actorID, SessionID: "sess-1",
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
		{TenantID: tenantID, ItemID: item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventItemOpened, OccurredAt: base, ClientEventID: uuid.NewString()},
		// First play: watches [0,5000) of the video.
		{TenantID: tenantID, ItemID: item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVideoPlay, OccurredAt: base.Add(1 * time.Second), Payload: domain.ReviewEventPayload{VideoPositionMs: pos(0), VideoDurationMs: &durationMs}, ClientEventID: uuid.NewString()},
		{TenantID: tenantID, ItemID: item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVideoPause, OccurredAt: base.Add(2 * time.Second), Payload: domain.ReviewEventPayload{VideoPositionMs: pos(5000)}, ClientEventID: uuid.NewString()},
		// Replay: watches [2000,7000) -- overlaps the first span by 3000ms.
		{TenantID: tenantID, ItemID: item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVideoPlay, OccurredAt: base.Add(3 * time.Second), Payload: domain.ReviewEventPayload{VideoPositionMs: pos(2000), VideoDurationMs: &durationMs}, ClientEventID: uuid.NewString()},
		{TenantID: tenantID, ItemID: item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVideoPause, OccurredAt: base.Add(4 * time.Second), Payload: domain.ReviewEventPayload{VideoPositionMs: pos(7000)}, ClientEventID: uuid.NewString()},
		{TenantID: tenantID, ItemID: item.ItemID, ActorID: actorID, SessionID: "sess-1", EventType: domain.ReviewEventVerdictRecorded, OccurredAt: base.Add(5 * time.Second), ClientEventID: uuid.NewString()},
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
	} else if *f.TimeToVerdictSeconds < 4.9 || *f.TimeToVerdictSeconds > 5.1 {
		// item_opened is at base, verdict_recorded at base+5s.
		t.Fatalf("time_to_verdict_seconds = %f, want ~5", *f.TimeToVerdictSeconds)
	}
}
