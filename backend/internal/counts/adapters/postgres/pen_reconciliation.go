package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

// PEN RECONCILIATION (maintainer decision 2026-09-02). The herd register is TRUTH; a weighing
// submit that finds a scanned animal in a pen the register disagrees with raises one card, the
// operator physically returns the animal with a mandatory video, and the tenant verifier
// reviews it. No approver gate exists, and nothing here ever writes the register.
//
// The raise READS weighing source rows (weighing_observations, weighing_campaign_sheds)
// outward-only, the recorded kernel-consumer pattern (growthdirector and herdsignals already
// read them the same way). Weighing itself knows nothing about this table, gates no scan on
// it, and stays free-flow: a tag that resolves to no live animal simply raises no card.
//
// State machine:
//
//	open ──complete(video)──► pending_verification ──approve──► completed   (terminal)
//	 ▲                              │
//	 └───────── (re-shoot) ◄── rework ◄──reject─┘
//
// One open card per animal is the pen_reconciliation_cards_one_open_goat_uidx partial unique
// index; it is also what makes the event-driven raise idempotent across bus deliveries.

// The partition comparison below uses the shared scrubbed matching key, the exact idiom
// weight_demographics.go and sex_scope.go use: strip a Part/Pt prefix, lower, trim, so the
// register's "Part 1" and a bucket's "1" compare equal. 'whole' and blank both mean "no
// partition" and are normalized to ” before the scrub is applied.
const raisePenReconciliationSQL = `
-- projection-review: membership=one row per DISTINCT scanned tag in the submitted individual
-- bucket that resolves to a live animal whose canonical registered pen differs from the
-- bucket's canonical pen and that has no non-completed card; group_key=(tenant_id, goat_id)
-- via the DISTINCT ON tag -> ident DISTINCT ON tag joins, both 0..1 per tag so the join is
-- 1:1 at the tag grain; join_cardinality=goats 0..1 (PK), goat_shed_partitions 0..1 (PK
-- tenant,goat), canon_bucket exactly 1 row; pagination=NONE (bounded by one bucket's distinct
-- tags); scope=tenant_id + campaign_shed_id.
WITH bucket AS (
  SELECT cs.tenant_id, cs.campaign_id, cs.campaign_shed_id, cs.park_id,
         cs.location_id, COALESCE(cs.partition_label, '') AS partition_label,
         cs.display_name
  FROM weighing_campaign_sheds cs
  WHERE cs.tenant_id = $1::uuid
    AND cs.campaign_shed_id = $2::uuid
    AND cs.weighing_category = 'individual_animal'
),
-- Canonical FOUND pen. The bucket may point at a legacy ALIAS location row ("Castro 1" as its
-- own location) while the register puts animals on the physical shed ("Castro" + partition
-- "1"); comparing raw ids would then flag every animal in the pen. Same resolution idiom as
-- weight_demographics.go shed_targets: a location that itself holds live animals IS the
-- physical shed; otherwise resolve the sibling physical shed by scrubbing the trailing
-- partition digits off the name. The partition is the bucket's own label, else the digits the
-- alias name carries.
canon_bucket AS (
  SELECT x.tenant_id, x.campaign_id, x.campaign_shed_id, x.park_id, x.location_id,
         x.display_name,
         CASE WHEN x.is_physical THEN x.location_id
              ELSE COALESCE(x.alias_physical_id, x.location_id) END AS found_shed_id,
         -- The trailing digits of the location NAME are a partition ONLY when the bucket
         -- points at a resolved ALIAS row ("Castro 1" as its own location). An undivided
         -- physical shed whose name ends in a number ("Ho Chi Minh 1") is NEVER split — the
         -- digit is part of the name, so the bucket's own partition_label is the only source.
         CASE WHEN NOT x.is_physical AND x.alias_physical_id IS NOT NULL
              THEN COALESCE(NULLIF(x.partition_label, ''), x.name_digits, '')
              ELSE COALESCE(NULLIF(x.partition_label, ''), '') END AS found_partition
  FROM (
    SELECT b.*,
           EXISTS (SELECT 1 FROM goats gg WHERE gg.tenant_id = b.tenant_id
                    AND gg.lifecycle_status = 'alive' AND gg.shed_id = b.location_id) AS is_physical,
           (SELECT phys.location_id FROM locations phys
              JOIN locations l ON l.location_id = b.location_id AND l.tenant_id = b.tenant_id
             WHERE phys.tenant_id = l.tenant_id
               AND phys.parent_location_id = l.parent_location_id
               AND phys.location_type = 'shed'
               AND phys.location_id <> b.location_id
               AND phys.name = regexp_replace(l.name, '\s*(-\s*)?(Part\s*)?[0-9]+$', '')
             LIMIT 1) AS alias_physical_id,
           NULLIF((regexp_match((SELECT l.name FROM locations l
                                 WHERE l.location_id = b.location_id AND l.tenant_id = b.tenant_id),
                                '\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1], '') AS name_digits
    FROM bucket b
  ) x
),
tags AS (
  SELECT DISTINCT ON (lower(btrim(o.scanned_identifier)))
         lower(btrim(o.scanned_identifier)) AS tag,
         btrim(o.scanned_identifier) AS raw_tag
  FROM weighing_observations o
  WHERE o.tenant_id = $1::uuid AND o.campaign_shed_id = $2::uuid
  ORDER BY lower(btrim(o.scanned_identifier)), o.accepted_at DESC
),
ident AS (
  SELECT DISTINCT ON (lower(btrim(gi.identifier_value)))
         lower(btrim(gi.identifier_value)) AS tag, gi.goat_id
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid
  ORDER BY lower(btrim(gi.identifier_value)), gi.created_at DESC
),
resolved AS (
  SELECT t.raw_tag, g.goat_id,
         COALESCE(gsp.shed_id, g.shed_id) AS reg_shed_id,
         CASE WHEN gsp.partition_label IS NULL
                OR lower(btrim(gsp.partition_label)) IN ('', 'whole')
              THEN '' ELSE btrim(gsp.partition_label) END AS reg_partition
  FROM tags t
  JOIN ident i ON i.tag = t.tag
  JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = i.goat_id
    AND g.lifecycle_status = 'alive'
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
),
mismatch AS (
  SELECT r.raw_tag, r.goat_id, r.reg_shed_id, r.reg_partition,
         cb.found_shed_id, cb.found_partition, cb.display_name,
         cb.park_id, cb.campaign_id, cb.campaign_shed_id
  FROM resolved r
  CROSS JOIN canon_bucket cb
  WHERE r.reg_shed_id IS NOT NULL
    AND (r.reg_shed_id <> cb.found_shed_id
         OR regexp_replace(lower(btrim(r.reg_partition)), '^(part|pt)[\s.-]*', '')
            <> regexp_replace(lower(btrim(cb.found_partition)), '^(part|pt)[\s.-]*', ''))
)
INSERT INTO pen_reconciliation_cards (
  tenant_id, goat_id, scanned_identifier,
  found_location_id, found_partition_label, found_display_name,
  registered_shed_id, registered_partition_label,
  park_id, campaign_id, campaign_shed_id, raised_at
)
SELECT $1::uuid, m.goat_id, m.raw_tag,
       m.found_shed_id, m.found_partition, m.display_name,
       m.reg_shed_id, m.reg_partition,
       m.park_id, m.campaign_id, m.campaign_shed_id, $3::timestamptz
FROM mismatch m
ON CONFLICT (tenant_id, goat_id) WHERE status <> 'completed' DO NOTHING
`

