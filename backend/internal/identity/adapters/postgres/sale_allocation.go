package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// Sale-allocation reads. The WRITE lives in sale_allocation_write.go.
//
// NO VERDICT IS COMPUTED HERE. These queries fetch the gate's inputs -- lifecycle,
// management stage, withdrawal date, an existing live tagging -- and hand them to
// identity/domain.ResolveSaleBlocker in the app layer. Deciding sellability in SQL would
// create a second gate that has to be kept in step with the Go one by hand, and the two
// drifting is how a quarantined animal becomes sellable on one path only.

// saleCandidateSelect is the shared projection behind BOTH candidate reads: the picker's
// paged list and the by-ids read the confirm makes.
//
// It is ONE string rather than two similar queries on purpose. The confirm re-judges
// against exactly the facts the picker showed, so any column that appears in one and not
// the other is a way for the two to disagree about the same animal.
//
// The joins, and why each is 0..1 per goat (so none of them fans the row set out):
//
//	gsp   goat_shed_partitions   PK (tenant_id, goat_id)             -> exactly 0..1
//	sh    locations              PK location_id                      -> exactly 0..1
//	pk    locations              PK location_id                      -> exactly 0..1
//	tag   LATERAL ... LIMIT 1    one identifier by explicit priority  -> exactly 0..1
//	wd    LATERAL max(...)       aggregate over completions          -> exactly 1
//	alloc LATERAL ... LIMIT 1    live allocation, unique per goat     -> exactly 0..1
//
// The two LATERALs that use LIMIT 1 are ranking rows that are NOT interchangeable -- the
// identifier priority is explicit and total, so the pick is deterministic, not an
// arbitrary "any row will do" that would silently flip between requests.
const saleCandidateSelect = `
SELECT g.goat_id::text,
       COALESCE(g.display_id, ''),
       COALESCE(tag.identifier_value, ''),
       COALESCE(g.park_id::text, ''),
       COALESCE(NULLIF(pk.location_code, ''), pk.name, ''),
       COALESCE(g.shed_id::text, ''),
       COALESCE(sh.name, ''),
       -- 'whole' is the unpartitioned sentinel and is a MATCHING KEY, never user copy.
       -- Blanking it here is what stops "Yashoda whole" reaching a screen.
       COALESCE(NULLIF(gsp.partition_label, 'whole'), ''),
       COALESCE(g.breed, ''),
       COALESCE(g.sex, ''),
       g.lifecycle_status,
       COALESCE(g.management_stage, ''),
       g.row_version,
       g.merged_into_goat_id IS NOT NULL,
       COALESCE(to_char(wd.withdrawal_until, 'YYYY-MM-DD'), ''),
       COALESCE(alloc.sales_deal_id::text, '')
FROM goats g
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
LEFT JOIN locations sh ON sh.location_id = g.shed_id AND sh.tenant_id = g.tenant_id
LEFT JOIN locations pk ON pk.location_id = g.park_id AND pk.tenant_id = g.tenant_id
LEFT JOIN LATERAL (
  -- The identifier a person reads off the animal, by the REAL type vocabulary:
  -- goat_identifiers_type_check admits only animal_identifier_1, animal_identifier_2 and
  -- temporary_tag. animal_identifier_1 is the primary tag (the 15-digit RFID on this
  -- tenant), _2 the secondary, and a temporary_tag is a stand-in that should lose to
  -- either real one.
  --
  -- An earlier version of this CASE ranked 'rfid'/'visual_tag'/'tag' -- values the CHECK
  -- constraint does not permit -- so every branch fell through to ELSE and the priority
  -- did nothing at all. Ranking by a vocabulary the column cannot hold is dead code that
  -- reads as a rule.
  SELECT gi.identifier_value
  FROM goat_identifiers gi
  WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id
    AND gi.status = 'active' AND gi.valid_to IS NULL
  ORDER BY CASE gi.identifier_type
             WHEN 'animal_identifier_1' THEN 0
             WHEN 'animal_identifier_2' THEN 1
             WHEN 'temporary_tag' THEN 2
             ELSE 3
           END,
           gi.is_primary_for_goat DESC,
           gi.valid_from DESC,
           gi.identifier_id
  LIMIT 1
) tag ON TRUE
LEFT JOIN LATERAL (
  -- The LATEST medicine withdrawal recorded against this animal. max() over an
  -- aggregate, never ORDER BY ... LIMIT 1: several accepted completions can carry
  -- withdrawal dates and the animal is clear only after the LAST of them.
  --
  -- Only ACCEPTED completions count. A rejected or reversed completion is not a dose the
  -- animal received, and holding an animal back for a dose nobody gave would be a
  -- fabricated blocker.
  SELECT max(vc.withdrawal_until_date) AS withdrawal_until
  FROM vaccination_completions vc
  WHERE vc.tenant_id = g.tenant_id AND vc.goat_id = g.goat_id
    AND vc.status = 'accepted' AND vc.withdrawal_until_date IS NOT NULL
) wd ON TRUE
LEFT JOIN LATERAL (
  -- A LIVE tagging on some OTHER sale. $EXCLUDE_DEAL is this deal, excluded so a repeated
  -- confirm of the same sale reads as a no-op rather than colliding with itself.
  SELECT gsa.sales_deal_id
  FROM goat_sale_allocations gsa
  WHERE gsa.tenant_id = g.tenant_id AND gsa.goat_id = g.goat_id
    AND gsa.status = 'tagged'
    AND (NULLIF($EXCLUDE_DEAL, '')::uuid IS NULL
         OR gsa.sales_deal_id <> NULLIF($EXCLUDE_DEAL, '')::uuid)
  LIMIT 1
) alloc ON TRUE`

