package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

type Repository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = 3 * time.Second
	}
	return &Repository{pool: pool, queryTimeout: queryTimeout}
}

func (r *Repository) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.pool.Ping(ctx)
}

func (r *Repository) GetGoatByID(ctx context.Context, tenantID, goatID string) (*domain.GoatPassport, error) {
	return r.getGoat(ctx, tenantID, "g.goat_id = $2::uuid", goatID)
}

func (r *Repository) GetGoatByDisplayID(ctx context.Context, tenantID, displayID string) (*domain.GoatPassport, error) {
	return r.getGoat(ctx, tenantID, "g.display_id = $2", displayID)
}

func (r *Repository) getGoat(ctx context.Context, tenantID, predicate, value string) (*domain.GoatPassport, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	query := goatSummarySelect() + " WHERE g.tenant_id = $1::uuid AND " + predicate
	row := r.pool.QueryRow(ctx, query, tenantID, value)
	summary, species, mergedInto, rowVersion, err := scanGoatRow(row)
	if err != nil {
		return nil, err
	}
	identifiers, err := r.listIdentifiers(ctx, tenantID, summary.GoatID)
	if err != nil {
		return nil, err
	}
	return &domain.GoatPassport{
		GoatID:           summary.GoatID,
		DisplayID:        summary.DisplayID,
		Species:          species,
		IdentityState:    summary.IdentityState,
		Summary:          summary,
		Identifiers:      identifiers,
		EvidenceRefs:     []domain.EvidenceRef{},
		FamilyRefs:       []domain.FamilyRef{},
		MergedIntoGoatID: mergedInto,
		RowVersion:       rowVersion,
	}, nil
}

