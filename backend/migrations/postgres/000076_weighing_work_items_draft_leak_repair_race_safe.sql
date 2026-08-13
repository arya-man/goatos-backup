-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing free-flow kernel work items are written by the Weighing publish transaction and the kernel worker; they do not change the Vaccination HRMS seed contract
--
-- FORWARD-REPAIR (M9, P1) for 000072_weighing_backfill_work_items_draft_leak_repair.sql.
--
-- 000072 IS NOT EDITED HERE, for the same checksum-drift reason it itself
-- documents about 000069: it is already merged to main and may already be
-- applied.
--
-- ROOT CAUSE: 000072's repair --
--   DELETE FROM weighing_work_items wi USING weighing_campaigns c
--   WHERE ... AND c.status = 'draft'
-- -- reads c.status under an ordinary MVCC snapshot and takes NO lock on the
-- weighing_campaigns row before deleting. PublishCampaign flips
-- weighing_campaigns.status to 'published' and inserts the campaign's real
-- work items via `... ON CONFLICT DO NOTHING` in the SAME transaction
-- (createWorkItemsForPublishTx). ON CONFLICT DO NOTHING that hits the
-- conflict path does not bump the row's xmax, so if 000072's DELETE and a
-- concurrent PublishCampaign both start before either commits, 000072 can
-- read status='draft' (pre-publish), delete a work item row PublishCampaign
-- is concurrently inserting/relying on, and PublishCampaign's own conflict
-- check never re-validates against the now-missing row (no xmax change to
-- trigger Postgres's EPQ re-check). The campaign ends up published with a
-- silently missing work item.
--
-- 000072 already reasoned carefully about being lock-safe from a LOCK
-- DURATION standpoint (shed-grain, bounded, no batching needed) but that
-- reasoning covered index/lock contention, not this check-then-act race
-- against a concurrent status transition. This migration is the race-safe
-- replacement rollout of the identical repair, for any environment where
-- migrate runs while the app is live (a rolling deploy, or a fresh database
-- bootstrap racing seed/smoke traffic) and for defense against any residual
-- leaked row 000072 itself could have missed under the same race on its own
-- first run.
--
-- FIX: explicitly lock the candidate weighing_campaigns rows FOR UPDATE
-- before deleting. A concurrent PublishCampaign's `UPDATE weighing_campaigns
-- SET status='published' ...` needs the SAME row lock, so it blocks until
-- this migration's transaction commits or rolls back:
--   * If this transaction acquires the lock first and the campaign is still
--     'draft', it deletes the (illegitimate, per 000072's own predicate)
--     leaked work items and commits; PublishCampaign then proceeds normally
--     against a campaign with no work items yet, inserts them fresh.
--   * If PublishCampaign acquires the lock first, it commits its own
--     transaction (status now 'published', legitimate work items inserted)
--     before this migration's FOR UPDATE can proceed; by the time this
--     migration reads the row, status is no longer 'draft', so the WHERE
--     clause correctly excludes it and nothing is deleted.
-- Either interleaving leaves exactly the correct, race-free outcome.
--
-- LOCK SAFETY: identical grain/cardinality reasoning as 000072 -- a handful
-- of draft campaigns across the whole database, shed-grain work item rows.
-- SET LOCAL lock_timeout bounds how long this migration will wait to acquire
-- the FOR UPDATE lock against a live publish, instead of blocking migrate
-- indefinitely.
--
-- IDEMPOTENT / RE-RUNNABLE: after the first correct run (whether this
-- migration or 000072), no work item rows remain under a still-draft
-- campaign, so a re-run deletes zero rows.
--
-- projection-review: producer = weighing_campaigns (tenant_id, campaign_id,
-- status), read under FOR UPDATE. Consumer = weighing_work_items (tenant_id,
-- campaign_id, work_item_id). Row multiplicity: many:1 weighing_work_items ->
-- weighing_campaigns on (tenant_id, campaign_id), a plain equality lookup on
-- the campaign's own primary key -- no fan-out.

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

WITH locked_draft_campaigns AS (
  SELECT tenant_id, campaign_id
  FROM weighing_campaigns
  WHERE status = 'draft'
  FOR UPDATE
)
DELETE FROM weighing_work_items wi
USING locked_draft_campaigns c
WHERE wi.tenant_id = c.tenant_id
  AND wi.campaign_id = c.campaign_id;

-- +goose Down
-- Removal is not possible without risking data integrity the other direction
-- (same rationale as 000072's Down): DOWN is a no-op; recovery from an
-- incorrect run is a fresh publish, which createWorkItemsForPublishTx makes
-- idempotent and safe to re-run.