// RaisePenReconciliationCards raises one card per mismatched live animal scanned in the named
// submitted individual weighing bucket. Idempotent: an animal already carrying a
// non-completed card conflicts on the partial unique index and inserts nothing, so duplicate
// event deliveries and re-submitted buckets cannot duplicate work.
func (r *Repository) RaisePenReconciliationCards(
	ctx context.Context, in domain.PenReconciliationRaiseCommand,
) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	raisedAt := in.RaisedAt
	if raisedAt.IsZero() {
		raisedAt = time.Now()
	}
	tag, err := r.pool.Exec(ctx, raisePenReconciliationSQL,
		in.TenantID, in.CampaignShedID, raisedAt.UTC())
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

const listPenReconciliationSQL = `
-- projection-review: membership=one row per pen_reconciliation_card in the tenant matching
-- the selected status bucket, keyset-bounded; group_key=card_id (PK, no grouping);
-- join_cardinality=goats 0..1 (PK), reg_shed/park locations 0..1 each (PK) so the join fans
-- nothing out; pagination=keyset (raised_at DESC, card_id DESC) LIMIT $5+1; scope=tenant_id.
SELECT c.card_id, c.status, c.goat_id, COALESCE(g.display_id, ''), c.scanned_identifier,
       c.found_location_id, c.found_partition_label, c.found_display_name,
       c.registered_shed_id, COALESCE(reg.name, ''), c.registered_partition_label,
       c.park_id, park.name,
       c.campaign_id, c.campaign_shed_id, c.raised_at,
       c.proof_ref, c.completed_by, c.completed_at,
       c.verified_by, c.verified_at, c.rework_reason
FROM pen_reconciliation_cards c
LEFT JOIN goats g ON g.tenant_id = c.tenant_id AND g.goat_id = c.goat_id
LEFT JOIN locations reg ON reg.tenant_id = c.tenant_id AND reg.location_id = c.registered_shed_id
LEFT JOIN locations park ON park.tenant_id = c.tenant_id AND park.location_id = c.park_id
WHERE c.tenant_id = $1::uuid
  AND ($2::text = 'all' OR c.status = $2::text)
  AND ($3::timestamptz IS NULL OR (c.raised_at, c.card_id) < ($3::timestamptz, $4::uuid))
ORDER BY c.raised_at DESC, c.card_id DESC
LIMIT $5
`

