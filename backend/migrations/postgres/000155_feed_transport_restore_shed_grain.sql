-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: feed transport grain repair only; no seed contract change
--
-- Feed Transport is ONE daily task per active physical shed. That is the recorded contract in
-- docs/decisions/feed-transport-verification.md ("Grain: (tenant_id, business_date, shed_id). There
-- is no session, batch, workflow, or consolidation grain") and in AGENTS.md. Migration 000143
-- fanned the materializer out over shed_partitions, so a partitioned shed started showing one
-- transport task per pen -- asking an operator to film the same physical load two or three times.
-- The feed for every pen of a shed leaves on one trip; the pen grain belongs to PACKING and
-- DISTRIBUTION, which really are one bag per pen.
--
-- 000143 and 000146 are already applied and STG records migration checksums, so neither is amended.
-- This is the forward repair.
--
-- The uniqueness arbiter is deliberately LEFT ALONE. 000143's index is
-- (tenant_id, business_date, shed_id, COALESCE(NULLIF(btrim(partition_label),''),'whole')), and the
-- materializer now writes '' for every new row, so that key degenerates to exactly one row per shed
-- per day. Surviving pen rows keep their own key and cannot collide with it. Rebuilding the index
-- CONCURRENTLY on a live table would buy nothing and could fail mid-deploy.
--
-- partition_label is KEPT and stops being populated. A pen row that already carries a video was
-- filmed for that pen, and rewriting its label would make the evidence trail lie about where the
-- operator stood.

-- Unstarted pen work has no proof to protect and no shed-grain identity, so it retires and the
-- materializer's shed row takes over. Anything with an attempt -- verification_due, rework, or
-- completed -- is left exactly as it is: an operator recorded that video, a verifier may have
-- already judged it, and retiring it would discard real work. Those sheds simply carry both the
-- surviving pen rows and one new shed row for the day, which is the honest state.
UPDATE public.feed_transport_tasks
SET status = 'retired',
    updated_at = now(),
    row_version = row_version + 1
WHERE COALESCE(NULLIF(BTRIM(partition_label), ''), 'whole') <> 'whole'
  AND status = 'due'
  AND current_attempt_id IS NULL
  AND completed_at IS NULL;

-- +goose Down
-- +goose NO TRANSACTION
-- Forward data repair only. A retired unstarted task carries no evidence, so there is nothing to
-- restore; re-materializing pen tasks would mean reinstating the defect.
