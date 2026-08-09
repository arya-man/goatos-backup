package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// WEIGHING PHASE 2 — work items on publish + the bounded time-driven sweep.
//
// Everything in this file obeys the business-DAY time grain: the caller hands in
// an instant, `biztime.BusinessDate` resolves it to the Asia/Kolkata business
// date once, and every SQL comparison is `::date` against that value. There is no
// `now()` business comparison and no `now ± N hours` anywhere.

const (
	// nilUUIDCursor starts every keyset claim. Cursors only ever move forward.
	nilUUIDCursor = "00000000-0000-0000-0000-000000000000"

	// nilDateCursor starts every DATE-then-ID composite keyset claim. It is a
	// fixed date far before any real business date, so the first chunk of a
	// pass always claims from the very beginning of its ordered index scan.
	nilDateCursor = "0001-01-01"

	// defaultKernelChunkSize / defaultKernelMaxChunks bound one tick. A tick can
	// never scan or lock the whole table: it claims at most
	// ChunkSize * MaxChunks rows per pass and reports Truncated when it stops
	// early, so the next tick resumes from the still-matching predicate.
	defaultKernelChunkSize = 200
	defaultKernelMaxChunks = 50
)

// claimedWorkItem is one row a sweep pass claimed and mutated.
type claimedWorkItem struct {
	WorkItemID          string
	CampaignID          string
	CampaignShedID      string
	ShedID              string
	ParkID              string
	OperatorUserID      string
	ShedLabel           string
	PlannedBusinessDate string
	DueBusinessDate     string
	// SortDate is the PRE-UPDATE value of the column the pass orders and
	// keysets on (captured by the `claimed` CTE before the UPDATE runs, so a
	// pass that mutates its own sort column — roll-forward mutates
	// due_business_date — still yields the value the claim actually matched,
	// never the post-update value). Cadence passes use it as the date half of
	// the composite (date, work_item_id) cursor. It is unused (empty) for the
	// UUID-only terminal-reconciliation pass.
	SortDate string
}