const penReconciliationStatusCountsSQL = `
-- projection-review: membership=every pen_reconciliation_card in the tenant; group_key=NONE
-- (whole-filter aggregate, one output row); join_cardinality=no joins, each card counted
-- exactly once per FILTER arm and the arms are disjoint on status; pagination=NONE (summary
-- is whole-filter truth, never page-local); scope=tenant_id.
SELECT count(*) AS all_count,
       count(*) FILTER (WHERE status = 'open') AS open_count,
       count(*) FILTER (WHERE status = 'pending_verification') AS submitted_count,
       count(*) FILTER (WHERE status = 'rework') AS rework_count,
       count(*) FILTER (WHERE status = 'completed') AS completed_count
FROM pen_reconciliation_cards
WHERE tenant_id = $1::uuid
`

// ListPenReconciliationCards returns one keyset page of the Reconcile queue plus whole-filter
// status counts.
func (r *Repository) ListPenReconciliationCards(
	ctx context.Context, q domain.PenReconciliationQuery,
) (domain.PenReconciliationPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	pageSize := q.PageSize
	if pageSize < 1 || pageSize > domain.MaxPenReconciliationPageSize {
		pageSize = domain.MaxPenReconciliationPageSize
	}
	status := strings.TrimSpace(q.Status)
	if status == "" {
		status = domain.PenReconciliationBucketAll
	}

	var cursorAt *time.Time
	var cursorID *string
	if q.Cursor != nil {
		at := q.Cursor.RaisedAt.UTC()
		cursorAt, cursorID = &at, &q.Cursor.CardID
	}

	rows, err := r.pool.Query(ctx, listPenReconciliationSQL,
		q.TenantID, status, cursorAt, cursorID, pageSize+1)
	if err != nil {
		return domain.PenReconciliationPage{}, err
	}
	defer rows.Close()

	items := make([]domain.PenReconciliationCard, 0, pageSize)
	for rows.Next() {
		var card domain.PenReconciliationCard
		if err := rows.Scan(
			&card.CardID, &card.Status, &card.GoatID, &card.GoatDisplayID, &card.ScannedIdentifier,
			&card.FoundLocationID, &card.FoundPartitionLabel, &card.FoundDisplayName,
			&card.RegisteredShedID, &card.RegisteredShedName, &card.RegisteredPartitionLabel,
			&card.ParkID, &card.ParkName,
			&card.CampaignID, &card.CampaignShedID, &card.RaisedAt,
			&card.ProofRef, &card.CompletedBy, &card.CompletedAt,
			&card.VerifiedBy, &card.VerifiedAt, &card.ReworkReason,
		); err != nil {
			return domain.PenReconciliationPage{}, err
		}
		card.PrimaryActionKey = penReconciliationPrimaryAction(card.Status)
		items = append(items, card)
	}
	if err := rows.Err(); err != nil {
		return domain.PenReconciliationPage{}, err
	}

	page := domain.PenReconciliationPage{}
	if len(items) > pageSize {
		items = items[:pageSize]
		last := items[len(items)-1]
		next, err := domain.EncodePenReconciliationCursor(domain.PenReconciliationCursor{
			RaisedAt: last.RaisedAt, CardID: last.CardID,
		})
		if err != nil {
			return domain.PenReconciliationPage{}, err
		}
		page.NextCursor = next
	}
	page.Items = items

	if err := r.pool.QueryRow(ctx, penReconciliationStatusCountsSQL, q.TenantID).Scan(
		&page.StatusCounts.All, &page.StatusCounts.Open, &page.StatusCounts.Submitted,
		&page.StatusCounts.Rework, &page.StatusCounts.Completed,
	); err != nil {
		return domain.PenReconciliationPage{}, err
	}
	return page, nil
}

