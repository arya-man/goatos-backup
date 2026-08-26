package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
	"github.com/vgoats/goatos/backend/internal/verification/samplingsql"
)

// RANDOMIZED VERIFICATION SAMPLING -- storage side (maintainer decision 2026-08-26). See
// migration 000214 and verification/domain/sampling.go for the rule; this file only reads and
// writes it.

// samplingPredicateSQL narrows a queue read to the items DRAWN for review at the percentage in
// force on each item's OWN business day.
//
// Per ITEM rather than per request, because one queue page can span days: a verifier working a
// backlog sees yesterday's items under yesterday's percentage and today's under today's, which is
// the whole point of effective-dating the policy.
//
// The correlated subquery costs NOTHING when sampling is off: `NOT $n` is then true and the OR
// short-circuits before the subquery is evaluated, so every leadership read keeps the plan it has
// today. When on, it is one primary-key seek per candidate row on a table with at most one row per
// (category, change) -- and the queue page is at most 21 rows.
//
// COALESCE 100: a category the CEO has never set is verified in full, which is exactly how the
// queue behaved before this feature existed.
func samplingPredicateSQL(paramIndex int) string {
	return fmt.Sprintf("\n  AND (NOT $%d::boolean OR %s)", paramIndex, samplingInSampleSQL())
}

// samplingInSampleSQL is the predicate on its own, for the reads that are ALWAYS sampled rather
// than sampled-for-some-callers: the leadership KPI strip's "videos waiting for review" and its
// per-module backlog, which must describe the same set of work the throughput beside them is
// measured against, or the estimated days-to-clear divides one population by another's rate.
//
// The definition itself lives in verification/samplingsql, because the vaccination live tracker
// counts the same fact from another package and the two must not be able to disagree.
func samplingInSampleSQL() string {
	return samplingsql.InSample("vi")
}

