package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// Feed PACKING verification gate write path (maintainer decision, 2026-07-26, SUPERSEDING the "packing
// stays instant" rule). This file owns the module's THIRD write path -- the NEW feed_packing_completions
// table -- entirely separate from completions.go (the old instant feed_direction_session_completions
// path, now inert) and distribution_completions.go (feed_distribution_completions). A packing completion
// is a two-phase, verifier-gated fact requiring ONE mandatory video:
//
//	CompletePacking        -> writes 'pending_verification' with the video (NOTHING completed)
//	ApplyVerifiedPacking   -> verifier approved: 'pending_verification' -> 'completed'; emits
//	                          feed.packing.completed (the ONLY producer of that event)
//	BouncePackingForRework -> verifier rejected: 'pending_verification' -> 'rework'
//
// It is registered in context/architecture/domain-event-registry.json; the literal event type below
// plus the outbox_messages INSERT are what the domain-event-architecture guard matches against.
const (
	feedPackingCompletedEventType     = "feed.packing.completed"
	feedPackingCompletedSchemaVersion = "1.0.0"
	feedPackingCompletedSchemaRef     = "domain-event-envelope.v1"
	feedPackingCompletedTopic         = "feed.events"
	feedPackingCompletedAggregateType = "feed_packing_completion"
	feedPackingIdemScope              = "feed.packing.complete"
	feedPackingResourceType           = "feed_packing_completion"

	feedPackingPendingAction   = "feed.packing.pending_verification"
	feedPackingReworkAction    = "feed.packing.rework"
	feedPackingCompletedAction = "feed.packing.completed"
	// feedPackingReopenedAction distinguishes a pen reopened by the afternoon feed correction from
	// one a verifier rejected. Both land in 'rework'; only the audit says which, and an operator
	// asking "why am I packing this again" is answered by the reason on the row, not by the state.
	feedPackingReopenedAction = "feed.packing.reopened_for_feed_change"
)

var _ ports.PackingCompletionStore = (*Repository)(nil)

