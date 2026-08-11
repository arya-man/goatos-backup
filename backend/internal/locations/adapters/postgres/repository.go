package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/locations/domain"
	"github.com/vgoats/goatos/backend/internal/locations/ports"
)

const defaultQueryTimeout = 3 * time.Second

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

func (r *Repository) ListLocations(ctx context.Context, params ports.ListParams) ([]domain.LocationSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, locationSelectSQL(`
WHERE l.tenant_id = $1::uuid
  AND ($2 = '' OR l.location_type = $2)
  AND ($3 = '' OR l.status = $3)
  AND ($4 = '' OR l.parent_location_id = $4::uuid)
  AND (
    $5 = ''
    OR lower(l.name) LIKE '%' || lower($5) || '%'
    OR lower(COALESCE(l.location_code, '')) LIKE '%' || lower($5) || '%'
    OR EXISTS (
      SELECT 1
      FROM location_aliases la_search
      WHERE la_search.tenant_id = l.tenant_id
        AND la_search.canonical_location_id = l.location_id
        AND lower(la_search.alias_code) LIKE '%' || lower($5) || '%'
    )
  )
  AND (
    $6 = ''
    OR EXISTS (
      SELECT 1
      FROM location_aliases la_filter
      WHERE la_filter.tenant_id = l.tenant_id
        AND la_filter.canonical_location_id = l.location_id
        AND lower(la_filter.alias_code) = lower($6)
    )
  )
ORDER BY l.location_type, l.display_order, l.name, l.location_id
LIMIT $7 OFFSET $8`), params.TenantID, params.LocationType, params.Status, params.ParentLocationID, params.Search, params.Alias, params.Limit, params.Offset)
	if err != nil {
		return nil, err
	}
	return scanLocations(rows)
}

func (r *Repository) GetLocation(ctx context.Context, tenantID, locationID string) (domain.LocationSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, locationSelectSQL(`
WHERE l.tenant_id = $1::uuid
  AND l.location_id = $2::uuid
LIMIT 1`), tenantID, locationID)
	if err != nil {
		return domain.LocationSummary{}, err
	}
	items, err := scanLocations(rows)
	if err != nil {
		return domain.LocationSummary{}, err
	}
	if len(items) == 0 {
		return domain.LocationSummary{}, ports.ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) ListChildren(ctx context.Context, tenantID, locationID string, limit int) ([]domain.LocationSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, locationSelectSQL(`
WHERE l.tenant_id = $1::uuid
  AND l.parent_location_id = $2::uuid
ORDER BY l.location_type, l.display_order, l.name, l.location_id
LIMIT $3`), tenantID, locationID, limit)
	if err != nil {
		return nil, err
	}
	return scanLocations(rows)
}