// ListSamplingPolicies returns the standing policy row per category for one business date: the
// newest row on or before it. A category with no row is absent from the result and resolves to
// domain.DefaultSamplePercent in the service.
func (r *Repository) ListSamplingPolicies(ctx context.Context, tenantID, businessDate string) ([]ports.SamplingPolicyRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT DISTINCT ON (p.category)
  p.category,
  p.sample_percent::int,
  to_char(p.effective_business_date, 'YYYY-MM-DD'),
  p.set_by::text,
  COALESCE(setter.display_name, ''),
  p.set_at
FROM verification_sampling_policies p
LEFT JOIN LATERAL (
  SELECT wm.display_name
  FROM workforce_members wm
  WHERE wm.tenant_id = p.tenant_id
    AND (wm.workforce_member_id = p.set_by OR wm.user_id = p.set_by)
  ORDER BY
    (wm.workforce_member_id = p.set_by) DESC,
    (wm.status = 'active') DESC,
    wm.updated_at DESC,
    wm.workforce_member_id
  LIMIT 1
) setter ON true
WHERE p.tenant_id = $1::uuid
  AND p.effective_business_date <= $2::date
ORDER BY p.category, p.effective_business_date DESC`, tenantID, businessDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ports.SamplingPolicyRow, 0, 16)
	for rows.Next() {
		var row ports.SamplingPolicyRow
		var setBy *string
		if err := rows.Scan(&row.Category, &row.Percent, &row.EffectiveBusinessDate, &setBy, &row.SetByName, &row.SetAt); err != nil {
			return nil, err
		}
		row.SetBy = setBy
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpsertSamplingPolicy writes one category's percentage for one business date.
//
// Naturally idempotent: the primary key IS (tenant, category, effective business date), so a
// replay of the same request rewrites the same row to the same value and changes nothing else. A
// second, DIFFERENT value on the same day is a deliberate correction and replaces the first --
// there is one percentage in force per category per day, not a stack of them.
func (r *Repository) UpsertSamplingPolicy(ctx context.Context, in domain.SetSamplingPolicy) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	// The write is already replay-safe by construction -- same triple, same value -- but the key is
	// reserved anyway so a SAME-KEY DIFFERENT-PAYLOAD replay is REFUSED rather than silently
	// applying whichever request arrived last. That is the case the natural key cannot see: two
	// different shares racing on the same day are two different decisions, and the loser must be
	// told, not dropped.
	reservation, err := reserveIdempotency(ctx, tx, in.TenantID, "verification.sampling", in.IdempotencyKey,
		requestFingerprint(in.Category, in.EffectiveBusinessDate, strconv.Itoa(in.Percent)))
	if err != nil {
		return mapWriteErr(err)
	}
	// NOTE: an exact replay still WRITES, and that is deliberate.
	//
	// The obvious shape -- return early when the key was already used -- shipped and was caught by
	// the randomization E2E. The key is derived from (category, business day, share), so setting 40%,
	// then 80%, then BACK to 40% on the same day reuses the first key. Skipping the write there does
	// not replay a previous outcome; it silently refuses to return to a share the CEO just asked for,
	// and the panel then reports 40% while the queue still runs at 80%.
	//
	// Re-applying is safe precisely because the write is VALUE-idempotent: one row per
	// (tenant, category, day), so writing it twice leaves the identical row. The reservation above
	// therefore earns its place as the CONFLICT detector -- a same-key DIFFERENT-payload request is
	// refused with ErrIdempotencyConflict rather than letting whichever request arrived last win --
	// and not as a write suppressor. _ = reservation keeps that explicit.
	_ = reservation
	var actor any
	if strings.TrimSpace(in.ActorID) != "" {
		actor = in.ActorID
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO verification_sampling_policies (tenant_id, category, effective_business_date, sample_percent, set_by, set_at)
VALUES ($1::uuid, $2, $3::date, $4, $5::uuid, now())
ON CONFLICT (tenant_id, category, effective_business_date)
DO UPDATE SET sample_percent = EXCLUDED.sample_percent, set_by = EXCLUDED.set_by, set_at = now()`,
		in.TenantID, in.Category, in.EffectiveBusinessDate, in.Percent, actor); err != nil {
		return mapWriteErr(err)
	}
	// The policy row's identity is its natural key, so the reservation records a UUID derived from
	// that same triple rather than a surrogate the table does not have.
	resultID := platformoutbox.DeterministicUUID("verification_sampling_policy:" + in.TenantID + ":" + in.Category + ":" + in.EffectiveBusinessDate)
	if err := completeIdempotency(ctx, tx, in.TenantID, "verification.sampling", in.IdempotencyKey,
		"verification_sampling_policy", resultID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListSamplingDayStats is the Randomization panel's whole-day aggregate, per category, for ONE
// Asia/Kolkata business date.
//
// projection-review: membership=verification_items rows captured within the one business day, unique key (tenant_id, item_id) so COUNT(*) is exact; group_key=category, the same grain the policy is keyed by; join_cardinality=the policy CTE is DISTINCT ON (category) so it contributes at most ONE row per category and cannot multiply an item row, and there is no other join; pagination=none, this is the whole-day aggregate and is never derived from a page; scope=tenant_id + captured_at within the requested business day
//
// The numerator (reviewed) and the denominator (selected) range over the IDENTICAL key set -- the
// same rows of the same single table under the same WHERE -- and differ only by the status/auto
// filters inside FILTER, so the ratio can never exceed 1.
func (r *Repository) ListSamplingDayStats(ctx context.Context, tenantID, businessDate string) (map[string]domain.SamplingDayStats, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	dayStart, err := time.ParseInLocation("2006-01-02", businessDate, biztime.DefaultLocation())
	if err != nil {
		return nil, fmt.Errorf("verification: invalid sampling business date: %w", err)
	}
	dayEnd := dayStart.AddDate(0, 0, 1)
	rows, err := r.pool.Query(ctx, `
WITH policy AS (
  SELECT DISTINCT ON (p.category) p.category, p.sample_percent
  FROM verification_sampling_policies p
  WHERE p.tenant_id = $1::uuid
    AND p.effective_business_date <= $2::date
  ORDER BY p.category, p.effective_business_date DESC
)
SELECT vi.category,
  COUNT(*) FILTER (WHERE vi.status <> 'withdrawn')::int,
  COUNT(*) FILTER (WHERE vi.status <> 'withdrawn'
    AND vi.sampling_bucket < COALESCE(policy.sample_percent, `+fmt.Sprint(domain.DefaultSamplePercent)+`))::int,
  COUNT(*) FILTER (WHERE vi.status IN ('approved', 'rejected')
    AND vi.auto_resolution IS NULL
    AND vi.sampling_bucket < COALESCE(policy.sample_percent, `+fmt.Sprint(domain.DefaultSamplePercent)+`))::int,
  COUNT(*) FILTER (WHERE vi.auto_resolution = $5)::int
FROM verification_items vi
LEFT JOIN policy ON policy.category = vi.category
WHERE vi.tenant_id = $1::uuid
  AND vi.captured_at >= $3::timestamptz
  AND vi.captured_at < $4::timestamptz
GROUP BY vi.category`,
		tenantID, businessDate, dayStart, dayEnd, domain.AutoResolutionNotSampled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := map[string]domain.SamplingDayStats{}
	for rows.Next() {
		var category string
		var row domain.SamplingDayStats
		if err := rows.Scan(&category, &row.Captured, &row.Selected, &row.Reviewed, &row.AutoAccepted); err != nil {
			return nil, err
		}
		stats[category] = row
	}
	return stats, rows.Err()
}

// SettleUnsampledItems approves the pending items of CLOSED business days that the policy did not
// draw, stamping auto_resolution = 'not_sampled'.
//
// WHY IT WAITS FOR THE DAY TO CLOSE. The percentage is live: raising it at 15:00 must pull more of
// TODAY's already-captured videos into her queue (maintainer decision 2026-08-26). An item waived
// the moment it arrived could not be recruited back, so waiving is deferred until the day -- and
// with it the day's percentage -- can no longer change. `before` is the caller's business-day
// start; nothing captured on or after it is touched.
//
// WHY IT MUST HAPPEN AT ALL. Verifier approval is the gate that COMPLETES the work for feed and
// weighing. An item left pending forever would hold a feed pen-session out of 'completed' and hold
// a weighing bucket open against an unconditional close gate. Settling it emits the ordinary
// verification.verdict.approved event, so every producer's consumer applies exactly as it does for
// a human approve -- no second apply path, no producer change.
//
// waivableCategories is the service's registry-derived list; a category whose approve must carry a
// measurement is never in it (see domain.CategoryDefinition.SamplingWaivable) and its items are
// left for a human, which is why an empty list settles nothing rather than everything.
func (r *Repository) SettleUnsampledItems(ctx context.Context, in ports.SettleUnsampledParams) (int, error) {
	if len(in.WaivableCategories) == 0 || in.Limit <= 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer rollback(ctx, tx)

	// One claim read for the whole tick: the items this worker will settle, locked so a second
	// worker instance takes a different batch instead of racing this one.
	claimRows, err := tx.Query(ctx, `
SELECT `+itemColumns+`
FROM verification_items vi
WHERE vi.tenant_id = $1::uuid
  AND vi.status = 'pending'
  AND vi.closed_at IS NULL
  AND vi.captured_at < $2::timestamptz
  AND vi.category = ANY($3::text[])
  AND vi.sampling_bucket >= COALESCE((
        SELECT p.sample_percent
        FROM verification_sampling_policies p
        WHERE p.tenant_id = vi.tenant_id
          AND p.category = vi.category
          AND p.effective_business_date <= (vi.captured_at AT TIME ZONE '`+biztime.DefaultTimezone+`')::date
        ORDER BY p.effective_business_date DESC
        LIMIT 1), `+fmt.Sprint(domain.DefaultSamplePercent)+`)
ORDER BY vi.captured_at ASC, vi.item_id ASC
LIMIT $4
FOR UPDATE SKIP LOCKED`, in.TenantID, in.Before, in.WaivableCategories, in.Limit)
	if err != nil {
		return 0, err
	}
	claimed := make([]domain.Item, 0, in.Limit)
	for claimRows.Next() {
		item, scanErr := scanItemRow(claimRows)
		if scanErr != nil {
			claimRows.Close()
			return 0, scanErr
		}
		claimed = append(claimed, item)
	}
	claimRows.Close()
	if err := claimRows.Err(); err != nil {
		return 0, err
	}
	if len(claimed) == 0 {
		return 0, tx.Commit(ctx)
	}

	settled := 0
	for _, item := range claimed {
		// Background closeout over a batch the claim read above already bounded to in.Limit rows
		// (100/tick by default), not a per-request fan-out. Each item needs its OWN durable outbox
		// row and its own vaccination-drive readiness check, and reusing the same two helpers the
		// human verdict path uses is what keeps the waived payload from drifting away from the
		// reviewed one.
		// scale-guard:ignore: bounded background sweep, one claimed batch per tick
		tag, execErr := tx.Exec(ctx, `
UPDATE verification_items
SET status = 'approved',
    auto_resolution = $1,
    verified_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $2::uuid
  AND item_id = $3::uuid
  AND status = 'pending'
  AND closed_at IS NULL`, domain.AutoResolutionNotSampled, in.TenantID, item.ItemID)
		if execErr != nil {
			return 0, mapWriteErr(execErr)
		}
		if tag.RowsAffected() == 0 {
			// A verifier decided it between the claim and here. Her verdict wins; skip.
			continue
		}
		settled++
		item.Status = domain.StatusApproved
		item.RowVersion++
		idempotencyKey := fmt.Sprintf("%s:%s:%d", EventVerdictApproved, item.ItemID, item.RowVersion)
		if err := insertOutboxEvent(ctx, tx, in.TenantID, EventVerdictApproved, item.ItemID, idempotencyKey,
			verificationVerdictPayload(item)); err != nil {
			return 0, err
		}
		if err := insertVaccinationDriveReadyOutboxIfReady(ctx, tx, item); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return settled, nil
}