func (r *Repository) SearchGoats(ctx context.Context, params ports.SearchGoatsParams) ([]domain.GoatSummary, *string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	args := []any{params.TenantID, params.Limit + 1}
	where := []string{"g.tenant_id = $1::uuid", "g.identity_state <> 'merged'"}

	if params.Cursor != nil && strings.TrimSpace(*params.Cursor) != "" {
		args = append(args, *params.Cursor)
		where = append(where, fmt.Sprintf("g.display_id > $%d", len(args)))
	}
	if params.FarmID != nil {
		args = append(args, *params.FarmID)
		where = append(where, fmt.Sprintf("g.farm_id = $%d::uuid", len(args)))
	}
	if params.ParkID != nil {
		args = append(args, *params.ParkID)
		where = append(where, fmt.Sprintf("g.park_id = $%d::uuid", len(args)))
	}
	if params.LocationID != nil {
		args = append(args, *params.LocationID)
		where = append(where, fmt.Sprintf("g.current_location_id = $%d::uuid", len(args)))
	}
	if params.Status != nil {
		args = append(args, *params.Status)
		where = append(where, fmt.Sprintf("g.lifecycle_status = $%d", len(args)))
	}
	if params.Query != nil && strings.TrimSpace(*params.Query) != "" {
		q := strings.TrimSpace(*params.Query)
		args = append(args, q)
		qArg := len(args)
		identifierTypeClause := ""
		scopeClause := ""
		if params.IdentifierType != nil {
			args = append(args, *params.IdentifierType)
			identifierTypeClause = fmt.Sprintf(" AND gi.identifier_type = $%d", len(args))
		}
		if params.ScopeKey != nil {
			args = append(args, *params.ScopeKey)
			scopeClause = fmt.Sprintf(" AND gi.scope_key = $%d", len(args))
		}
		where = append(where, fmt.Sprintf(`(
			g.display_id = $%d
			OR EXISTS (
				SELECT 1
				FROM goat_identifiers gi
				WHERE gi.tenant_id = g.tenant_id
				  AND gi.goat_id = g.goat_id
				  AND gi.status = 'active'
				  AND gi.normalized_value = $%d%s%s
			)
		)`, qArg, qArg, identifierTypeClause, scopeClause))
	}

	query := goatSummarySelect() + " WHERE " + strings.Join(where, " AND ") + " ORDER BY g.display_id ASC LIMIT $2"
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	items := make([]domain.GoatSummary, 0, params.Limit)
	for rows.Next() {
		summary, _, _, _, err := scanGoatRow(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var next *string
	if len(items) > params.Limit {
		cursor := items[params.Limit-1].DisplayID
		next = &cursor
		items = items[:params.Limit]
	}
	return items, next, nil
}

func (r *Repository) FindIdentifierMatches(ctx context.Context, params ports.ResolveIdentifierParams) ([]domain.IdentifierMatch, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	args := []any{params.TenantID, params.IdentifierType, params.NormalizedValue}
	where := []string{
		"gi.tenant_id = $1::uuid",
		"gi.identifier_type = $2",
		"gi.normalized_value = $3",
		"gi.status IN ('active', 'retired', 'disputed')",
	}
	if params.ScopeKey != nil {
		args = append(args, *params.ScopeKey)
		where = append(where, fmt.Sprintf("gi.scope_key = $%d", len(args)))
	}
	if params.FarmID != nil {
		args = append(args, *params.FarmID)
		where = append(where, fmt.Sprintf("g.farm_id = $%d::uuid", len(args)))
	}
	if params.ParkID != nil {
		args = append(args, *params.ParkID)
		where = append(where, fmt.Sprintf("g.park_id = $%d::uuid", len(args)))
	}
	if params.LocationID != nil {
		args = append(args, *params.LocationID)
		where = append(where, fmt.Sprintf("g.current_location_id = $%d::uuid", len(args)))
	}

	query := `
SELECT
  gi.identifier_id::text,
  gi.identifier_type,
  gi.identifier_value,
  gi.scope_key,
  gi.status,
  gi.is_primary_for_goat,
  gi.valid_from,
  gi.valid_to,
  gi.source_system,
  gi.source_record_id,
  gi.confidence::float8,
  ` + strings.TrimPrefix(goatSummaryColumns(), "  ") + `
FROM goat_identifiers gi
JOIN goats g ON g.tenant_id = gi.tenant_id AND g.goat_id = gi.goat_id
LEFT JOIN locations loc ON loc.tenant_id = g.tenant_id AND loc.location_id = g.current_location_id
LEFT JOIN goat_identifiers old_tag ON old_tag.tenant_id = g.tenant_id
  AND old_tag.goat_id = g.goat_id
  AND old_tag.identifier_type = 'old_tag'
  AND old_tag.status = 'active'
  AND old_tag.is_primary_for_goat
LEFT JOIN goat_identifiers rfid ON rfid.tenant_id = g.tenant_id
  AND rfid.goat_id = g.goat_id
  AND rfid.identifier_type = 'rfid'
  AND rfid.status = 'active'
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY CASE gi.status WHEN 'active' THEN 0 WHEN 'disputed' THEN 1 ELSE 2 END, gi.valid_from DESC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	matches := []domain.IdentifierMatch{}
	for rows.Next() {
		identifier, summary, err := scanIdentifierMatch(rows)
		if err != nil {
			return nil, err
		}
		matches = append(matches, domain.IdentifierMatch{Identifier: identifier, Goat: summary})
	}
	return matches, rows.Err()
}

func (r *Repository) FindOpenConflictForIdentifier(ctx context.Context, tenantID, identifierType, normalizedValue string) (*string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var conflictID string
	err := r.pool.QueryRow(ctx, `
SELECT conflict_id::text
FROM identity_conflicts
WHERE tenant_id = $1::uuid
  AND identifier_type = $2
  AND identifier_value = $3
  AND state IN ('open', 'needs_field_check')
ORDER BY created_at DESC
LIMIT 1`, tenantID, identifierType, normalizedValue).Scan(&conflictID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &conflictID, nil
}

func (r *Repository) ListConflicts(ctx context.Context, params ports.ListConflictsParams) ([]domain.ConflictSummary, *string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	args := []any{params.TenantID, params.Limit + 1}
	where := []string{"c.tenant_id = $1::uuid"}
	if params.State != nil {
		args = append(args, *params.State)
		where = append(where, fmt.Sprintf("c.state = $%d", len(args)))
	}
	if params.ConflictType != nil {
		args = append(args, *params.ConflictType)
		where = append(where, fmt.Sprintf("c.conflict_type = $%d", len(args)))
	}
	if params.Cursor != nil && *params.Cursor != "" {
		args = append(args, *params.Cursor)
		where = append(where, fmt.Sprintf("c.conflict_id::text > $%d", len(args)))
	}

	query := conflictSummarySelect() + " WHERE " + strings.Join(where, " AND ") + " ORDER BY c.created_at DESC LIMIT $2"
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	items := []domain.ConflictSummary{}
	for rows.Next() {
		item, err := scanConflictSummary(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(items) > params.Limit {
		cursor := items[params.Limit-1].ConflictID
		next = &cursor
		items = items[:params.Limit]
	}
	return items, next, nil
}

func (r *Repository) GetConflict(ctx context.Context, tenantID, conflictID string) (*domain.ConflictDetailResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	row := r.pool.QueryRow(ctx, conflictSummarySelect()+" WHERE c.tenant_id = $1::uuid AND c.conflict_id = $2::uuid", tenantID, conflictID)
	summary, err := scanConflictSummary(row)
	if err != nil {
		return nil, err
	}

	goatRows, err := r.pool.Query(ctx, `
SELECT goat_id::text
FROM identity_conflict_goats
WHERE tenant_id = $1::uuid AND conflict_id = $2::uuid
ORDER BY created_at ASC`, tenantID, conflictID)
	if err != nil {
		return nil, err
	}
	defer goatRows.Close()

	goats := []domain.ConflictGoatReview{}
	for goatRows.Next() {
		var goatID string
		if err := goatRows.Scan(&goatID); err != nil {
			return nil, err
		}
		passport, err := r.GetGoatByID(ctx, tenantID, goatID)
		if err != nil {
			return nil, err
		}
		goats = append(goats, domain.ConflictGoatReview{
			Goat:         passport.Summary,
			Identifiers:  passport.Identifiers,
			EvidenceRefs: []domain.EvidenceRef{},
			RowVersion:   passport.RowVersion,
		})
	}
	if err := goatRows.Err(); err != nil {
		return nil, err
	}

	sourceRows, err := r.pool.Query(ctx, `
SELECT source_system, source_record_id
FROM identity_conflict_source_records
WHERE tenant_id = $1::uuid AND conflict_id = $2::uuid
ORDER BY created_at ASC`, tenantID, conflictID)
	if err != nil {
		return nil, err
	}
	defer sourceRows.Close()

	sourceRecords := []domain.ConflictSourceRecord{}
	for sourceRows.Next() {
		var rec domain.ConflictSourceRecord
		if err := sourceRows.Scan(&rec.SourceSystem, &rec.SourceRecordID); err != nil {
			return nil, err
		}
		rec.EvidenceRefs = []domain.EvidenceRef{{
			EvidenceType: "source_record",
			EvidenceID:   rec.SourceRecordID,
			SourceSystem: &rec.SourceSystem,
		}}
		sourceRecords = append(sourceRecords, rec)
	}
	if err := sourceRows.Err(); err != nil {
		return nil, err
	}

	return &domain.ConflictDetailResult{
		Conflict:      summary,
		Goats:         goats,
		SourceRecords: sourceRecords,
		DecisionOptions: []string{
			"same_goat_merge",
			"different_goats_mark_identifier_disputed",
			"create_new_goat",
			"reject_candidate",
			"needs_field_verification",
		},
	}, nil
}

func (r *Repository) ListIdentityCounts(ctx context.Context, params ports.CountParams) ([]domain.IdentityCount, domain.Freshness, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	args := []any{params.Grain, params.TenantID}
	where := []string{"counter_grain = $1", "tenant_id = $2::uuid"}
	addNullableFilter := func(column string, value *string, uuid bool) {
		if value == nil {
			return
		}
		args = append(args, *value)
		if uuid {
			where = append(where, fmt.Sprintf("%s = $%d::uuid", column, len(args)))
			return
		}
		where = append(where, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	addNullableFilter("custodian_party_id", params.CustodianPartyID, true)
	addNullableFilter("farm_id", params.FarmID, true)
	addNullableFilter("park_id", params.ParkID, true)
	addNullableFilter("shed_id", params.ShedID, true)
	addNullableFilter("cohort_id", params.CohortID, true)
	addNullableFilter("lifecycle_status", params.LifecycleStatus, false)
	addNullableFilter("reproductive_status", params.ReproductiveStatus, false)
	addNullableFilter("growth_cohort_tag", params.GrowthCohortTag, false)
	addNullableFilter("management_stage", params.ManagementStage, false)
	addNullableFilter("health_status", params.HealthStatus, false)
	addNullableFilter("identity_state", params.IdentityState, false)
	addNullableFilter("breed_id", params.BreedID, true)
	addNullableFilter("sex", params.Sex, false)

	query := `
SELECT
  counter_grain,
  tenant_id::text,
  custodian_party_id::text,
  farm_id::text,
  park_id::text,
  shed_id::text,
  cohort_id::text,
  lifecycle_status,
  reproductive_status,
  growth_cohort_tag,
  management_stage,
  health_status,
  identity_state,
  breed_id::text,
  sex,
  count_value,
  as_of_recorded_at,
  source_import_run_id::text,
  is_rebuilding,
  updated_at
FROM goat_identity_counters
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY updated_at DESC
LIMIT 100`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, domain.Freshness{}, err
	}
	defer rows.Close()

	items := []domain.IdentityCount{}
	var freshness domain.Freshness
	for rows.Next() {
		item, err := scanIdentityCount(rows)
		if err != nil {
			return nil, domain.Freshness{}, err
		}
		items = append(items, item)
		if freshness.AsOfRecordedAt == nil && item.AsOfRecordedAt != nil {
			freshness.AsOfRecordedAt = item.AsOfRecordedAt
		}
		if freshness.SourceImportRunID == nil && item.SourceImportRunID != nil {
			freshness.SourceImportRunID = item.SourceImportRunID
		}
		freshness.IsRebuilding = freshness.IsRebuilding || item.IsRebuilding
	}
	return items, freshness, rows.Err()
}

func (r *Repository) listIdentifiers(ctx context.Context, tenantID, goatID string) ([]domain.GoatIdentifier, error) {
	rows, err := r.pool.Query(ctx, `
SELECT
  identifier_id::text,
  identifier_type,
  identifier_value,
  scope_key,
  status,
  is_primary_for_goat,
  valid_from,
  valid_to,
  source_system,
  source_record_id,
  confidence::float8
FROM goat_identifiers
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid
ORDER BY is_primary_for_goat DESC, status ASC, valid_from DESC`, tenantID, goatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []domain.GoatIdentifier{}
	for rows.Next() {
		item, err := scanIdentifier(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func goatSummarySelect() string {
	return "SELECT " + goatSummaryColumns() + `
FROM goats g
LEFT JOIN locations loc ON loc.tenant_id = g.tenant_id AND loc.location_id = g.current_location_id
LEFT JOIN goat_identifiers old_tag ON old_tag.tenant_id = g.tenant_id
  AND old_tag.goat_id = g.goat_id
  AND old_tag.identifier_type = 'old_tag'
  AND old_tag.status = 'active'
  AND old_tag.is_primary_for_goat
LEFT JOIN goat_identifiers rfid ON rfid.tenant_id = g.tenant_id
  AND rfid.goat_id = g.goat_id
  AND rfid.identifier_type = 'rfid'
  AND rfid.status = 'active'`
}

func goatSummaryColumns() string {
	return `
  g.goat_id::text,
  g.display_id,
  old_tag.identifier_value,
  rfid.identifier_value,
  g.breed,
  g.sex,
  g.age_band,
  g.lifecycle_status,
  g.reproductive_status,
  g.growth_cohort_tag,
  g.management_stage,
  g.health_status,
  g.identity_state,
  COALESCE(loc.name, 'Unknown location'),
  g.farm_id::text,
  g.park_id::text,
  g.shed_id::text,
  g.cohort_id::text,
  g.species,
  g.merged_into_goat_id::text,
  g.row_version`
}

type scanner interface {
	Scan(dest ...any) error
}

func scanGoatRow(row scanner) (domain.GoatSummary, string, *string, int, error) {
	var (
		summary       domain.GoatSummary
		primaryOldTag sql.NullString
		rfid          sql.NullString
		breed         sql.NullString
		sex           sql.NullString
		ageBand       sql.NullString
		repro         sql.NullString
		growth        sql.NullString
		management    sql.NullString
		health        sql.NullString
		farmID        sql.NullString
		parkID        sql.NullString
		shedID        sql.NullString
		cohortID      sql.NullString
		species       string
		mergedInto    sql.NullString
		rowVersion    int
	)
	err := row.Scan(
		&summary.GoatID,
		&summary.DisplayID,
		&primaryOldTag,
		&rfid,
		&breed,
		&sex,
		&ageBand,
		&summary.LifecycleStatus,
		&repro,
		&growth,
		&management,
		&health,
		&summary.IdentityState,
		&summary.LocationPath.Display,
		&farmID,
		&parkID,
		&shedID,
		&cohortID,
		&species,
		&mergedInto,
		&rowVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GoatSummary{}, "", nil, 0, ports.ErrNotFound
	}
	if err != nil {
		return domain.GoatSummary{}, "", nil, 0, err
	}
	summary.PrimaryOldTag = stringPtr(primaryOldTag)
	summary.RFID = stringPtr(rfid)
	summary.Breed = stringPtr(breed)
	summary.Sex = stringPtr(sex)
	summary.AgeBand = stringPtr(ageBand)
	summary.ReproductiveStatus = stringPtr(repro)
	summary.GrowthCohortTag = stringPtr(growth)
	summary.ManagementStage = stringPtr(management)
	summary.HealthStatus = stringPtr(health)
	summary.LocationPath.FarmID = stringPtr(farmID)
	summary.LocationPath.ParkID = stringPtr(parkID)
	summary.LocationPath.ShedID = stringPtr(shedID)
	summary.LocationPath.CohortID = stringPtr(cohortID)
	summary.Warnings = []domain.Warning{}
	return summary, species, stringPtr(mergedInto), rowVersion, nil
}

func scanIdentifier(row scanner) (domain.GoatIdentifier, error) {
	var (
		item           domain.GoatIdentifier
		validTo        sql.NullTime
		sourceSystem   sql.NullString
		sourceRecordID sql.NullString
		confidence     sql.NullFloat64
	)
	if err := row.Scan(
		&item.IdentifierID,
		&item.IdentifierType,
		&item.IdentifierValue,
		&item.ScopeKey,
		&item.Status,
		&item.IsPrimaryForGoat,
		&item.ValidFrom,
		&validTo,
		&sourceSystem,
		&sourceRecordID,
		&confidence,
	); err != nil {
		return domain.GoatIdentifier{}, err
	}
	item.ValidTo = timePtr(validTo)
	item.SourceSystem = stringPtr(sourceSystem)
	item.SourceRecordID = stringPtr(sourceRecordID)
	item.Confidence = floatPtr(confidence)
	return item, nil
}

func scanIdentifierMatch(row scanner) (domain.GoatIdentifier, domain.GoatSummary, error) {
	var (
		identifier     domain.GoatIdentifier
		validTo        sql.NullTime
		sourceSystem   sql.NullString
		sourceRecordID sql.NullString
		confidence     sql.NullFloat64
		summary        domain.GoatSummary
		primaryOldTag  sql.NullString
		rfid           sql.NullString
		breed          sql.NullString
		sex            sql.NullString
		ageBand        sql.NullString
		repro          sql.NullString
		growth         sql.NullString
		management     sql.NullString
		health         sql.NullString
		farmID         sql.NullString
		parkID         sql.NullString
		shedID         sql.NullString
		cohortID       sql.NullString
		species        string
		mergedInto     sql.NullString
		rowVersion     int
	)
	if err := row.Scan(
		&identifier.IdentifierID,
		&identifier.IdentifierType,
		&identifier.IdentifierValue,
		&identifier.ScopeKey,
		&identifier.Status,
		&identifier.IsPrimaryForGoat,
		&identifier.ValidFrom,
		&validTo,
		&sourceSystem,
		&sourceRecordID,
		&confidence,
		&summary.GoatID,
		&summary.DisplayID,
		&primaryOldTag,
		&rfid,
		&breed,
		&sex,
		&ageBand,
		&summary.LifecycleStatus,
		&repro,
		&growth,
		&management,
		&health,
		&summary.IdentityState,
		&summary.LocationPath.Display,
		&farmID,
		&parkID,
		&shedID,
		&cohortID,
		&species,
		&mergedInto,
		&rowVersion,
	); err != nil {
		return domain.GoatIdentifier{}, domain.GoatSummary{}, err
	}
	_ = species
	_ = mergedInto
	_ = rowVersion
	identifier.ValidTo = timePtr(validTo)
	identifier.SourceSystem = stringPtr(sourceSystem)
	identifier.SourceRecordID = stringPtr(sourceRecordID)
	identifier.Confidence = floatPtr(confidence)
	summary.PrimaryOldTag = stringPtr(primaryOldTag)
	summary.RFID = stringPtr(rfid)
	summary.Breed = stringPtr(breed)
	summary.Sex = stringPtr(sex)
	summary.AgeBand = stringPtr(ageBand)
	summary.ReproductiveStatus = stringPtr(repro)
	summary.GrowthCohortTag = stringPtr(growth)
	summary.ManagementStage = stringPtr(management)
	summary.HealthStatus = stringPtr(health)
	summary.LocationPath.FarmID = stringPtr(farmID)
	summary.LocationPath.ParkID = stringPtr(parkID)
	summary.LocationPath.ShedID = stringPtr(shedID)
	summary.LocationPath.CohortID = stringPtr(cohortID)
	summary.Warnings = []domain.Warning{}
	return identifier, summary, nil
}

func conflictSummarySelect() string {
	return `
SELECT
  c.conflict_id::text,
  c.conflict_type,
  c.severity,
  c.identifier_type,
  c.identifier_value,
  COALESCE(c.evidence->>'scope_key', 'unknown'),
  COALESCE((SELECT count(*)::int FROM identity_conflict_goats cg WHERE cg.tenant_id = c.tenant_id AND cg.conflict_id = c.conflict_id), cardinality(c.goat_ids), 0),
  COALESCE((SELECT count(*)::int FROM identity_conflict_source_records sr WHERE sr.tenant_id = c.tenant_id AND sr.conflict_id = c.conflict_id), cardinality(c.source_record_ids), 0),
  c.state,
  c.created_at
FROM identity_conflicts c`
}

func scanConflictSummary(row scanner) (domain.ConflictSummary, error) {
	var (
		item            domain.ConflictSummary
		identifierType  sql.NullString
		identifierValue sql.NullString
		scopeKey        sql.NullString
	)
	err := row.Scan(
		&item.ConflictID,
		&item.ConflictType,
		&item.Severity,
		&identifierType,
		&identifierValue,
		&scopeKey,
		&item.GoatCount,
		&item.SourceRecordCount,
		&item.State,
		&item.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ConflictSummary{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.ConflictSummary{}, err
	}
	if identifierType.Valid && identifierValue.Valid {
		scope := "unknown"
		if scopeKey.Valid && scopeKey.String != "" {
			scope = scopeKey.String
		}
		item.Identifier = &domain.IdentifierReference{
			IdentifierType:  identifierType.String,
			IdentifierValue: identifierValue.String,
			ScopeKey:        scope,
		}
	}
	return item, nil
}

func scanIdentityCount(row scanner) (domain.IdentityCount, error) {
	var (
		item              domain.IdentityCount
		custodianPartyID  sql.NullString
		farmID            sql.NullString
		parkID            sql.NullString
		shedID            sql.NullString
		cohortID          sql.NullString
		lifecycle         sql.NullString
		repro             sql.NullString
		growth            sql.NullString
		management        sql.NullString
		health            sql.NullString
		identityState     sql.NullString
		breedID           sql.NullString
		sex               sql.NullString
		asOf              sql.NullTime
		sourceImportRunID sql.NullString
	)
	if err := row.Scan(
		&item.CounterGrain,
		&item.Dimensions.TenantID,
		&custodianPartyID,
		&farmID,
		&parkID,
		&shedID,
		&cohortID,
		&lifecycle,
		&repro,
		&growth,
		&management,
		&health,
		&identityState,
		&breedID,
		&sex,
		&item.CountValue,
		&asOf,
		&sourceImportRunID,
		&item.IsRebuilding,
		&item.UpdatedAt,
	); err != nil {
		return domain.IdentityCount{}, err
	}
	item.Dimensions.CustodianPartyID = stringPtr(custodianPartyID)
	item.Dimensions.FarmID = stringPtr(farmID)
	item.Dimensions.ParkID = stringPtr(parkID)
	item.Dimensions.ShedID = stringPtr(shedID)
	item.Dimensions.CohortID = stringPtr(cohortID)
	item.Dimensions.LifecycleStatus = stringPtr(lifecycle)
	item.Dimensions.ReproductiveStatus = stringPtr(repro)
	item.Dimensions.GrowthCohortTag = stringPtr(growth)
	item.Dimensions.ManagementStage = stringPtr(management)
	item.Dimensions.HealthStatus = stringPtr(health)
	item.Dimensions.IdentityState = stringPtr(identityState)
	item.Dimensions.BreedID = stringPtr(breedID)
	item.Dimensions.Sex = stringPtr(sex)
	item.AsOfRecordedAt = timePtr(asOf)
	item.SourceImportRunID = stringPtr(sourceImportRunID)
	return item, nil
}

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.queryTimeout)
}

func stringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func timePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}

func floatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}