// createWorkItemsForPublishTx materializes the durable weighing work items for a
// campaign being published. It runs INSIDE the publish transaction, so a campaign
// can never become `published` without its work items and a rolled-back publish
// leaves none behind (required-recorder fail-closed).
//
// ONE SET-BASED STATEMENT, never a per-shed loop of queries. Suggested business
// dates come from the greedy planner expressed as a window function: buckets are
// ordered per OPERATOR (capacity is operator-business-date grain, so two
// operators do not consume each other's cap), and the day offset is the running
// EXCLUSIVE bucket size divided by planned_cap_per_day.
//
// `ON CONFLICT (tenant_id, campaign_shed_id) DO NOTHING` plus the unique bucket
// index is what makes publish idempotent: republish, an exact replay, and a
// retried transaction all converge on exactly one work item per bucket.
//
// projection-review: producer unique columns = weighing_campaign_sheds
// (tenant_id, campaign_id, location_id) with PK campaign_shed_id; consumer
// match/group columns = weighing_work_items (tenant_id, campaign_shed_id).
// Row multiplicity: weighing_campaign_sheds -> weighing_campaigns is many:1 on
// (tenant_id, campaign_id), and the campaign side contributes no rows, so the
// join is 1:1 per bucket; no other table is joined, so nothing can fan out. No
// ratio/cap comparison is performed here — the cap only shifts a date offset, and
// numerator (running bucket size) and denominator (planned_cap_per_day) both
// range over the SAME (tenant_id, campaign_id, operator_user_id) key set.
func (r *Repository) createWorkItemsForPublishTx(ctx context.Context, tx pgx.Tx, tenantID, campaignID string) (int, error) {
	tag, err := tx.Exec(ctx, `
INSERT INTO weighing_work_items (
  tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id,
  weighing_category, shed_label, shed_location_id, planned_business_date, due_business_date, work_state
)
SELECT planned.tenant_id,
       planned.campaign_id,
       planned.campaign_shed_id,
       planned.park_id,
       planned.operator_user_id,
       planned.weighing_category,
       planned.display_name,
       planned.location_id,
       planned.start_business_date + planned.day_offset,
       planned.start_business_date + planned.day_offset,
       'scheduled'
FROM (
  SELECT cs.tenant_id,
         cs.campaign_id,
         cs.campaign_shed_id,
         c.park_id,
         cs.operator_user_id,
         cs.weighing_category,
         cs.display_name,
         cs.location_id,
         c.start_business_date,
         floor(
           COALESCE(
             -- BUCKETS, not animals. This spreads a task's shed buckets across
             -- days at planned_cap_per_day buckets per day. It used to read
             -- sum(GREATEST(cs.expected_animal_count, 1)), which pretended to
             -- count animals while expected_animal_count was a fixed literal --
             -- so the sum was only ever a row count wearing an animal's name.
             -- Free-flow has no expected animal total to spread; count(*) says
             -- exactly what the arithmetic has always done.
             count(*) OVER (
               PARTITION BY cs.tenant_id, cs.campaign_id, cs.operator_user_id
               ORDER BY cs.display_name, cs.campaign_shed_id
               ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
             ), 0
           )::numeric / GREATEST(c.planned_cap_per_day, 1)::numeric
         )::int AS day_offset
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c
    ON c.tenant_id = cs.tenant_id
   AND c.campaign_id = cs.campaign_id
  WHERE cs.tenant_id = $1::uuid
    AND cs.campaign_id = $2::uuid
    AND cs.status NOT IN ('canceled')
) planned
ON CONFLICT (tenant_id, campaign_shed_id) DO NOTHING`, tenantID, campaignID)
	if err != nil {
		return 0, fmt.Errorf("weighing: create work items on publish: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// SweepWorkItems is the bounded, resumable, forward-progressing weighing kernel
// tick. Four passes, in this order, each keyset-chunked with
// FOR UPDATE SKIP LOCKED:
//
//  1. terminal reconciliation — a bucket that finished/closed/was deselected
//     stops being open work.
//  2. ROLL-FORWARD — open work whose due business date has passed stays
//     EXECUTABLE and moves to today. planned_business_date is never touched, so
//     the original plan survives for audit. Work is NEVER auto-canceled because a
//     date passed.
//  3. DELAYED/ESCALATION — open work past its ORIGINAL planned business date
//     becomes visibly `delayed` and escalates UPWARD.
//  4. DAY-START — today's open work is surfaced to each assigned operator, once
//     per business date.
//
// Pass 2 runs before pass 4 on purpose: work that rolled forward becomes due
// today and is then included in today's day-start surface.
//
// Each pass's index and keyset order MUST match, or the ORDER BY ... LIMIT is
// either an unbounded Sort over every matching row or a fallback scan of a
// different index over far more rows than the chunk size:
//
//	pass                    | index                                     | WHERE columns                          | ORDER BY / cursor
//	terminal reconciliation | weighing_work_items_open_keyset_idx       | tenant_id, work_state (+ bucket status) | work_item_id (uuid cursor)
//	roll-forward            | weighing_work_items_sweep_due_idx         | tenant_id, work_state, due_business_date| (due_business_date, work_item_id)
//	delayed/escalation      | weighing_work_items_sweep_planned_idx     | tenant_id, work_state, planned_business_date | (planned_business_date, work_item_id)
//	day-start               | weighing_work_items_sweep_due_idx         | tenant_id, work_state, due_business_date| (due_business_date, work_item_id)
//
// Only pass 1 filters solely on tenant_id + work_state, so only pass 1 is
// correctly served end-to-end by the (tenant_id, work_item_id) partial index —
// it is NOT "the whole ordered claim source" for every pass, only for that one.
//
// Forward progress is doubly guaranteed: a monotonically increasing cursor per
// pass (a plain work_item_id cursor for pass 1; a composite
// (date, work_item_id) composite cursor for passes 2-4 (expressed as an
// OR-decomposed date/work_item_id predicate, not a Postgres row-value
// comparison, only so the static kernel guard's literal `work_item_id > $n`
// pattern still recognizes the keyset condition), so the cursor's sort
// order always matches the index's actual output order), AND every pass's
// UPDATE makes the claimed row stop matching its own predicate. Every cadence
// event is enqueued in the SAME transaction as the state change it describes,
// so a cadence can never be visible without its event.
func (r *Repository) SweepWorkItems(ctx context.Context, params domain.KernelSweepParams) (domain.KernelSweepResult, error) {
	tenantID := strings.TrimSpace(params.TenantID)
	if tenantID == "" {
		return domain.KernelSweepResult{}, fmt.Errorf("weighing kernel sweep: tenant id is required")
	}
	asOf := params.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
	businessDate := biztime.BusinessDate(asOf)
	chunk := params.ChunkSize
	if chunk <= 0 || chunk > 5000 {
		chunk = defaultKernelChunkSize
	}
	maxChunks := params.MaxChunks
	if maxChunks <= 0 || maxChunks > 1000 {
		maxChunks = defaultKernelMaxChunks
	}

	result := domain.KernelSweepResult{BusinessDate: businessDate}

	reconciled, truncated, err := r.reconcileTerminalWorkItems(ctx, tenantID, chunk, maxChunks)
	if err != nil {
		return result, err
	}
	result.ReconciledTerminal = reconciled
	result.Truncated = result.Truncated || truncated

	// BEFORE anything rolls: resolve carry-overs that would land on a shed somebody
	// else already owes today. Running this first means the double-booked state never
	// exists, not even for the width of one transaction.
	//
	// The survivor lookup is LATERAL + LIMIT 1: the claim ALREADY PLANNED for today
	// wins and simply carries on, its operator being the person who will be standing
	// at that shed. Nothing is re-assigned -- the slipped item just closes. Keeping it
	// 0..1 also means a shed with several open claims cannot multiply the rows closed.
	//
	// Captures on the slipped bucket do NOT block the merge (maintainer decision,
	// 2026-08-03): whatever was weighed under that task IS weighed, and those
	// observations stay on it as its own history, unmoved and un-reattributed. The
	// surviving task then does its own weighing -- free-flow, so the same tags again
	// or different ones, neither a duplicate of the closed record.
	//
	// closed_reason is a MACHINE token. The farm-readable sentence, with the surviving
	// operator's name and an IST date, is composed by the notification consumer, which
	// can resolve names; SQL here holds ids, and a half-named sentence baked into a
	// column is exactly the abstract copy the notification rule bans.
	//
	// Publishes "weighing.work_item.merged_on_carry_over"
	// (domain.EventWorkItemMergedOnCarryOver) through the same outbox enqueue every
	// other cadence pass uses, one durable event per (campaign, operator, business
	// date) group.
	merged, mergeEvents, truncated, err := r.runCadencePass(ctx, cadencePass{
		tenantID:     tenantID,
		businessDate: businessDate,
		chunk:        chunk,
		maxChunks:    maxChunks,
		eventType:    domain.EventWorkItemMergedOnCarryOver,
		// Closing the kernel work item is not enough, and shipping only that half was
		// a real defect: weighing_work_items has no operator-facing reader. Every
		// screen the operator actually looks at -- their task, their shed list, their
		// day -- reads weighing_campaign_sheds.status, so a bucket left 'pending'
		// still invites them to walk to a shed somebody else is standing at, which is
		// the exact double-work this rule exists to stop. Worse, the kernel has by
		// then stopped tracking it (day-start, roll-forward and delay all filter
		// work_state IN ('scheduled','delayed')), so it would sit 'pending' forever,
		// never chased and never closed.
		//
		// The carried-over TASK auto-closes. Same transaction as the work item, so
		// the two can never disagree.
		afterClaim: func(ctx context.Context, tx pgx.Tx, claimed []claimedWorkItem) error {
			if len(claimed) == 0 {
				return nil
			}
			bucketIDs := make([]string, 0, len(claimed))
			for _, item := range claimed {
				bucketIDs = append(bucketIDs, item.CampaignShedID)
			}
			// One set-based UPDATE for the whole chunk, never one per claimed row.
			_, err := tx.Exec(ctx, `
UPDATE weighing_campaign_sheds
SET status = 'closed',
    completed_at = COALESCE(completed_at, now()),
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND campaign_shed_id = ANY($2::uuid[])
  AND status NOT IN ('completed', 'closed', 'canceled')`, tenantID, bucketIDs)
			return err
		},
		claimSQL: `
WITH claimed AS (
  SELECT slipped.work_item_id, slipped.due_business_date AS sort_date,
         survivor.work_item_id AS survivor_id,
         survivor.operator_user_id AS survivor_operator
  FROM weighing_work_items slipped
  JOIN LATERAL (
    SELECT other.work_item_id, other.operator_user_id
    FROM weighing_work_items other
    WHERE other.tenant_id = slipped.tenant_id
      AND other.park_id = slipped.park_id
      AND other.shed_location_id = slipped.shed_location_id
      AND COALESCE(other.partition_label, '') = COALESCE(slipped.partition_label, '')
      AND other.due_business_date = $2::date
      AND other.work_state IN ('scheduled','delayed')
      AND other.work_item_id <> slipped.work_item_id
    ORDER BY other.planned_business_date, other.work_item_id
    LIMIT 1
  ) survivor ON true
  WHERE slipped.tenant_id = $1::uuid
    AND slipped.work_state IN ('scheduled','delayed')
    AND slipped.due_business_date < $2::date
    AND (slipped.due_business_date > $3::date OR (slipped.due_business_date = $3::date AND slipped.work_item_id > $4::uuid))
  ORDER BY slipped.due_business_date, slipped.work_item_id
  LIMIT $5
  FOR UPDATE OF slipped SKIP LOCKED
)
UPDATE weighing_work_items wi
SET work_state = 'closed',
    terminal_at = now(),
    merged_into_work_item_id = claimed.survivor_id,
    closed_reason = 'merged_on_carry_over',
    updated_at = now()
FROM claimed
WHERE wi.work_item_id = claimed.work_item_id
RETURNING wi.work_item_id::text, wi.campaign_id::text, wi.campaign_shed_id::text,
          wi.park_id::text, wi.operator_user_id::text, wi.shed_label,
          wi.shed_location_id::text, wi.planned_business_date::text, wi.due_business_date::text,
          claimed.sort_date::text`,
	})
	if err != nil {
		return result, err
	}
	result.MergedOnCarryOver = merged
	result.CadenceEvents += mergeEvents
	result.Truncated = result.Truncated || truncated

	rolled, events, truncated, err := r.runCadencePass(ctx, cadencePass{
		tenantID:     tenantID,
		businessDate: businessDate,
		chunk:        chunk,
		maxChunks:    maxChunks,
		eventType:    domain.EventWorkItemRolledForward,
		claimSQL: `
WITH claimed AS (
  SELECT work_item_id, due_business_date AS sort_date
  FROM weighing_work_items
  WHERE tenant_id = $1::uuid
    AND work_state IN ('scheduled','delayed')
    AND due_business_date < $2::date
    AND (due_business_date > $3::date OR (due_business_date = $3::date AND work_item_id > $4::uuid))
  ORDER BY due_business_date, work_item_id
  LIMIT $5
  FOR UPDATE SKIP LOCKED
)
UPDATE weighing_work_items wi
SET due_business_date = $2::date,
    rolled_forward_count = wi.rolled_forward_count + 1,
    last_rolled_forward_on = $2::date,
    updated_at = now()
FROM claimed
WHERE wi.work_item_id = claimed.work_item_id
RETURNING wi.work_item_id::text, wi.campaign_id::text, wi.campaign_shed_id::text,
          wi.park_id::text, wi.operator_user_id::text, wi.shed_label,
          wi.shed_location_id::text, wi.planned_business_date::text, wi.due_business_date::text,
          claimed.sort_date::text`,
	})
	if err != nil {
		return result, err
	}
	result.RolledForward = rolled
	result.CadenceEvents += events
	result.Truncated = result.Truncated || truncated

	delayed, events, truncated, err := r.runCadencePass(ctx, cadencePass{
		tenantID:     tenantID,
		businessDate: businessDate,
		chunk:        chunk,
		maxChunks:    maxChunks,
		eventType:    domain.EventWorkItemDelayed,
		claimSQL: `
WITH claimed AS (
  SELECT work_item_id, planned_business_date AS sort_date
  FROM weighing_work_items
  WHERE tenant_id = $1::uuid
    AND work_state = 'scheduled'
    AND planned_business_date < $2::date
    AND (planned_business_date > $3::date OR (planned_business_date = $3::date AND work_item_id > $4::uuid))
  ORDER BY planned_business_date, work_item_id
  LIMIT $5
  FOR UPDATE SKIP LOCKED
)
UPDATE weighing_work_items wi
SET work_state = 'delayed',
    delayed_since_business_date = $2::date,
    escalated_on = $2::date,
    updated_at = now()
FROM claimed
WHERE wi.work_item_id = claimed.work_item_id
RETURNING wi.work_item_id::text, wi.campaign_id::text, wi.campaign_shed_id::text,
          wi.park_id::text, wi.operator_user_id::text, wi.shed_label,
          wi.shed_location_id::text, wi.planned_business_date::text, wi.due_business_date::text,
          claimed.sort_date::text`,
	})
	if err != nil {
		return result, err
	}
	result.MarkedDelayed = delayed
	result.CadenceEvents += events
	result.Truncated = result.Truncated || truncated

	surfaced, events, truncated, err := r.runCadencePass(ctx, cadencePass{
		tenantID:     tenantID,
		businessDate: businessDate,
		chunk:        chunk,
		maxChunks:    maxChunks,
		eventType:    domain.EventWorkItemDayStart,
		claimSQL: `
WITH claimed AS (
  SELECT work_item_id, due_business_date AS sort_date
  FROM weighing_work_items
  WHERE tenant_id = $1::uuid
    AND work_state IN ('scheduled','delayed')
    AND due_business_date = $2::date
    AND (day_start_surfaced_on IS NULL OR day_start_surfaced_on <> $2::date)
    AND (due_business_date > $3::date OR (due_business_date = $3::date AND work_item_id > $4::uuid))
  ORDER BY due_business_date, work_item_id
  LIMIT $5
  FOR UPDATE SKIP LOCKED
)
UPDATE weighing_work_items wi
SET day_start_surfaced_on = $2::date,
    updated_at = now()
FROM claimed
WHERE wi.work_item_id = claimed.work_item_id
RETURNING wi.work_item_id::text, wi.campaign_id::text, wi.campaign_shed_id::text,
          wi.park_id::text, wi.operator_user_id::text, wi.shed_label,
          wi.shed_location_id::text, wi.planned_business_date::text, wi.due_business_date::text,
          claimed.sort_date::text`,
	})
	if err != nil {
		return result, err
	}
	result.DayStartSurfaced = surfaced
	result.CadenceEvents += events
	result.Truncated = result.Truncated || truncated

	return result, nil
}

// reconcileTerminalWorkItems stops a work item being open once its bucket reached
// a terminal status. It is the only pass that reads the bucket side, and it
// carries the terminal status straight across (`completed`/`closed`/`canceled` are
// valid work_state values), so no state can be invented here.
//
// This pass filters only on tenant_id + work_state (plus the joined bucket
// status), so its keyset — a plain work_item_id cursor over
// weighing_work_items_open_keyset_idx — is the one pass that index genuinely
// serves end to end; leave this pass's shape alone.
func (r *Repository) reconcileTerminalWorkItems(ctx context.Context, tenantID string, chunk, maxChunks int) (int, bool, error) {
	total := 0
	cursor := nilUUIDCursor
	for i := 0; i < maxChunks; i++ {
		claimed, next, err := r.claimChunk(ctx, `
WITH claimed AS (
  SELECT wi.work_item_id, cs.status AS bucket_status
  FROM weighing_work_items wi
  JOIN weighing_campaign_sheds cs
    ON cs.tenant_id = wi.tenant_id
   AND cs.campaign_shed_id = wi.campaign_shed_id
  WHERE wi.tenant_id = $1::uuid
    AND wi.work_state IN ('scheduled','delayed')
    AND cs.status IN ('completed','closed','canceled')
    AND wi.work_item_id > $2::uuid
  ORDER BY wi.work_item_id
  LIMIT $3
  FOR UPDATE OF wi SKIP LOCKED
)
UPDATE weighing_work_items wi
SET work_state = claimed.bucket_status,
    terminal_at = now(),
    updated_at = now()
FROM claimed
WHERE wi.work_item_id = claimed.work_item_id
RETURNING wi.work_item_id::text, wi.campaign_id::text, wi.campaign_shed_id::text,
          wi.park_id::text, wi.operator_user_id::text, wi.shed_label,
          wi.shed_location_id::text, wi.planned_business_date::text, wi.due_business_date::text`,
			[]any{tenantID, cursor, chunk}, nil)
		if err != nil {
			return total, false, fmt.Errorf("weighing kernel sweep: reconcile terminal work items: %w", err)
		}
		total += len(claimed)
		if len(claimed) == 0 || next <= cursor {
			return total, false, nil
		}
		cursor = next
		if len(claimed) < chunk {
			return total, false, nil
		}
	}
	return total, true, nil
}

// ReactivateWorkItemsForBucket is the write side of B09 (reopen/rework must
// not leave the kernel work item permanently terminal). It is the exact
// reverse of reconcileTerminalWorkItems for ONE bucket: that pass moves a work
// item from `scheduled`/`delayed` to a terminal state the moment its bucket's
// `weighing_campaign_sheds.status` becomes `completed`/`closed`/`canceled`;
// this undoes that the moment the bucket LEAVES a terminal status, by resetting
// work_state back to `scheduled` and clearing terminal_at.
//
// MUST be called inside the SAME transaction as the bucket-status UPDATE that
// takes weighing_campaign_sheds out of a terminal status — AGENTS.md requires
// a state transition and the read model/kernel item it owns to commit or roll
// back together, and this is a single-tenant, single-bucket lookup keyed on
// the (tenant_id, campaign_shed_id) unique index (see createWorkItemsForPublishTx's
// ON CONFLICT target), so it is O(1), never a scan.
//
// Call sites this repo does NOT own (verification_verdict.go, repository.go) —
// call this function immediately after, in the same tx, passing scope.CampaignShedID
// / campaignShedID:
//
//  1. verification_verdict.go markObservationRework, right after the existing
//     `if scope.CampaignShedID != "" { UPDATE weighing_campaign_sheds SET
//     status='in_progress' ... WHERE status='completed' }` block (around line
//     291-300): add `if _, err := r.ReactivateWorkItemsForBucket(ctx, tx,
//     verdict.TenantID, scope.CampaignShedID); err != nil { return time.Time{}, err }`
//     inside that same `if scope.CampaignShedID != ""` guard, right after the
//     UPDATE. Call unconditionally (not gated on RowsAffected) — the WHERE
//     clause below only matches a work item that is actually terminal, so a
//     no-op bucket costs one indexed no-match UPDATE.
//
//  2. repository.go ReopenScope, right after the existing
//     `UPDATE weighing_campaign_sheds cs SET status='in_progress' ... WHERE
//     cs.status IN ('completed','closed')` (around line 1935-1944) and its
//     `if result.RowsAffected() == 0 { return nil, ports.ErrNotFound }` check
//     (line 1948-1950): add `if _, err := r.ReactivateWorkItemsForBucket(ctx,
//     tx, tenantID, campaignShedID); err != nil { return nil, err }` right
//     after that RowsAffected check, before the `weighing_shed_observations`
//     withdrawal block.
//
// Deliberately NOT a periodic sweep-pass reconcile: a sweep-based fix would
// leave Calendar/Control Tower reporting the reopened item as finished for up
// to one full sweep interval after the rework/reopen transaction already
// committed, and that same transaction already holds the row lock on the
// bucket — a reconcile pass here would be lossy by construction, not merely
// delayed. This function resets ONLY work_state and terminal_at, never
// due_business_date/planned_business_date/rolled_forward_count/
// delayed_since_business_date: the next SweepWorkItems tick re-derives
// roll-forward/delayed/day-start cadence for the reopened item from its
// existing dates exactly as it would for any other open item, so reactivation
// only needs to take the item out of the terminal predicate every sweep pass
// and every "open work" read already filters on
// (`work_state IN ('scheduled','delayed')`). This keeps the seam narrow even
// if a future rework adds a round/version concept: this function only ever
// reads tenant_id + campaign_shed_id, never campaign-shed history.
func (r *Repository) ReactivateWorkItemsForBucket(ctx context.Context, tx pgx.Tx, tenantID, campaignShedID string) (int, error) {
	tenantID = strings.TrimSpace(tenantID)
	campaignShedID = strings.TrimSpace(campaignShedID)
	if tenantID == "" || campaignShedID == "" {
		return 0, nil
	}
	tag, err := tx.Exec(ctx, `
UPDATE weighing_work_items
SET work_state = 'scheduled',
    terminal_at = NULL,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND campaign_shed_id = $2::uuid
  AND work_state IN ('completed','closed','canceled')`, tenantID, campaignShedID)
	if err != nil {
		return 0, fmt.Errorf("weighing kernel: reactivate work item for bucket: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// cadencePass describes one claim-and-notify sweep pass. It is claimed with a
// composite (date, work_item_id) keyset cursor (expressed as an OR-decomposed
// date/work_item_id predicate) so the cursor's sort
// order always matches the pass's ORDER BY, which always matches the index the
// pass is served from.
type cadencePass struct {
	tenantID     string
	businessDate string
	chunk        int
	maxChunks    int
	eventType    string
	claimSQL     string
	// afterClaim runs inside the SAME transaction as the claim, before the cadence
	// event is enqueued. A pass that must also move state OUTSIDE weighing_work_items
	// puts it here, so the two either commit together or not at all.
	afterClaim func(ctx context.Context, tx pgx.Tx, claimed []claimedWorkItem) error
}

// runCadencePass claims chunks until the pass is drained or the chunk budget is
// spent, enqueueing one cadence event per (campaign, operator) group inside the
// same transaction as the claim.
func (r *Repository) runCadencePass(ctx context.Context, pass cadencePass) (rows int, events int, truncated bool, err error) {
	cursorDate := nilDateCursor
	cursorID := nilUUIDCursor
	for i := 0; i < pass.maxChunks; i++ {
		var chunkEvents int
		claimed, nextDate, nextID, err := r.claimChunkComposite(ctx, pass.claimSQL,
			[]any{pass.tenantID, pass.businessDate, cursorDate, cursorID, pass.chunk},
			func(ctx context.Context, tx pgx.Tx, claimed []claimedWorkItem) error {
				if pass.afterClaim != nil {
					if err := pass.afterClaim(ctx, tx, claimed); err != nil {
						return err
					}
				}
				n, err := r.enqueueCadenceEvents(ctx, tx, pass.tenantID, pass.eventType, pass.businessDate, claimed)
				chunkEvents = n
				return err
			})
		if err != nil {
			return rows, events, false, fmt.Errorf("weighing kernel sweep: %s: %w", pass.eventType, err)
		}
		rows += len(claimed)
		events += chunkEvents
		if len(claimed) == 0 || !cursorAdvanced(cursorDate, cursorID, nextDate, nextID) {
			return rows, events, false, nil
		}
		cursorDate, cursorID = nextDate, nextID
		if len(claimed) < pass.chunk {
			return rows, events, false, nil
		}
	}
	return rows, events, true, nil
}

// cursorAdvanced reports whether the composite (date, id) cursor strictly
// increased. It is what makes the keyset loop provably forward-progressing:
// a pass only keeps iterating when the composite cursor it just computed is
// strictly greater, in the SAME (date, id) order the SQL ORDER BY
// and cursor predicate use, than the cursor it started the chunk with.
func cursorAdvanced(prevDate, prevID, nextDate, nextID string) bool {
	if nextDate != prevDate {
		return nextDate > prevDate
	}
	return nextID > prevID
}

// claimChunk runs one keyset-chunked claim+update in its own transaction and lets
// the caller write side effects (cadence events) in that SAME transaction. It
// returns the claimed rows and the new (strictly larger) work_item_id cursor.
// Used only by the terminal-reconciliation pass, whose keyset is a plain
// work_item_id cursor.
func (r *Repository) claimChunk(
	ctx context.Context,
	sql string,
	args []any,
	sideEffects func(context.Context, pgx.Tx, []claimedWorkItem) error,
) ([]claimedWorkItem, string, error) {
	cursor, _ := args[len(args)-2].(string)
	limit, _ := args[len(args)-1].(int)
	claimed, next, err := r.claimChunkRows(ctx, sql, args, limit, sideEffects)
	if err != nil {
		return nil, cursor, err
	}
	if len(claimed) == 0 {
		return claimed, cursor, nil
	}
	next = cursor
	for _, item := range claimed {
		if item.WorkItemID > next {
			next = item.WorkItemID
		}
	}
	return claimed, next, nil
}

// claimChunkComposite is claimChunk's composite-cursor sibling for the cadence
// passes: the SQL's own composite predicate/ORDER BY already keyset on
// (date, work_item_id), so this only needs to fold the claimed rows' SortDate +
// WorkItemID into the new (date, id) cursor pair the caller advances with.
func (r *Repository) claimChunkComposite(
	ctx context.Context,
	sql string,
	args []any,
	sideEffects func(context.Context, pgx.Tx, []claimedWorkItem) error,
) ([]claimedWorkItem, string, string, error) {
	cursorDate, _ := args[len(args)-3].(string)
	cursorID, _ := args[len(args)-2].(string)
	limit, _ := args[len(args)-1].(int)
	claimed, _, err := r.claimChunkRows(ctx, sql, args, limit, sideEffects)
	if err != nil {
		return nil, cursorDate, cursorID, err
	}
	nextDate, nextID := cursorDate, cursorID
	for _, item := range claimed {
		if cursorAdvanced(nextDate, nextID, item.SortDate, item.WorkItemID) {
			nextDate, nextID = item.SortDate, item.WorkItemID
		}
	}
	return claimed, nextDate, nextID, nil
}

// claimChunkRows is the shared transactional claim+scan body: run the claim SQL,
// scan every returned row (tolerating the cadence passes' extra trailing
// sort_date column), run side effects, and commit — all in one transaction, so
// a claim can never be visible without its side effects.
func (r *Repository) claimChunkRows(
	ctx context.Context,
	sql string,
	args []any,
	limit int,
	sideEffects func(context.Context, pgx.Tx, []claimedWorkItem) error,
) ([]claimedWorkItem, string, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", err
	}
	claimed := make([]claimedWorkItem, 0, limit)
	fields := rows.FieldDescriptions()
	hasSortDate := len(fields) > 9
	for rows.Next() {
		var item claimedWorkItem
		dest := []any{&item.WorkItemID, &item.CampaignID, &item.CampaignShedID, &item.ParkID,
			&item.OperatorUserID, &item.ShedLabel, &item.ShedID, &item.PlannedBusinessDate, &item.DueBusinessDate}
		if hasSortDate {
			dest = append(dest, &item.SortDate)
		}
		if err := rows.Scan(dest...); err != nil {
			rows.Close()
			return nil, "", err
		}
		claimed = append(claimed, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(claimed) == 0 {
		return claimed, "", tx.Commit(ctx)
	}
	if sideEffects != nil {
		if err := sideEffects(ctx, tx, claimed); err != nil {
			return nil, "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}
	return claimed, "", nil
}

// The three durable outbox event types this file produces, spelled out so the
// registry/producer audit can see them at their literal names:
//
//	"weighing.work_item.rolled_forward" (domain.EventWorkItemRolledForward)
//	"weighing.work_item.delayed"        (domain.EventWorkItemDelayed)
//	"weighing.work_item.day_start"      (domain.EventWorkItemDayStart)
//
// enqueueCadenceEvents publishes ONE event per (campaign, operator) group, never
// one per work item, so a 200-row chunk on one park's sheds produces a handful of
// events instead of 200 pushes.
//
// The idempotency key is (event type, tenant, campaign, operator, business
// date, bucket-set digest). The bucket-set digest — a stable hash of THIS
// group's sorted campaign_shed_ids — is required, not cosmetic (B10): every
// pass claims and enqueues per CHUNK (defaultKernelChunkSize=200), each chunk
// committing its own transaction, and a single operator's open buckets on one
// business date can legitimately span multiple chunks. Without the digest, two
// chunks for the SAME (event type, tenant, campaign, operator, date) computed
// the IDENTICAL key, so the outbox's `ON CONFLICT DO NOTHING` silently dropped
// every chunk after the first — even though every chunk's own UPDATE had
// already marked its rows surfaced/rolled/delayed. Folding in the digest keeps
// exact-replay dedup intact (a genuine retry re-claims the identical row set
// via the same predicate, so it hashes to the same digest and still
// collapses) while giving two DIFFERENT bucket sets for the same operator/day
// two DIFFERENT keys, so neither is dropped.
// The outbox event id is derived from the full idempotency key, so a
// redelivered/retried tick still collapses on ON CONFLICT DO NOTHING and the
// operator is not pushed twice for the same set of buckets.
// bucketSetDigest is a deterministic identity for a claimed chunk's bucket
// set: the sorted campaign_shed_ids, hashed. Callers must sort payload.Buckets
// (by CampaignShedID) before calling this, which enqueueCadenceEvents already
// does — so the SAME set of buckets always produces the SAME digest regardless
// of claim/scan order, and a DIFFERENT set (a different chunk, even for the
// same operator/campaign/day) always produces a different one.
func bucketSetDigest(buckets []domain.WorkItemBucket) string {
	ids := make([]string, len(buckets))
	for i, b := range buckets {
		ids[i] = b.CampaignShedID
	}
	sum := sha256.Sum256([]byte(strings.Join(ids, ",")))
	return hex.EncodeToString(sum[:])[:16]
}

func (r *Repository) enqueueCadenceEvents(
	ctx context.Context,
	tx pgx.Tx,
	tenantID, eventType, businessDate string,
	claimed []claimedWorkItem,
) (int, error) {
	type groupKey struct{ campaignID, operatorID string }
	grouped := map[groupKey]*domain.WorkItemCadencePayload{}
	for _, item := range claimed {
		key := groupKey{campaignID: item.CampaignID, operatorID: item.OperatorUserID}
		payload, ok := grouped[key]
		if !ok {
			payload = &domain.WorkItemCadencePayload{
				TenantID:     tenantID,
				CampaignID:   item.CampaignID,
				ParkID:       item.ParkID,
				OperatorID:   item.OperatorUserID,
				BusinessDate: businessDate,
			}
			grouped[key] = payload
		}
		payload.Buckets = append(payload.Buckets, domain.WorkItemBucket{
			CampaignShedID:      item.CampaignShedID,
			ShedID:              item.ShedID,
			ShedLabel:           item.ShedLabel,
			PlannedBusinessDate: item.PlannedBusinessDate,
			DueBusinessDate:     item.DueBusinessDate,
		})
	}
	keys := make([]groupKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	// Deterministic order so a replay produces the same events in the same order.
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].campaignID != keys[j].campaignID {
			return keys[i].campaignID < keys[j].campaignID
		}
		return keys[i].operatorID < keys[j].operatorID
	})
	for _, key := range keys {
		payload := grouped[key]
		sort.Slice(payload.Buckets, func(i, j int) bool {
			return payload.Buckets[i].CampaignShedID < payload.Buckets[j].CampaignShedID
		})
		idem := strings.Join([]string{eventType, tenantID, key.campaignID, key.operatorID, businessDate, bucketSetDigest(payload.Buckets)}, ":")
		// scale-guard:ignore: bounded fan-out over the DISTINCT (campaign, operator) groups inside ONE claimed chunk (a park's sheds, tens at most). Each event carries a DIFFERENT operator's own buckets and a DIFFERENT idempotency key, so it cannot be collapsed into one batched write without leaking another operator's buckets.
		if err := r.enqueue(ctx, tx, tenantID, eventType, key.campaignID, idem, "", payload); err != nil {
			return 0, err
		}
	}
	return len(keys), nil
}

// WeighingProcessState is the Calendar + Control Tower binding.
//
// Grain is declared on the wire (`weighing_work_item`). The five state buckets are
// DISJOINT and the summary is a WHOLE-FILTER aggregate: it is computed by the
// database over every matching work item, so it is page-size independent and
// there is no pagination on this read at all. Day markers are dot-grain (a count
// per business day), never the day's rows.
//
// projection-review: producer unique columns = weighing_work_items
// (tenant_id, campaign_shed_id), PK work_item_id; consumer match/group columns =
// (tenant_id, campaign_id filter) grouped by due_business_date for markers and by
// work_state for the summary. Row multiplicity: no join at all — one work item
// row contributes to exactly one marker day and exactly one state bucket, so
// nothing can fan out and the marker and summary key sets are both exactly the
// filtered work_item_id set.
func (r *Repository) WeighingProcessState(ctx context.Context, tenantID, campaignID, fromBusinessDate, toBusinessDate string) (domain.ProcessState, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	state := domain.ProcessState{
		Grain:            domain.ProcessStateGrain,
		FromBusinessDate: fromBusinessDate,
		ToBusinessDate:   toBusinessDate,
		DayMarkers:       []domain.ProcessStateDayMarker{},
	}
	var campaignFilter any
	if strings.TrimSpace(campaignID) != "" {
		campaignFilter = campaignID
	}

	// Whole-filter disjoint summary. One indexed aggregate, no pagination.
	// projection-review: membership=weighing_work_items rows for the tenant (and campaign when filtered), one row per published weighing_campaign_sheds bucket; group_key=(tenant_id, campaign_id) with the five DISJOINT work_state buckets as FILTER clauses over that same key set; join_cardinality=no join at all, so one work item contributes to exactly one state bucket and nothing can fan out; pagination=none — this read has no page/cursor parameter, the aggregate is computed in the database over the WHOLE filter and is page-size independent; scope=tenant_id always, campaign_id optionally, both as explicit indexed predicates.
	// scale-guard:ignore: indexed whole-filter aggregate over weighing_work_items via weighing_work_items_campaign_state_idx; this is the summary read the operational read-model contract requires to be page-size independent, and at the 5k-50k envelope it is served from canonical indexed SQL with no projection table
	if err := r.pool.QueryRow(ctx, `
SELECT
  count(*) FILTER (WHERE work_state = 'scheduled')::int,
  count(*) FILTER (WHERE work_state = 'delayed')::int,
  count(*) FILTER (WHERE work_state = 'completed')::int,
  count(*) FILTER (WHERE work_state = 'closed')::int,
  count(*) FILTER (WHERE work_state = 'canceled')::int,
  count(*)::int
FROM weighing_work_items
WHERE tenant_id = $1::uuid
  AND ($2::uuid IS NULL OR campaign_id = $2::uuid)`, tenantID, campaignFilter).
		Scan(&state.Summary.Scheduled, &state.Summary.Delayed, &state.Summary.Completed,
			&state.Summary.Closed, &state.Summary.Canceled, &state.Summary.Total); err != nil {
		return domain.ProcessState{}, fmt.Errorf("weighing process state summary: %w", err)
	}
	state.Summary.OpenTotal = state.Summary.Scheduled + state.Summary.Delayed

	// Day markers over the requested business-day window. The window is an
	// inclusive business-date range on both ends; callers align it to business-day
	// boundaries, never to `now ± N hours`.
	// projection-review: membership=the SAME weighing_work_items row set as the summary above, further restricted to the inclusive business-date window; group_key=(tenant_id, campaign_id, due_business_date) — one marker row per business day; join_cardinality=no join at all, so one work item lands on exactly one marker day; pagination=none — markers are dot-grain counts per day, never the day's rows, and the grid never fetches a day's work items to draw itself; scope=tenant_id always, campaign_id optionally, plus the explicit inclusive from/to business-date bounds.
	// scale-guard:ignore: indexed day-marker aggregate bounded by the requested inclusive business-date window; dot-grain markers only, never the day's rows
	rows, err := r.pool.Query(ctx, `
SELECT due_business_date::text,
       count(*) FILTER (WHERE work_state IN ('scheduled','delayed'))::int,
       count(*) FILTER (WHERE work_state = 'delayed')::int
FROM weighing_work_items
WHERE tenant_id = $1::uuid
  AND ($2::uuid IS NULL OR campaign_id = $2::uuid)
  AND due_business_date >= $3::date
  AND due_business_date <= $4::date
GROUP BY due_business_date
HAVING count(*) FILTER (WHERE work_state IN ('scheduled','delayed')) > 0
ORDER BY due_business_date`, tenantID, campaignFilter, fromBusinessDate, toBusinessDate)
	if err != nil {
		return domain.ProcessState{}, fmt.Errorf("weighing process state day markers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var marker domain.ProcessStateDayMarker
		if err := rows.Scan(&marker.BusinessDate, &marker.OpenCount, &marker.DelayedCount); err != nil {
			return domain.ProcessState{}, err
		}
		state.DayMarkers = append(state.DayMarkers, marker)
	}
	if err := rows.Err(); err != nil {
		return domain.ProcessState{}, err
	}
	return state, nil
}