// CompletePacking records the operator's mandatory packing video at 'pending_verification' (or moves a
// 'rework' row back to it on a re-submit). Canonical write + audit + idempotency reservation are one
// transaction; idempotent on both the request key and the shed-session natural key. It does NOT emit
// feed.packing.completed -- that fires only when a verifier approves (ApplyVerifiedPacking).
func (r *Repository) CompletePacking(ctx context.Context, p ports.CompletePackingParams) (ports.CompletePackingResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	packingProof := strings.TrimSpace(p.PackingProofRef)
	targetDate := p.TargetDate.Format("2006-01-02")

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: begin packing completion tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	shedID, partitionLabel, err := canonicalizeFeedCompletionParams(ctx, tx, p.TenantID, p.ParkID, p.ShedID, p.PartitionLabel)
	if err != nil {
		return ports.CompletePackingResult{}, err
	}
	p.ShedID = shedID
	p.PartitionLabel = partitionLabel

	// The shed's display NAME, for the verification item's subject label.
	//
	// CompletePackingResult has carried ShedName/PartitionLabel since the packing gate was written and
	// NOTHING EVER FILLED THEM, so every packing item reached the verifier with an EMPTY subject
	// label -- a card in the review queue naming no location at all. The field existed, the enqueue
	// read it, and the value was always "": the exact declared-but-never-populated shape AGENTS.md
	// bans, and invisible to any test that checks whether a field is carried rather than what it says.
	//
	// Only the NAME is taken from the canonical resolver. Its partition half is agree-or-go-bare at
	// SHED grain, which is right for a caller that knows only a shed and wrong here: this completion
	// is for ONE named pen, and Castro (three pens) would resolve bare and lose it. The caller's own
	// partition is the honest value.
	shedLocation, err := oploc.ResolveShedLocation(ctx, tx.QueryRow(ctx, oploc.ShedScopedLocationSQL, p.TenantID, p.ShedID))
	if err != nil {
		return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: resolve packing shed location: %w", err)
	}
	shedName := shedLocation.ShedName

	fingerprint := requestFingerprint(
		p.ParkID,
		p.ShedID,
		domain.PartitionMatchKey(p.PartitionLabel),
		// The SESSION is part of the fingerprint, as it is part of the natural key. Without it a
		// same-key resend that switched session would read as an exact replay and be answered with the
		// other session's result.
		fmt.Sprintf("%d", p.SessionNo),
		targetDate,
		p.Workflow,
		packingProof,
	)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, feedPackingIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.CompletePackingResult{}, err
	}
	if !reservation.proceed {
		// Exact replay of the same request: return the original result, run NO side effects. Read the row
		// so the caller still sees its current status/row_version, but NewlyPending stays false so no
		// verification item is re-enqueued.
		status, rowVersion, readErr := r.readPackingByID(ctx, tx, p.TenantID, reservation.resultID)
		if readErr != nil {
			return ports.CompletePackingResult{}, readErr
		}
		if err := tx.Commit(ctx); err != nil {
			return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: commit idempotent packing replay: %w", err)
		}
		committed = true
		// The location travels on the replay path too. It costs nothing and keeps every return from
		// this method the same shape, so a future caller cannot find it populated on one path and
		// blank on another.
		return ports.CompletePackingResult{
			CompletionID: reservation.resultID, Status: status, RowVersion: rowVersion, NewlyPending: false,
			ShedID: p.ShedID, ShedName: shedName, PartitionLabel: p.PartitionLabel,
		}, nil
	}

	var (
		completionID string
		rowVersion   int32
		status       = domain.PackingStatusPendingVerification
		newlyPending bool
	)
	// session_no is a REAL session again (maintainer decision 2026-08-11, reverting the 2026-08-10
	// pen-day sentinel 0). It is back in the ON CONFLICT target, which is
	// feed_packing_completions_natural_uq -- the index that survived the pen-day release because its
	// contract half was never shipped.
	err = tx.QueryRow(ctx, `
INSERT INTO feed_packing_completions (
  tenant_id, park_id, shed_id, partition_label, session_no, target_date, workflow, status,
  packing_proof_ref, completed_by, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, nullif($4::text, ''), $5, $6::date, $7, 'pending_verification',
  $8, nullif($9::text, '')::uuid, $10
)
ON CONFLICT (tenant_id, park_id, shed_id, partition_key, session_no, target_date, workflow) DO NOTHING
RETURNING completion_id::text, row_version`,
		p.TenantID, p.ParkID, p.ShedID, p.PartitionLabel, p.SessionNo, targetDate, p.Workflow,
		packingProof, p.CompletedBy, p.IdempotencyKey).Scan(&completionID, &rowVersion)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Natural-key conflict: a row for this SHED-SESSION already exists. Its state AND its stored
		// video decide the outcome -- the proof_ref is read because a second, DIFFERENT video is a
		// conflict, not a replay. See ports.ErrPackingAlreadyRecorded.
		var existingStatus, existingProof string
		if err := tx.QueryRow(ctx, `
SELECT completion_id::text, status, row_version, coalesce(packing_proof_ref, '')
FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
  AND partition_key = $6 AND session_no = $7 AND target_date = $4::date AND workflow = $5`,
			p.TenantID, p.ParkID, p.ShedID, targetDate, p.Workflow,
			domain.PartitionMatchKey(p.PartitionLabel), p.SessionNo).
			Scan(&completionID, &existingStatus, &rowVersion, &existingProof); err != nil {
			return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: read existing packing completion: %w", err)
		}
		switch existingStatus {
		case domain.PackingStatusRework:
			// A verifier rejected the prior video; the operator re-recorded. Move the row back to
			// pending_verification with the NEW video, bump row_version, clear the rework reason. This is a
			// fresh pending transition, so it enqueues a fresh verification item.
			if err := tx.QueryRow(ctx, `
UPDATE feed_packing_completions
SET status = 'pending_verification',
    packing_proof_ref = $3,
    rework_reason = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'rework'
RETURNING row_version`,
				p.TenantID, completionID, packingProof).Scan(&rowVersion); err != nil {
				return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: resubmit packing for verification: %w", err)
			}
			status = domain.PackingStatusPendingVerification
			newlyPending = true
			if err := writePackingAudit(ctx, tx, p, completionID, feedPackingPendingAction); err != nil {
				return ports.CompletePackingResult{}, err
			}
		case domain.PackingStatusPendingVerification, domain.PackingStatusCompleted:
			// The shed-session already holds a video. Whether this is an idempotent no-op or a CONFLICT
			// depends entirely on whether it is the SAME video.
			//
			// SAME proof -> a genuine re-send under a different idempotency key. Nothing to do, and
			// answering success is correct: the operator's recording IS on the row.
			//
			// DIFFERENT proof -> a second, distinct recording for a unit that accepts exactly one. It
			// cannot be stored, so it must not be acknowledged. Returning success here told the
			// operator their video was accepted while nothing recorded it and no verifier ever saw
			// it -- silent loss of work they had physically done.
			if packingProof != existingProof {
				return ports.CompletePackingResult{}, ports.ErrPackingAlreadyRecorded
			}
			status = existingStatus
		default:
			return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: unexpected packing status %q", existingStatus)
		}
	case err != nil:
		return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: insert packing completion: %w", err)
	default:
		// Fresh insert: a brand-new pending_verification row.
		newlyPending = true
		if err := writePackingAudit(ctx, tx, p, completionID, feedPackingPendingAction); err != nil {
			return ports.CompletePackingResult{}, err
		}
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, feedPackingIdemScope, p.IdempotencyKey, feedPackingResourceType, completionID); err != nil {
		return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: complete packing idempotency: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: commit packing completion: %w", err)
	}
	committed = true
	return ports.CompletePackingResult{
		CompletionID: completionID, Status: status, RowVersion: rowVersion, NewlyPending: newlyPending,
		// Carried on EVERY path, including the idempotent replay above: the enqueue reads these to
		// compose the verifier's subject label, and a path that leaves them blank ships an item
		// naming no location.
		ShedID: p.ShedID, ShedName: shedName, PartitionLabel: p.PartitionLabel,
	}, nil
}