func (r *Repository) ListAliases(ctx context.Context, tenantID, locationID string, limit int) ([]domain.LocationAlias, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT
  alias_id::text,
  alias_code,
  canonical_location_id::text,
  source_context,
  status,
  notes,
  row_version,
  created_at,
  updated_at
FROM location_aliases
WHERE tenant_id = $1::uuid
  AND canonical_location_id = $2::uuid
ORDER BY status, source_context, alias_code, alias_id
LIMIT $3`, tenantID, locationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.LocationAlias{}
	for rows.Next() {
		var item domain.LocationAlias
		var notes pgtype.Text
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&item.AliasID, &item.AliasCode, &item.CanonicalLocationID, &item.SourceContext, &item.Status, &notes, &item.RowVersion, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.Notes = textPtr(notes)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListCapacity(ctx context.Context, tenantID, locationID string, limit int) ([]domain.LocationCapacityRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT
  capacity_record_id::text,
  location_id::text,
  capacity_kind,
  capacity_value,
  effective_from::text,
  effective_to::text,
  source,
  source_ref,
  notes,
  row_version,
  created_at,
  updated_at
FROM location_capacity_records
WHERE tenant_id = $1::uuid
  AND location_id = $2::uuid
ORDER BY effective_from DESC, capacity_record_id DESC
LIMIT $3`, tenantID, locationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.LocationCapacityRecord{}
	for rows.Next() {
		var item domain.LocationCapacityRecord
		var effectiveTo, sourceRef, notes pgtype.Text
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&item.CapacityRecordID, &item.LocationID, &item.CapacityKind, &item.CapacityValue, &item.EffectiveFrom, &effectiveTo, &item.Source, &sourceRef, &notes, &item.RowVersion, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.EffectiveTo = textPtr(effectiveTo)
		item.SourceRef = textPtr(sourceRef)
		item.Notes = textPtr(notes)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListReviewItems(ctx context.Context, params ports.ReviewParams) ([]domain.LocationReviewItem, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT
  review_id::text,
  review_type,
  status,
  source_context,
  source_label,
  normalized_source_label,
  canonical_location_id::text,
  candidate_location_ids,
  evidence_hash,
  resolution_notes,
  row_version,
  created_at,
  updated_at,
  resolved_at
FROM location_review_items
WHERE tenant_id = $1::uuid
  AND ($2 = '' OR status = $2)
  AND ($3 = '' OR review_type = $3)
ORDER BY status, updated_at DESC, review_id DESC
LIMIT $4`, params.TenantID, params.Status, params.ReviewType, params.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.LocationReviewItem{}
	for rows.Next() {
		var item domain.LocationReviewItem
		var sourceContext, sourceLabel, normalizedLabel, canonicalLocationID, resolutionNotes pgtype.Text
		var candidateBytes []byte
		var createdAt, updatedAt time.Time
		var resolvedAt pgtype.Timestamptz
		if err := rows.Scan(&item.ReviewID, &item.ReviewType, &item.Status, &sourceContext, &sourceLabel, &normalizedLabel, &canonicalLocationID, &candidateBytes, &item.EvidenceHash, &resolutionNotes, &item.RowVersion, &createdAt, &updatedAt, &resolvedAt); err != nil {
			return nil, err
		}
		item.SourceContext = textPtr(sourceContext)
		item.SourceLabel = textPtr(sourceLabel)
		item.NormalizedSourceLabel = textPtr(normalizedLabel)
		item.CanonicalLocationID = textPtr(canonicalLocationID)
		item.CandidateLocationIDs = decodeStringArray(candidateBytes)
		item.ResolutionNotes = textPtr(resolutionNotes)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		item.ResolvedAt = timePtr(resolvedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Usage(ctx context.Context, tenantID, locationID string) (domain.LocationUsageResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var usage domain.LocationUsageResponse
	usage.LocationID = locationID
	err := r.pool.QueryRow(ctx, `
SELECT
  (
    SELECT count(*)
    FROM goats g
    WHERE g.tenant_id = $1::uuid
      AND (g.current_location_id = $2::uuid OR g.farm_id = $2::uuid OR g.park_id = $2::uuid OR g.shed_id = $2::uuid OR g.cohort_id = $2::uuid) -- operational-location:ignore: owner=ravi issue=location-usage-reference-safety scope=usage-count-must-find-any-reference-not-exact-residence-filter expiry=2027-08-11
      AND g.merged_into_goat_id IS NULL
  ) AS goats_currently_assigned,
  (
    SELECT count(*)
    FROM goat_location_history glh
    WHERE glh.tenant_id = $1::uuid
      AND (glh.from_location_id = $2::uuid OR glh.to_location_id = $2::uuid)
  ) AS goat_location_history_rows,
  (
    SELECT count(*)
    FROM locations child
    WHERE child.tenant_id = $1::uuid
      AND child.parent_location_id = $2::uuid
      AND child.status = 'active'
  ) AS child_locations,
  (
    SELECT count(*)
    FROM location_aliases la
    WHERE la.tenant_id = $1::uuid
      AND la.canonical_location_id = $2::uuid
      AND la.status = 'active'
  ) AS active_aliases,
  (
    SELECT count(*)
    FROM user_scope_grants usg
    WHERE usg.tenant_id = $1::uuid
      AND usg.scope_id = $2::uuid
      AND (usg.valid_to IS NULL OR usg.valid_to > now())
  ) AS active_rbac_grants,
  0::bigint AS active_sop_dependencies,
  0::bigint AS import_or_source_rows`, tenantID, locationID).Scan(
		&usage.GoatsCurrentlyAssigned,
		&usage.GoatLocationHistoryRows,
		&usage.ChildLocations,
		&usage.ActiveAliases,
		&usage.ActiveRBACGrants,
		&usage.ActiveSOPDependencies,
		&usage.ImportOrSourceRows,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.LocationUsageResponse{}, ports.ErrNotFound
		}
		return domain.LocationUsageResponse{}, err
	}
	usage.HasBlockingUsage = usage.GoatsCurrentlyAssigned > 0 || usage.ChildLocations > 0 || usage.ActiveAliases > 0 || usage.ActiveRBACGrants > 0 || usage.ActiveSOPDependencies > 0
	return usage, nil
}

func (r *Repository) FindActiveAliases(ctx context.Context, query ports.SourceLabelQuery) ([]ports.SourceLabelAliasMatch, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT
  alias_id::text,
  alias_code,
  canonical_location_id::text,
  source_context,
  status,
  notes,
  row_version,
  created_at,
  updated_at
FROM location_aliases
WHERE tenant_id = $1::uuid
  AND source_context = $2
  AND status = 'active'
  AND lower(regexp_replace(btrim(alias_code), '\s+', ' ', 'g')) = $3
ORDER BY alias_id
LIMIT 2`, query.TenantID, query.SourceContext, query.NormalizedSourceLabel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	aliases := []domain.LocationAlias{}
	for rows.Next() {
		var item domain.LocationAlias
		var notes pgtype.Text
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&item.AliasID, &item.AliasCode, &item.CanonicalLocationID, &item.SourceContext, &item.Status, &notes, &item.RowVersion, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.Notes = textPtr(notes)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		aliases = append(aliases, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	matches := make([]ports.SourceLabelAliasMatch, 0, len(aliases))
	for _, alias := range aliases {
		location, err := r.GetLocation(ctx, query.TenantID, alias.CanonicalLocationID)
		if err != nil {
			return nil, err
		}
		matches = append(matches, ports.SourceLabelAliasMatch{Alias: alias, Location: location})
	}
	return matches, nil
}

func locationSelectSQL(whereClause string) string {
	return `
SELECT
  l.location_id::text,
  l.location_type,
  l.location_code,
  l.name,
  l.parent_location_id::text,
  parent.name,
  l.status,
  l.country,
  l.state_region,
  l.district,
  l.pincode,
  l.lat::float8,
  l.lng::float8,
  l.timezone,
  COALESCE(loa.usable_for_counts, true),
  COALESCE(loa.usable_for_feed, true),
  COALESCE(loa.usable_for_vaccination, true),
  COALESCE(loa.usable_for_sop, true),
  COALESCE(loa.is_holding, false),
  COALESCE(loa.is_quarantine, false),
  COALESCE(loa.is_icu, false),
  COALESCE(loa.display_order, l.display_order),
  loa.notes,
  cap.capacity_value,
  (
    SELECT count(*)::int
    FROM location_aliases la
    WHERE la.tenant_id = l.tenant_id
      AND la.canonical_location_id = l.location_id
      AND la.status = 'active'
  ) AS alias_count,
  (
    SELECT count(*)::int
    FROM locations child
    WHERE child.tenant_id = l.tenant_id
      AND child.parent_location_id = l.location_id
      AND child.status = 'active'
  ) AS child_count,
  l.row_version,
  l.created_at,
  l.updated_at
FROM locations l
LEFT JOIN locations parent ON parent.tenant_id = l.tenant_id AND parent.location_id = l.parent_location_id
LEFT JOIN location_operational_attributes loa ON loa.tenant_id = l.tenant_id AND loa.location_id = l.location_id
LEFT JOIN LATERAL (
  SELECT capacity_value
  FROM location_capacity_records lcr
  WHERE lcr.tenant_id = l.tenant_id
    AND lcr.location_id = l.location_id
    AND lcr.capacity_kind = 'goat_occupancy'
    AND lcr.effective_from <= current_date
    AND (lcr.effective_to IS NULL OR lcr.effective_to > current_date)
  ORDER BY lcr.effective_from DESC, lcr.capacity_record_id DESC
  LIMIT 1
) cap ON true
` + whereClause
}

func scanLocations(rows pgx.Rows) ([]domain.LocationSummary, error) {
	defer rows.Close()
	items := []domain.LocationSummary{}
	for rows.Next() {
		var item domain.LocationSummary
		var code, parentID, parentName, stateRegion, district, pincode, opNotes pgtype.Text
		var lat, lng pgtype.Float8
		var capacity pgtype.Int4
		var createdAt, updatedAt time.Time
		if err := rows.Scan(
			&item.LocationID,
			&item.LocationType,
			&code,
			&item.Name,
			&parentID,
			&parentName,
			&item.Status,
			&item.Country,
			&stateRegion,
			&district,
			&pincode,
			&lat,
			&lng,
			&item.Timezone,
			&item.Operational.UsableForCounts,
			&item.Operational.UsableForFeed,
			&item.Operational.UsableForVaccination,
			&item.Operational.UsableForSOP,
			&item.Operational.IsHolding,
			&item.Operational.IsQuarantine,
			&item.Operational.IsICU,
			&item.Operational.DisplayOrder,
			&opNotes,
			&capacity,
			&item.AliasCount,
			&item.ChildCount,
			&item.RowVersion,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		item.LocationCode = textPtr(code)
		item.ParentLocationID = textPtr(parentID)
		item.ParentName = textPtr(parentName)
		item.StateRegion = textPtr(stateRegion)
		item.District = textPtr(district)
		item.Pincode = textPtr(pincode)
		item.Lat = floatPtr(lat)
		item.Lng = floatPtr(lng)
		item.Operational.Notes = textPtr(opNotes)
		if capacity.Valid {
			v := int(capacity.Int32)
			item.CurrentCapacity = &v
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

func textPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func floatPtr(value pgtype.Float8) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func timePtr(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}
	text := value.Time.UTC().Format(time.RFC3339)
	return &text
}

func decodeStringArray(raw []byte) []string {
	out := []string{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}
