package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// verification_review_item_watch (migration 000431) replaces the per-request re-aggregation of
// verification_review_events in the oversight watch-integrity read. These tests hold it to the
// one claim that makes that safe: after ANY sequence of event writes, the stored summary equals
// the live per-(item, actor) aggregate the read used to compute, and so do the verifier figures.

// liveWatchIntegritySQL is the pre-000431 read, kept here verbatim as the oracle.
const liveWatchIntegritySQL = `
WITH decided_items AS (
  SELECT item_id, verified_by
  FROM verification_items
  WHERE tenant_id = $1::uuid AND verified_by IS NOT NULL AND verified_at IS NOT NULL
    AND verified_at >= now() - interval '14 days'
),
per_item AS (
  SELECT di.verified_by,
         di.item_id,
         bool_or(e.event_type = 'item_opened') AS opened,
         bool_or(e.event_type = 'video_play') AS played,
         max((e.payload->>'video_position_ms')::bigint) AS max_position_ms,
         max((e.payload->>'video_duration_ms')::bigint) AS max_duration_ms
  FROM decided_items di
  LEFT JOIN verification_review_events e
    ON e.tenant_id = $1::uuid AND e.item_id = di.item_id AND e.actor_id = di.verified_by
  GROUP BY di.verified_by, di.item_id
)
SELECT verified_by::text,
       count(*) FILTER (WHERE opened OR played OR max_duration_ms IS NOT NULL),
       count(*) FILTER (
         WHERE max_duration_ms IS NOT NULL AND max_duration_ms > 0
           AND max_position_ms IS NOT NULL
           AND max_position_ms::float8 / max_duration_ms::float8 >= 0.9
       ),
       count(*) FILTER (WHERE NOT played)
FROM per_item
GROUP BY verified_by`

func TestReviewItemWatchSummaryMatchesLiveAggregate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedOversightTenant(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	items := make([]string, 6)
	for i := range items {
		items[i] = fmt.Sprintf("60000000-0000-4000-8000-0000000000%02d", i)
		seedDecidedItem(t, ctx, pool, items[i], "vaccination", "approved")
	}
	const otherActor = "10000000-0000-4000-8000-0000000000ff"
	// item 0: fully watched across two separate flushes (the second raises the max position).
	insertReviewEvents(t, ctx, pool, oversightTestVerifier, items[0], [][2]string{{"item_opened", `{}`}, {"video_play", `{"video_position_ms": 1000, "video_duration_ms": 10000}`}})
	insertReviewEvents(t, ctx, pool, oversightTestVerifier, items[0], [][2]string{{"video_pause", `{"video_position_ms": 9500, "video_duration_ms": 10000}`}})
	// item 1: opened, never played -> tracked AND verdict-without-play.
	insertReviewEvents(t, ctx, pool, oversightTestVerifier, items[1], [][2]string{{"item_opened", `{}`}, {"verdict_recorded", `{}`}})
	// item 2: only verdict_recorded -> not tracked, but counted as verdict-without-play.
	insertReviewEvents(t, ctx, pool, oversightTestVerifier, items[2], [][2]string{{"verdict_recorded", `{}`}})
	// item 3: no events by the verifier; another actor watched it fully -> must not count.
	insertReviewEvents(t, ctx, pool, otherActor, items[3], [][2]string{{"item_opened", `{}`}, {"video_play", `{"video_position_ms": 10000, "video_duration_ms": 10000}`}})
	// item 4: played to 50% in ONE multi-event statement.
	insertReviewEvents(t, ctx, pool, oversightTestVerifier, items[4], [][2]string{{"video_play", `{"video_position_ms": 0, "video_duration_ms": 8000}`}, {"video_pause", `{"video_position_ms": 4000, "video_duration_ms": 8000}`}})
	// item 5: nothing at all.
	// A queue_opened row carries no item and must be ignored by the fold.
	if _, err := pool.Exec(ctx, `
		INSERT INTO verification_review_events (tenant_id, item_id, actor_id, event_type, occurred_at, payload, session_id, client_event_id)
		VALUES ($1::uuid, NULL, $2::uuid, 'queue_opened', now(), '{}'::jsonb, 's', gen_random_uuid())`,
		oversightTestTenantID, oversightTestVerifier); err != nil {
		t.Fatalf("queue_opened: %v", err)
	}

	assertSummaryEqualsEvents(t, ctx, pool)
	assertIntegrityMatchesLive(t, ctx, pool, repo, 3, 1, 2)

	// A DELETE (e.g. a future retention job) re-derives the touched pairs from what is left.
	if _, err := pool.Exec(ctx, `DELETE FROM verification_review_events WHERE item_id = $1::uuid AND event_type = 'video_pause'`, items[0]); err != nil {
		t.Fatalf("delete: %v", err)
	}
	assertSummaryEqualsEvents(t, ctx, pool)
	assertIntegrityMatchesLive(t, ctx, pool, NewRepository(pool, 10*time.Second), 3, 0, 2)

	if _, err := pool.Exec(ctx, `DELETE FROM verification_review_events WHERE item_id = $1::uuid`, items[1]); err != nil {
		t.Fatalf("delete all of item 1: %v", err)
	}
	assertSummaryEqualsEvents(t, ctx, pool)
	assertIntegrityMatchesLive(t, ctx, pool, NewRepository(pool, 10*time.Second), 2, 0, 1)
}

func insertReviewEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor, item string, events [][2]string) {
	t.Helper()
	types := make([]string, len(events))
	payloads := make([]string, len(events))
	for i, e := range events {
		types[i], payloads[i] = e[0], e[1]
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO verification_review_events (tenant_id, item_id, actor_id, event_type, occurred_at, payload, session_id, client_event_id)
		SELECT $1::uuid, $2::uuid, $3::uuid, t.event_type, now(), t.payload::jsonb, 's', gen_random_uuid()
		FROM unnest($4::text[], $5::text[]) AS t(event_type, payload)`,
		oversightTestTenantID, item, actor, types, payloads); err != nil {
		t.Fatalf("insert events for %s: %v", item, err)
	}
}

func assertSummaryEqualsEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var diff int
	if err := pool.QueryRow(ctx, `
WITH live AS (
  SELECT tenant_id, item_id, actor_id,
         bool_or(event_type = 'item_opened') AS opened,
         bool_or(event_type = 'video_play') AS played,
         max((payload->>'video_position_ms')::bigint) AS max_position_ms,
         max((payload->>'video_duration_ms')::bigint) AS max_duration_ms
  FROM verification_review_events WHERE item_id IS NOT NULL
  GROUP BY tenant_id, item_id, actor_id
), stored AS (
  SELECT tenant_id, item_id, actor_id, opened, played, max_position_ms, max_duration_ms
  FROM verification_review_item_watch
)
SELECT count(*) FROM ((SELECT * FROM live EXCEPT ALL SELECT * FROM stored)
                UNION ALL (SELECT * FROM stored EXCEPT ALL SELECT * FROM live)) d`).Scan(&diff); err != nil {
		t.Fatalf("compare summary: %v", err)
	}
	if diff != 0 {
		t.Fatalf("verification_review_item_watch differs from the live event aggregate in %d rows", diff)
	}
}

func assertIntegrityMatchesLive(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository, tracked, watched, withoutPlay int) {
	t.Helper()
	var liveVerifier string
	var liveTracked, liveWatched, liveWithout int
	if err := pool.QueryRow(ctx, liveWatchIntegritySQL, oversightTestTenantID).Scan(&liveVerifier, &liveTracked, &liveWatched, &liveWithout); err != nil {
		t.Fatalf("live oracle: %v", err)
	}
	got, err := repo.oversightIntegrity(ctx, oversightTestTenantID)
	if err != nil {
		t.Fatalf("oversightIntegrity: %v", err)
	}
	if len(got) != 1 || got[0].verifierID != liveVerifier || got[0].itemsTracked != liveTracked ||
		got[0].watchedToEnd != liveWatched || got[0].verdictWithoutPlay != liveWithout {
		t.Fatalf("stored-summary integrity %+v != live aggregate {%s %d %d %d}", got, liveVerifier, liveTracked, liveWatched, liveWithout)
	}
	if liveTracked != tracked || liveWatched != watched || liveWithout != withoutPlay {
		t.Fatalf("live aggregate = tracked %d watched %d without-play %d, want %d %d %d", liveTracked, liveWatched, liveWithout, tracked, watched, withoutPlay)
	}
}
