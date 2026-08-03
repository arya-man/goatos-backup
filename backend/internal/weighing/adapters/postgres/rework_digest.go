package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Per-shed rework digest.
//
// PROBLEM. A rework verdict is applied by the durable-bus consumer one observation at a
// time, and there is no shed-level "review finished" action anywhere in the product: no
// batch verdict endpoint, no per-shed submit on the verifier's side. So a verifier who
// bounces five of a shed's fifteen captures produced FIVE pushes for one trip the operator
// makes back to one shed.
//
// MECHANISM. The grouping moment is invented here as a DEBOUNCE over derived state. An
// un-delivered bounce is exactly `verification_status='rework' AND rework_notified_at IS
// NULL` on weighing_observations -- no digest table, no parallel queue, nothing that can
// drift from the bounce it describes. Each tick:
//
//  1. pick buckets that have at least one un-delivered bounce AND have gone quiet
//     (newest bounce older than QuietWindow) or have waited too long (oldest bounce older
//     than MaxAge, so a steadily-rejecting verifier cannot starve the operator);
//  2. claim that bucket's un-delivered bounces FOR UPDATE SKIP LOCKED;
//  3. stamp rework_notified_at on exactly those rows AND enqueue ONE
//     weighing.observation.rework_digest naming them -- in the SAME transaction.
//
// NOTHING IS DROPPED. A sixth rejection that lands after the digest already fired is
// simply another row with rework_notified_at IS NULL; the next tick flushes it as its own
// (smaller) digest. There is no state in which a bounce exists and no digest will ever
// name it. A re-submission that is bounced again clears the stamp back to NULL inside the
// verdict UPDATE itself (verification_verdict.go), so a second round needs no special case.
//
// IDEMPOTENT. The event's idempotency key is derived from the exact set of observation ids
// in the flush, and the flush is only ever built from rows that were un-stamped a moment
// earlier under FOR UPDATE, so a retry of the tick cannot re-name a delivered bounce. A
// redelivered VERDICT event never reaches the digest at all: ApplyVerificationVerdict
// short-circuits on the verification event id before it touches the row.
//
// LUMP-SUM IS NOT HERE. weighing_shed_observations is one capture for the whole shed, so
// its rework push is already one-per-shed; routing it through a debounce would only add
// latency to a message that was never duplicated.
//
// BOUNDED. Chunked by bucket with FOR UPDATE SKIP LOCKED and a MaxChunks ceiling, like
// SweepWorkItems. The driving index is PARTIAL on the un-delivered rework set
// (weighing_observations_rework_undelivered_idx), which holds a handful of rows at any
// instant even at the top of the 5k-50k envelope.
const eventTypeObservationReworkDigest = "weighing.observation.rework_digest"

const (
	defaultReworkDigestQuietWindow = 3 * time.Minute
	defaultReworkDigestMaxAge      = 20 * time.Minute
	defaultReworkDigestNamedLimit  = 3
	defaultReworkDigestChunkSize   = 50
	defaultReworkDigestMaxChunks   = 20
)

// reworkDigestBucketsSQL selects the buckets whose un-delivered bounces are ready to flush.
//
// A bucket is ready when its NEWEST un-delivered bounce is older than the quiet window (the
// verifier has moved on) OR its OLDEST is older than the max age (the verifier has not moved
// on, and the operator has waited long enough). Ordered by the oldest bounce so the operator
// kept waiting longest is served first, and keyset-resumable by campaign_shed_id.
const reworkDigestBucketsSQL = `
SELECT o.campaign_shed_id::text
FROM weighing_observations o
WHERE o.tenant_id = $1::uuid
  AND o.verification_status = 'rework'
  AND o.rework_notified_at IS NULL
  AND o.campaign_shed_id IS NOT NULL
GROUP BY o.campaign_shed_id
HAVING max(o.verified_at) <= $2::timestamptz
    OR min(o.verified_at) <= $3::timestamptz
ORDER BY min(o.verified_at), o.campaign_shed_id
LIMIT $4`

// reworkDigestClaimSQL locks one bucket's un-delivered bounces. SKIP LOCKED so two workers
// (or a worker racing a concurrent verdict on the same row) never block each other; the
// skipped row keeps rework_notified_at IS NULL and is picked up next tick.
const reworkDigestClaimSQL = `
SELECT o.observation_id::text,
  COALESCE(o.scanned_identifier, ''),
  o.weight_kg::float8,
  COALESCE(o.rework_reason, ''),
  o.verified_at
FROM weighing_observations o
WHERE o.tenant_id = $1::uuid
  AND o.campaign_shed_id = $2::uuid
  AND o.verification_status = 'rework'
  AND o.rework_notified_at IS NULL
ORDER BY o.verified_at, o.observation_id
FOR UPDATE SKIP LOCKED`