// penReconciliationPrimaryAction is backend-owned row behavior: only a card the operator can
// act on offers the execute action.
func penReconciliationPrimaryAction(status string) string {
	switch status {
	case domain.PenReconciliationStatusOpen, domain.PenReconciliationStatusRework:
		return "execute"
	}
	return "none"
}

// ---------------------------------------------------------------------------
// Complete
// ---------------------------------------------------------------------------

const completePenReconciliationSQL = `
UPDATE pen_reconciliation_cards
SET status = 'pending_verification',
    proof_ref = $3,
    completed_by = $4::uuid,
    completed_at = $5::timestamptz,
    completion_idempotency_key = $6,
    completion_request_fingerprint = $7,
    rework_reason = NULL,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid AND card_id = $2::uuid
`

// applyPenReconciliationSQL / bouncePenReconciliationSQL are the two verdict writes. Both are
// gated on status = 'pending_verification', which is what makes a redelivered verdict event a
// no-op and keeps a verdict from force-completing a card in any other state.
const applyPenReconciliationSQL = `
UPDATE pen_reconciliation_cards
SET status = 'completed',
    verified_by = $3::uuid,
    verified_at = $4::timestamptz,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid AND card_id = $2::uuid
  AND status = 'pending_verification'
`

const bouncePenReconciliationSQL = `
UPDATE pen_reconciliation_cards
SET status = 'rework',
    rework_reason = NULLIF($5, ''),
    verified_by = $3::uuid,
    verified_at = $4::timestamptz,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid AND card_id = $2::uuid
  AND status = 'pending_verification'
`

const lockPenReconciliationSQL = `
SELECT c.status, c.goat_id, c.scanned_identifier, c.found_display_name,
       c.registered_shed_id, COALESCE(reg.name, ''), c.registered_partition_label,
       c.park_id, c.proof_ref, c.completed_at,
       c.completion_idempotency_key, c.completion_request_fingerprint
FROM pen_reconciliation_cards c
LEFT JOIN locations reg ON reg.tenant_id = c.tenant_id AND reg.location_id = c.registered_shed_id
WHERE c.tenant_id = $1::uuid AND c.card_id = $2::uuid
FOR UPDATE OF c
`

