// Package postgres implements the Verification module's persistence, including the outbox insert
// for the status-event seam (item pending / verdict approved / verdict rework), written in the SAME
// transaction as the state change (backend/AGENTS.md atomic transition + read-model/event rule).
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

const defaultQueryTimeout = 3 * time.Second

// Status-event types emitted on the existing outbox bus (build-handover-20260713.md §1 P0 1a — "the
// Max seam"). The notification producer session consumes these to fan out pushes; this module only
// publishes.
const (
	EventItemPending                  = "verification.item.pending"
	EventVerdictApproved              = "verification.verdict.approved"
	EventVerdictRework                = "verification.verdict.rework"
	EventItemClosed                   = "verification.item.closed"
	EventVaccinationDriveReady        = "verification.vaccination_drive.ready"
	EventVaccinationDriveClosed       = "verification.vaccination_drive.closed"
	verificationTopic                 = "verification"
	vaccinationCompletedEventType     = "vaccination.completed"
	vaccinationCompletedSchemaVersion = "1.0.0"
	vaccinationCompletedSchemaRef     = "domain-event-envelope.v1"
	vaccinationCompletedTopic         = "vaccination.events"
	// verificationSchemaVer must be semver to satisfy the domain-event-envelope schema
	// (schema_version pattern ^[0-9]+\.[0-9]+\.[0-9]+$) enforced by the outbox relay's
	// EnvelopeValidator before publish. A non-semver value (was "1") fails validation, so the
	// event is marked invalid_event_envelope and NEVER delivered to any consumer.
	verificationSchemaVer = "1.0.0"
	// verificationSchemaRef is the required schema_ref pointer for the envelope. Mirrors the
	// notification.exhausted producer's "<schema file>#<event_type>" convention.
	verificationSchemaRef = "contracts/jsonschema/domain-event-envelope.schema.json"
)

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

const itemColumns = `item_id::text, tenant_id::text, vertical, module, category, source_module,
  source_task_id::text, source_submission_id::text, source_ref_type, source_ref_id::text, subject_label, subject_note, media_refs, context_rows,
  status, verdict_reason, operator_id::text, shed_id::text, partition_label, park_id::text, captured_at, verified_by::text,
  verified_at, closed_by::text, closed_at, applier_ack_expected, applied_at, applied_by_module,
  row_version, created_at, updated_at`

const itemColumnsWithLabels = `vi.item_id::text, vi.tenant_id::text, vi.vertical, vi.module, vi.category, vi.source_module,
  vi.source_task_id::text, vi.source_submission_id::text, vi.source_ref_type, vi.source_ref_id::text, vi.subject_label, vi.subject_note, vi.media_refs, vi.context_rows,
  vi.status, vi.verdict_reason, vi.operator_id::text, vi.shed_id::text, vi.partition_label, vi.park_id::text, vi.captured_at, vi.verified_by::text,
  vi.verified_at, vi.closed_by::text, vi.closed_at, vi.applier_ack_expected, vi.applied_at, vi.applied_by_module,
  vi.row_version, vi.created_at, vi.updated_at,
  operator.display_name::text, verifier.display_name::text,
  shed_loc.name::text, park_loc.name::text`