// readPackingByID reads a row's status and row_version within the transaction, for the idempotent
// replay echo.
func (r *Repository) readPackingByID(ctx context.Context, tx pgx.Tx, tenantID, completionID string) (string, int32, error) {
	if strings.TrimSpace(completionID) == "" {
		return "", 0, nil
	}
	var status string
	var rowVersion int32
	err := tx.QueryRow(ctx, `
SELECT status, row_version
FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, tenantID, completionID).Scan(&status, &rowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("feeddirection: read packing completion by id: %w", err)
	}
	return status, rowVersion, nil
}

// ListVerifiedPacking returns every VERIFIED (status='completed') (shed, partition, session,
// workflow) for one park-day in one bounded indexed read.
func (r *Repository) ListVerifiedPacking(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]ports.VerifiedPacking, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded read of ONE park-day's VERIFIED packing lines, covered by feed_packing_completions_serving_idx (tenant_id, park_id, target_date, workflow). Bounded by the park's pen catalog x sessions (physical infrastructure), never by herd size; binds are cast, indexed columns stay bare.
	rows, err := r.pool.Query(ctx, `
SELECT shed_id::text, coalesce(partition_label, ''), session_no, workflow
FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND target_date = $3::date AND status = 'completed'`,
		tenantID, parkID, targetDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("feeddirection: list verified packing: %w", err)
	}
	defer rows.Close()
	out := make([]ports.VerifiedPacking, 0)
	for rows.Next() {
		var d ports.VerifiedPacking
		if err := rows.Scan(&d.ShedID, &d.PartitionLabel, &d.SessionNo, &d.Workflow); err != nil {
			return nil, fmt.Errorf("feeddirection: scan verified packing: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListPackingCompletionStatuses returns EVERY (shed, partition, session, workflow) with a
// feed_packing_completions row for one park-day plus its RAW status -- the packing serve path's
// status overlay + filter source (includes pending_verification and rework, not just completed).
//
// One row per SHED-SESSION, which is what the overlay keys on. session_no MUST be selected and
// carried: keying the overlay on the pen alone would let the morning's completion mark the evening
// line packed too, which is the shape of the 2026-08-08 partition defect one column over.
func (r *Repository) ListPackingCompletionStatuses(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]ports.PackingCompletionStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded read of ONE park-day's packing line statuses, covered by feed_packing_completions_serving_idx (tenant_id, park_id, target_date, workflow). Bounded by the park's pen catalog x sessions (physical infrastructure), never by herd size; binds are cast, indexed columns stay bare.
	rows, err := r.pool.Query(ctx, `
SELECT shed_id::text, coalesce(partition_label, ''), session_no, workflow, status, coalesce(rework_reason, '')
FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND target_date = $3::date`,
		tenantID, parkID, targetDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("feeddirection: list packing completion statuses: %w", err)
	}
	defer rows.Close()
	out := make([]ports.PackingCompletionStatus, 0)
	for rows.Next() {
		var d ports.PackingCompletionStatus
		if err := rows.Scan(&d.ShedID, &d.PartitionLabel, &d.SessionNo, &d.Workflow, &d.Status, &d.ReworkReason); err != nil {
			return nil, fmt.Errorf("feeddirection: scan packing completion status: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ApplyVerifiedPacking flips a packing completion whose video a verifier APPROVED
// 'pending_verification' -> 'completed' and emits feed.packing.completed, in one transaction. It runs
// from the verification.verdict.approved consumer, never from the operator's phone.
//
// IDEMPOTENT: a re-delivered verdict on an already-'completed' row returns false with no side effects.
// A verdict for a row no longer 'pending_verification' (bounced to rework, or otherwise moved on) is
// ignored as stale (returns false).
func (r *Repository) ApplyVerifiedPacking(ctx context.Context, p ports.ApplyPackingParams) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("feeddirection: begin apply packing tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var (
		status     string
		parkID     string
		shedID     string
		workflow   string
		sessionNo  int32
		targetDate time.Time
	)
	err = tx.QueryRow(ctx, `
SELECT status, park_id::text, shed_id::text, workflow, session_no, target_date
FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid
FOR UPDATE`, p.TenantID, p.CompletionID).Scan(&status, &parkID, &shedID, &workflow, &sessionNo, &targetDate)
	if errors.Is(err, pgx.ErrNoRows) {
		// No such row for this tenant: a stale/foreign verdict. Ignore.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("feeddirection: lock packing completion: %w", err)
	}
	// Already completed (re-delivered verdict) or no longer pending (stale delivery): no side effects.
	if status != domain.PackingStatusPendingVerification {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}

	tag, err := tx.Exec(ctx, `
UPDATE feed_packing_completions
SET status = 'completed',
    verified_by = nullif($3::text, '')::uuid,
    verified_at = now(),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'pending_verification'`,
		p.TenantID, p.CompletionID, p.VerifiedBy)
	if err != nil {
		return false, fmt.Errorf("feeddirection: apply verified packing: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Lost the race to a concurrent transition; treat as stale.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}

	if err := insertFeedPackingCompletedOutbox(ctx, tx, feedPackingCompletedOutbox{
		TenantID:     p.TenantID,
		CompletionID: p.CompletionID,
		ParkID:       parkID,
		ShedID:       shedID,
		SessionNo:    sessionNo,
		TargetDate:   targetDate.Format("2006-01-02"),
		Workflow:     workflow,
		VerifiedBy:   strings.TrimSpace(p.VerifiedBy),
		TraceID:      p.TraceID,
	}); err != nil {
		return false, err
	}
	if err := writePackingVerdictAudit(ctx, tx, p.TenantID, shedID, parkID, p.CompletionID, feedPackingCompletedAction, p.VerifiedBy, p.TraceID); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("feeddirection: commit apply verified packing: %w", err)
	}
	committed = true
	return true, nil
}

// BouncePackingForRework flips a packing completion whose video a verifier REJECTED
// 'pending_verification' -> 'rework'. It runs from the verification.verdict.rework consumer. NOTHING is
// completed. Idempotent + stale-guarded: only a 'pending_verification' row is bounced.
func (r *Repository) BouncePackingForRework(ctx context.Context, p ports.BouncePackingParams) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tag, err := r.pool.Exec(ctx, `
UPDATE feed_packing_completions
SET status = 'rework',
    rework_reason = nullif($3, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'pending_verification'`,
		p.TenantID, p.CompletionID, strings.TrimSpace(p.Reason))
	if err != nil {
		return false, fmt.Errorf("feeddirection: bounce packing for rework: %w", err)
	}
	// Zero rows affected is an accepted stale/duplicate verdict; no error.
	return tag.RowsAffected() > 0, nil
}

// ReopenPackingForFeedChange moves every named pen's submitted packing back to 'rework' because the
// afternoon correction changed how many animals that pen feeds, and withdraws the verification items
// queued for the superseded videos. See ports.ReopenPackingForFeedChange for the rule.
//
// ALL OF A PEN'S SESSIONS MOVE. There is deliberately NO session predicate below: head count scales
// the morning and evening rations alike, so both videos now prove the wrong quantity. Adding
// `AND c.session_no = ...` here would leave one bag packed to a head count the farm no longer has --
// the failure this whole path exists to prevent, half-done.
//
// ONE set-based statement per step over the pens the amend diff named -- never a query per pen.
func (r *Repository) ReopenPackingForFeedChange(ctx context.Context, p ports.ReopenPackingParams) (ports.ReopenPackingResult, error) {
	if len(p.Pens) == 0 {
		return ports.ReopenPackingResult{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	shedIDs := make([]string, 0, len(p.Pens))
	partitionKeys := make([]string, 0, len(p.Pens))
	for _, pen := range p.Pens {
		shedIDs = append(shedIDs, pen.ShedID)
		// Normalized here as well as by the caller: this is the value that must line up with the
		// generated partition_key column, and a raw 'Part 3' would silently match nothing.
		partitionKeys = append(partitionKeys, domain.PartitionMatchKey(pen.PartitionKey))
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.ReopenPackingResult{}, fmt.Errorf("feeddirection: begin reopen packing tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// The pen list is unnested into a join rather than compared with a pair of parallel = ANY()
	// predicates. Two independent array predicates would match the CROSS PRODUCT of the sheds and
	// the partitions -- reopening Castro - 3 because Castro - 1 changed and Godel 1 Part 3 did.
	// UNNEST(...) WITH ORDINALITY-free positional pairing keeps each shed bound to its own pen.
	//
	// verified_by/verified_at are cleared: the row is no longer verified, and leaving the old
	// verifier stamped on it would credit them with approving a video for quantities they never saw.
	//
	// scale-guard:ignore: one set-based UPDATE over the pens named by a single park-day's amend diff (bounded by the park's pen catalog x sessions, never by herd size), covered by the leading columns of feed_packing_completions_natural_uq (tenant_id, park_id, shed_id, partition_key, session_no, target_date, workflow) -- session_no is left unconstrained on purpose so every session of a named pen is swept; binds carry the casts and the indexed columns stay bare.
	rows, err := tx.Query(ctx, `
UPDATE feed_packing_completions c
SET status = 'rework',
    rework_reason = nullif($6, ''),
    verified_by = NULL,
    verified_at = NULL,
    updated_at = now(),
    row_version = c.row_version + 1
FROM unnest($4::uuid[], $5::text[]) AS pen(shed_id, partition_key)
WHERE c.tenant_id = $1::uuid
  AND c.park_id = $2::uuid
  AND c.target_date = $3::date
  AND c.workflow = $7
  AND c.shed_id = pen.shed_id
  AND c.partition_key = pen.partition_key
  -- 'rework' is excluded: that row is already back with the operator, and touching it would bump
  -- row_version and overwrite a verifier's real rejection reason with this one.
  AND c.status IN ('pending_verification', 'completed')
RETURNING c.completion_id::text, c.shed_id::text`,
		p.TenantID, p.ParkID, p.TargetDate.Format("2006-01-02"),
		shedIDs, partitionKeys, strings.TrimSpace(p.Reason), p.Workflow)
	if err != nil {
		return ports.ReopenPackingResult{}, fmt.Errorf("feeddirection: reopen packing for feed change: %w", err)
	}
	type reopened struct{ completionID, shedID string }
	// Capacity is per PEN x its sessions, not per pen: a two-session park returns two rows for each
	// pen named. Sizing it at len(p.Pens) would reallocate on every correction.
	moved := make([]reopened, 0, len(p.Pens)*2)
	for rows.Next() {
		var m reopened
		if err := rows.Scan(&m.completionID, &m.shedID); err != nil {
			rows.Close()
			return ports.ReopenPackingResult{}, fmt.Errorf("feeddirection: scan reopened packing: %w", err)
		}
		moved = append(moved, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ports.ReopenPackingResult{}, fmt.Errorf("feeddirection: iterate reopened packing: %w", err)
	}
	if len(moved) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return ports.ReopenPackingResult{}, fmt.Errorf("feeddirection: commit empty reopen: %w", err)
		}
		committed = true
		return ports.ReopenPackingResult{}, nil
	}

	completionIDs := make([]string, 0, len(moved))
	for _, m := range moved {
		completionIDs = append(completionIDs, m.completionID)
	}

	// 'withdrawn', not DELETE -- the clip and its trail stay readable while the item leaves the
	// verifier's PENDING queue. Without this the verifier would still be holding a card for a video
	// of the old quantity, and approving it would flip the row straight back to 'completed' behind
	// the operator who is at that moment repacking the pen. Same mechanism as migration 000148 step 2.
	//
	// scale-guard:ignore: one set-based UPDATE over the completion ids just returned above (bounded by the park's pen catalog); source_ref_id is compared bare against a cast bind array.
	tag, err := tx.Exec(ctx, `
UPDATE verification_items
SET status = 'withdrawn',
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND source_module = 'feed'
  AND source_ref_type = 'feed_packing_completion'
  -- source_ref_id is a uuid column, so the bind array carries the uuid cast and the column stays
  -- bare: a ::text[] array raises "operator does not exist: uuid = text", and casting the COLUMN to
  -- text instead would compile while disabling its index (the non-sargable-cast anti-pattern).
  AND source_ref_id = ANY($2::uuid[])
  AND status = 'pending'`, p.TenantID, completionIDs)
	if err != nil {
		return ports.ReopenPackingResult{}, fmt.Errorf("feeddirection: withdraw superseded packing verification items: %w", err)
	}

	recorder := audit.NewTxRecorder(tx)
	for _, m := range moved {
		// scale-guard:ignore: the audit recorder buffers rows on the open transaction; this loop issues no query per iteration and is bounded by the pens of one park-day.
		if err := recorder.Record(ctx, audit.Event{
			TenantID: p.TenantID,
			// No ActorID: nobody pressed anything. audit_log.actor_id is a UUID, so the provenance
			// string the service could otherwise pass ("goatos-api") is not a shortened actor but a
			// type error that aborts this INSERT and rolls the whole reopen back -- leaving every pen
			// that should have gone back to its packer still marked done. ActorType carries the
			// system-ness; see ports.ReopenPackingParams for why the field does not exist.
			ActorType:    "system",
			Action:       feedPackingReopenedAction,
			ResourceType: feedPackingResourceType,
			ResourceID:   m.completionID,
			ScopeType:    "shed",
			ScopeID:      m.shedID,
			AfterState: map[string]any{
				"park_id":     p.ParkID,
				"shed_id":     m.shedID,
				"target_date": p.TargetDate.Format("2006-01-02"),
				"workflow":    p.Workflow,
				"status":      domain.PackingStatusRework,
				"reason":      strings.TrimSpace(p.Reason),
			},
			Metadata: map[string]any{"source": "feed-packing-shifting-correction"},
			TraceID:  p.TraceID,
		}); err != nil {
			return ports.ReopenPackingResult{}, fmt.Errorf("feeddirection: write packing reopen audit: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return ports.ReopenPackingResult{}, fmt.Errorf("feeddirection: commit reopen packing: %w", err)
	}
	committed = true
	return ports.ReopenPackingResult{
		ReopenedCompletionIDs: completionIDs,
		WithdrawnItemCount:    int(tag.RowsAffected()),
	}, nil
}

func writePackingAudit(ctx context.Context, tx pgx.Tx, p ports.CompletePackingParams, completionID, action string) error {
	actorType := strings.TrimSpace(p.ActorType)
	if actorType == "" {
		actorType = "operator"
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       action,
		ResourceType: feedPackingResourceType,
		ResourceID:   completionID,
		ScopeType:    "shed",
		ScopeID:      p.ShedID,
		AfterState: map[string]any{
			"park_id": p.ParkID,
			"shed_id": p.ShedID,
			// The PEN, which the natural key and the request fingerprint both use and the audit did
			// not record. With the session gone from the grain it is the only thing distinguishing
			// one Castro completion from another, so an audit row without it cannot say which pen
			// was packed.
			"partition_label": p.PartitionLabel,
			"target_date":     p.TargetDate.Format("2006-01-02"),
			"workflow":        p.Workflow,
			"status":          domain.PackingStatusPendingVerification,
		},
		Metadata: map[string]any{"source": "feed-packing-completion"},
		TraceID:  p.TraceID,
	}); err != nil {
		return fmt.Errorf("feeddirection: write packing audit: %w", err)
	}
	return nil
}

func writePackingVerdictAudit(ctx context.Context, tx pgx.Tx, tenantID, shedID, parkID, completionID, action, verifiedBy, traceID string) error {
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      strings.TrimSpace(verifiedBy),
		ActorType:    "verifier",
		Action:       action,
		ResourceType: feedPackingResourceType,
		ResourceID:   completionID,
		ScopeType:    "shed",
		ScopeID:      shedID,
		AfterState: map[string]any{
			"park_id": parkID,
			"shed_id": shedID,
			"status":  domain.PackingStatusCompleted,
		},
		Metadata: map[string]any{"source": "feed-packing-verification"},
		TraceID:  traceID,
	}); err != nil {
		return fmt.Errorf("feeddirection: write packing verdict audit: %w", err)
	}
	return nil
}

// feedPackingCompletedOutbox is the input for the feed.packing.completed outbox envelope.
type feedPackingCompletedOutbox struct {
	TenantID     string
	CompletionID string
	ParkID       string
	ShedID       string
	SessionNo    int32
	TargetDate   string
	Workflow     string
	VerifiedBy   string
	TraceID      string
}

func insertFeedPackingCompletedOutbox(ctx context.Context, tx pgx.Tx, o feedPackingCompletedOutbox) error {
	idempotencyKey := feedPackingCompletedEventType + ":" + o.CompletionID
	eventID := platformoutbox.DeterministicUUID(feedPackingCompletedEventType + ":" + o.TenantID + ":" + o.CompletionID)

	payload := map[string]any{
		"completion_id": o.CompletionID,
		"park_id":       o.ParkID,
		"shed_id":       o.ShedID,
		"session_no":    o.SessionNo,
		"target_date":   o.TargetDate,
		"workflow":      o.Workflow,
		"verified_by":   o.VerifiedBy,
	}
	envelope := feedEventEnvelope{
		EventID:        eventID,
		EventType:      feedPackingCompletedEventType,
		SchemaVersion:  feedPackingCompletedSchemaVersion,
		SchemaRef:      feedPackingCompletedSchemaRef,
		AggregateType:  feedPackingCompletedAggregateType,
		AggregateID:    o.CompletionID,
		IdempotencyKey: idempotencyKey,
		TenantID:       o.TenantID,
		ParkID:         o.ParkID,
		ShedID:         o.ShedID,
		// The verifier who approved the video is the actor; a blank one emits actor_type "system".
		ActorID: o.VerifiedBy,
		// The FEED DAY, not the moment the verdict landed: the business fact this event reports is
		// that a pen's feed for that day is packed and proved.
		OccurredAt: businessInstant(o.TargetDate),
		Payload:    payload,
		TraceID:    o.TraceID,
	}.build()
	envelopeJSON, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("feeddirection: marshal packing outbox envelope: %w", err)
	}
	headersJSON, err := json.Marshal(map[string]any{"content_type": "application/json"})
	if err != nil {
		return fmt.Errorf("feeddirection: marshal packing outbox headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, $11, 'pending', now()
)
ON CONFLICT DO NOTHING`,
		o.TenantID, eventID, feedPackingCompletedEventType, feedPackingCompletedSchemaVersion,
		feedPackingCompletedAggregateType, o.CompletionID, feedPackingCompletedTopic,
		envelopeJSON, headersJSON, idempotencyKey, o.TraceID)
	if err != nil {
		return fmt.Errorf("feeddirection: insert packing outbox: %w", err)
	}
	return nil
}
