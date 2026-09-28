package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
)

// THE DISTRIBUTION VERIFIER RECORDS THE TOTAL FEED (maintainer decision 2026-09-28).
//
// The reading lives on the completion row itself (migration 000455), not in a side table: a
// distribution has exactly ONE combined quantity per pen-session -- the feed is mixed by the time it
// reaches the trough -- so there is no per-item grain to key a second table on.
//
// A rework re-submit clears it (CompleteDistribution's rework branch): the new video is a new
// submission, and a reading taken off the rejected one must never let the new item approve blank.

var _ ports.DistributionFeedReadingStore = (*Repository)(nil)

// feedDistributionFeedRecordedAction is the audit action for the verifier's total-feed reading.
const feedDistributionFeedRecordedAction = "feed.distribution.total_feed_recorded"

// distributionCompletionPlanRowSQL reads one completion's natural-key grain (a PK lookup).
const distributionCompletionPlanRowSQL = `
SELECT target_date, park_id::text, shed_id::text, partition_key, session_no, workflow
FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`

// distributionPackedSnapshotSQL is the SAME pen-session's packing bag, when one was packed and is
// not being redone. Unique on the natural key feed_packing_completions_natural_uq, so at most one row.
const distributionPackedSnapshotSQL = `
SELECT packed_items
FROM feed_packing_completions
WHERE tenant_id = $1::uuid
  AND park_id = $2::uuid
  AND shed_id = $3::uuid
  AND partition_key = $4
  AND session_no = $5
  AND target_date = $6::date
  AND workflow = $7
  AND status IN ('pending_verification', 'completed')
  AND packed_items IS NOT NULL`

// distributionFrozenSheetTotalSQL is the fallback plan: the frozen issued sheet for the pen-session,
// summed over EVERY feed item (the trough receives them mixed).
//
// projection-review: membership=feed_direction_issue_rows at their natural key (tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, shed_tag_key, breed_key, feed_item_key) under the at-most-one live issue per (tenant, park, feed_day, workflow); group_key=none, one scalar for ONE completion's (feed_day=target_date, park_id, shed_id, partition_key, session_no, workflow) -- the columns feed_distribution_completions' natural key makes unique, so the consumer's match set is the producer's unique set; join_cardinality=issues to rows 1:N by feed_direction_issue_id, the N side (every ration/breed/item cell of one pen-session) SUMMED to one total, which is exactly what reaches the trough; pagination=none, one pen-session's bounded row set; scope=tenant_id on both tables plus the completion's own park/shed
const distributionFrozenSheetTotalSQL = `
SELECT COALESCE(SUM(r.quantity_kg), 0)::float8
FROM feed_direction_issues i
JOIN feed_direction_issue_rows r
  ON r.tenant_id = i.tenant_id AND r.feed_direction_issue_id = i.feed_direction_issue_id
WHERE i.tenant_id = $1::uuid
  AND i.park_id = $2::uuid
  AND i.feed_day = $3::date
  AND i.workflow = $4
  AND i.state IN ('issued', 'amended', 'locked')
  AND r.shed_id = $5::uuid
  AND r.partition_key = $6
  AND r.session_no = $7
  AND r.quantity_kg IS NOT NULL`

// DistributionPlannedFeedKg is the planned combined feed the verifier's reading is checked against.
//
// Source order, and why:
//
//  1. The PACKED-AGAINST SNAPSHOT of the same pen-session's packing bag. That bag IS what the crew
//     carried to the trough; if the afternoon correction rewrote the sheet after it was packed (an
//     experiment pen, which the correction deliberately does not reopen), the bag -- not the
//     rewritten sheet -- is what an honest reading will match. A bag in `rework` is excluded: it is
//     being refilled to the new sheet, so its old snapshot is known to be stale.
//  2. The FROZEN ISSUED SHEET for the pen-session, every item summed.
//
// The figure stops in the applier: only a direction ever reaches a verifier surface.
func (r *Repository) DistributionPlannedFeedKg(ctx context.Context, tenantID, completionID string) (float64, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var (
		targetDate   time.Time
		parkID       string
		shedID       string
		partitionKey string
		sessionNo    int32
		workflow     string
	)
	err := r.pool.QueryRow(ctx, distributionCompletionPlanRowSQL, tenantID, completionID).
		Scan(&targetDate, &parkID, &shedID, &partitionKey, &sessionNo, &workflow)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, ports.ErrDistributionCompletionNotFound
	}
	if err != nil {
		return 0, false, fmt.Errorf("feeddirection: read distribution completion plan: %w", err)
	}

	var packedItems []byte
	err = r.pool.QueryRow(ctx, distributionPackedSnapshotSQL, tenantID, parkID, shedID, partitionKey, sessionNo, targetDate, workflow).
		Scan(&packedItems)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("feeddirection: read packed snapshot for distribution plan: %w", err)
	}
	if total, ok := packedSnapshotTotalKg(packedItems); ok {
		return total, true, nil
	}

	var total float64
	if err := r.pool.QueryRow(ctx, distributionFrozenSheetTotalSQL, tenantID, parkID, targetDate, workflow, shedID, partitionKey, sessionNo).
		Scan(&total); err != nil {
		return 0, false, fmt.Errorf("feeddirection: read frozen sheet total for distribution plan: %w", err)
	}
	if total <= 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, false, nil
	}
	return total, true, nil
}

