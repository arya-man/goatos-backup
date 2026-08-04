-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing observations are written by the Weighing mobile/verifier flow; they do not change the Vaccination HRMS seed contract
--
-- FORWARD-REPAIR (M11(a), P1) for 000073_weighing_observations_one_open_tag_uidx.sql.
--
-- 000073 IS NOT EDITED HERE, for the same checksum-drift reason documented in
-- the earlier repairs in this series; it is already merged to main and may
-- already be applied (its CONCURRENTLY index build also cannot run inside the
-- same transactional migration as this one -- NO TRANSACTION migrations and
-- ordinary migrations are never combined).
--
-- ROOT CAUSE: 000073's pre-cleanup step collapses each duplicate-tag group
-- down to one open survivor by stamping `submitted_at = accepted_at` on the
-- LOSER rows (correctly, to satisfy the new partial unique index) but never
-- touches their verification_status, which keeps its default 'pending'. Every
-- capture already raised its own verification_items row at accept time
-- (auditAnimalObservation / enqueueVerification fire on every accept, not
-- just the eventual "current" one), so each loser's verification_items row is
-- also still 'pending'. The verifier queue (verification module, status=
-- 'pending') and weighing_observations.verification_status <> 'verified'
-- (close.go's pending_verification_count) can no longer tell a loser's
-- verification_items row apart from a genuine, still-undecided submission --
-- a verifier can be asked to decide on video/weight evidence that was never
-- the animal's accepted round.
--
-- FIX SCOPE AND WHAT THIS DOES NOT DO: this migration withdraws the loser
-- rows' verification_items using the module's own existing "superseded, not a
-- verdict" retire path (verification_items.status = 'withdrawn', added in
-- 000067 specifically for this "producer supersedes, not decides" case --
-- see WithdrawItemsBySource / reviseVerificationRound, which use exactly this
-- mechanism for reopened/edited observations). It deliberately does NOT
-- touch weighing_observations.submitted_at (000073 already made the correct,
-- immutable-history call there -- "don't fabricate submitted work" cuts both
-- ways: we don't manufacture a verifier decision ('verified'/'rework') for a
-- round nobody actually reviewed as final, and we don't erase the historical
-- submitted_at 000073 stamped). weighing_observations.verification_status
-- itself is also left as-is: its CHECK constraint only allows
-- 'pending'/'verified'/'rework' (000058), and close.go's
-- pending_verification_count reads that column directly with no third
-- "excluded" bucket recognised by any query in this codebase today -- adding
-- one there is an application-code change (close.go query + a widened CHECK
-- constraint) outside this worktree's ownership (backend/migrations/postgres
-- and backend/cmd/migrate only) and is called out here as a known, narrow
-- residual: a closed bucket's ready-to-close gate may still count a
-- withdrawn-verification loser as "pending" until that follow-up lands. The
-- verifier-facing queue -- the concrete complaint in M11(a), "duplicates
-- enter the verifier queue indistinguishable from real submissions" -- is
-- fully fixed by this migration, since that queue is driven by
-- verification_items.status, not weighing_observations.verification_status.
--
-- IDENTIFYING LOSERS: recompute the exact same partition/rank 000073 used
-- (tenant_id, campaign_shed_id, lower(btrim(scanned_identifier)) ordered by
-- accepted_at DESC, observation_id DESC), restricted to rows whose
-- submitted_at exactly equals their own accepted_at AND where a sibling row
-- for the same key has a strictly later accepted_at. That combination is
-- 000073's loser fingerprint: a genuine submit stamps submitted_at=now()
-- strictly after its own accepted_at (SubmitIndividualScope, repository.go
-- ~line 2079-2086) and 000070/000074's audit-derived backfills stamp an
-- audit/submit-event timestamp, neither of which coincides with accepted_at
-- itself except by the 000073 loser mechanism.
--
-- LOCK SAFETY: the identification predicate scopes this to the small,
-- specific duplicate-tag anomaly set (not a full-table scan target), but
-- weighing_observations/verification_items are still hot animal-grain
-- tables, so SET LOCAL lock_timeout/statement_timeout bound contention with
-- the live capture/verify paths as defense-in-depth.
--
-- IDEMPOTENT / RE-RUNNABLE: only verification_items rows still 'pending' are
-- withdrawn, and the loser-identification predicate is stable, so a re-run
-- (or a fresh database that never had a duplicate-tag race) withdraws zero
-- rows.
--
-- projection-review: producer = weighing_observations (tenant_id,
-- campaign_shed_id, scanned_identifier, accepted_at, submitted_at) used only
-- to compute the loser id set. Consumer = verification_items (source_module=
-- 'weighing', source_ref_type='weighing_observation', source_ref_id=
-- observation_id, status). Row multiplicity: 1:1 -- each loser observation_id
-- maps to at most one still-pending verification_items row for it (a fresh
-- capture always gets its own idempotency-keyed verification item).

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

WITH ranked AS (
  SELECT observation_id, tenant_id,
    row_number() OVER (
      PARTITION BY tenant_id, campaign_shed_id, lower(btrim(scanned_identifier))
      ORDER BY accepted_at DESC, observation_id DESC
    ) AS rn
  FROM weighing_observations
  WHERE submitted_at IS NOT NULL
    AND submitted_at = accepted_at
    AND campaign_shed_id IS NOT NULL
    AND btrim(scanned_identifier) <> ''
),
loser_ids AS (
  SELECT observation_id, tenant_id FROM ranked WHERE rn > 1
)
UPDATE verification_items vi
SET status = 'withdrawn',
    row_version = row_version + 1,
    updated_at = now()
FROM loser_ids l
WHERE vi.tenant_id = l.tenant_id
  AND vi.source_module = 'weighing'
  AND vi.source_ref_type = 'weighing_observation'
  AND vi.source_ref_id = l.observation_id
  AND vi.status = 'pending';

-- +goose Down
-- Removal is not possible without re-exposing withdrawn duplicate rounds to
-- the verifier queue as if they were genuine, undecided submissions again,
-- and a DOWN cannot distinguish a row this migration withdrew from one a
-- legitimate ReopenScope/rework path withdrew afterwards. DOWN is a no-op.
