package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

type Repository struct {
	pool           *pgxpool.Pool
	queries        *identitydb.Queries
	queryTimeout   time.Duration
	afterAuditHook func(context.Context) error
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = 3 * time.Second
	}
	return &Repository{pool: pool, queries: identitydb.New(pool), queryTimeout: queryTimeout}
}

func (r *Repository) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.pool.Ping(ctx)
}

func (r *Repository) GetGoatByID(ctx context.Context, tenantID, goatID string) (*domain.GoatPassport, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(tenantID)
	if err != nil {
		return nil, err
	}
	goatUUID, err := uuidParam(goatID)
	if err != nil {
		return nil, err
	}

	row, err := r.queries.GetGoatByID(ctx, identitydb.GetGoatByIDParams{
		TenantID: tenantUUID,
		GoatID:   goatUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.getGoatFromSQLC(ctx, tenantID, sqlcGoatRowFromID(row))
}

func (r *Repository) GetGoatByDisplayID(ctx context.Context, tenantID, displayID string) (*domain.GoatPassport, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(tenantID)
	if err != nil {
		return nil, err
	}

	row, err := r.queries.GetGoatByDisplayID(ctx, identitydb.GetGoatByDisplayIDParams{
		TenantID:  tenantUUID,
		DisplayID: displayID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.getGoatFromSQLC(ctx, tenantID, sqlcGoatRowFromDisplayID(row))
}

func (r *Repository) getGoatFromSQLC(ctx context.Context, tenantID string, row sqlcGoatRow) (*domain.GoatPassport, error) {
	summary, species, mergedInto, rowVersion := goatSummaryFromSQLC(row)
	identifiers, err := r.listIdentifiers(ctx, tenantID, summary.GoatID)
	if err != nil {
		return nil, err
	}
	return &domain.GoatPassport{
		GoatID:           summary.GoatID,
		DisplayID:        summary.DisplayID,
		Species:          species,
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
	where := []string{"g.tenant_id = $1::uuid", "g.merged_into_goat_id IS NULL"}

	if params.Cursor != nil && strings.TrimSpace(*params.Cursor) != "" {
		args = append(args, *params.Cursor)
		where = append(where, fmt.Sprintf("g.display_id > $%d", len(args)))
	}
	if params.GoatID != nil {
		args = append(args, *params.GoatID)
		where = append(where, fmt.Sprintf("g.goat_id = $%d::uuid", len(args)))
	}
	if params.Breed != nil {
		args = append(args, *params.Breed)
		where = append(where, fmt.Sprintf("g.breed = $%d", len(args)))
	}
	if params.Sex != nil {
		args = append(args, *params.Sex)
		where = append(where, fmt.Sprintf("g.sex = $%d", len(args)))
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

// ListTemporaryTaggedGoats returns one keyset page of goats that still carry an active temporary tag
// (the operator "Awaiting RFID" list). The INNER JOIN to the active temporary_tag identifier is the
// driving set -- kept small and index-selective by goat_identifiers_active_temporary_tag_idx -- and
// the page is keyset-ordered by g.display_id exactly like SearchGoats so next_cursor is the last
// row's display_id.
func (r *Repository) ListTemporaryTaggedGoats(ctx context.Context, params ports.ListTemporaryTaggedGoatsParams) ([]domain.TemporaryTaggedGoat, *string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	args := []any{params.TenantID, params.Limit + 1}
	where := []string{"g.tenant_id = $1::uuid", "g.merged_into_goat_id IS NULL"}
	if params.Cursor != nil && strings.TrimSpace(*params.Cursor) != "" {
		args = append(args, strings.TrimSpace(*params.Cursor))
		where = append(where, fmt.Sprintf("g.display_id > $%d", len(args)))
	}
	// Optional location filter (operator "Awaiting RFID" park -> shed cascade). The bind param is
	// cast to uuid, not the indexed column, so g.park_id / g.shed_id stay SARGable.
	if strings.TrimSpace(params.ParkID) != "" {
		args = append(args, strings.TrimSpace(params.ParkID))
		where = append(where, fmt.Sprintf("g.park_id = $%d::uuid", len(args)))
	}
	if strings.TrimSpace(params.ShedID) != "" {
		args = append(args, strings.TrimSpace(params.ShedID))
		where = append(where, fmt.Sprintf("g.shed_id = $%d::uuid", len(args)))
	}

	query := `SELECT
  g.goat_id::text,
  g.display_id,
  tt.identifier_value,
  -- Shed first, then park, matching SearchGoats' location_display shape.
  COALESCE(shed.name, park.name, 'Unknown location') AS location_display,
  g.row_version
FROM goats g
JOIN goat_identifiers tt ON tt.tenant_id = g.tenant_id
  AND tt.goat_id = g.goat_id
  AND tt.identifier_type = 'temporary_tag'
  AND tt.status = 'active'
LEFT JOIN locations park ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
LEFT JOIN locations shed ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY g.display_id ASC
LIMIT $2`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	items := make([]domain.TemporaryTaggedGoat, 0, params.Limit)
	for rows.Next() {
		var row domain.TemporaryTaggedGoat
		if err := rows.Scan(&row.GoatID, &row.DisplayID, &row.TemporaryIdentifier, &row.LocationDisplay, &row.RowVersion); err != nil {
			return nil, nil, err
		}
		items = append(items, row)
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
LEFT JOIN locations farm ON farm.tenant_id = g.tenant_id AND farm.location_id = g.farm_id
LEFT JOIN locations park ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
LEFT JOIN locations shed ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
LEFT JOIN locations shed_group ON shed_group.tenant_id = g.tenant_id AND shed_group.location_id = g.shed_group_id
LEFT JOIN locations cohort ON cohort.tenant_id = g.tenant_id AND cohort.location_id = g.cohort_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
LEFT JOIN LATERAL (
  SELECT (gie.payload->>'weight_kg')::float8 AS weight_kg
  FROM goat_identity_events gie
  WHERE gie.tenant_id = g.tenant_id
    AND gie.goat_id = g.goat_id
    AND gie.payload ? 'weight_kg'
    AND (gie.payload->>'weight_kg') ~ '^[0-9]+(\.[0-9]+)?$'
  ORDER BY gie.occurred_at DESC, gie.recorded_at DESC, gie.identity_event_id DESC
  LIMIT 1
) latest_weight ON true
LEFT JOIN goat_identifiers animal_id_1 ON animal_id_1.tenant_id = g.tenant_id
  AND animal_id_1.goat_id = g.goat_id
  AND animal_id_1.identifier_type = 'animal_identifier_1'
  AND animal_id_1.status = 'active'
LEFT JOIN goat_identifiers animal_id_2 ON animal_id_2.tenant_id = g.tenant_id
  AND animal_id_2.goat_id = g.goat_id
  AND animal_id_2.identifier_type = 'animal_identifier_2'
  AND animal_id_2.status = 'active'
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

func (r *Repository) FindOpenConflictForIdentifier(ctx context.Context, tenantID, identifierType, normalizedValue, scopeKey string) (*string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(tenantID)
	if err != nil {
		return nil, err
	}
	conflictID, err := r.queries.FindOpenConflictForIdentifier(ctx, identitydb.FindOpenConflictForIdentifierParams{
		TenantID:        tenantUUID,
		IdentifierType:  textParam(identifierType),
		IdentifierValue: textParam(normalizedValue),
		ScopeKey:        scopeKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &conflictID, nil
}

func (r *Repository) listIdentifiers(ctx context.Context, tenantID, goatID string) ([]domain.GoatIdentifier, error) {
	tenantUUID, err := uuidParam(tenantID)
	if err != nil {
		return nil, err
	}
	goatUUID, err := uuidParam(goatID)
	if err != nil {
		return nil, err
	}

	rows, err := r.queries.ListIdentifiersForGoat(ctx, identitydb.ListIdentifiersForGoatParams{
		TenantID: tenantUUID,
		GoatID:   goatUUID,
	})
	if err != nil {
		return nil, err
	}

	items := make([]domain.GoatIdentifier, 0, len(rows))
	for _, row := range rows {
		items = append(items, identifierFromSQLC(row))
	}
	return items, nil
}

func goatSummarySelect() string {
	return "SELECT " + goatSummaryColumns() + `
FROM goats g
LEFT JOIN locations farm ON farm.tenant_id = g.tenant_id AND farm.location_id = g.farm_id
LEFT JOIN locations park ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
LEFT JOIN locations shed ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
LEFT JOIN locations cohort ON cohort.tenant_id = g.tenant_id AND cohort.location_id = g.cohort_id
-- 1:{0,1} per animal (goat_shed_partitions PK is (tenant_id, goat_id)) -- no fan-out.
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
LEFT JOIN LATERAL (
  SELECT (gie.payload->>'weight_kg')::float8 AS weight_kg
  FROM goat_identity_events gie
  WHERE gie.tenant_id = g.tenant_id
    AND gie.goat_id = g.goat_id
    AND gie.payload ? 'weight_kg'
    AND (gie.payload->>'weight_kg') ~ '^[0-9]+(\.[0-9]+)?$'
  ORDER BY gie.occurred_at DESC, gie.recorded_at DESC, gie.identity_event_id DESC
  LIMIT 1
) latest_weight ON true
LEFT JOIN goat_identifiers animal_id_1 ON animal_id_1.tenant_id = g.tenant_id
  AND animal_id_1.goat_id = g.goat_id
  AND animal_id_1.identifier_type = 'animal_identifier_1'
  AND animal_id_1.status = 'active'
LEFT JOIN goat_identifiers animal_id_2 ON animal_id_2.tenant_id = g.tenant_id
  AND animal_id_2.goat_id = g.goat_id
  AND animal_id_2.identifier_type = 'animal_identifier_2'
  AND animal_id_2.status = 'active'`
}

func goatSummaryColumns() string {
	return `
  g.goat_id::text,
  g.display_id,
  animal_id_1.identifier_value,
  animal_id_2.identifier_value,
  g.breed,
  g.sex,
  g.age_band,
  g.lifecycle_status,
  g.reproductive_status,
  g.growth_cohort_tag,
  g.management_stage,
  g.health_status,
  -- location_display is the exact operational residence. shed_group_id is grouping metadata only.
  COALESCE(shed.name, park.name, 'Unknown location'),
  g.farm_id::text,
  farm.location_code,
  farm.name,
  g.park_id::text,
  park.location_code,
  park.name,
  g.shed_id::text,
  shed.location_code,
  shed.name,
  g.cohort_id::text,
  cohort.location_code,
  cohort.name,
  ''::text,
  '',
  latest_weight.weight_kg,
  g.species,
  g.merged_into_goat_id::text,
  g.row_version`
}

type scanner interface {
	Scan(dest ...any) error
}

type sqlcGoatRow struct {
	GoatID             string
	DisplayID          string
	AnimalIdentifier1  pgtype.Text
	AnimalIdentifier2  pgtype.Text
	Breed              pgtype.Text
	Sex                string
	AgeBand            pgtype.Text
	LifecycleStatus    string
	ReproductiveStatus pgtype.Text
	GrowthCohortTag    pgtype.Text
	ManagementStage    pgtype.Text
	HealthStatus       pgtype.Text
	LocationDisplay    string
	FarmID             string
	FarmCode           string
	FarmName           string
	ParkID             string
	ParkCode           string
	ParkName           string
	ShedID             string
	ShedCode           string
	ShedName           string
	CohortID           string
	CohortCode         string
	CohortName         string
	PartitionLabel     string
	SourceShedName     string
	WeightKg           *float64
	Species            string
	MergedIntoGoatID   string
	RowVersion         int32
}

func sqlcGoatRowFromID(row identitydb.GetGoatByIDRow) sqlcGoatRow {
	return sqlcGoatRow{
		GoatID:             row.GoatID,
		DisplayID:          row.DisplayID,
		AnimalIdentifier1:  row.AnimalIdentifier1,
		AnimalIdentifier2:  row.AnimalIdentifier2,
		Breed:              row.Breed,
		Sex:                row.Sex,
		AgeBand:            row.AgeBand,
		LifecycleStatus:    row.LifecycleStatus,
		ReproductiveStatus: row.ReproductiveStatus,
		GrowthCohortTag:    row.GrowthCohortTag,
		ManagementStage:    row.ManagementStage,
		HealthStatus:       row.HealthStatus,
		LocationDisplay:    row.LocationDisplay,
		FarmID:             row.FarmID,
		FarmCode:           row.FarmCode,
		FarmName:           row.FarmName,
		ParkID:             row.ParkID,
		ParkCode:           row.ParkCode,
		ParkName:           row.ParkName,
		ShedID:             row.ShedID,
		ShedCode:           row.ShedCode,
		ShedName:           row.ShedName,
		CohortID:           row.CohortID,
		CohortCode:         row.CohortCode,
		CohortName:         row.CohortName,
		PartitionLabel:     row.PartitionLabel,
		SourceShedName:     row.SourceShedName,
		Species:            row.Species,
		MergedIntoGoatID:   row.MergedIntoGoatID,
		RowVersion:         row.RowVersion,
	}
}

func sqlcGoatRowFromDisplayID(row identitydb.GetGoatByDisplayIDRow) sqlcGoatRow {
	return sqlcGoatRow{
		GoatID:             row.GoatID,
		DisplayID:          row.DisplayID,
		AnimalIdentifier1:  row.AnimalIdentifier1,
		AnimalIdentifier2:  row.AnimalIdentifier2,
		Breed:              row.Breed,
		Sex:                row.Sex,
		AgeBand:            row.AgeBand,
		LifecycleStatus:    row.LifecycleStatus,
		ReproductiveStatus: row.ReproductiveStatus,
		GrowthCohortTag:    row.GrowthCohortTag,
		ManagementStage:    row.ManagementStage,
		HealthStatus:       row.HealthStatus,
		LocationDisplay:    row.LocationDisplay,
		FarmID:             row.FarmID,
		FarmCode:           row.FarmCode,
		FarmName:           row.FarmName,
		ParkID:             row.ParkID,
		ParkCode:           row.ParkCode,
		ParkName:           row.ParkName,
		ShedID:             row.ShedID,
		ShedCode:           row.ShedCode,
		ShedName:           row.ShedName,
		CohortID:           row.CohortID,
		CohortCode:         row.CohortCode,
		CohortName:         row.CohortName,
		PartitionLabel:     row.PartitionLabel,
		SourceShedName:     row.SourceShedName,
		Species:            row.Species,
		MergedIntoGoatID:   row.MergedIntoGoatID,
		RowVersion:         row.RowVersion,
	}
}

func goatSummaryFromSQLC(row sqlcGoatRow) (domain.GoatSummary, string, *string, int) {
	summary := domain.GoatSummary{
		GoatID:             row.GoatID,
		DisplayID:          row.DisplayID,
		AnimalIdentifier1:  pgTextPtr(row.AnimalIdentifier1),
		AnimalIdentifier2:  pgTextPtr(row.AnimalIdentifier2),
		Breed:              pgTextPtr(row.Breed),
		Sex:                nonEmptyStringPtr(row.Sex),
		AgeBand:            pgTextPtr(row.AgeBand),
		LifecycleStatus:    row.LifecycleStatus,
		ReproductiveStatus: pgTextPtr(row.ReproductiveStatus),
		GrowthCohortTag:    pgTextPtr(row.GrowthCohortTag),
		ManagementStage:    pgTextPtr(row.ManagementStage),
		HealthStatus:       pgTextPtr(row.HealthStatus),
		LocationPath: domain.LocationPath{
			// Seeded with the bare shed/park label; applyLocationPartition overwrites it with the
			// partition-aware composition when the animal is in a pen.
			OperationalLocationDisplay: row.LocationDisplay,
			FarmID:                     nonEmptyStringPtr(row.FarmID),
			FarmCode:                   nonEmptyStringPtr(row.FarmCode),
			FarmName:                   nonEmptyStringPtr(row.FarmName),
			ParkID:                     nonEmptyStringPtr(row.ParkID),
			ParkCode:                   nonEmptyStringPtr(row.ParkCode),
			ParkName:                   nonEmptyStringPtr(row.ParkName),
			ShedID:                     nonEmptyStringPtr(row.ShedID),
			ShedCode:                   nonEmptyStringPtr(row.ShedCode),
			ShedName:                   nonEmptyStringPtr(row.ShedName),
			CohortID:                   nonEmptyStringPtr(row.CohortID),
			CohortCode:                 nonEmptyStringPtr(row.CohortCode),
			CohortName:                 nonEmptyStringPtr(row.CohortName),
		},
		WeightKg:         row.WeightKg,
		RowVersion:       int32(row.RowVersion),
		Warnings:         []domain.Warning{},
		MergedIntoGoatID: nonEmptyStringPtr(row.MergedIntoGoatID),
	}
	applyLocationPartition(
		&summary.LocationPath,
		sql.NullString{String: row.PartitionLabel, Valid: strings.TrimSpace(row.PartitionLabel) != ""},
		sql.NullString{String: row.SourceShedName, Valid: strings.TrimSpace(row.SourceShedName) != ""},
	)
	return summary, row.Species, nonEmptyStringPtr(row.MergedIntoGoatID), int(row.RowVersion)
}

func identifierFromSQLC(row identitydb.ListIdentifiersForGoatRow) domain.GoatIdentifier {
	confidence := row.Confidence
	var confidencePtr *float64
	if !math.IsNaN(confidence) {
		confidencePtr = &confidence
	}
	return domain.GoatIdentifier{
		IdentifierID:     row.IdentifierID,
		IdentifierType:   row.IdentifierType,
		IdentifierValue:  row.IdentifierValue,
		ScopeKey:         row.ScopeKey,
		Status:           row.Status,
		IsPrimaryForGoat: row.IsPrimaryForGoat,
		ValidFrom:        pgTime(row.ValidFrom),
		ValidTo:          pgTimePtr(row.ValidTo),
		SourceSystem:     pgTextPtr(row.SourceSystem),
		SourceRecordID:   pgTextPtr(row.SourceRecordID),
		Confidence:       confidencePtr,
	}
}

func scanGoatRow(row scanner) (domain.GoatSummary, string, *string, int, error) {
	var (
		summary           domain.GoatSummary
		animalIdentifier1 sql.NullString
		animalIdentifier2 sql.NullString
		breed             sql.NullString
		sex               sql.NullString
		ageBand           sql.NullString
		repro             sql.NullString
		growth            sql.NullString
		management        sql.NullString
		health            sql.NullString
		farmID            sql.NullString
		farmCode          sql.NullString
		farmName          sql.NullString
		parkID            sql.NullString
		parkCode          sql.NullString
		parkName          sql.NullString
		shedID            sql.NullString
		shedCode          sql.NullString
		shedName          sql.NullString
		cohortID          sql.NullString
		cohortCode        sql.NullString
		cohortName        sql.NullString
		partitionLabel    sql.NullString
		sourceShedName    sql.NullString
		weightKg          sql.NullFloat64
		species           string
		mergedInto        sql.NullString
		rowVersion        int
	)
	err := row.Scan(
		&summary.GoatID,
		&summary.DisplayID,
		&animalIdentifier1,
		&animalIdentifier2,
		&breed,
		&sex,
		&ageBand,
		&summary.LifecycleStatus,
		&repro,
		&growth,
		&management,
		&health,
		&summary.LocationPath.OperationalLocationDisplay,
		&farmID,
		&farmCode,
		&farmName,
		&parkID,
		&parkCode,
		&parkName,
		&shedID,
		&shedCode,
		&shedName,
		&cohortID,
		&cohortCode,
		&cohortName,
		&partitionLabel,
		&sourceShedName,
		&weightKg,
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
	summary.AnimalIdentifier1 = stringPtr(animalIdentifier1)
	summary.AnimalIdentifier2 = stringPtr(animalIdentifier2)
	summary.Breed = stringPtr(breed)
	summary.Sex = stringPtr(sex)
	summary.AgeBand = stringPtr(ageBand)
	summary.ReproductiveStatus = stringPtr(repro)
	summary.GrowthCohortTag = stringPtr(growth)
	summary.ManagementStage = stringPtr(management)
	summary.HealthStatus = stringPtr(health)
	summary.LocationPath.FarmID = stringPtr(farmID)
	summary.LocationPath.FarmCode = stringPtr(farmCode)
	summary.LocationPath.FarmName = stringPtr(farmName)
	summary.LocationPath.ParkID = stringPtr(parkID)
	summary.LocationPath.ParkCode = stringPtr(parkCode)
	summary.LocationPath.ParkName = stringPtr(parkName)
	summary.LocationPath.ShedID = stringPtr(shedID)
	summary.LocationPath.ShedCode = stringPtr(shedCode)
	summary.LocationPath.ShedName = stringPtr(shedName)
	summary.LocationPath.CohortID = stringPtr(cohortID)
	summary.LocationPath.CohortCode = stringPtr(cohortCode)
	summary.LocationPath.CohortName = stringPtr(cohortName)
	applyLocationPartition(&summary.LocationPath, partitionLabel, sourceShedName)
	summary.WeightKg = floatPtr(weightKg)
	summary.RowVersion = int32(rowVersion)
	summary.Warnings = []domain.Warning{}
	return summary, species, stringPtr(mergedInto), rowVersion, nil
}

// applyLocationPartition is now a compatibility no-op for current residence display. The query
// already returns exact shed id/name; legacy partition fields must not re-suffix it.
func applyLocationPartition(loc *domain.LocationPath, partitionLabel, sourceShedName sql.NullString) {
	if loc.ShedID != nil && loc.ShedName != nil {
		loc.OperationalLocationDisplay = strings.TrimSpace(*loc.ShedName)
	}
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
		identifier        domain.GoatIdentifier
		validTo           sql.NullTime
		sourceSystem      sql.NullString
		sourceRecordID    sql.NullString
		confidence        sql.NullFloat64
		summary           domain.GoatSummary
		animalIdentifier1 sql.NullString
		animalIdentifier2 sql.NullString
		breed             sql.NullString
		sex               sql.NullString
		ageBand           sql.NullString
		repro             sql.NullString
		growth            sql.NullString
		management        sql.NullString
		health            sql.NullString
		farmID            sql.NullString
		farmCode          sql.NullString
		farmName          sql.NullString
		parkID            sql.NullString
		parkCode          sql.NullString
		parkName          sql.NullString
		shedID            sql.NullString
		shedCode          sql.NullString
		shedName          sql.NullString
		cohortID          sql.NullString
		cohortCode        sql.NullString
		cohortName        sql.NullString
		partitionLabel    sql.NullString
		sourceShedName    sql.NullString
		weightKg          sql.NullFloat64
		species           string
		mergedInto        sql.NullString
		rowVersion        int
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
		&animalIdentifier1,
		&animalIdentifier2,
		&breed,
		&sex,
		&ageBand,
		&summary.LifecycleStatus,
		&repro,
		&growth,
		&management,
		&health,
		&summary.LocationPath.OperationalLocationDisplay,
		&farmID,
		&farmCode,
		&farmName,
		&parkID,
		&parkCode,
		&parkName,
		&shedID,
		&shedCode,
		&shedName,
		&cohortID,
		&cohortCode,
		&cohortName,
		&partitionLabel,
		&sourceShedName,
		&weightKg,
		&species,
		&mergedInto,
		&rowVersion,
	); err != nil {
		return domain.GoatIdentifier{}, domain.GoatSummary{}, err
	}
	_ = species
	_ = rowVersion
	identifier.ValidTo = timePtr(validTo)
	identifier.SourceSystem = stringPtr(sourceSystem)
	identifier.SourceRecordID = stringPtr(sourceRecordID)
	identifier.Confidence = floatPtr(confidence)
	summary.AnimalIdentifier1 = stringPtr(animalIdentifier1)
	summary.AnimalIdentifier2 = stringPtr(animalIdentifier2)
	summary.Breed = stringPtr(breed)
	summary.Sex = stringPtr(sex)
	summary.AgeBand = stringPtr(ageBand)
	summary.ReproductiveStatus = stringPtr(repro)
	summary.GrowthCohortTag = stringPtr(growth)
	summary.ManagementStage = stringPtr(management)
	summary.HealthStatus = stringPtr(health)
	summary.LocationPath.FarmID = stringPtr(farmID)
	summary.LocationPath.FarmCode = stringPtr(farmCode)
	summary.LocationPath.FarmName = stringPtr(farmName)
	summary.LocationPath.ParkID = stringPtr(parkID)
	summary.LocationPath.ParkCode = stringPtr(parkCode)
	summary.LocationPath.ParkName = stringPtr(parkName)
	summary.LocationPath.ShedID = stringPtr(shedID)
	summary.LocationPath.ShedCode = stringPtr(shedCode)
	summary.LocationPath.ShedName = stringPtr(shedName)
	summary.LocationPath.CohortID = stringPtr(cohortID)
	summary.LocationPath.CohortCode = stringPtr(cohortCode)
	summary.LocationPath.CohortName = stringPtr(cohortName)
	applyLocationPartition(&summary.LocationPath, partitionLabel, sourceShedName)
	summary.WeightKg = floatPtr(weightKg)
	summary.Warnings = []domain.Warning{}
	summary.MergedIntoGoatID = stringPtr(mergedInto)
	return identifier, summary, nil
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

func uuidParam(value string) (pgtype.UUID, error) {
	var out pgtype.UUID
	if err := out.Scan(value); err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid uuid %q: %w", value, err)
	}
	return out, nil
}

func textParam(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func pgTextPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nonEmptyStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func pgTime(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

func pgTimePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
