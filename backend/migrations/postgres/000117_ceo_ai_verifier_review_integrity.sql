-- no-mismatch-review-queue:ignore: owner=ravi issue=maintainer-decision-2026-08-06 scope=verifier-watch-telemetry-not-a-reconciliation-queue expiry=2026-11-30
-- seed-migration-guard:ignore owner=ravi issue=maintainer-decision-2026-08-06 reason=append-only-telemetry-accrues-at-runtime-no-seed-companion expiry=2026-11-30
-- +goose Up
--
-- Leadership assistant coverage for verifier video-review integrity (closes gap G10,
-- docs/ceo-ai/coverage-matrix.md). "As CEO I need to know those analytics" -- maintainer,
-- 2026-08-06: which verifiers are rubber-stamping proof videos rather than watching them.
--
-- _grain: ONE ROW PER (tenant_id, verifier=verification_items.verified_by, park_id, category,
-- business_day), where business_day is the Asia/Kolkata calendar date of verified_at (the
-- AUTHORITATIVE backend-recorded verdict timestamp -- NOT the client-telemetry
-- verdict_recorded event, which is corroborating evidence, not the source of truth for when a
-- verdict landed). This mirrors the business-day convention every other ceo_ai view uses
-- (AT TIME ZONE 'Asia/Kolkata', see 000096).
--
-- SOURCE TABLES: verification_items (the decided-item population) LEFT JOINed to a per-
-- (tenant_id,item_id,actor_id) rollup of verification_review_events (migration 000112, the
-- client video-review telemetry) and to locations (park label only). It does NOT join goats,
-- goat_identifiers, or any clinical table -- verifier review behavior is orthogonal to animal
-- identity, exactly like the weighing views' herd-isolation rule (000080).
--
-- WATCH-FRACTION DERIVATION (per item, per actor): distinct-covered played span, merged via the
-- standard SQL islands-and-gaps technique, over milliseconds bounded by each video_play's
-- reported/implied start and the next stop event (video_pause/video_seek_attempt/video_ended)'s
-- reported/implied end -- the same idea backend/internal/verification/adapters/postgres/
-- review_events.go's mergeIntervalsDistinctMs uses for the per-item read endpoint, so overlapping
-- replays merge instead of summing. KNOWN LIMITATION vs. the Go path: this SQL derivation only
-- pairs a video_play with the IMMEDIATELY FOLLOWING stop-type event by occurred_at; a
-- video_seek_attempt that does not interrupt an open play span (no video_play immediately
-- preceding it) is counted as a seek but does not itself open/close a played interval. This is a
-- conservative approximation, not a byte-identical reimplementation -- documented here rather than
-- silently diverging.
--
-- WATCHED_FULL THRESHOLD: 0.9, matching
-- backend/internal/verification/adapters/postgres/review_events.go's WatchedFullThreshold.
-- below_watch_threshold_count counts verdicts with a KNOWN watch_fraction under that bar --
-- the CEO's actual integrity question ("how many decisions were made without proving a full
-- watch"). missing_review_telemetry_count is reported SEPARATELY: a verifier who sent no
-- telemetry at all is not proven to have watched EITHER, but conflating "watched 10%" with "sent
-- nothing" would hide a client-wiring gap behind a real integrity number.
--
-- SCALE: full-view aggregate, consistent with every other ceo_ai.* view in this codebase
-- (ceo_ai.verification_queue_status has no WHERE clause either) -- the assistant queries this
-- WHOLE, never paginates it. The base verification_items scan is served by
-- verification_items_verified_by_review_idx (migration 000113, partial on verified_by IS NOT
-- NULL, carrying park_id/category/verified_at). The event-side window functions partition by
-- (tenant_id, item_id, actor_id), which verification_review_events_item_actor_time_idx (migration
-- 000112) exists to serve. Not compute-on-read over unbounded UNINDEXED history.
--
-- LOCK SAFETY: CREATE VIEW only -- no table DDL, no backfill, no rewrite of any write path.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.verifier_review_integrity AS
WITH ordered_video_events AS (
    SELECT tenant_id, item_id, actor_id, event_type, occurred_at,
           (payload->>'video_position_ms')::bigint AS position_ms,
           (payload->>'video_duration_ms')::bigint AS duration_ms
    FROM verification_review_events
    WHERE event_type IN ('video_play', 'video_pause', 'video_seek_attempt', 'video_ended')
),
next_event AS (
    SELECT *,
           LEAD(event_type) OVER w   AS next_type,
           LEAD(occurred_at) OVER w  AS next_at,
           LEAD(position_ms) OVER w  AS next_position_ms
    FROM ordered_video_events
    WINDOW w AS (PARTITION BY tenant_id, item_id, actor_id ORDER BY occurred_at)
),
-- One row per video_play that is immediately followed by a stop-type event: the played span
-- [start_ms, end_ms) that play resumed. See header for the seek-without-preceding-play limitation.
play_spans AS (
    SELECT tenant_id, item_id, actor_id,
           COALESCE(position_ms, 0) AS start_ms,
           COALESCE(
               next_position_ms,
               COALESCE(position_ms, 0) + GREATEST(EXTRACT(EPOCH FROM (next_at - occurred_at)) * 1000, 0)
           )::bigint AS end_ms
    FROM next_event
    WHERE event_type = 'video_play'
      AND next_type IN ('video_pause', 'video_seek_attempt', 'video_ended')
),
valid_spans AS (
    SELECT * FROM play_spans WHERE end_ms > start_ms
),
-- Classic islands-and-gaps interval merge: a new island starts whenever a span's start exceeds
-- every prior span's end seen so far (in start-ordered sequence) for the SAME (item, actor).
spans_with_prev_end AS (
    SELECT *,
           MAX(end_ms) OVER (
               PARTITION BY tenant_id, item_id, actor_id ORDER BY start_ms
               ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
           ) AS prev_max_end
    FROM valid_spans
),
spans_grouped AS (
    SELECT *,
           SUM(CASE WHEN prev_max_end IS NULL OR start_ms > prev_max_end THEN 1 ELSE 0 END)
               OVER (PARTITION BY tenant_id, item_id, actor_id ORDER BY start_ms) AS island
    FROM spans_with_prev_end
),
merged_spans AS (
    SELECT tenant_id, item_id, actor_id, island, MIN(start_ms) AS island_start, MAX(end_ms) AS island_end
    FROM spans_grouped
    GROUP BY tenant_id, item_id, actor_id, island
),
watch_distinct AS (
    SELECT tenant_id, item_id, actor_id, SUM(island_end - island_start)::bigint AS watched_distinct_ms
    FROM merged_spans
    GROUP BY tenant_id, item_id, actor_id
),
event_stats AS (
    SELECT tenant_id, item_id, actor_id,
           MAX((payload->>'video_duration_ms')::bigint) AS proof_duration_ms,
           COUNT(*) FILTER (WHERE event_type = 'video_play')          AS play_count,
           COUNT(*) FILTER (WHERE event_type = 'video_pause')         AS pause_count,
           COUNT(*) FILTER (WHERE event_type = 'video_seek_attempt')  AS seek_attempt_count,
           MIN(occurred_at) FILTER (WHERE event_type = 'item_opened') AS item_opened_at
    FROM verification_review_events
    GROUP BY tenant_id, item_id, actor_id
),
item_facts AS (
    SELECT es.tenant_id, es.item_id, es.actor_id, es.proof_duration_ms, es.play_count,
           es.pause_count, es.seek_attempt_count, es.item_opened_at,
           COALESCE(wd.watched_distinct_ms, 0) AS watched_distinct_ms,
           CASE WHEN es.proof_duration_ms > 0
                THEN LEAST(COALESCE(wd.watched_distinct_ms, 0)::numeric / es.proof_duration_ms, 1)
           END AS watch_fraction
    FROM event_stats es
    LEFT JOIN watch_distinct wd
        ON wd.tenant_id = es.tenant_id AND wd.item_id = es.item_id AND wd.actor_id = es.actor_id
),
-- projection-review: membership=every verification_items row with a recorded verdict (verified_by IS NOT NULL AND status IN ('approved','rejected')); group_key=(tenant_id, item_id) implicitly, one verification_items row per item_id, its own PK; join_cardinality=item_facts 0..1 per (tenant_id,item_id,actor_id) matched on actor_id=vi.verified_by (item_facts is itself GROUP BY (tenant_id,item_id,actor_id), so at most one row can match the single verified_by value on this vi row), locations pk 0..1 per park_id (PK join) -- neither joined side can multiply a verification_items row, so reviewed_items stays at exactly one row per decided item; pagination=NONE, this is a view and every consumer paginates over it; scope=tenant_id, exposed as vi.tenant_id and carried through every downstream CTE and the outer GROUP BY
reviewed_items AS (
    SELECT vi.tenant_id, vi.item_id, vi.verified_by AS actor_id, vi.status, vi.verdict_reason,
           vi.category, vi.park_id, pk.name AS park_label,
           (vi.verified_at AT TIME ZONE 'Asia/Kolkata')::date AS business_day,
           jf.watch_fraction, jf.proof_duration_ms, jf.watched_distinct_ms,
           jf.play_count, jf.pause_count, jf.seek_attempt_count,
           CASE WHEN jf.item_opened_at IS NOT NULL AND vi.verified_at IS NOT NULL
                THEN EXTRACT(EPOCH FROM (vi.verified_at - jf.item_opened_at))
           END AS time_to_verdict_seconds
    FROM verification_items vi
    LEFT JOIN item_facts jf
        ON jf.tenant_id = vi.tenant_id AND jf.item_id = vi.item_id AND jf.actor_id = vi.verified_by
    LEFT JOIN locations pk ON pk.location_id = vi.park_id
    WHERE vi.verified_by IS NOT NULL AND vi.status IN ('approved', 'rejected')
),
-- Two-step aggregation for the reject-reason breakdown so it composes as a plain aggregate of
-- already-grouped rows rather than a nested/correlated aggregate: reject_reason_grain groups to
-- (grain key, reason); reject_reason_rollup re-aggregates that to exactly the outer grain, one
-- jsonb object per (verifier, park, category, day).
reject_reason_grain AS (
    SELECT tenant_id, actor_id, park_id, category, business_day,
           COALESCE(verdict_reason, 'unspecified') AS reason,
           COUNT(*)::bigint AS reason_count
    FROM reviewed_items
    WHERE status = 'rejected'
    GROUP BY tenant_id, actor_id, park_id, category, business_day, COALESCE(verdict_reason, 'unspecified')
),
reject_reason_rollup AS (
    SELECT tenant_id, actor_id, park_id, category, business_day,
           jsonb_object_agg(reason, reason_count) AS reject_reason_breakdown
    FROM reject_reason_grain
    GROUP BY tenant_id, actor_id, park_id, category, business_day
)
SELECT
    ri.tenant_id                                                                AS tenant_id,
    ri.actor_id                                                                 AS verifier_id,
    ri.park_id                                                                  AS park_id,
    ri.park_label                                                               AS park_label,
    ri.category                                                                 AS category,
    ri.business_day                                                            AS business_day,
    COUNT(*)::bigint                                                            AS videos_reviewed,
    percentile_cont(0.5) WITHIN GROUP (ORDER BY ri.time_to_verdict_seconds)
        FILTER (WHERE ri.time_to_verdict_seconds IS NOT NULL)                  AS median_time_to_verdict_seconds,
    percentile_cont(0.9) WITHIN GROUP (ORDER BY ri.time_to_verdict_seconds)
        FILTER (WHERE ri.time_to_verdict_seconds IS NOT NULL)                  AS p90_time_to_verdict_seconds,
    percentile_cont(0.5) WITHIN GROUP (ORDER BY ri.watch_fraction)
        FILTER (WHERE ri.watch_fraction IS NOT NULL)                           AS median_watch_fraction,
    COUNT(*) FILTER (WHERE ri.watch_fraction IS NOT NULL AND ri.watch_fraction < 0.9)::bigint
                                                                                 AS below_watch_threshold_count,
    COUNT(*) FILTER (WHERE ri.watch_fraction IS NULL)::bigint                   AS missing_review_telemetry_count,
    COUNT(*) FILTER (WHERE ri.status = 'rejected')::bigint                     AS rejected_count,
    (COUNT(*) FILTER (WHERE ri.status = 'rejected'))::numeric
        / NULLIF(COUNT(*), 0)                                                  AS reject_rate,
    -- jsonb has no MAX/MIN aggregate in Postgres; rr contributes AT MOST one row per outer group
    -- (it is itself grouped by exactly this key), so array_agg + [1] picks that single value
    -- without implying any ordering choice among multiple rows (there is never more than one).
    COALESCE(
        (array_agg(rr.reject_reason_breakdown) FILTER (WHERE rr.reject_reason_breakdown IS NOT NULL))[1],
        '{}'::jsonb
    )                                                                            AS reject_reason_breakdown
FROM reviewed_items ri
LEFT JOIN reject_reason_rollup rr
    ON rr.tenant_id = ri.tenant_id
   AND rr.actor_id = ri.actor_id
   AND rr.park_id IS NOT DISTINCT FROM ri.park_id
   AND rr.category = ri.category
   AND rr.business_day IS NOT DISTINCT FROM ri.business_day
GROUP BY ri.tenant_id, ri.actor_id, ri.park_id, ri.park_label, ri.category, ri.business_day;
-- +goose StatementEnd

-- ===========================================================================
-- Grants. Guarded on role existence exactly like 000080/000085 -- the reader roles are
-- provisioned per environment (tools/dev/setup-ceo-ai-local-role.sh + Secret Manager), never by a
-- migration, and local/pgtest databases have neither role.
-- ===========================================================================
-- +goose StatementBegin
DO $verifier_review_integrity_grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly', 'mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.verifier_review_integrity TO %I', r);
        END IF;
    END LOOP;
END;
$verifier_review_integrity_grants$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';
DROP VIEW IF EXISTS ceo_ai.verifier_review_integrity;