func (r *Repository) CreateItem(ctx context.Context, in domain.CreateItem) (domain.CreateItemResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	mediaJSON, err := json.Marshal(nonNilStrings(in.MediaRefs))
	if err != nil {
		return domain.CreateItemResult{}, fmt.Errorf("verification: marshal media_refs: %w", err)
	}
	// Always an ARRAY, never null: the column's CHECK requires jsonb_typeof = 'array', and a nil
	// slice marshals to `null`. A producer attaching no context stores [] and reads back empty.
	contextJSON, err := json.Marshal(nonNilContextRows(in.ContextRows))
	if err != nil {
		return domain.CreateItemResult{}, fmt.Errorf("verification: marshal context_rows: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.CreateItemResult{}, err
	}
	defer rollback(ctx, tx)

	var itemID string
	err = tx.QueryRow(ctx, `
INSERT INTO verification_items (
  tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id,
  source_ref_type, source_ref_id, subject_label, subject_note, media_refs, context_rows, status, operator_id, shed_id, partition_label, park_id,
  captured_at, idempotency_key, applier_ack_expected
) VALUES (
  $1::uuid, $2, $3, $4, $5, nullif($6, '')::uuid, nullif($7, '')::uuid, $8, $9::uuid, nullif($10, ''),
  nullif($18, ''),
  $11::jsonb, $20::jsonb, 'pending', nullif($12, '')::uuid, nullif($13, '')::uuid, nullif($19, ''), nullif($14, '')::uuid, $15, $16, $17
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING item_id::text`,
		in.TenantID, in.Vertical, in.Module, in.Category, in.Source.Module,
		derefStr(in.Source.TaskID), derefStr(in.Source.SubmissionID), in.Source.RefType, in.Source.RefID,
		derefStr(in.SubjectLabel), string(mediaJSON), derefStr(in.OperatorID), derefStr(in.ShedID), derefStr(in.ParkID),
		in.CapturedAt.UTC(), in.IdempotencyKey, in.ApplierAckExpected, derefStr(in.SubjectNote),
		derefStr(in.PartitionLabel), string(contextJSON),
	).Scan(&itemID)
	created := true
	if errors.Is(err, pgx.ErrNoRows) {
		created = false
	} else if err != nil {
		return domain.CreateItemResult{}, mapWriteErr(err)
	}

	if created {
		if err := insertOutboxEvent(ctx, tx, in.TenantID, EventItemPending, itemID,
			"verification.item.pending:"+itemID, verificationItemPendingPayload(itemID, in)); err != nil {
			return domain.CreateItemResult{}, err
		}
	}

	var item domain.Item
	if created {
		item, err = scanItemRow(tx.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", in.TenantID, itemID))
	} else {
		item, err = scanItemRow(tx.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND idempotency_key = $2", in.TenantID, in.IdempotencyKey))
	}
	if err != nil {
		return domain.CreateItemResult{}, mapWriteErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.CreateItemResult{}, err
	}
	return domain.CreateItemResult{Item: item, Created: created}, nil
}

func (r *Repository) GetItem(ctx context.Context, tenantID, itemID string) (domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	item, err := scanItemRow(r.pool.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", tenantID, itemID))
	if err != nil {
		return domain.Item{}, mapWriteErr(err)
	}
	return item, nil
}

// GetItemCategories batch-resolves item_id -> category for every id in itemIDs in ONE query
// (= ANY($1)) -- see ports.Repository for why this exists instead of GetItem-in-a-loop.
func (r *Repository) GetItemCategories(ctx context.Context, tenantID string, itemIDs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(itemIDs) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx,
		`SELECT item_id::text, category FROM verification_items WHERE tenant_id = $1::uuid AND item_id = ANY($2::uuid[])`,
		tenantID, itemIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var itemID, category string
		if err := rows.Scan(&itemID, &category); err != nil {
			return nil, err
		}
		out[itemID] = category
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetItemProofRefs batch-resolves item_id -> its own proof ids (media_refs) in ONE query, so the
// telemetry write path can reject a proof_id that belongs to a different item. See ports.Repository.
func (r *Repository) GetItemProofRefs(ctx context.Context, tenantID string, itemIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(itemIDs) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx,
		`SELECT item_id::text, media_refs FROM verification_items WHERE tenant_id = $1::uuid AND item_id = ANY($2::uuid[])`,
		tenantID, itemIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var itemID string
		var raw []byte
		if err := rows.Scan(&itemID, &raw); err != nil {
			return nil, err
		}
		refs := []string{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &refs); err != nil {
				return nil, fmt.Errorf("verification: unmarshal media_refs for item %s: %w", itemID, err)
			}
		}
		out[itemID] = refs
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) GetSubmissionItems(ctx context.Context, tenantID, submissionID string) ([]domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT `+itemColumns+`
FROM verification_items
WHERE tenant_id = $1::uuid
  AND source_submission_id = $2::uuid
ORDER BY captured_at, item_id`, tenantID, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Item, 0, 20)
	for rows.Next() {
		item, scanErr := scanItemRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ports.ErrNotFound
	}
	return items, nil
}

func (r *Repository) ListQueue(ctx context.Context, params ports.ListQueueParams) ([]domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var cursorCapturedAt any
	var cursorItemID any
	if params.Cursor != nil {
		cursorCapturedAt = params.Cursor.CapturedAt
		cursorItemID = params.Cursor.ItemID
	}
	filterShedID, filterPartition := splitShedFilter(params.ShedID)
	// projection-review: producer grain is verification_items(item_id); the consumer matches that
	// same item_id grain. Operator/verifier LATERAL, shed, and park label joins are each
	// 0..1, so no joined side multiplies a queue row. This query computes no numerator/denominator.

	// For multi-category queries (verifier lens "All evidence" across authorized categories),
	// use Categories slice instead of single Category. Single Category takes precedence for
	// backward compatibility; Categories is only used when Category is empty and Categories is non-empty.
	// Always use an array type so the SQL predicate is consistent.
	var categoryFilterList []string
	if params.Category != "" {
		categoryFilterList = []string{params.Category}
	} else if len(params.Categories) > 0 {
		categoryFilterList = params.Categories
	}

	rows, err := r.pool.Query(ctx, `
SELECT `+itemColumnsWithLabels+`
FROM verification_items vi
LEFT JOIN LATERAL (
  SELECT wm.display_name
  FROM workforce_members wm
  WHERE wm.tenant_id = vi.tenant_id
    AND (wm.workforce_member_id = vi.operator_id OR wm.user_id = vi.operator_id)
  ORDER BY
    (wm.workforce_member_id = vi.operator_id) DESC,
    (wm.status = 'active') DESC,
    wm.updated_at DESC,
    wm.workforce_member_id
  LIMIT 1
) operator ON true
LEFT JOIN LATERAL (
  SELECT wm.display_name
  FROM workforce_members wm
  WHERE wm.tenant_id = vi.tenant_id
    AND (wm.workforce_member_id = vi.verified_by OR wm.user_id = vi.verified_by)
  ORDER BY
    (wm.workforce_member_id = vi.verified_by) DESC,
    (wm.status = 'active') DESC,
    wm.updated_at DESC,
    wm.workforce_member_id
  LIMIT 1
) verifier ON true
LEFT JOIN locations shed_loc ON vi.tenant_id = shed_loc.tenant_id AND vi.shed_id = shed_loc.location_id
LEFT JOIN locations park_loc ON vi.tenant_id = park_loc.tenant_id AND vi.park_id = park_loc.location_id
WHERE vi.tenant_id = $1::uuid
  AND ($2 = '' OR vi.status = $2)
  AND ($3 = '' OR vi.category = ANY(string_to_array($3, ',')))
  AND ($4 = '' OR vi.vertical = $4)
  AND ($5 = '' OR vi.module = $5)
  AND (NOT $6::boolean OR vi.park_id = ANY($7::uuid[]))
  AND ($14 = '' OR vi.park_id = $14::uuid)
  AND ($15 = '' OR vi.shed_id = $15::uuid)
  AND ($19 = '' OR `+shedPartitionPredicate+` = $19)
  AND ($16::timestamptz IS NULL OR vi.captured_at >= $16::timestamptz)
  AND ($17::timestamptz IS NULL OR vi.captured_at < $17::timestamptz)
  AND ($8::timestamptz IS NULL OR (vi.captured_at, vi.item_id) > ($8::timestamptz, $9::uuid))
  AND (
    NOT $10::boolean
    OR (
      vi.source_submission_id IS NOT NULL
      AND vi.closed_at IS NULL
      AND NOT EXISTS (
        SELECT 1
        FROM verification_items sibling
        WHERE sibling.tenant_id = vi.tenant_id
          AND sibling.source_submission_id = vi.source_submission_id
          AND (sibling.status <> 'approved' OR sibling.closed_at IS NOT NULL)
      )
    )
  )
  AND (NOT $12::boolean OR vi.source_submission_id IS NOT NULL)
  AND (NOT $13::boolean OR vi.closed_at IS NULL)
  AND (
    NOT $18::boolean
    OR (
      vi.applier_ack_expected
      AND vi.applied_at IS NULL
      AND vi.closed_at IS NULL
      AND vi.status NOT IN ('pending', 'withdrawn')
    )
  )
ORDER BY vi.captured_at ASC, vi.item_id ASC
LIMIT $11`,
		params.TenantID, params.Status, strings.Join(categoryFilterList, ","), params.Vertical, params.Module,
		params.ScopeRestricted, params.ParkIDs, cursorCapturedAt, cursorItemID,
		params.ReadyForClosure, params.Limit, params.SubmissionScopedOnly, params.OpenOnly,
		params.ParkID, filterShedID,
		params.CapturedFrom, params.CapturedBefore,
		params.AwaitingApplicationOnly,
		filterPartition,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Item, 0, params.Limit)
	for rows.Next() {
		item, err := scanItemWithLabels(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListQueueFilterOptions(ctx context.Context, params ports.ListQueueParams) (domain.QueueFilterOptions, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	options := domain.QueueFilterOptions{}
	filterShedID, filterPartition := splitShedFilter(params.ShedID)

	// For multi-category queries (verifier lens "All evidence" across authorized categories),
	// use Categories slice instead of single Category. Single Category takes precedence for
	// backward compatibility; Categories is only used when Category is empty and Categories is non-empty.
	// Always use an array type so the SQL predicate is consistent.
	var categoryFilterList []string
	if params.Category != "" {
		categoryFilterList = []string{params.Category}
	} else if len(params.Categories) > 0 {
		categoryFilterList = params.Categories
	}

	parkRows, err := r.pool.Query(ctx, `
SELECT vi.park_id::text, COALESCE(park_loc.name, vi.park_id::text) AS label
FROM verification_items vi
LEFT JOIN locations park_loc ON vi.tenant_id = park_loc.tenant_id AND vi.park_id = park_loc.location_id
WHERE vi.tenant_id = $1::uuid
  AND vi.park_id IS NOT NULL
  AND ($2 = '' OR vi.status = $2)
  AND ($3 = '' OR vi.category = ANY(string_to_array($3, ',')))
  AND ($4 = '' OR vi.vertical = $4)
  AND ($5 = '' OR vi.module = $5)
  AND (NOT $6::boolean OR vi.park_id = ANY($7::uuid[]))
  AND (NOT $8::boolean OR vi.source_submission_id IS NOT NULL)
  AND (NOT $9::boolean OR vi.closed_at IS NULL)
  AND ($10::timestamptz IS NULL OR vi.captured_at >= $10::timestamptz)
  AND ($11::timestamptz IS NULL OR vi.captured_at < $11::timestamptz)
GROUP BY vi.park_id, park_loc.name
ORDER BY label, vi.park_id::text`,
		params.TenantID, params.Status, strings.Join(categoryFilterList, ","), params.Vertical, params.Module,
		params.ScopeRestricted, params.ParkIDs, params.SubmissionScopedOnly, params.OpenOnly,
		params.CapturedFrom, params.CapturedBefore,
	)
	if err != nil {
		return options, err
	}
	defer parkRows.Close()
	for parkRows.Next() {
		var id, label string
		if err := parkRows.Scan(&id, &label); err != nil {
			return options, err
		}
		options.Parks = append(options.Parks, domain.LocationFilterOption{ID: id, Label: label})
	}
	if err := parkRows.Err(); err != nil {
		return options, err
	}

	shedRows, err := r.pool.Query(ctx, `
SELECT
  vi.shed_id::text,
  COALESCE(shed_loc.name, vi.shed_id::text) AS shed_label,
  COALESCE(MAX(sp.partition_label), MAX(NULLIF(btrim(vi.partition_label), ''))) AS partition_label,
  `+shedPartitionPredicate+` AS partition_key,
  -- Agree-or-go-bare, the same discipline oploc applies to a partition: a shed belongs to one
  -- park, so every row in this group should carry one park_id, and if they ever disagree the
  -- honest answer is no park rather than whichever MAX() happens to win. An option with no park
  -- still filters correctly -- the ID is the shed -- it just cannot be grouped.
  -- MAX over the TEXT form: Postgres has no max(uuid), and the group is already known to hold
  -- exactly one park_id when this branch is taken, so the aggregate is only picking that value.
  CASE WHEN count(DISTINCT vi.park_id) = 1 THEN COALESCE(MAX(vi.park_id::text), '') ELSE '' END AS park_id,
  CASE WHEN count(DISTINCT vi.park_id) = 1 THEN COALESCE(MAX(park_loc.name), '') ELSE '' END AS park_label
FROM verification_items vi
LEFT JOIN locations shed_loc ON vi.tenant_id = shed_loc.tenant_id AND vi.shed_id = shed_loc.location_id
LEFT JOIN locations park_loc ON vi.tenant_id = park_loc.tenant_id AND vi.park_id = park_loc.location_id
LEFT JOIN shed_partitions sp
  ON sp.tenant_id = vi.tenant_id
 AND sp.shed_id = vi.shed_id
 AND sp.normalized_label = `+shedPartitionPredicate+`
 AND sp.status = 'active'
WHERE vi.tenant_id = $1::uuid
  AND vi.shed_id IS NOT NULL
  AND ($2 = '' OR vi.status = $2)
  AND ($3 = '' OR vi.category = ANY(string_to_array($3, ',')))
  AND ($4 = '' OR vi.vertical = $4)
  AND ($5 = '' OR vi.module = $5)
  AND (NOT $6::boolean OR vi.park_id = ANY($7::uuid[]))
  AND ($8 = '' OR vi.park_id = $8::uuid)
  AND (NOT $9::boolean OR vi.source_submission_id IS NOT NULL)
  AND (NOT $10::boolean OR vi.closed_at IS NULL)
  AND ($11::timestamptz IS NULL OR vi.captured_at >= $11::timestamptz)
  AND ($12::timestamptz IS NULL OR vi.captured_at < $12::timestamptz)
GROUP BY vi.shed_id, shed_loc.name, `+shedPartitionPredicate+`
-- Park first so a park's sheds arrive contiguously and a client can group without sorting.
-- shed_id still breaks the final tie, so two identically-named sheds in ONE park stay stable.
ORDER BY park_label, shed_label, partition_key, vi.shed_id::text`,
		params.TenantID, params.Status, strings.Join(categoryFilterList, ","), params.Vertical, params.Module,
		params.ScopeRestricted, params.ParkIDs, params.ParkID, params.SubmissionScopedOnly, params.OpenOnly,
		params.CapturedFrom, params.CapturedBefore,
	)
	if err != nil {
		return options, err
	}
	defer shedRows.Close()
	for shedRows.Next() {
		var id, shedLabel, partitionKey, parkID, parkLabel string
		var partitionLabel *string
		if err := shedRows.Scan(&id, &shedLabel, &partitionLabel, &partitionKey, &parkID, &parkLabel); err != nil {
			return options, err
		}
		loc := oploc.OperationalLocation{ShedID: id, ShedName: shedLabel}
		if partitionLabel != nil {
			loc.PartitionLabel = *partitionLabel
		}
		options.Sheds = append(options.Sheds, domain.LocationFilterOption{
			ID:    id + "#" + partitionKey,
			Label: loc.Display(),
			// Carried beside the label, never folded into it: the label is the shed's
			// operational location and oploc owns that string.
			ParkID:                     parkID,
			ParkLabel:                  parkLabel,
			PartitionLabel:             partitionLabel,
			OperationalLocationDisplay: loc.Display(),
		})
	}
	if err := shedRows.Err(); err != nil {
		return options, err
	}
	// Whole-filter status-count aggregate (domain.QueueStatusCounts) for the mock's dot-legend
	// pills — the SAME scope as the paginated queue read (tenant + category/vertical/module + park
	// + shed + business date) but with NO status predicate and NO cursor/limit, grouped by status
	// in the database. This is deliberately a separate query from ListQueue's page read: the page
	// is keyset-limited, this aggregate never is. Uses verification_items_queue_idx
	// (tenant_id, status, category, captured_at, item_id) to seek per status.
	//
	// projection-review: membership=verification_items rows with status IN (pending, approved,
	// rejected), unique key (tenant_id, item_id) — one row per verification item, so COUNT(*) is
	// exact; group_key=status; join_cardinality=no join (single table, so no fan-out is possible —
	// this is the "join_cardinality=n/a" case); pagination=none — this is the whole-filter
	// aggregate, computed with no LIMIT/OFFSET/cursor, never derived from ListQueue's keyset page;
	// scope=tenant_id + category + vertical + module + park_id + shed_id + captured_at window,
	// identical predicates to ListQueue's own page read (minus the status predicate, since this
	// query needs all three buckets at once). Disjointness: pending/approved/rejected are mutually
	// exclusive values of the single `status` column on verification_items (CHECK-constrained,
	// see domain.QueueStatusCounts doc comment), so a row lands in exactly one bucket and the three
	// counts partition — never overlap — the in-scope backlog.
	countRows, err := r.pool.Query(ctx, `
SELECT vi.status, COUNT(*)::int AS n
FROM verification_items vi
WHERE vi.tenant_id = $1::uuid
  AND vi.status IN ('pending', 'approved', 'rejected')
  AND ($2 = '' OR vi.category = ANY(string_to_array($2, ',')))
  AND ($3 = '' OR vi.vertical = $3)
  AND ($4 = '' OR vi.module = $4)
  AND (NOT $5::boolean OR vi.park_id = ANY($6::uuid[]))
  AND ($7 = '' OR vi.park_id = $7::uuid)
  AND ($8 = '' OR vi.shed_id = $8::uuid)
  AND ($9::timestamptz IS NULL OR vi.captured_at >= $9::timestamptz)
  AND ($10::timestamptz IS NULL OR vi.captured_at < $10::timestamptz)
  AND ($11 = '' OR `+shedPartitionPredicate+` = $11)
GROUP BY vi.status`,
		params.TenantID, strings.Join(categoryFilterList, ","), params.Vertical, params.Module,
		params.ScopeRestricted, params.ParkIDs, params.ParkID, filterShedID,
		params.CapturedFrom, params.CapturedBefore, filterPartition,
	)
	if err != nil {
		return options, err
	}
	defer countRows.Close()
	for countRows.Next() {
		var status string
		var n int
		if err := countRows.Scan(&status, &n); err != nil {
			return options, err
		}
		switch status {
		case domain.StatusPending:
			options.Counts.Pending = n
		case domain.StatusApproved:
			options.Counts.Approved = n
		case domain.StatusRejected:
			options.Counts.Rejected = n
		}
	}
	if err := countRows.Err(); err != nil {
		return options, err
	}
	if params.MissedBefore != nil {
		err = r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM verification_items vi
  WHERE vi.tenant_id = $1::uuid
    AND vi.status = 'pending'
    AND ($2 = '' OR $2 IS NULL OR vi.category = ANY(string_to_array($2, ',')))
    AND ($3 = '' OR vi.vertical = $3)
    AND ($4 = '' OR vi.module = $4)
    AND (NOT $5::boolean OR vi.park_id = ANY($6::uuid[]))
    AND ($7 = '' OR vi.park_id = $7::uuid)
    AND ($8 = '' OR vi.shed_id = $8::uuid)
    AND vi.captured_at < $9::timestamptz
    AND ($10 = '' OR `+shedPartitionPredicate+` = $10)
  LIMIT 1
)`,
			params.TenantID, strings.Join(categoryFilterList, ","), params.Vertical, params.Module,
			params.ScopeRestricted, params.ParkIDs, params.ParkID, filterShedID, params.MissedBefore,
			filterPartition,
		).Scan(&options.HasMissed)
		if err != nil {
			return options, err
		}
	}
	return options, nil
}

func (r *Repository) CloseItem(ctx context.Context, in domain.CloseAction) (domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	defer rollback(ctx, tx)
	reservation, err := reserveIdempotency(ctx, tx, in.TenantID, "verification.close-item", in.IdempotencyKey,
		requestFingerprint(in.ItemID, in.ActorID, fmt.Sprintf("%d", in.RowVersion)))
	if err != nil {
		return domain.Item{}, err
	}
	if !reservation.proceed {
		item, err := scanItemRow(tx.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", in.TenantID, reservation.resultID))
		if err != nil {
			return domain.Item{}, mapWriteErr(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Item{}, err
		}
		return item, nil
	}
	tag, err := tx.Exec(ctx, `
UPDATE verification_items
SET closed_by = $1::uuid, closed_at = now(), row_version = row_version + 1
WHERE tenant_id = $2::uuid
  AND item_id = $3::uuid
  AND row_version = $4
  AND status = 'approved'
  AND closed_at IS NULL
  AND source_submission_id IS NULL`,
		in.ActorID, in.TenantID, in.ItemID, in.RowVersion)
	if err != nil {
		return domain.Item{}, mapWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if scanErr := tx.QueryRow(ctx, `
SELECT true
FROM verification_items
WHERE tenant_id = $1::uuid
  AND item_id = $2::uuid`, in.TenantID, in.ItemID).Scan(&exists); scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return domain.Item{}, ports.ErrNotFound
			}
			return domain.Item{}, scanErr
		}
		return domain.Item{}, ports.ErrConflict
	}
	item, err := scanItemRow(tx.QueryRow(ctx,
		"SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid",
		in.TenantID, in.ItemID))
	if err != nil {
		return domain.Item{}, mapWriteErr(err)
	}
	idempotencyKey := fmt.Sprintf("%s:%s:%d", EventItemClosed, in.ItemID, item.RowVersion)
	if err := insertOutboxEvent(ctx, tx, in.TenantID, EventItemClosed, in.ItemID, idempotencyKey, verificationVerdictPayload(item)); err != nil {
		return domain.Item{}, err
	}
	if err := completeIdempotency(ctx, tx, in.TenantID, "verification.close-item", in.IdempotencyKey, "verification_item", item.ItemID); err != nil {
		return domain.Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Item{}, err
	}
	return item, nil
}

func (r *Repository) CloseSubmission(ctx context.Context, in domain.CloseSubmissionAction) ([]domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, tx)
	reservation, err := reserveIdempotency(ctx, tx, in.TenantID, "verification.close-submission", in.IdempotencyKey,
		requestFingerprint(in.SubmissionID, in.ActorID))
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
SELECT `+itemColumns+`
FROM verification_items
WHERE tenant_id = $1::uuid
  AND source_submission_id = $2::uuid
ORDER BY captured_at, item_id
FOR UPDATE`, in.TenantID, in.SubmissionID)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Item, 0, 20)
	for rows.Next() {
		item, scanErr := scanItemRow(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(items) == 0 {
		return nil, ports.ErrNotFound
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return items, nil
	}

	allClosed := true
	for _, item := range items {
		if item.Status != domain.StatusApproved {
			return nil, ports.ErrConflict
		}
		if item.ClosedAt == nil {
			allClosed = false
		}
	}
	acceptVaccinationCompletions := func() error {
		if _, err := tx.Exec(ctx, `
UPDATE vaccination_completions vc
SET status = 'accepted',
    verified_by = $1::uuid,
    verified_at = now(),
    row_version = vc.row_version + 1,
    updated_at = now()
FROM sop_submission_items si
WHERE vc.tenant_id = $2::uuid
  AND si.tenant_id = vc.tenant_id
  AND si.submission_id = $3::uuid
  AND vc.sop_submission_item_id = si.item_id
  AND vc.status = 'recorded'
  AND EXISTS (
    SELECT 1
    FROM verification_items vi
    WHERE vi.tenant_id = vc.tenant_id
      AND vi.source_submission_id = si.submission_id
      AND (
        (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
        OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
      )
      AND vi.status = 'approved'
      AND vi.closed_at IS NOT NULL
)`, in.ActorID, in.TenantID, in.SubmissionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE obligation_instances oi
SET status = 'completed',
    completed_at = COALESCE(
      oi.completed_at,
      (
        -- projection-review: membership=accepted vaccination_completions attached to this exact obligation and submission; group_key=the outer obligation row; join_cardinality=sop_submission_items is one row per completion item and min(administered_at) collapses any same-obligation accepted retries to one medical instant; pagination=n/a single-row closeout mutation; scope=tenant+obligation+submission, with no park/shed inference
        SELECT min(vc.administered_at)
        FROM vaccination_completions vc
        JOIN sop_submission_items si
          ON si.tenant_id = vc.tenant_id
         AND si.item_id = vc.sop_submission_item_id
        WHERE vc.tenant_id = oi.tenant_id
          AND vc.obligation_id = oi.obligation_id
          AND vc.status = 'accepted'
          AND si.submission_id = $2::uuid
      ),
      now()
    ),
    updated_at = now()
WHERE oi.tenant_id = $1::uuid
  AND oi.status IN ('scheduled', 'due', 'in_progress')
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    JOIN sop_submission_items si
      ON si.tenant_id = vc.tenant_id
     AND si.item_id = vc.sop_submission_item_id
    WHERE vc.tenant_id = oi.tenant_id
      AND vc.obligation_id = oi.obligation_id
      AND vc.status = 'accepted'
      AND si.submission_id = $2::uuid
  )`, in.TenantID, in.SubmissionID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
SELECT DISTINCT vc.obligation_id::text
FROM vaccination_completions vc
JOIN obligation_instances oi
  ON oi.tenant_id = vc.tenant_id
 AND oi.obligation_id = vc.obligation_id
JOIN sop_submission_items si
  ON si.tenant_id = vc.tenant_id
 AND si.item_id = vc.sop_submission_item_id
WHERE vc.tenant_id = $1::uuid
  AND si.submission_id = $2::uuid
  AND vc.status = 'accepted'
  AND oi.status = 'completed'`, in.TenantID, in.SubmissionID)
		if err != nil {
			return err
		}
		obligationIDs := make([]string, 0, len(items))
		for rows.Next() {
			var obligationID string
			if err := rows.Scan(&obligationID); err != nil {
				rows.Close()
				return err
			}
			obligationIDs = append(obligationIDs, obligationID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, obligationID := range obligationIDs {
			if _, err := insertVaccinationCompletedOutbox(ctx, tx, in.TenantID, obligationID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `
UPDATE obligation_batches ob
SET status = 'completed',
    updated_at = now()
WHERE ob.tenant_id = $1::uuid
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    JOIN sop_submission_items si
      ON si.tenant_id = vc.tenant_id
     AND si.item_id = vc.sop_submission_item_id
    WHERE vc.tenant_id = ob.tenant_id
      AND vc.batch_id = ob.batch_id
      AND vc.status = 'accepted'
      AND si.submission_id = $2::uuid
  )
  AND NOT EXISTS (
    SELECT 1
    FROM obligation_instances oi
    WHERE oi.tenant_id = ob.tenant_id
      AND oi.batch_id = ob.batch_id
      AND oi.status <> 'completed'
  )`, in.TenantID, in.SubmissionID)
		return err
	}
	// A replay after a lost response is a read-only success. A partially closed drive can only be
	// legacy/item-level state and is failed closed rather than silently mixing authority actions.
	if allClosed {
		if err := acceptVaccinationCompletions(); err != nil {
			return nil, mapWriteErr(err)
		}
		if err := completeIdempotency(ctx, tx, in.TenantID, "verification.close-submission", in.IdempotencyKey, "verification_submission", in.SubmissionID); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return items, nil
	}
	for _, item := range items {
		if item.ClosedAt != nil {
			return nil, ports.ErrConflict
		}
	}

	if _, err := tx.Exec(ctx, `
UPDATE verification_items
SET closed_by = $1::uuid, closed_at = now(), row_version = row_version + 1
WHERE tenant_id = $2::uuid
  AND source_submission_id = $3::uuid
  AND status = 'approved'
  AND closed_at IS NULL`, in.ActorID, in.TenantID, in.SubmissionID); err != nil {
		return nil, mapWriteErr(err)
	}
	if err := acceptVaccinationCompletions(); err != nil {
		return nil, mapWriteErr(err)
	}
	closedRows, err := tx.Query(ctx, `
SELECT `+itemColumns+`
FROM verification_items
WHERE tenant_id = $1::uuid
  AND source_submission_id = $2::uuid
ORDER BY captured_at, item_id`, in.TenantID, in.SubmissionID)
	if err != nil {
		return nil, err
	}
	closedItems := make([]domain.Item, 0, len(items))
	for closedRows.Next() {
		item, scanErr := scanItemRow(closedRows)
		if scanErr != nil {
			closedRows.Close()
			return nil, scanErr
		}
		closedItems = append(closedItems, item)
	}
	if err := closedRows.Err(); err != nil {
		closedRows.Close()
		return nil, err
	}
	closedRows.Close()
	if len(closedItems) != len(items) {
		return nil, ports.ErrConflict
	}
	for _, item := range closedItems {
		idempotencyKey := fmt.Sprintf("%s:%s:%d", EventItemClosed, item.ItemID, item.RowVersion)
		if err := insertOutboxEvent(ctx, tx, in.TenantID, EventItemClosed, item.ItemID, idempotencyKey, verificationVerdictPayload(item)); err != nil {
			return nil, err
		}
	}
	if err := completeIdempotency(ctx, tx, in.TenantID, "verification.close-submission", in.IdempotencyKey, "verification_submission", in.SubmissionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return closedItems, nil
}

// vaccinationProofCategory scopes the vaccination batch close to its OWN evidence. It mirrors the
// Category the ready query and the HTTP handler pass ("vaccination_proof"); it is declared locally
// rather than imported from sopbridge to keep this adapter free of that dependency.
const vaccinationProofCategory = "vaccination_proof"

func (r *Repository) ListReadyVaccinationBatchClosures(ctx context.Context, params ports.ListQueueParams) ([]domain.VaccinationBatchClosure, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	filterShedID, filterPartition := splitShedFilter(params.ShedID)
	rows, err := r.pool.Query(ctx, `
WITH batch_scope AS (
  SELECT vda.batch_id, vda.park_id, vda.shed_id
  FROM vaccination_drive_assignments vda
  WHERE vda.tenant_id = $1::uuid
  UNION
  SELECT vc.batch_id, vi.park_id, vi.shed_id
  FROM vaccination_completions vc
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  JOIN verification_items vi
    ON vi.tenant_id = vc.tenant_id
   AND vi.source_submission_id = si.submission_id
   AND (
     (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
     OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
   )
  WHERE vc.tenant_id = $1::uuid
    AND vi.category = $2
    AND vc.status IN ('recorded', 'accepted')
),
expected AS (
  SELECT
    ob.batch_id,
	completion_counts.completion_count,
    completion_counts.total_count,
    ob.protocol_version_id::text AS protocol_version_id,
    COALESCE(MIN(vda.park_id::text), '') AS park_id,
    COALESCE(MIN(park.name), '') AS park_label,
    string_agg(DISTINCT NULLIF(vda.physical_shed, ''), ', ' ORDER BY NULLIF(vda.physical_shed, '')) AS shed_labels,
    COUNT(DISTINCT NULLIF(vda.physical_shed, ''))::int AS planned_shed_count,
    MIN(COALESCE(vda.planned_date, ob.planned_date)) AS start_date,
    MAX(COALESCE(vda.planned_date, ob.planned_date)) AS end_date
  FROM obligation_batches ob
	JOIN (
	    SELECT
	      vc.batch_id,
	      COUNT(*)::int AS completion_count,
	      COUNT(DISTINCT vc.goat_id)::int AS total_count
	    FROM (
	      SELECT tenant_id, batch_id, goat_id, sop_submission_item_id
	      FROM vaccination_completions
	      WHERE tenant_id = $1::uuid
	        AND batch_id IS NOT NULL
	        AND status IN ('recorded', 'accepted')
      UNION ALL
      -- A sent-back animal MUST still be counted in the drive. Its completion row was moved to
      -- the rejection archive (migration 000093), so counting only the live table dropped it out
      -- of the drive entirely: the denominators shrank to the animals that went well, the
      -- rejected-count read 0, and the drive offered a Close button while an animal was still
      -- waiting to be redone. Close is only allowed when EVERY video in the drive, across all its
      -- sheds, has been verified.
	      SELECT vcr.tenant_id, vcr.batch_id, vcr.goat_id, vcr.sop_submission_item_id
	      FROM vaccination_completion_rejections vcr
	      WHERE vcr.tenant_id = $1::uuid
	        AND vcr.batch_id IS NOT NULL
        -- ...but NOT one that has since been redone. A sent-back animal that was re-vaccinated and
        -- re-approved has BOTH an archived rejection and a live completion, so counting both made
        -- completion_count exceed the number of proofs that can ever exist (5 live + 2 archived = 7
        -- against 5 proofs). The gate proof_count = completion_count then failed permanently and
        -- the drive could never be closed -- the animal was punished twice for being redone.
        -- Superseded rejections are history; the live completion carries its latest verdict.
        AND NOT EXISTS (
          SELECT 1
          FROM vaccination_completions live
          WHERE live.tenant_id = vcr.tenant_id
            AND live.batch_id = vcr.batch_id
            AND live.goat_id = vcr.goat_id
	            AND live.status IN ('recorded', 'accepted')
	        )
	    ) vc
	    WHERE (
	      ($9 = '' AND $10 = '')
	      OR EXISTS (
	        SELECT 1
	        FROM sop_submission_items si_scope
	        JOIN verification_items vi_scope
	          ON vi_scope.tenant_id = si_scope.tenant_id
	         AND vi_scope.source_submission_id = si_scope.submission_id
	         AND (
	           (vi_scope.source_ref_type = 'sop_submission' AND vi_scope.source_ref_id = si_scope.submission_id)
	           OR (vi_scope.source_ref_type = 'vaccination_goat' AND vi_scope.source_ref_id = si_scope.goat_id)
	         )
	        WHERE si_scope.tenant_id = vc.tenant_id
	          AND si_scope.item_id = vc.sop_submission_item_id
	          AND vi_scope.category = $2
	          AND ($9 = '' OR vi_scope.shed_id = $9::uuid)
	          AND (
	            $10 = ''
	            OR regexp_replace(lower(btrim(COALESCE(vi_scope.partition_label, 'whole'))), '^part[[:space:]]+', '') = $10
	          )
	      )
	    )
	    GROUP BY vc.batch_id
	  ) completion_counts
    ON completion_counts.batch_id = ob.batch_id
  LEFT JOIN vaccination_drive_assignments vda
    ON vda.tenant_id = ob.tenant_id
   AND vda.batch_id = ob.batch_id
  LEFT JOIN locations park
    ON park.tenant_id = vda.tenant_id
   AND park.location_id = vda.park_id
  WHERE ob.tenant_id = $1::uuid
  GROUP BY ob.batch_id, completion_counts.completion_count, completion_counts.total_count, ob.protocol_version_id
),
proofs AS (
  SELECT
    vc.batch_id,
	vc.completion_id,
	vc.goat_id,
    vi.item_id,
    vi.status,
    vi.closed_at,
    vi.verified_at,
    vi.captured_at,
    vi.park_id,
    vi.shed_id
  FROM vaccination_completions vc
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  JOIN verification_items vi
    ON vi.tenant_id = vc.tenant_id
   AND vi.source_submission_id = si.submission_id
   AND (
     (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
     OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
   )
  WHERE vc.tenant_id = $1::uuid
    -- 'accepted' as well as 'recorded'. Approving the last video FLIPS the completion from
    -- 'recorded' to 'accepted', so a proofs CTE scoped to 'recorded' alone went empty exactly when
    -- the drive finished: proof_count fell to 0, the gate proof_count = completion_count could
    -- never hold, and the drive became un-closeable BY BEING FULLY APPROVED. The sibling
    -- sibling expected CTE above already counts IN ('recorded','accepted'); the two halves of it
    -- disagreed about what a completion is (observed 2026-08-08: 5/5 approved, close button absent
    -- for CEO and Director).
    AND vc.status IN ('recorded', 'accepted')
    AND vi.category = $2
    AND ($3 = '' OR vi.vertical = $3)
    AND ($4 = '' OR vi.module = $4)
    AND (NOT $7::boolean OR vi.closed_at IS NULL)
    AND (NOT $5::boolean OR vi.park_id = ANY($6::uuid[]))
    AND ($8 = '' OR vi.park_id = $8::uuid)
    AND ($9 = '' OR vi.shed_id = $9::uuid)
    AND (
      $10 = ''
      OR regexp_replace(lower(btrim(COALESCE(vi.partition_label, 'whole'))), '^part[[:space:]]+', '') = $10
    )
  UNION ALL
  -- The archived counterpart of the branch above: an animal whose clip was sent back still has
  -- its verification item, and the drive must keep seeing it as a rejected video. Without this
  -- the readiness gate (rejected_completion_count = 0) passed on a drive that still owed work.
  SELECT
    vcr.batch_id,
    vcr.completion_id,
    vcr.goat_id,
    vi.item_id,
    vi.status,
    vi.closed_at,
    vi.verified_at,
    vi.captured_at,
    vi.park_id,
    vi.shed_id
  FROM vaccination_completion_rejections vcr
  JOIN sop_submission_items si
    ON si.tenant_id = vcr.tenant_id
   AND si.item_id = vcr.sop_submission_item_id
  JOIN verification_items vi
    ON vi.tenant_id = vcr.tenant_id
   AND vi.source_submission_id = si.submission_id
   AND (
     (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
     OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
   )
  WHERE vcr.tenant_id = $1::uuid
    -- ...but NOT an attempt that has since been redone, mirroring the identical exclusion in the
    -- expected CTE above. The denominator (expected) and the numerator (proofs) MUST agree on what
    -- counts as a completion: excluding superseded rejections from one side only made proof_count
    -- exceed completion_count by exactly the redone animals (7 vs 5), so the readiness gate failed
    -- and the close button vanished again. Same rule, both sides.
    AND NOT EXISTS (
      SELECT 1
      FROM vaccination_completions live
      WHERE live.tenant_id = vcr.tenant_id
        AND live.batch_id = vcr.batch_id
        AND live.goat_id = vcr.goat_id
        AND live.status IN ('recorded', 'accepted')
    )
    AND vi.category = $2
    AND ($3 = '' OR vi.vertical = $3)
    AND ($4 = '' OR vi.module = $4)
    AND (NOT $7::boolean OR vi.closed_at IS NULL)
    AND (NOT $5::boolean OR vi.park_id = ANY($6::uuid[]))
    AND ($8 = '' OR vi.park_id = $8::uuid)
    AND ($9 = '' OR vi.shed_id = $9::uuid)
    AND (
      $10 = ''
      OR regexp_replace(lower(btrim(COALESCE(vi.partition_label, 'whole'))), '^part[[:space:]]+', '') = $10
    )
  UNION ALL
  -- Accepted completions whose verification item the OpenOnly filter above removed (it drops
  -- anything with closed_at set). They must still count toward readiness, but they must NOT be
  -- fabricated: NULL item_id/shed_id here is what produced "Closed · 0 sheds · 0/0 videos
  -- approved" on a finished drive, because these rows are all that survives once the real ones are
  -- filtered out, and every count derives from columns that are NULL.
  --
  -- Carry the REAL identity from the closed verification item instead of inventing one, so
  -- video_count and shed_count stay true whether the drive is open or closed.
  SELECT
    vc.batch_id,
	vc.completion_id,
	vc.goat_id,
    closed_vi.item_id,
    'approved'::text AS status,
    -- NOT COALESCE(..., now()). Stamping a close time on a row that is not closed makes
    -- unclosed_completion_count zero and the drive reports closed=true while it is still open.
    closed_vi.closed_at,
    closed_vi.verified_at,
    closed_vi.captured_at,
    closed_vi.park_id,
    closed_vi.shed_id
  FROM vaccination_completions vc
  LEFT JOIN LATERAL (
	    SELECT vi2.item_id, vi2.closed_at, vi2.verified_at, vi2.captured_at, vi2.park_id, vi2.shed_id
	    FROM sop_submission_items si2
	    JOIN verification_items vi2
      ON vi2.tenant_id = si2.tenant_id
     AND vi2.source_submission_id = si2.submission_id
     AND (
       (vi2.source_ref_type = 'sop_submission' AND vi2.source_ref_id = si2.submission_id)
       OR (vi2.source_ref_type = 'vaccination_goat' AND vi2.source_ref_id = si2.goat_id)
     )
	    WHERE si2.tenant_id = vc.tenant_id
	      AND si2.item_id = vc.sop_submission_item_id
	      AND vi2.category = $2
	      AND ($9 = '' OR vi2.shed_id = $9::uuid)
	      AND (
	        $10 = ''
	        OR regexp_replace(lower(btrim(COALESCE(vi2.partition_label, 'whole'))), '^part[[:space:]]+', '') = $10
	      )
	    ORDER BY vi2.verified_at DESC NULLS LAST, vi2.captured_at DESC NULLS LAST, vi2.item_id DESC
    LIMIT 1
  ) closed_vi ON TRUE
  WHERE vc.tenant_id = $1::uuid
    AND vc.status = 'accepted'
    AND (
      NOT $5::boolean
      OR EXISTS (
        SELECT 1
        FROM batch_scope bs
        WHERE bs.batch_id = vc.batch_id
          AND bs.park_id = ANY($6::uuid[])
      )
    )
    AND (
      $8 = ''
      OR EXISTS (
        SELECT 1
        FROM batch_scope bs
        WHERE bs.batch_id = vc.batch_id
          AND bs.park_id = $8::uuid
      )
    )
	    AND (
	      $9 = ''
	      OR EXISTS (
	        SELECT 1
	        FROM batch_scope bs
	        WHERE bs.batch_id = vc.batch_id
	          AND bs.shed_id = $9::uuid
	      )
	    )
	    AND (($9 = '' AND $10 = '') OR closed_vi.item_id IS NOT NULL)
	),
-- LATEST VERDICT PER PROOF. A rejection makes the operator re-shoot, so one completion/goat can
-- carry SEVERAL verification_items over time: rejected, rejected again, finally approved. The
-- readiness gate below gates on rejected_completion_count = 0, so counting every historical row
-- meant a superseded rejection held the drive open FOREVER -- the close button never appeared for
-- CEO/Director even though every animal's latest verdict was approved (observed 2026-08-08:
-- G-006004 rejected 19:54, rejected again 20:53, approved 20:57; drive permanently unclosable).
--
-- DISTINCT ON keeps exactly one row per (batch, completion, goat) -- the newest verdict -- so a
-- superseded rejection becomes history instead of outstanding work. It does NOT weaken the gate:
-- a proof whose LATEST verdict is rejected or still pending is counted exactly as before, which is
-- the property the archive UNION above was added to protect.
latest_proofs AS (
  SELECT DISTINCT ON (batch_id, completion_id, goat_id)
    batch_id, completion_id, goat_id, item_id, status, closed_at, park_id, shed_id
  FROM proofs
  -- Prefer a row that actually CARRIES the verification facts. proofs holds two kinds of row for
  -- the same completion, and ordering by closed_at first picked the one with NULL item_id/shed_id,
  -- so every count derived from them collapsed to zero and the close card read
  -- "0 sheds - 0/0 videos approved" on a drive with 5 videos across 2 sheds. Rank identity-bearing
  -- rows first, and only then take the newest verdict among them.
  --
  -- Rank on the VERDICT time, not closed_at. closed_at is NULL for every unclosed attempt, so
  -- ordering by it left several live attempts tied and the winner fell through to item_id DESC --
  -- random UUID order, which can pick an older rejection over the later approval and hide the
  -- close card again. verified_at is when the verifier actually decided; captured_at is NOT NULL
  -- and breaks the remaining tie deterministically; item_id is only the final total-order fallback.
  ORDER BY batch_id, completion_id, goat_id,
           (item_id IS NOT NULL) DESC, (shed_id IS NOT NULL) DESC,
           verified_at DESC NULLS LAST, captured_at DESC NULLS LAST, item_id DESC
),
-- projection-review: membership=vaccination_completions with non-null batch_id is the executed medical membership, independent of later obligation reassignment; group_key=batch_id; join_cardinality=verification_items may be one-to-many per submission/completion, so readiness counts DISTINCT completion_id while user-facing totals and status buckets count DISTINCT goat_id, and assignment rows are pre-aggregated inside expected; pagination=all completion/proof rows are reduced to one whole-batch rollup before the final LIMIT 20 closure page; scope=park/shed filters use explicit batch_scope rows from assignment or verification facts, never a generic hierarchy COALESCE
rollup AS (
  SELECT
    e.batch_id::text,
    CASE
      WHEN e.park_id = '' THEN 'vaccination:' || e.protocol_version_id
      ELSE 'vaccination:' || e.protocol_version_id || ':park:' || e.park_id
    END AS drive_key,
    trim(both ' · ' FROM concat_ws(
      ' · ',
      NULLIF(e.park_label, ''),
      CASE
        WHEN e.start_date IS NULL THEN ''
        WHEN e.end_date IS NULL OR e.start_date = e.end_date THEN to_char(e.start_date, 'DD Mon YYYY')
        ELSE to_char(e.start_date, 'DD Mon') || ' - ' || to_char(e.end_date, 'DD Mon YYYY')
      END
    )) AS drive_label,
    trim(both ' · ' FROM concat_ws(
      ' · ',
      CASE
        WHEN e.start_date IS NULL THEN ''
	        WHEN e.end_date IS NULL OR e.start_date = e.end_date THEN to_char(e.start_date, 'DD Mon YYYY')
	        ELSE to_char(e.start_date, 'DD Mon') || ' - ' || to_char(e.end_date, 'DD Mon YYYY')
	      END,
	      CASE
	        WHEN e.planned_shed_count = 1 THEN NULLIF(e.shed_labels, '')
	        WHEN e.planned_shed_count > 1 THEN e.planned_shed_count::text || ' sheds'
	        ELSE ''
	      END
	    )) AS batch_label,
    e.park_id,
    e.park_label,
    COALESCE(e.start_date::text, '') AS start_date,
    COALESCE(e.end_date::text, '') AS end_date,
	e.completion_count,
    e.total_count,
	COUNT(DISTINCT p.completion_id)::int AS proof_count,
	COUNT(DISTINCT p.completion_id) FILTER (WHERE p.status = 'approved')::int AS approved_completion_count,
	COUNT(DISTINCT p.completion_id) FILTER (WHERE p.status = 'rejected')::int AS rejected_completion_count,
	COUNT(DISTINCT p.completion_id) FILTER (WHERE p.status = 'pending')::int AS pending_completion_count,
	-- Work not yet CLOSED. Closing stamps closed_at on the drive's verification items, but nothing
	-- filtered on it, so a closed drive kept being offered for closing: the director tapped Close,
	-- the server returned 200 and stamped the items, and the card stayed put -- indistinguishable
	-- from a dead button ("on clicking close nothing happens", 2026-08-08).
	COUNT(DISTINCT p.completion_id) FILTER (WHERE p.closed_at IS NULL)::int AS unclosed_completion_count,
	MAX(p.closed_at) AS last_closed_at,
	-- Work that is not yet CLOSED. Closing a drive stamps closed_at on its verification items, but
	-- nothing filtered on that, so a closed drive kept being offered for closing: the operator
	-- tapped Close, the server returned 200 and stamped the items, and the card stayed exactly
	-- where it was -- indistinguishable from a dead button (reported 2026-08-08, "on clicking close
	COUNT(DISTINCT p.goat_id) FILTER (WHERE p.status = 'approved')::int AS approved_count,
	COUNT(DISTINCT p.goat_id) FILTER (WHERE p.status = 'rejected')::int AS rejected_count,
	COUNT(DISTINCT p.goat_id) FILTER (WHERE p.status = 'pending')::int AS pending_count,
    COUNT(DISTINCT p.item_id)::int AS video_count,
    COUNT(DISTINCT p.item_id) FILTER (WHERE p.status = 'approved')::int AS approved_videos,
    COUNT(DISTINCT p.item_id) FILTER (WHERE p.status = 'rejected')::int AS rejected_videos,
    COUNT(DISTINCT p.item_id) FILTER (WHERE p.status = 'pending')::int AS pending_videos,
    COUNT(DISTINCT p.shed_id)::int AS shed_count
  FROM expected e
  JOIN latest_proofs p ON p.batch_id = e.batch_id
	  GROUP BY e.batch_id, e.protocol_version_id, e.park_id, e.park_label, e.shed_labels, e.planned_shed_count, e.start_date, e.end_date, e.completion_count, e.total_count
)
SELECT batch_id, drive_key, drive_label, batch_label, park_id, park_label, start_date, end_date,
       total_count, approved_count, rejected_count, pending_count,
       video_count, approved_videos, rejected_videos, pending_videos, shed_count,
       (unclosed_completion_count = 0) AS closed,
       COALESCE(to_char(last_closed_at AT TIME ZONE 'Asia/Kolkata', 'DD Mon YYYY'), '') AS closed_at
FROM rollup
WHERE proof_count = completion_count
  AND approved_completion_count = completion_count
  AND rejected_completion_count = 0
  AND pending_completion_count = 0
ORDER BY batch_id
LIMIT 20`,
		params.TenantID, params.Category, params.Vertical, params.Module,
		params.ScopeRestricted, params.ParkIDs, params.OpenOnly, params.ParkID, filterShedID, filterPartition)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.VaccinationBatchClosure, 0, 4)
	for rows.Next() {
		var c domain.VaccinationBatchClosure
		if err := rows.Scan(
			&c.BatchID,
			&c.DriveKey,
			&c.DriveLabel,
			&c.BatchLabel,
			&c.ParkID,
			&c.ParkLabel,
			&c.StartDate,
			&c.EndDate,
			&c.TotalCount,
			&c.ApprovedCount,
			&c.RejectedCount,
			&c.PendingCount,
			&c.VideoCount,
			&c.ApprovedVideos,
			&c.RejectedVideos,
			&c.PendingVideos,
			&c.ShedCount,
			&c.Closed,
			&c.ClosedAt,
		); err != nil {
			return nil, err
		}
		c.Ready = true
		out = append(out, c)
	}
	return out, rows.Err()
}

// blockingSubjectLabel names one animal/shed blocking a drive closure for the refusal message.
// Prefers the human label captured at verification-item creation time (e.g. "Gandhi 1 - G-006004");
// falls back to the raw source ref id when no label was captured, so the caller always gets SOME
// identifier rather than a silently dropped blocker.
func blockingSubjectLabel(item domain.Item) string {
	if item.SubjectLabel != nil && strings.TrimSpace(*item.SubjectLabel) != "" {
		return strings.TrimSpace(*item.SubjectLabel)
	}
	if item.Source.RefID != "" {
		return item.Source.RefID
	}
	return item.ItemID
}

func (r *Repository) CloseVaccinationBatch(ctx context.Context, in domain.CloseVaccinationBatchAction) ([]domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, tx)
	reservation, err := reserveIdempotency(ctx, tx, in.TenantID, "verification.close-vaccination-batch", in.IdempotencyKey,
		requestFingerprint(in.BatchID, in.ActorID))
	if err != nil {
		return nil, err
	}

	var expectedCount int
	if err := tx.QueryRow(ctx, `
SELECT COUNT(*)::int
FROM vaccination_completions
WHERE tenant_id = $1::uuid
  AND batch_id = $2::uuid
  AND status IN ('recorded', 'accepted')`, in.TenantID, in.BatchID).Scan(&expectedCount); err != nil {
		return nil, err
	}
	if expectedCount == 0 {
		return nil, ports.ErrNotFound
	}

	rows, err := tx.Query(ctx, `
SELECT `+itemColumns+`
FROM verification_items vi
WHERE vi.tenant_id = $1::uuid
  AND vi.source_submission_id IS NOT NULL
  -- Vaccination proofs ONLY. The ready query that authorizes this button is category-filtered, so
  -- a category-blind close is not self-consistent with it. Before the latest-verdict skip below
  -- existed, being blind here merely OVER-blocked (a foreign-category rejection refused a
  -- vaccination close -- wrong, but fail-closed). Combined with that skip it became fail-OPEN: a
  -- higher-ranking approved item from another category on the same submission could win the
  -- DISTINCT ON and mask a REJECTED vaccination proof, closing and accepting a batch with real
  -- rework outstanding. Found in review of 99fd332b6.
  AND vi.category = $3
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    JOIN sop_submission_items si
      ON si.tenant_id = vc.tenant_id
     AND si.item_id = vc.sop_submission_item_id
    WHERE vc.tenant_id = vi.tenant_id
      AND vc.batch_id = $2::uuid
      AND vc.status IN ('recorded', 'accepted')
      AND si.submission_id = vi.source_submission_id
      AND (
        (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
        OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
      )
  )
ORDER BY vi.captured_at, vi.item_id
FOR UPDATE`, in.TenantID, in.BatchID, vaccinationProofCategory)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Item, 0, expectedCount)
	for rows.Next() {
		item, scanErr := scanItemRow(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(items) != expectedCount {
		var coveredCount int
		if err := tx.QueryRow(ctx, `
SELECT COUNT(*)::int
FROM vaccination_completions vc
LEFT JOIN sop_submission_items si
  ON si.tenant_id = vc.tenant_id
 AND si.item_id = vc.sop_submission_item_id
WHERE vc.tenant_id = $1::uuid
  AND vc.batch_id = $2::uuid
  AND vc.status IN ('recorded', 'accepted')
  AND (
    vc.status = 'accepted'
    OR EXISTS (
      SELECT 1
      FROM verification_items vi
      WHERE vi.tenant_id = vc.tenant_id
        AND vi.category = $3
        AND vi.source_submission_id = si.submission_id
        AND (
          (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
          OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
        )
    )
  )`, in.TenantID, in.BatchID, vaccinationProofCategory).Scan(&coveredCount); err != nil {
			return nil, err
		}
		if coveredCount != expectedCount {
			return nil, ports.ErrConflict
		}
	}
	// LATEST VERDICT PER PROOF, the same rule the ready query's latest_proofs CTE applies. The
	// items query above deliberately locks EVERY verification_item for the batch, superseded
	// verdicts included, so a concurrent verdict cannot slip in behind this close. But a superseded
	// verdict must not BLOCK the close: a rejected -> re-shot -> approved animal keeps its historical
	// rejected rows forever, and treating them as outstanding work made the read model and the write
	// path disagree. The ready list offered the drive (it dedupes to the latest verdict) and then
	// close refused it with "batch has unverified or rejected animals" -- moving the defect from
	// "button missing" to "button appears and does nothing", which is strictly worse because
	// leadership cannot tell a broken drive from a broken app. Found in review of 59ba8bac7.
	//
	// Ranking mirrors latest_proofs: identity-bearing row first, then newest close, then item_id.
	// The stamping UPDATE below already filters status='approved', so a superseded rejected row is
	// still never stamped closed (verification_items_closed_approved_check forbids it).
	latestVerdictItems := make(map[string]struct{}, len(items))
	latestRows, err := tx.Query(ctx, `
-- projection-review: membership=verification_items reachable from this batch's live vaccination_completions via sop_submission_items, the same membership the locking query above uses; group_key=(vc.completion_id, vc.goat_id) -- the proof grain, identical to the ready query's latest_proofs DISTINCT ON; join_cardinality=verification_items is one-to-many per completion (reject -> re-shoot -> approve), which is exactly why this DISTINCT ON exists, and both joined sides (sop_submission_items, vaccination_completions) are keyed 1:1 on (tenant_id, item_id) and (tenant_id, sop_submission_item_id) so neither fans the row set out; pagination=none -- this is a whole-batch close decision, never a page, and it is bounded by the batch's own animal count; scope=batch_id equality only, because a close acts on one batch and inherits that batch's park/shed scope rather than re-deriving it
SELECT DISTINCT ON (vc.completion_id, vc.goat_id) vi.item_id::text
FROM verification_items vi
JOIN sop_submission_items si
  ON si.tenant_id = vi.tenant_id
 AND si.submission_id = vi.source_submission_id
 AND (
   (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
   OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
 )
JOIN vaccination_completions vc
  ON vc.tenant_id = vi.tenant_id
 AND vc.sop_submission_item_id = si.item_id
 AND vc.batch_id = $2::uuid
 AND vc.status IN ('recorded', 'accepted')
WHERE vi.tenant_id = $1::uuid
  AND vi.source_submission_id IS NOT NULL
  AND vi.category = $3
ORDER BY vc.completion_id, vc.goat_id,
         (vi.shed_id IS NOT NULL) DESC, vi.closed_at DESC NULLS LAST, vi.item_id DESC`,
		in.TenantID, in.BatchID, vaccinationProofCategory)
	if err != nil {
		return nil, err
	}
	for latestRows.Next() {
		var itemID string
		if scanErr := latestRows.Scan(&itemID); scanErr != nil {
			latestRows.Close()
			return nil, scanErr
		}
		latestVerdictItems[itemID] = struct{}{}
	}
	if err := latestRows.Err(); err != nil {
		latestRows.Close()
		return nil, err
	}
	latestRows.Close()

	allClosed := true
	blocking := make([]string, 0)
	for _, item := range items {
		if _, isLatest := latestVerdictItems[item.ItemID]; !isLatest {
			// Superseded verdict: history, not outstanding work.
			continue
		}
		if item.Status != domain.StatusApproved {
			blocking = append(blocking, blockingSubjectLabel(item))
			continue
		}
		if item.ClosedAt == nil {
			allClosed = false
		}
	}
	// expectedCount/items above only see LIVE vaccination_completions -- a rejected animal's
	// completion is MOVED to vaccination_completion_rejections (migration 000093), so a rejected
	// goat that was never re-scanned has NO live completion and is otherwise invisible to this
	// whole function: it would silently drop out of the drive and let leadership close a batch
	// with real rework still outstanding. Name those animals too.
	reworkRows, err := tx.Query(ctx, `
SELECT DISTINCT vcr.goat_id::text
FROM vaccination_completion_rejections vcr
WHERE vcr.tenant_id = $1::uuid
  AND vcr.batch_id = $2::uuid
  AND NOT EXISTS (
    SELECT 1 FROM vaccination_completions vc2
    WHERE vc2.tenant_id = vcr.tenant_id
      AND vc2.batch_id = vcr.batch_id
      AND vc2.goat_id = vcr.goat_id
      AND vc2.status IN ('recorded', 'accepted')
  )`, in.TenantID, in.BatchID)
	if err != nil {
		return nil, err
	}
	for reworkRows.Next() {
		var goatID string
		if scanErr := reworkRows.Scan(&goatID); scanErr != nil {
			reworkRows.Close()
			return nil, scanErr
		}
		blocking = append(blocking, goatID)
	}
	if err := reworkRows.Err(); err != nil {
		reworkRows.Close()
		return nil, err
	}
	reworkRows.Close()
	if len(blocking) > 0 {
		// Named refusal (maintainer requirement): leadership cannot sign off on a drive with any
		// animal still pending or rejected, and the UI must be able to say WHICH animals are
		// blocking it, not render a bare 409. blockingSubjectLabel prefers the human subject_label
		// captured at verification-item creation, falling back to the raw goat/ref id.
		return nil, &ports.ErrBatchNotFullyVerified{Blocking: blocking}
	}
	if allClosed {
		if err := r.acceptVaccinationBatch(ctx, tx, in.TenantID, in.BatchID, in.ActorID); err != nil {
			return nil, mapWriteErr(err)
		}
		if reservation.proceed {
			if err := completeIdempotency(ctx, tx, in.TenantID, "verification.close-vaccination-batch", in.IdempotencyKey, "vaccination_batch", in.BatchID); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return items, nil
	}
	if !reservation.proceed && reservation.resultID != in.BatchID {
		return nil, ports.ErrConflict
	}

	if _, err := tx.Exec(ctx, `
UPDATE verification_items vi
SET closed_by = $1::uuid, closed_at = now(), row_version = row_version + 1
WHERE vi.tenant_id = $2::uuid
  AND vi.source_submission_id IS NOT NULL
  AND vi.status = 'approved'
  AND vi.closed_at IS NULL
  AND vi.category = $4
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    JOIN sop_submission_items si
      ON si.tenant_id = vc.tenant_id
     AND si.item_id = vc.sop_submission_item_id
    WHERE vc.tenant_id = vi.tenant_id
      AND vc.batch_id = $3::uuid
      AND vc.status IN ('recorded', 'accepted')
      AND si.submission_id = vi.source_submission_id
      AND (
        (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
        OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
      )
  )`, in.ActorID, in.TenantID, in.BatchID, vaccinationProofCategory); err != nil {
		return nil, mapWriteErr(err)
	}
	if err := r.acceptVaccinationBatch(ctx, tx, in.TenantID, in.BatchID, in.ActorID); err != nil {
		return nil, mapWriteErr(err)
	}

	closedRows, err := tx.Query(ctx, `
SELECT `+itemColumns+`
FROM verification_items vi
WHERE vi.tenant_id = $1::uuid
  AND vi.source_submission_id IS NOT NULL
  -- Vaccination proofs ONLY, the same scope as the decision queries and the stamping update above.
  -- This read feeds BOTH the returned items and one EventItemClosed per row, and the stamping
  -- update is category-scoped, so a foreign-category row reached here UNSTAMPED and still had a
  -- close event published for it -- a vaccination close announcing that a weighing proof was
  -- closed, to consumers that would act on it. Found in review of 628eee913, the query the
  -- "all four queries" sweep in that commit missed.
  AND vi.category = $3
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    JOIN sop_submission_items si
      ON si.tenant_id = vc.tenant_id
     AND si.item_id = vc.sop_submission_item_id
    WHERE vc.tenant_id = vi.tenant_id
      AND vc.batch_id = $2::uuid
      AND vc.status IN ('recorded', 'accepted')
      AND si.submission_id = vi.source_submission_id
      AND (
        (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
        OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
      )
  )
ORDER BY vi.captured_at, vi.item_id`, in.TenantID, in.BatchID, vaccinationProofCategory)
	if err != nil {
		return nil, err
	}
	closedItems := make([]domain.Item, 0, len(items))
	for closedRows.Next() {
		item, scanErr := scanItemRow(closedRows)
		if scanErr != nil {
			closedRows.Close()
			return nil, scanErr
		}
		closedItems = append(closedItems, item)
	}
	if err := closedRows.Err(); err != nil {
		closedRows.Close()
		return nil, err
	}
	closedRows.Close()
	for _, item := range closedItems {
		idempotencyKey := fmt.Sprintf("%s:%s:%d", EventItemClosed, item.ItemID, item.RowVersion)
		if err := insertOutboxEvent(ctx, tx, in.TenantID, EventItemClosed, item.ItemID, idempotencyKey, verificationVerdictPayload(item)); err != nil {
			return nil, err
		}
	}
	if err := insertVaccinationDriveClosedOutbox(ctx, tx, in.TenantID, in.BatchID, in.ActorID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, in.TenantID, "verification.close-vaccination-batch", in.IdempotencyKey, "vaccination_batch", in.BatchID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return closedItems, nil
}

func (r *Repository) acceptVaccinationBatch(ctx context.Context, tx pgx.Tx, tenantID, batchID, actorID string) error {
	if _, err := tx.Exec(ctx, `
UPDATE vaccination_completions vc
SET status = 'accepted',
    verified_by = $1::uuid,
    verified_at = now(),
    row_version = vc.row_version + 1,
    updated_at = now()
FROM sop_submission_items si
WHERE vc.tenant_id = $2::uuid
  AND vc.batch_id = $3::uuid
  AND si.tenant_id = vc.tenant_id
  AND vc.sop_submission_item_id = si.item_id
  AND vc.status = 'recorded'
  AND EXISTS (
    SELECT 1
    FROM verification_items vi
    WHERE vi.tenant_id = vc.tenant_id
      AND vi.source_submission_id = si.submission_id
      AND (
        (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
        OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
      )
      AND vi.status = 'approved'
      AND vi.closed_at IS NOT NULL
)`, actorID, tenantID, batchID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
	UPDATE obligation_instances oi
	SET status = 'completed',
	    completed_at = COALESCE(
	      oi.completed_at,
	      (
	        SELECT min(vc.administered_at)
	        FROM vaccination_completions vc
	        WHERE vc.tenant_id = oi.tenant_id
	          AND vc.obligation_id = oi.obligation_id
	          AND vc.status = 'accepted'
	      ),
	      now()
	    ),
	    updated_at = now()
	WHERE oi.tenant_id = $1::uuid
	  AND oi.status IN ('scheduled', 'due', 'in_progress')
	  AND EXISTS (
	    SELECT 1
	    FROM vaccination_completions vc
	    WHERE vc.tenant_id = oi.tenant_id
	      AND vc.obligation_id = oi.obligation_id
	      AND vc.batch_id = $2::uuid
	      AND vc.status = 'accepted'
	  )`, tenantID, batchID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
SELECT obligation_id::text
FROM obligation_instances
WHERE tenant_id = $1::uuid
  AND status = 'completed'
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    WHERE vc.tenant_id = obligation_instances.tenant_id
      AND vc.obligation_id = obligation_instances.obligation_id
      AND vc.batch_id = $2::uuid
      AND vc.status = 'accepted'
  )`, tenantID, batchID)
	if err != nil {
		return err
	}
	obligationIDs := make([]string, 0)
	for rows.Next() {
		var obligationID string
		if err := rows.Scan(&obligationID); err != nil {
			rows.Close()
			return err
		}
		obligationIDs = append(obligationIDs, obligationID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, obligationID := range obligationIDs {
		if _, err := insertVaccinationCompletedOutbox(ctx, tx, tenantID, obligationID); err != nil {
			return err
		}
	}
	if err := r.acceptSOPBatch(ctx, tx, tenantID, batchID, actorID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
UPDATE obligation_batches ob
SET status = 'completed',
    updated_at = now()
WHERE ob.tenant_id = $1::uuid
  AND ob.batch_id = $2::uuid
  AND NOT EXISTS (
    SELECT 1
    FROM obligation_instances oi
    WHERE oi.tenant_id = ob.tenant_id
      AND oi.batch_id = ob.batch_id
      AND oi.status <> 'completed'
  )`, tenantID, batchID)
	return err
}

func (r *Repository) acceptSOPBatch(ctx context.Context, tx pgx.Tx, tenantID, batchID, actorID string) error {
	if _, err := tx.Exec(ctx, `
WITH batch_items AS (
  SELECT DISTINCT si.submission_id, si.task_id, si.item_id
  FROM vaccination_completions vc
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  WHERE vc.tenant_id = $1::uuid
    AND vc.batch_id = $2::uuid
    AND vc.status = 'accepted'
)
UPDATE sop_submission_items si
SET state = 'accepted',
    result = COALESCE(si.result, '{}'::jsonb) || jsonb_build_object(
      'verification_closed_at', now(),
      'verification_closed_by', $3::text
    )
FROM batch_items bi
WHERE si.tenant_id = $1::uuid
  AND si.item_id = bi.item_id
  AND si.state = 'needs_review'`, tenantID, batchID, actorID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
WITH candidate_submissions AS (
  SELECT DISTINCT si.submission_id, si.task_id
  FROM vaccination_completions vc
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  WHERE vc.tenant_id = $1::uuid
    AND vc.batch_id = $2::uuid
    AND vc.status = 'accepted'
)
UPDATE sop_submissions ss
SET state = 'accepted',
    accepted_at = COALESCE(ss.accepted_at, now()),
    row_version = ss.row_version + 1
FROM candidate_submissions cs
WHERE ss.tenant_id = $1::uuid
  AND ss.submission_id = cs.submission_id
  AND ss.state IN ('submitted', 'needs_review')
  AND NOT EXISTS (
    SELECT 1
    FROM sop_submission_items remaining
    WHERE remaining.tenant_id = ss.tenant_id
      AND remaining.submission_id = ss.submission_id
      AND remaining.state NOT IN ('accepted', 'skipped')
  )`, tenantID, batchID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
WITH candidate_tasks AS (
  SELECT DISTINCT si.task_id
  FROM vaccination_completions vc
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  WHERE vc.tenant_id = $1::uuid
    AND vc.batch_id = $2::uuid
    AND vc.status = 'accepted'
)
UPDATE sop_tasks st
SET state = 'accepted',
    verified_by = $3::uuid,
    verified_at = COALESCE(st.verified_at, now()),
    updated_at = now(),
    row_version = st.row_version + 1
FROM candidate_tasks ct
WHERE st.tenant_id = $1::uuid
  AND st.task_id = ct.task_id
  AND st.state IN ('submitted', 'needs_review')
  AND NOT EXISTS (
    SELECT 1
    FROM sop_submission_items remaining
    WHERE remaining.tenant_id = st.tenant_id
      AND remaining.task_id = st.task_id
      AND remaining.state NOT IN ('accepted', 'skipped')
  )`, tenantID, batchID, actorID); err != nil {
		return err
	}
	return nil
}

func (r *Repository) RecordVerdict(ctx context.Context, in domain.Verdict) (domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	defer rollback(ctx, tx)
	reservation, err := reserveIdempotency(ctx, tx, in.TenantID, "verification.verdict", in.IdempotencyKey,
		requestFingerprint(in.ItemID, in.Decision, in.Reason, in.VerifierID, fmt.Sprintf("%d", in.RowVersion)))
	if err != nil {
		return domain.Item{}, err
	}
	if !reservation.proceed {
		item, err := scanItemRow(tx.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", in.TenantID, reservation.resultID))
		if err != nil {
			return domain.Item{}, mapWriteErr(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Item{}, err
		}
		return item, nil
	}

	status := domain.StatusApproved
	if in.Decision == domain.DecisionRejected {
		status = domain.StatusRejected
	}
	var reason any
	if in.Reason != "" {
		reason = in.Reason
	}
	tag, err := tx.Exec(ctx, `
UPDATE verification_items
SET status = $1, verdict_reason = $2, verified_by = $3::uuid, verified_at = now(), row_version = row_version + 1
WHERE tenant_id = $4::uuid
  AND item_id = $5::uuid
  AND row_version = $6
  AND status = 'pending'
  AND closed_at IS NULL
  AND (operator_id IS NULL OR operator_id <> $3::uuid)`,
		status, reason, in.VerifierID, in.TenantID, in.ItemID, in.RowVersion,
	)
	if err != nil {
		return domain.Item{}, mapWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		// Distinguish "does not exist" (404) from "row_version is stale" (409).
		var exists bool
		if scanErr := tx.QueryRow(ctx, "SELECT true FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", in.TenantID, in.ItemID).Scan(&exists); scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return domain.Item{}, ports.ErrNotFound
			}
			return domain.Item{}, scanErr
		}
		// Separate "already decided" from "someone else saved first". Both are refusals and both
		// stay errors.Is(ErrConflict), but only one is worth retrying: a stale row_version means
		// reload and try again, while a terminal verdict means the decision is made and no amount
		// of retrying will change it. Reporting the terminal case as "modified by someone else"
		// sent verifiers hunting for a colleague who never touched the item.
		var currentStatus string
		var closed bool
		if scanErr := tx.QueryRow(ctx,
			"SELECT status, closed_at IS NOT NULL FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid",
			in.TenantID, in.ItemID).Scan(&currentStatus, &closed); scanErr == nil {
			if closed || currentStatus != string(domain.StatusPending) {
				return domain.Item{}, ports.AlreadyDecided(currentStatus)
			}
		}
		return domain.Item{}, ports.ErrConflict
	}

	item, err := scanItemRow(tx.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", in.TenantID, in.ItemID))
	if err != nil {
		return domain.Item{}, mapWriteErr(err)
	}

	eventType := EventVerdictApproved
	if in.Decision == domain.DecisionRejected {
		eventType = EventVerdictRework
	}
	idempotencyKey := fmt.Sprintf("%s:%s:%d", eventType, in.ItemID, item.RowVersion)
	if err := insertOutboxEvent(ctx, tx, in.TenantID, eventType, in.ItemID, idempotencyKey, verificationVerdictPayload(item)); err != nil {
		return domain.Item{}, err
	}
	if in.Decision == domain.DecisionApproved {
		if err := insertVaccinationDriveReadyOutboxIfReady(ctx, tx, item); err != nil {
			return domain.Item{}, err
		}
	}
	if err := completeIdempotency(ctx, tx, in.TenantID, "verification.verdict", in.IdempotencyKey, "verification_item", item.ItemID); err != nil {
		return domain.Item{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Item{}, err
	}
	return item, nil
}

// verificationItemPendingPayload is the verification.item.pending outbox payload. Field set is fixed
// by contract with the notification producer session (build-handover-20260713.md §1 P0 1a "the Max
// seam"): tenant/item identity + classification + WHO to route to (operator/shed/partition/park) + WHEN
// captured, so the notifier can resolve the Verifier's assignment without a callback into this
// module.
func verificationItemPendingPayload(itemID string, in domain.CreateItem) map[string]any {
	return map[string]any{
		"tenant_id":     in.TenantID,
		"item_id":       itemID,
		"vertical":      in.Vertical,
		"module":        in.Module,
		"category":      in.Category,
		"subject_label": derefStr(in.SubjectLabel),
		"source": map[string]any{
			"module":        in.Source.Module,
			"task_id":       derefStr(in.Source.TaskID),
			"submission_id": derefStr(in.Source.SubmissionID),
			"ref_type":      in.Source.RefType,
			"ref_id":        in.Source.RefID,
		},
		"operator_id":     derefStr(in.OperatorID),
		"shed_id":         derefStr(in.ShedID),
		"partition_label": derefStr(in.PartitionLabel),
		"park_id":         derefStr(in.ParkID),
		"captured_at":     in.CapturedAt.UTC().Format(time.RFC3339Nano),
	}
}

// verificationVerdictPayload is the verification.verdict.approved / verification.verdict.rework
// outbox payload. Carries the SAME who-to-route-to fields as the pending payload (operator_id,
// shed_id, partition_label, park_id) plus the decision + reason, so the notifier can apply its own routing (rework ->
// operator + park head; approved -> digest/no-op) without a callback into this module.
//
// It also carries source.evidence_id: the proof the verifier ACTUALLY reviewed, taken from the
// item's own media_refs, which are frozen at CreateItem time and never rewritten (a re-shoot
// withdraws this item and raises a new one -- see weighing's reviseVerificationRound). The
// consuming module has a stale-evidence guard that compares this id against the proof currently
// attached to its record, but the payload never carried an id, so every production verdict reached
// that guard with an empty value and took its backward-compatibility skip. The guard was therefore
// live only in tests: in production a verdict rendered against an older video was applied to
// whatever video happened to be attached when it landed. Naming the evidence here is what makes the
// existing guard real; the consumer needs no second, parallel check.
func verificationVerdictPayload(item domain.Item) map[string]any {
	payload := map[string]any{
		"tenant_id": item.TenantID,
		"item_id":   item.ItemID,
		"vertical":  item.Vertical,
		"module":    item.Module,
		"category":  item.Category,
		"status":    item.Status,
		"decision":  item.Status, // "approved" | "rejected" -- explicit alias, kept alongside status for notifier clarity.
		// C-defect-B (2026-08-04): verificationItemPendingPayload has always carried subject_label
		// (the "Shed · Animal-tag" sentence built at CreateItem time -- see
		// vaccination_submission.go vaccinationAnimalSubjectLabel), but this verdict payload never
		// did. That is why a rejected/approved push named no animal: notificationbridge's
		// VerificationEventPayload.SubjectLabel decoded to "", so handleVerdictRework's own
		// "subject + body" concatenation always took the empty branch. The field exists on the row
		// (verification_items.subject_label, populated by RecordVerdict's own scanItemRow read
		// immediately above) -- it just was not being put on the wire.
		"subject_label":   derefStr(item.SubjectLabel),
		"operator_id":     derefStr(item.OperatorID),
		"shed_id":         derefStr(item.ShedID),
		"partition_label": derefStr(item.PartitionLabel),
		"park_id":         derefStr(item.ParkID),
		"source": map[string]any{
			"module":        item.Source.Module,
			"task_id":       derefStr(item.Source.TaskID),
			"submission_id": derefStr(item.Source.SubmissionID),
			"ref_type":      item.Source.RefType,
			"ref_id":        item.Source.RefID,
			// The PRIMARY proof only. Producers that attach several artefacts to one item
			// (a lump-sum shed submission) put the observation's own proof_artifact_id
			// first -- that is the single id the producing module stores on its record and
			// can compare against, so a list here would give the consumer nothing to match.
			"evidence_id": firstMediaRef(item.MediaRefs),
		},
	}
	if item.VerdictReason != nil {
		payload["reason"] = *item.VerdictReason
	}
	if item.VerifiedBy != nil {
		payload["verified_by"] = *item.VerifiedBy
	}
	if item.ClosedBy != nil {
		payload["closed_by"] = *item.ClosedBy
	}
	if item.ClosedAt != nil {
		payload["closed_at"] = item.ClosedAt.UTC().Format(time.RFC3339Nano)
	}
	return payload
}

// insertOutboxEvent inserts one outbox_messages row in the SAME transaction as the caller's state
// change, mirroring the vaccination.completed precedent
// (backend/internal/vaccination/adapters/postgres/repository.go:insertVaccinationCompletedOutbox).
// Idempotent: ON CONFLICT on the partial unique index for verification event types is a no-op.
func insertOutboxEvent(ctx context.Context, tx pgx.Tx, tenantID, eventType, aggregateID, idempotencyKey string, payload map[string]any) error {
	eventID := platformoutbox.DeterministicUUID(eventType + ":" + tenantID + ":" + idempotencyKey)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": verificationSchemaVer,
		"schema_ref":     verificationSchemaRef + "#" + eventType,
		"aggregate_type": "verification_item",
		"aggregate_id":   aggregateID,
		"occurred_at":    now,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "verification",
		},
		"idempotency_key": idempotencyKey,
		// A verdict/pending record is a system-rule transition (the module writes the outbox row in
		// the same transaction as the state change), not a direct human actor keystroke.
		"actor": map[string]any{
			"actor_type": "system_rule",
			"actor_ref":  "verification",
		},
		"subject_type": "verification_item",
		"subject_id":   aggregateID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
		},
		"evidence_refs": []any{},
		"payload":       payload,
		"trace_id":      idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("verification: outbox envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "verification",
		"schema_version":  verificationSchemaVer,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("verification: outbox headers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'verification_item', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
)
ON CONFLICT (tenant_id, idempotency_key) WHERE event_type IN (
  'verification.item.pending', 'verification.verdict.approved', 'verification.verdict.rework',
  'verification.item.closed'
) DO NOTHING`,
		tenantID, eventID, eventType, verificationSchemaVer, aggregateID,
		verificationTopic, envelope, headers, idempotencyKey); err != nil {
		return fmt.Errorf("verification: outbox insert: %w", err)
	}
	return nil
}

func insertVaccinationCompletedOutbox(ctx context.Context, tx pgx.Tx, tenantID, obligationID string) (bool, error) {
	eventID := platformoutbox.DeterministicUUID("vaccination.completed:" + tenantID + ":" + obligationID)
	idempotencyKey := "vaccination.completed:" + obligationID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload := map[string]any{
		"tenant_id":     tenantID,
		"obligation_id": obligationID,
		"status":        "completed",
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     vaccinationCompletedEventType,
		"schema_version": vaccinationCompletedSchemaVersion,
		"schema_ref":     vaccinationCompletedSchemaRef,
		"aggregate_type": "obligation_instance",
		"aggregate_id":   obligationID,
		"occurred_at":    now,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "vaccination",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "system_rule",
			"actor_id":   nil,
			"actor_ref":  nil,
		},
		"subject_type": "obligation_instance",
		"subject_id":   obligationID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "obligation_status_event",
			"evidence_id":   obligationID + ":completed",
		}},
		"payload":  payload,
		"trace_id": idempotencyKey,
	})
	if err != nil {
		return false, fmt.Errorf("vaccination completed envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "verification.CloseSubmission",
		"schema_version":  vaccinationCompletedSchemaVersion,
		"obligation_id":   obligationID,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return false, fmt.Errorf("vaccination completed headers: %w", err)
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'obligation_instance', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
)
ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = 'vaccination.completed' DO NOTHING`,
		tenantID, eventID, vaccinationCompletedEventType, vaccinationCompletedSchemaVersion,
		obligationID, vaccinationCompletedTopic, envelope, headers, idempotencyKey)
	if err != nil {
		return false, fmt.Errorf("vaccination completed outbox: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func insertVaccinationDriveReadyOutboxIfReady(ctx context.Context, tx pgx.Tx, item domain.Item) error {
	if item.Source.SubmissionID == nil || (item.Source.RefType != "sop_submission" && item.Source.RefType != "vaccination_goat") {
		return nil
	}
	var batchID string
	var expectedCount, proofCount, approvedCount, rejectedCount, pendingCount int
	if err := tx.QueryRow(ctx, `
WITH item_batch AS (
  SELECT vc.batch_id
  FROM vaccination_completions vc
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  WHERE vc.tenant_id = $1::uuid
    AND vc.status IN ('recorded', 'accepted')
    AND si.submission_id = $2::uuid
    AND (
      ($4 = 'sop_submission' AND si.submission_id = $3::uuid)
      OR ($4 = 'vaccination_goat' AND si.goat_id = $3::uuid)
    )
  LIMIT 1
),
expected AS (
  -- projection-review: membership=active recorded/accepted vaccination_completions for the one batch resolved from item_batch; group_key=batch_id; join_cardinality=item_batch is one row and each active completion contributes exactly one count, while rejected/reversed audit attempts are excluded; pagination=n/a readiness check for one verdict; scope=tenant+batch from the verified submission item
  SELECT COUNT(*)::int AS total_count
  FROM vaccination_completions vc
  JOIN item_batch ib ON ib.batch_id = vc.batch_id
  WHERE vc.tenant_id = $1::uuid
    AND vc.status IN ('recorded', 'accepted')
),
proofs AS (
  SELECT vi.status
  FROM item_batch ib
  JOIN vaccination_completions vc
    ON vc.tenant_id = $1::uuid
   AND vc.batch_id = ib.batch_id
   AND vc.status IN ('recorded', 'accepted')
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  JOIN verification_items vi
    ON vi.tenant_id = vc.tenant_id
   AND vi.source_submission_id = si.submission_id
   AND (
     (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
     OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
   )
   AND vi.closed_at IS NULL
)
SELECT ib.batch_id::text,
       e.total_count,
       COUNT(p.status)::int,
       COUNT(*) FILTER (WHERE p.status = 'approved')::int,
       COUNT(*) FILTER (WHERE p.status = 'rejected')::int,
       COUNT(*) FILTER (WHERE p.status = 'pending')::int
FROM item_batch ib
CROSS JOIN expected e
LEFT JOIN proofs p ON true
GROUP BY ib.batch_id, e.total_count`,
		item.TenantID, *item.Source.SubmissionID, item.Source.RefID, item.Source.RefType).Scan(
		&batchID, &expectedCount, &proofCount, &approvedCount, &rejectedCount, &pendingCount,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if expectedCount == 0 || proofCount != expectedCount || approvedCount != expectedCount || rejectedCount != 0 || pendingCount != 0 {
		return nil
	}
	return insertVaccinationDriveLifecycleOutbox(ctx, tx, item.TenantID, batchID, derefStr(item.ParkID), "", EventVaccinationDriveReady, "ready")
}

func insertVaccinationDriveClosedOutbox(ctx context.Context, tx pgx.Tx, tenantID, batchID, actorID string) error {
	return insertVaccinationDriveLifecycleOutbox(ctx, tx, tenantID, batchID, "", actorID, EventVaccinationDriveClosed, "closed")
}

func insertVaccinationDriveLifecycleOutbox(ctx context.Context, tx pgx.Tx, tenantID, batchID, parkID, actorID, eventType, status string) error {
	idempotencyKey := eventType + ":" + batchID
	eventID := platformoutbox.DeterministicUUID(idempotencyKey + ":" + tenantID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload := map[string]any{
		"tenant_id": tenantID,
		"batch_id":  batchID,
		"park_id":   parkID,
		"status":    status,
	}
	if actorID != "" {
		payload["closed_by"] = actorID
	}
	actorType := "system_rule"
	var actorValue any
	if actorID != "" {
		actorType = "human"
		actorValue = actorID
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": verificationSchemaVer,
		"schema_ref":     verificationSchemaRef,
		"aggregate_type": "vaccination_batch",
		"aggregate_id":   batchID,
		"occurred_at":    now,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "verification",
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": actorType,
			"actor_id":   actorValue,
		},
		"subject_type": "vaccination_batch",
		"subject_id":   batchID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
		},
		"evidence_refs": []any{},
		"payload":       payload,
		"trace_id":      idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("verification: drive lifecycle envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "verification.vaccination_drive",
		"schema_version":  verificationSchemaVer,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("verification: drive lifecycle headers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'vaccination_batch', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
)
ON CONFLICT (event_id) DO NOTHING`,
		tenantID, eventID, eventType, verificationSchemaVer, batchID,
		verificationTopic, envelope, headers, idempotencyKey); err != nil {
		return fmt.Errorf("verification: drive lifecycle outbox insert: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanItemRow(row rowScanner) (domain.Item, error) {
	return scanItem(row)
}

func scanItem(row rowScanner) (domain.Item, error) {
	var (
		item                                                                            domain.Item
		sourceTaskID, sourceSubmissionID                                                *string
		operatorID, shedID, partitionLabel, parkID, verifiedBy, closedBy, verdictReason *string
		subjectLabel, subjectNote                                                       *string
		mediaJSON, contextJSON                                                          []byte
		appliedByModule                                                                 *string
		verifiedAt, closedAt, appliedAt                                                 *time.Time
	)
	if err := row.Scan(
		&item.ItemID, &item.TenantID, &item.Vertical, &item.Module, &item.Category,
		&item.Source.Module, &sourceTaskID, &sourceSubmissionID, &item.Source.RefType, &item.Source.RefID,
		&subjectLabel, &subjectNote, &mediaJSON, &contextJSON, &item.Status, &verdictReason, &operatorID, &shedID, &partitionLabel, &parkID,
		&item.CapturedAt, &verifiedBy, &verifiedAt, &closedBy, &closedAt,
		&item.ApplierAckExpected, &appliedAt, &appliedByModule,
		&item.RowVersion, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return domain.Item{}, err
	}
	item.Source.TaskID = sourceTaskID
	item.Source.SubmissionID = sourceSubmissionID
	item.VerdictReason = verdictReason
	item.SubjectLabel = subjectLabel
	item.SubjectNote = subjectNote
	item.OperatorID = operatorID
	item.ShedID = shedID
	item.PartitionLabel = partitionLabel
	item.ParkID = parkID
	item.VerifiedBy = verifiedBy
	item.VerifiedAt = verifiedAt
	item.ClosedBy = closedBy
	item.ClosedAt = closedAt
	item.AppliedAt = appliedAt
	item.AppliedByModule = appliedByModule
	item.CapturedAt = item.CapturedAt.UTC()
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if len(mediaJSON) > 0 {
		if err := json.Unmarshal(mediaJSON, &item.MediaRefs); err != nil {
			return domain.Item{}, fmt.Errorf("verification: unmarshal media_refs: %w", err)
		}
	}
	if len(contextJSON) > 0 {
		if err := json.Unmarshal(contextJSON, &item.ContextRows); err != nil {
			return domain.Item{}, fmt.Errorf("verification: unmarshal context_rows: %w", err)
		}
	}
	return item, nil
}

func scanItemWithLabels(row rowScanner) (domain.Item, error) {
	var (
		item                                                                            domain.Item
		sourceTaskID, sourceSubmissionID                                                *string
		operatorID, shedID, partitionLabel, parkID, verifiedBy, closedBy, verdictReason *string
		subjectLabel, subjectNote                                                       *string
		operatorName, verifiedByName, shedLabel, parkLabel                              *string
		appliedByModule                                                                 *string
		mediaJSON, contextJSON                                                          []byte
		verifiedAt, closedAt, appliedAt                                                 *time.Time
	)
	if err := row.Scan(
		&item.ItemID, &item.TenantID, &item.Vertical, &item.Module, &item.Category,
		&item.Source.Module, &sourceTaskID, &sourceSubmissionID, &item.Source.RefType, &item.Source.RefID,
		&subjectLabel, &subjectNote, &mediaJSON, &contextJSON, &item.Status, &verdictReason, &operatorID, &shedID, &partitionLabel, &parkID,
		&item.CapturedAt, &verifiedBy, &verifiedAt, &closedBy, &closedAt,
		&item.ApplierAckExpected, &appliedAt, &appliedByModule,
		&item.RowVersion, &item.CreatedAt, &item.UpdatedAt,
		&operatorName, &verifiedByName, &shedLabel, &parkLabel,
	); err != nil {
		return domain.Item{}, err
	}
	item.Source.TaskID = sourceTaskID
	item.Source.SubmissionID = sourceSubmissionID
	item.VerdictReason = verdictReason
	item.SubjectLabel = subjectLabel
	item.SubjectNote = subjectNote
	item.OperatorID = operatorID
	item.OperatorName = operatorName
	item.ShedID = shedID
	item.ShedLabel = shedLabel
	item.PartitionLabel = partitionLabel
	item.ParkID = parkID
	item.ParkLabel = parkLabel
	item.VerifiedBy = verifiedBy
	item.VerifiedByName = verifiedByName
	item.VerifiedAt = verifiedAt
	item.ClosedBy = closedBy
	item.ClosedAt = closedAt
	item.AppliedAt = appliedAt
	item.AppliedByModule = appliedByModule
	item.CapturedAt = item.CapturedAt.UTC()
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if len(mediaJSON) > 0 {
		if err := json.Unmarshal(mediaJSON, &item.MediaRefs); err != nil {
			return domain.Item{}, fmt.Errorf("verification: unmarshal media_refs: %w", err)
		}
	}
	if len(contextJSON) > 0 {
		if err := json.Unmarshal(contextJSON, &item.ContextRows); err != nil {
			return domain.Item{}, fmt.Errorf("verification: unmarshal context_rows: %w", err)
		}
	}
	return item, nil
}

// firstMediaRef is the item's primary proof id, or "" for an item raised with no media at
// all. Empty stays empty rather than becoming a sentinel: the consumer's guard already has a
// defined meaning for an absent evidence id, and inventing a placeholder would make a
// media-less item look like a mismatch against every record.
func firstMediaRef(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	return refs[0]
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// nonNilContextRows keeps a nil slice out of the jsonb column: encoding/json writes nil as `null`,
// which fails the column's jsonb_typeof = 'array' CHECK. An empty array round-trips as "no context".
// Rows with a blank label AND a blank value are dropped -- an empty row renders as a stray divider
// on the verifier's screen and states nothing.
func nonNilContextRows(rows []domain.ContextRow) []domain.ContextRow {
	out := make([]domain.ContextRow, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Label) == "" && strings.TrimSpace(row.Value) == "" {
			continue
		}
		out = append(out, row)
	}
	return out
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func mapWriteErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ports.ErrConflict
		case "23503", "23514", "22P02":
			return ports.ErrConflict
		}
	}
	return err
}

// WithdrawItemsBySource retires the PENDING items raised for source records the
// producing module has superseded. It is deliberately status='pending'-only and
// verdict-free: an approved/rejected item is a decision that already happened and
// stays exactly as it was, and a withdrawn item records no verdict, no verifier and
// no verdict_reason because nobody decided anything.
//
// Every actionable path in this repository is already gated on status='pending'
// (the RecordVerdict UPDATE, the queue's status filter, the pending/approved/
// rejected roll-ups), so 'withdrawn' drops the item out of all of them at once.
//
// A withdrawal is NOT silent. Every item retired here already published
// verification.item.pending when it was raised, and consumers acted on it -- the
// notification bridge turned it into a push telling a verifier to go review the
// proof. Retiring the item with a bare UPDATE left those consumers holding work
// that no longer exists. The withdrawal therefore publishes
// verification.item.closed, the module's existing "this item is no longer
// decidable" event, on the SAME transaction as the status change (state change +
// outbox are one unit). It carries status/decision='withdrawn' so a consumer can
// tell a retraction from a verdict; a withdrawn item has no verifier and no
// reason, so verificationVerdictPayload simply omits those fields.
//
// A new event TYPE was deliberately not minted: verification.item.closed already
// carries the identical routing fields, already has a registered consumer, and is
// already enumerated in the outbox partial unique index that makes these inserts
// idempotent. Reusing it keeps the retraction inside the existing contract instead
// of adding a fourth lifecycle event that means the same thing.
//
// Replay-safe twice over: the UPDATE only matches status='pending', so a second
// withdrawal of an already-withdrawn item matches no rows and publishes nothing,
// and the event's idempotency key is versioned on the post-update row_version, so
// even a retried transaction collides on that index and no-ops.
func (r *Repository) WithdrawItemsBySource(ctx context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string) (int, error) {
	if len(sourceRefIDs) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, mapWriteErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
UPDATE verification_items
SET status = 'withdrawn', row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid
  AND source_module = $2
  AND source_ref_type = $3
  AND source_ref_id = ANY($4::uuid[])
  AND status = 'pending'
RETURNING `+itemColumns, tenantID, sourceModule, sourceRefType, sourceRefIDs)
	if err != nil {
		return 0, mapWriteErr(err)
	}
	withdrawn := make([]domain.Item, 0, len(sourceRefIDs))
	for rows.Next() {
		item, scanErr := scanItemRow(rows)
		if scanErr != nil {
			rows.Close()
			return 0, scanErr
		}
		withdrawn = append(withdrawn, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, mapWriteErr(err)
	}
	rows.Close()

	for _, item := range withdrawn {
		idempotencyKey := fmt.Sprintf("%s:%s:%d", EventItemClosed, item.ItemID, item.RowVersion)
		if err := insertOutboxEvent(ctx, tx, tenantID, EventItemClosed, item.ItemID, idempotencyKey, verificationVerdictPayload(item)); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, mapWriteErr(err)
	}
	return len(withdrawn), nil
}

// MarkVerdictApplied is the producing module's RECEIPT that it wrote a verdict
// outcome onto its own record. It is the second half of the ack protocol whose
// first half is CreateItem's applier_ack_expected.
//
// Why this exists (W-18): verdicts are applied asynchronously. RecordVerdict
// flips status pending -> approved/rejected and enqueues verification.verdict.*;
// the producing module's applier consumes that on the DURABLE bus (cmd/outbox-relay,
// cmd/domain-event-consumer) -- the API's in-process bus deliberately does not
// receive it. So the verifier's queue emptied the instant the verdict was
// SUBMITTED, while the farm's records only changed when it was APPLIED. With the
// relay stopped, lagging, or the event dead-lettered, those are not the same
// event and there was no signal anywhere that said so: the queue was empty and
// nothing had happened. This stamp is what lets a surface tell the two apart.
//
// It writes NOTHING about the outcome itself -- no status, no verdict, no reason.
// The applier remains the single writer of the verdict's effect on its own
// module; this is a downstream receipt of that write, never a parallel copy of
// it. Attempting to derive outcome state from here would be the second writer
// the design exists to avoid.
//
// Called AFTER the applier's own transaction commits, deliberately not inside it:
// the two live in different databases-of-record conceptually and a crash between
// them must fail SAFE. It does: the item stays in VerdictStateApplying, which
// reads as "not confirmed yet" -- visibly wrong rather than invisibly wrong -- and
// the at-least-once redelivery of the same verdict event re-runs the applier
// (idempotent on the event id) and re-attempts this stamp.
//
// Replay-safe: the UPDATE matches only rows not yet acked, so a redelivery
// matches nothing, returns 0, and preserves the ORIGINAL applied_at rather than
// advancing it to a later instant that never corresponded to a real application.
// It publishes no event -- an ack is the end of a chain, not a new fact for
// anyone else to consume, and minting an event with no consumer is exactly the
// silent drop this whole bug was.
func (r *Repository) MarkVerdictApplied(
	ctx context.Context,
	tenantID, sourceModule, sourceRefType string,
	sourceRefIDs []string,
	appliedByModule string,
) (int, error) {
	if len(sourceRefIDs) == 0 {
		return 0, nil
	}
	if strings.TrimSpace(appliedByModule) == "" {
		// The CHECK constraint enforces this in the database too; failing here
		// keeps the error a caller-fixable one rather than a constraint violation.
		return 0, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
UPDATE verification_items
SET applied_at = now(), applied_by_module = $5, updated_at = now()
WHERE tenant_id = $1::uuid
  AND source_module = $2
  AND source_ref_type = $3
  AND source_ref_id = ANY($4::uuid[])
  AND status <> 'pending'
  AND applied_at IS NULL`, tenantID, sourceModule, sourceRefType, sourceRefIDs, appliedByModule)
	if err != nil {
		return 0, mapWriteErr(err)
	}
	return int(tag.RowsAffected()), nil
}