// CompletePenReconciliationCard stores the operator's mandatory return video and flips the
// card open/rework -> pending_verification.
//
// IDEMPOTENCY. A phone in a park WILL retry this: a card already submitted answers an exact
// same-key replay with the original result (replay=true) and writes nothing; a same-key
// different-fingerprint replay is ErrIdempotencyConflict; a different key against an already
// submitted/completed card is ErrPenReconciliationNotActionable.
func (r *Repository) CompletePenReconciliationCard(
	ctx context.Context, in domain.PenReconciliationCompletionCommand,
) (domain.PenReconciliationCompletionResult, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if strings.TrimSpace(in.ProofRef) == "" {
		return domain.PenReconciliationCompletionResult{}, false, ports.ErrPenReconciliationProofRequired
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.PenReconciliationCompletionResult{}, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var (
		status, goatID, tag, foundDisplay, regShedID, regShedName, regPartition string
		parkID, storedProof, storedKey, storedFingerprint                       *string
		completedAt                                                             *time.Time
	)
	err = tx.QueryRow(ctx, lockPenReconciliationSQL, in.TenantID, in.CardID).Scan(
		&status, &goatID, &tag, &foundDisplay,
		&regShedID, &regShedName, &regPartition,
		&parkID, &storedProof, &completedAt,
		&storedKey, &storedFingerprint,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PenReconciliationCompletionResult{}, false, ports.ErrPenReconciliationCardNotFound
	}
	if err != nil {
		return domain.PenReconciliationCompletionResult{}, false, err
	}

	result := domain.PenReconciliationCompletionResult{
		CardID:                   in.CardID,
		GoatID:                   goatID,
		ScannedIdentifier:        tag,
		FoundDisplayName:         foundDisplay,
		RegisteredShedID:         regShedID,
		RegisteredShedName:       regShedName,
		RegisteredPartitionLabel: regPartition,
		ParkID:                   parkID,
	}

	if status == domain.PenReconciliationStatusPendingVerification ||
		status == domain.PenReconciliationStatusCompleted {
		if storedKey != nil && *storedKey == in.IdempotencyKey {
			if storedFingerprint == nil || *storedFingerprint != in.RequestFingerprint {
				return domain.PenReconciliationCompletionResult{}, false, ports.ErrIdempotencyConflict
			}
			result.Status = status
			if storedProof != nil {
				result.ProofRef = *storedProof
			}
			result.CompletedAt = completedAt
			committed = true
			if err := tx.Commit(ctx); err != nil {
				return domain.PenReconciliationCompletionResult{}, false, err
			}
			return result, true, nil
		}
		return domain.PenReconciliationCompletionResult{}, false, ports.ErrPenReconciliationNotActionable
	}
	if status != domain.PenReconciliationStatusOpen && status != domain.PenReconciliationStatusRework {
		return domain.PenReconciliationCompletionResult{}, false, ports.ErrPenReconciliationNotActionable
	}

	completedAtValue := in.CompletedAt.UTC()
	if _, err := tx.Exec(ctx, completePenReconciliationSQL,
		in.TenantID, in.CardID, strings.TrimSpace(in.ProofRef), in.CompletedByUserID,
		completedAtValue, in.IdempotencyKey, in.RequestFingerprint); err != nil {
		return domain.PenReconciliationCompletionResult{}, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.PenReconciliationCompletionResult{}, false, err
	}
	committed = true

	result.Status = domain.PenReconciliationStatusPendingVerification
	result.ProofRef = strings.TrimSpace(in.ProofRef)
	result.CompletedAt = &completedAtValue
	return result, false, nil
}

// ---------------------------------------------------------------------------
// Verdicts
// ---------------------------------------------------------------------------

// ApplyVerifiedPenReconciliation flips a submitted card to completed on verifier approve.
// Idempotent: a card already completed (a redelivered verdict event) changes nothing, and a
// card in any other state is left for its own workflow rather than force-completed.
func (r *Repository) ApplyVerifiedPenReconciliation(
	ctx context.Context, in domain.PenReconciliationVerdictCommand,
) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	_, err := r.pool.Exec(ctx, applyPenReconciliationSQL,
		in.TenantID, in.CardID, nullableUUID(in.VerifiedBy), in.VerifiedAt.UTC())
	return err
}

// BouncePenReconciliationForRework flips a submitted card back to rework on verifier reject,
// recording the reason so the phone can render why. Idempotent for redelivered events.
func (r *Repository) BouncePenReconciliationForRework(
	ctx context.Context, in domain.PenReconciliationVerdictCommand,
) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	_, err := r.pool.Exec(ctx, bouncePenReconciliationSQL,
		in.TenantID, in.CardID, nullableUUID(in.VerifiedBy), in.VerifiedAt.UTC(),
		strings.TrimSpace(in.Reason))
	return err
}

// nullableUUID maps an absent actor to NULL rather than an empty string the uuid cast rejects.
func nullableUUID(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