// packedSnapshotTotalKg sums a packing snapshot's positive quantities. An unreadable or empty
// snapshot reports ok=false so the caller falls back to the sheet rather than checking against zero.
func packedSnapshotTotalKg(raw []byte) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var items []packedItemSnapshotJSON
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0, false
	}
	total := 0.0
	for _, item := range items {
		kg, err := strconv.ParseFloat(strings.TrimSpace(item.QuantityKg), 64)
		if err != nil || kg <= 0 || math.IsNaN(kg) || math.IsInf(kg, 0) {
			continue
		}
		total += kg
	}
	return total, total > 0
}

// RecordDistributionVerifiedFeed writes the verifier's total-feed reading onto the completion and
// audits it, in one transaction. Runs BEFORE the verdict is recorded, so a refusal here stops the
// whole approve.
func (r *Repository) RecordDistributionVerifiedFeed(ctx context.Context, p ports.RecordDistributionVerifiedFeedParams) error {
	if math.IsNaN(p.EnteredKg) || math.IsInf(p.EnteredKg, 0) || p.EnteredKg < 0 || p.EnteredKg > domain.MaxDistributionTotalFeedKg {
		return ports.ErrDistributionFeedOutOfRange
	}
	// planned travels as text so "no readable plan" binds NULL, never 0 -- a zero plan would read to
	// every later reader as a pen directed no feed at all.
	plannedText := ""
	if p.PlannedKg != nil && !math.IsNaN(*p.PlannedKg) && !math.IsInf(*p.PlannedKg, 0) && *p.PlannedKg > 0 {
		plannedText = strconv.FormatFloat(*p.PlannedKg, 'f', 3, 64)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("feeddirection: begin record distribution total feed tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var shedID, parkID string
	err = tx.QueryRow(ctx, `
UPDATE feed_distribution_completions
SET verified_feed_kg = $3::numeric,
    verified_planned_feed_kg = nullif($4, '')::numeric,
    verified_feed_variance_acknowledged = $5,
    verified_feed_recorded_by = nullif($6::text, '')::uuid,
    verified_feed_recorded_at = now(),
    updated_at = now()
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid
RETURNING shed_id::text, park_id::text`,
		p.TenantID, p.CompletionID, p.EnteredKg, plannedText, p.VarianceAcknowledged, strings.TrimSpace(p.RecordedBy)).
		Scan(&shedID, &parkID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrDistributionCompletionNotFound
	}
	if err != nil {
		return fmt.Errorf("feeddirection: record distribution total feed: %w", err)
	}

	after := map[string]any{
		"park_id":               parkID,
		"shed_id":               shedID,
		"verified_feed_kg":      p.EnteredKg,
		"variance_acknowledged": p.VarianceAcknowledged,
	}
	if plannedText != "" {
		// What the reading was checked against: the audit row must say she was warned, not only
		// what she typed.
		after["planned_feed_kg"] = plannedText
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      strings.TrimSpace(p.RecordedBy),
		ActorType:    "verifier",
		Action:       feedDistributionFeedRecordedAction,
		ResourceType: feedDistributionResourceType,
		ResourceID:   p.CompletionID,
		ScopeType:    "shed",
		ScopeID:      shedID,
		AfterState:   after,
		Metadata:     map[string]any{"source": "feed-distribution-verification", "idempotency_key": strings.TrimSpace(p.IdempotencyKey)},
		TraceID:      p.TraceID,
	}); err != nil {
		return fmt.Errorf("feeddirection: write distribution total feed audit: %w", err)
	}

	if err := r.commitAndInvalidateReadCache(ctx, tx); err != nil {
		return fmt.Errorf("feeddirection: commit distribution total feed: %w", err)
	}
	committed = true
	return nil
}

// DistributionVerifiedFeedRecorded reports whether the completion's CURRENT submission carries a
// reading. The rework re-submit clears the column, so a reading taken off a rejected video never
// answers true for the new one.
func (r *Repository) DistributionVerifiedFeedRecorded(ctx context.Context, tenantID, completionID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var recorded bool
	err := r.pool.QueryRow(ctx, `
SELECT verified_feed_kg IS NOT NULL
FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, tenantID, completionID).Scan(&recorded)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ports.ErrDistributionCompletionNotFound
	}
	if err != nil {
		return false, fmt.Errorf("feeddirection: read distribution total feed recorded: %w", err)
	}
	return recorded, nil
}