// reworkDigestBucketScopeSQL resolves the routing context of the bucket: which campaign and
// park it belongs to, its farm-readable shed label, and the ONE operator who owns it.
const reworkDigestBucketScopeSQL = `
SELECT cs.campaign_id::text,
  cs.location_id::text,
  COALESCE(cs.display_name, ''),
  wc.park_id::text,
  COALESCE(cs.operator_user_id::text, wc.operator_user_id::text, '')
FROM weighing_campaign_sheds cs
JOIN weighing_campaigns wc
  ON wc.tenant_id = cs.tenant_id
 AND wc.campaign_id = cs.campaign_id
WHERE cs.tenant_id = $1::uuid
  AND cs.campaign_shed_id = $2::uuid`

const reworkDigestStampSQL = `
UPDATE weighing_observations
SET rework_notified_at = now()
WHERE tenant_id = $1::uuid
  AND observation_id = ANY($2::uuid[])`

// SweepReworkDigests performs one bounded per-shed rework-digest tick.
func (r *Repository) SweepReworkDigests(ctx context.Context, params domain.ReworkDigestSweepParams) (domain.ReworkDigestSweepResult, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	params = normalizeReworkDigestParams(params)
	if params.TenantID == "" {
		return domain.ReworkDigestSweepResult{}, fmt.Errorf("weighing rework digest sweep: tenant id is required")
	}

	quietBefore := params.AsOf.Add(-params.QuietWindow)
	oldestBefore := params.AsOf.Add(-params.MaxAge)

	result := domain.ReworkDigestSweepResult{}
	for chunk := 0; chunk < params.MaxChunks; chunk++ {
		bucketIDs, err := r.readyReworkDigestBuckets(ctx, params, quietBefore, oldestBefore)
		if err != nil {
			return domain.ReworkDigestSweepResult{}, err
		}
		if len(bucketIDs) == 0 {
			return result, nil
		}
		progressed := 0
		for _, bucketID := range bucketIDs {
			named, err := r.flushReworkDigestBucket(ctx, params, bucketID)
			if err != nil {
				return domain.ReworkDigestSweepResult{}, err
			}
			if named == 0 {
				// Every row was locked by someone else this instant. It stays un-notified and
				// the next tick picks it up; treating it as progress here would spin.
				continue
			}
			progressed++
			result.DigestsEmitted++
			result.ObservationsNamed += named
		}
		if progressed == 0 {
			return result, nil
		}
		if len(bucketIDs) < params.ChunkSize {
			return result, nil
		}
		result.Truncated = chunk == params.MaxChunks-1
	}
	return result, nil
}

func (r *Repository) readyReworkDigestBuckets(
	ctx context.Context,
	params domain.ReworkDigestSweepParams,
	quietBefore, oldestBefore time.Time,
) ([]string, error) {
	rows, err := r.pool.Query(ctx, reworkDigestBucketsSQL, params.TenantID, quietBefore, oldestBefore, params.ChunkSize)
	if err != nil {
		return nil, fmt.Errorf("weighing rework digest sweep: select buckets: %w", err)
	}
	defer rows.Close()
	bucketIDs := make([]string, 0, params.ChunkSize)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("weighing rework digest sweep: scan bucket: %w", err)
		}
		bucketIDs = append(bucketIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("weighing rework digest sweep: bucket rows: %w", err)
	}
	return bucketIDs, nil
}