func scanSaleCandidateRow(scan func(...any) error) (ports.SaleCandidateRow, error) {
	var row ports.SaleCandidateRow
	err := scan(
		&row.GoatID, &row.DisplayID, &row.TagNumber,
		&row.ParkID, &row.ParkName, &row.ShedID, &row.ShedName, &row.PartitionLabel,
		&row.Breed, &row.Sex,
		&row.State.LifecycleStatus, &row.State.ManagementStage, &row.State.RowVersion,
		&row.State.Merged, &row.State.WithdrawalUntil, &row.State.TaggedToOtherDealID,
	)
	if err != nil {
		return ports.SaleCandidateRow{}, err
	}
	row.State.GoatID = row.GoatID
	row.State.Exists = true
	return row, nil
}

// ListSaleCandidates is the picker's paged read.
//
// KEYSET, NOT OFFSET: ordered by (goat_id) with the cursor as a strict lower bound, so
// page N costs the same as page 1 no matter how large the pen is. goat_id is unique, so
// the order is total and no row can be skipped or repeated across pages.
func (r *Repository) ListSaleCandidates(ctx context.Context, params ports.ListSaleCandidatesParams) ([]ports.SaleCandidateRow, *string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	limit := params.Limit
	if limit <= 0 || limit > ports.SaleCandidatePageSize {
		limit = ports.SaleCandidatePageSize
	}

	// $6 is the exclude-deal placeholder the shared projection expects. The picker
	// judges against EVERY live tagging, including this deal's own, so an animal already
	// tagged shows as such instead of looking pickable twice.
	q := strings.ReplaceAll(saleCandidateSelect, "$EXCLUDE_DEAL", "''") + `
WHERE g.tenant_id = $1::uuid
  AND g.park_id = $2::uuid
  -- NULLIF, not a bare cast. An OR does NOT stop Postgres evaluating the cast of a
  -- constant, so comparing the raw parameter to an empty string on one side and casting
  -- it to uuid on the other fails outright with 'invalid input syntax for type uuid'
  -- whenever the filter is absent. Casting NULL is always safe. Found by running the
  -- endpoint, not by a unit test -- which is why the picker now has a DB test too.
  AND (NULLIF($3, '')::uuid IS NULL OR g.shed_id = NULLIF($3, '')::uuid)
  -- Empty label list means every pen of the shed. The comparison is on the catalog's
  -- normalized key, so a caller may pass '3' or 'Part 3' and mean the same pen.
  -- COALESCE, because a nil Go slice encodes as SQL NULL, not an empty array, and
  -- cardinality(NULL) is NULL rather than 0 -- which makes the whole predicate NULL and
  -- silently returns an EMPTY PICKER. The Go side also normalizes to a non-nil slice; both
  -- are kept because either alone would leave the other a trap for the next caller.
  AND (COALESCE(cardinality($4::text[]), 0) = 0
       OR lower(regexp_replace(COALESCE(NULLIF(gsp.partition_label, 'whole'), ''), '^\s*-\s*part\s*|\s+', '', 'gi'))
          = ANY(SELECT lower(regexp_replace(x, '^\s*-\s*part\s*|\s+', '', 'gi')) FROM unnest($4::text[]) AS x))
  -- SUBSTRING, not prefix. An operator reading a tag off an animal works from whatever
  -- part of the number is legible -- the middle three digits, the last four -- so a
  -- prefix-only match finds nothing and reads as a broken search box.
  --
  -- The identifier side is an EXISTS against goat_identifiers rather than a filter on the
  -- display LATERAL's output, which matters for more than tidiness: that LATERAL picks ONE
  -- identifier per animal by display priority, so filtering it would miss an animal whose
  -- OLD tag is the number being searched, and it could use no index either way. The EXISTS
  -- searches every active identifier the animal holds and is served by the pg_trgm GIN
  -- index added in 000178 -- which is what makes a leading wildcard indexable rather than
  -- the banned non-SARGable scan.
  AND ($5::text = ''
       OR g.display_id ILIKE '%' || $5::text || '%'
       OR EXISTS (SELECT 1 FROM goat_identifiers gs
                   WHERE gs.tenant_id = g.tenant_id AND gs.goat_id = g.goat_id
                     AND gs.status = 'active' AND gs.valid_to IS NULL
                     AND gs.identifier_value ILIKE '%' || $5::text || '%'))
  AND (NULLIF($6, '')::uuid IS NULL OR g.goat_id > NULLIF($6, '')::uuid)
ORDER BY g.goat_id
LIMIT $7`

	labels := params.PartitionLabels
	if labels == nil {
		labels = []string{}
	}
	rows, err := r.pool.Query(ctx, q,
		params.TenantID, params.ParkID, strings.TrimSpace(params.ShedID), labels,
		params.Query, strings.TrimSpace(params.Cursor), limit+1)
	if err != nil {
		return nil, nil, fmt.Errorf("identity: list sale candidates: %w", err)
	}
	defer rows.Close()

	out := make([]ports.SaleCandidateRow, 0, limit)
	for rows.Next() {
		row, err := scanSaleCandidateRow(rows.Scan)
		if err != nil {
			return nil, nil, fmt.Errorf("identity: scan sale candidate: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("identity: list sale candidates: %w", err)
	}

	// The extra row is the "is there more" probe and is never returned to the caller.
	var cursor *string
	if len(out) > limit {
		out = out[:limit]
		next := out[len(out)-1].GoatID
		cursor = &next
	}
	return out, cursor, nil
}

// ReadSaleCandidateRows reads a bounded named set in ONE query.
func (r *Repository) ReadSaleCandidateRows(ctx context.Context, tenantID, excludeDealID string, goatIDs []string) (map[string]ports.SaleCandidateRow, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	out := make(map[string]ports.SaleCandidateRow, len(goatIDs))
	if len(goatIDs) == 0 {
		return out, nil
	}
	q := strings.ReplaceAll(saleCandidateSelect, "$EXCLUDE_DEAL", "$3") + `
WHERE g.tenant_id = $1::uuid AND g.goat_id = ANY($2::uuid[])`

	rows, err := r.pool.Query(ctx, q, tenantID, goatIDs, strings.TrimSpace(excludeDealID))
	if err != nil {
		return nil, fmt.Errorf("identity: read sale candidates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		row, err := scanSaleCandidateRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("identity: scan sale candidate: %w", err)
		}
		out[row.GoatID] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: read sale candidates: %w", err)
	}
	return out, nil
}

// ListSaleAllocations returns the animals tagged to one deal, shed-wise.
//
// It reads the SNAPSHOT columns on the allocation row, not the goat's current location.
// A sold animal's goats.shed_id keeps moving (or is cleared), so reading it back would
// make an old sale silently re-describe itself as having come from wherever the animal
// sits today.
//
// projection-review: membership=one row per goat tagged to this deal; group_key=(shed_id, partition_label) from the allocation SNAPSHOT; join_cardinality=no joins, single-table aggregate; pagination=NONE, bounded by MaxSaleAllocationGoatsPerCommand per confirm; scope=tenant_id + sales_deal_id
func (r *Repository) ListSaleAllocations(ctx context.Context, tenantID, salesDealID string) ([]ports.SaleAllocationShedGroup, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.listSaleAllocations(ctx, r.pool, tenantID, salesDealID)
}

// listSaleAllocationsInTx is the same read inside a transaction the caller owns, so the
// confirm can read back what it just wrote without leaving its own transaction -- and
// therefore without being able to see a half-applied sale.
func (r *Repository) listSaleAllocationsInTx(ctx context.Context, tx pgx.Tx, tenantID, salesDealID string) ([]ports.SaleAllocationShedGroup, error) {
	return r.listSaleAllocations(ctx, tx, tenantID, salesDealID)
}

// saleAllocationQuerier is satisfied by both the pool and a transaction.
type saleAllocationQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (r *Repository) listSaleAllocations(ctx context.Context, q saleAllocationQuerier, tenantID, salesDealID string) ([]ports.SaleAllocationShedGroup, error) {
	rows, err := q.Query(ctx, `
SELECT COALESCE(NULLIF(pk.location_code, ''), pk.name, ''),
       COALESCE(a.shed_id::text, ''),
       COALESCE(sh.name, ''),
       COALESCE(a.partition_label, ''),
       count(*)::int,
       COALESCE(array_agg(a.tag_number ORDER BY a.tag_number)
                  FILTER (WHERE COALESCE(a.tag_number, '') <> ''), '{}')
FROM goat_sale_allocations a
LEFT JOIN locations sh ON sh.location_id = a.shed_id AND sh.tenant_id = a.tenant_id
LEFT JOIN locations pk ON pk.location_id = a.park_id AND pk.tenant_id = a.tenant_id
WHERE a.tenant_id = $1::uuid AND a.sales_deal_id = $2::uuid AND a.status = 'tagged'
GROUP BY pk.location_code, pk.name, a.shed_id, sh.name, a.partition_label
ORDER BY 1, 3, 4`, tenantID, salesDealID)
	if err != nil {
		return nil, fmt.Errorf("identity: list sale allocations: %w", err)
	}
	defer rows.Close()

	out := []ports.SaleAllocationShedGroup{}
	for rows.Next() {
		var g ports.SaleAllocationShedGroup
		if err := rows.Scan(&g.ParkName, &g.ShedID, &g.ShedName, &g.PartitionLabel,
			&g.Animals, &g.TagNumbers); err != nil {
			return nil, fmt.Errorf("identity: scan sale allocation group: %w", err)
		}
		// Composed through the canonical helper, never hand-rolled in SQL: it is the one
		// place that knows a numeric pen joins with a space ("Castro 1") and a worded one
		// with a dash ("Godel 1 - Part 3").
		g.OperationalLocationDisplay = (oploc.OperationalLocation{
			ShedID: g.ShedID, ShedName: g.ShedName, PartitionLabel: g.PartitionLabel,
		}).Display()
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: list sale allocations: %w", err)
	}
	return out, nil
}

var _ ports.SaleAllocationReader = (*Repository)(nil)