// flushReworkDigestBucket stamps and announces ONE bucket's un-delivered bounces atomically.
// It returns how many observations the emitted digest named (0 when another worker held them).
func (r *Repository) flushReworkDigestBucket(
	ctx context.Context,
	params domain.ReworkDigestSweepParams,
	campaignShedID string,
) (int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, reworkDigestClaimSQL, params.TenantID, campaignShedID)
	if err != nil {
		return 0, fmt.Errorf("weighing rework digest sweep: claim bucket %s: %w", campaignShedID, err)
	}
	type claimed struct {
		observationID string
		identifier    string
		weightKg      float64
		reason        string
		verifiedAt    time.Time
	}
	claims := make([]claimed, 0, 16)
	for rows.Next() {
		var c claimed
		if err := rows.Scan(&c.observationID, &c.identifier, &c.weightKg, &c.reason, &c.verifiedAt); err != nil {
			rows.Close()
			return 0, fmt.Errorf("weighing rework digest sweep: scan claim: %w", err)
		}
		claims = append(claims, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("weighing rework digest sweep: claim rows: %w", err)
	}
	if len(claims) == 0 {
		return 0, nil
	}

	var scope struct {
		campaignID string
		shedID     string
		shedLabel  string
		parkID     string
		operatorID string
	}
	if err := tx.QueryRow(ctx, reworkDigestBucketScopeSQL, params.TenantID, campaignShedID).Scan(
		&scope.campaignID, &scope.shedID, &scope.shedLabel, &scope.parkID, &scope.operatorID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The bucket vanished (campaign deleted). Nothing to route the push to; leaving the
			// rows un-stamped would spin this bucket forever, so stamp them and emit nothing.
			ids := make([]string, 0, len(claims))
			for _, c := range claims {
				ids = append(ids, c.observationID)
			}
			if _, err := tx.Exec(ctx, reworkDigestStampSQL, params.TenantID, ids); err != nil {
				return 0, err
			}
			return 0, tx.Commit(ctx)
		}
		return 0, fmt.Errorf("weighing rework digest sweep: resolve bucket scope: %w", err)
	}

	ids := make([]string, 0, len(claims))
	items := make([]domain.ReworkDigestItem, 0, params.NamedLimit)
	reason := ""
	newest := time.Time{}
	for i, c := range claims {
		ids = append(ids, c.observationID)
		if i < params.NamedLimit {
			items = append(items, domain.ReworkDigestItem{
				ObservationID:     c.observationID,
				ScannedIdentifier: c.identifier,
				WeightKg:          c.weightKg,
			})
		}
		// One shared reason is worth carrying ("blurred video"); several different ones would
		// make a single sentence lie, so the digest then carries none and the operator opens
		// the shed to see each row's own reason.
		if trimmed := strings.TrimSpace(c.reason); trimmed != "" {
			if reason == "" {
				reason = trimmed
			} else if reason != trimmed {
				reason = "\x00mixed"
			}
		}
		if c.verifiedAt.After(newest) {
			newest = c.verifiedAt
		}
	}
	if reason == "\x00mixed" {
		reason = ""
	}

	if _, err := tx.Exec(ctx, reworkDigestStampSQL, params.TenantID, ids); err != nil {
		return 0, fmt.Errorf("weighing rework digest sweep: stamp bucket %s: %w", campaignShedID, err)
	}

	payload := domain.ReworkDigestPayload{
		TenantID:       params.TenantID,
		CampaignID:     scope.campaignID,
		CampaignShedID: campaignShedID,
		ParkID:         scope.parkID,
		ShedID:         scope.shedID,
		ShedLabel:      scope.shedLabel,
		OperatorID:     scope.operatorID,
		Items:          items,
		TotalCount:     len(claims),
		Reason:         reason,
		// India business time: every Goat OS business meaning derives from Asia/Kolkata.
		DecidedAt: newest.In(biztime.DefaultLocation()).Format(time.RFC3339),
	}
	// Keyed on the EXACT set this flush named, so a retried tick that re-derives the same set
	// collapses onto one event and a later, different set gets its own.
	idem := eventTypeObservationReworkDigest + ":" + campaignShedID + ":" + strings.Join(ids, ",")
	// Outbox aggregate is the CAMPAIGN, not the bucket: the shared outbox validator
	// (migration 000004) resolves a 'weighing' aggregate id against campaigns/observations/
	// shed-observations only, exactly as the campaign-level cadence events already do. The
	// bucket this digest is about travels in the payload, where the consumer reads it.
	if err := r.enqueue(ctx, tx, params.TenantID, eventTypeObservationReworkDigest, scope.campaignID, idem, "", payload); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(claims), nil
}

func normalizeReworkDigestParams(params domain.ReworkDigestSweepParams) domain.ReworkDigestSweepParams {
	params.TenantID = strings.TrimSpace(params.TenantID)
	if params.AsOf.IsZero() {
		params.AsOf = time.Now()
	}
	if params.QuietWindow <= 0 {
		params.QuietWindow = defaultReworkDigestQuietWindow
	}
	if params.MaxAge <= 0 || params.MaxAge < params.QuietWindow {
		params.MaxAge = defaultReworkDigestMaxAge
	}
	if params.NamedLimit < 1 {
		params.NamedLimit = defaultReworkDigestNamedLimit
	}
	if params.ChunkSize < 1 {
		params.ChunkSize = defaultReworkDigestChunkSize
	}
	if params.MaxChunks < 1 {
		params.MaxChunks = defaultReworkDigestMaxChunks
	}
	return params
}
