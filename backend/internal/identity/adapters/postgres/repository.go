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

type sqlcGoatRow struct {
	GoatID             string
	DisplayID          string
	PrimaryOldTag      pgtype.Text
	RFID               pgtype.Text
	Breed              pgtype.Text
	Sex                pgtype.Text
	AgeBand            pgtype.Text
	LifecycleStatus    string
	ReproductiveStatus pgtype.Text
	GrowthCohortTag    pgtype.Text
	ManagementStage    pgtype.Text
	HealthStatus       pgtype.Text
	IdentityState      string
	LocationDisplay    string
	FarmID             string
	ParkID             string
	ShedID             string
	CohortID           string
	Species            string
	MergedIntoGoatID   string
	RowVersion         int32
}

func sqlcGoatRowFromID(row identitydb.GetGoatByIDRow) sqlcGoatRow {
	return sqlcGoatRow{
		GoatID:             row.GoatID,
		DisplayID:          row.DisplayID,
		PrimaryOldTag:      row.PrimaryOldTag,
		RFID:               row.Rfid,
		Breed:              row.Breed,
		Sex:                row.Sex,
		AgeBand:            row.AgeBand,
		LifecycleStatus:    row.LifecycleStatus,
		ReproductiveStatus: row.ReproductiveStatus,
		GrowthCohortTag:    row.GrowthCohortTag,
		ManagementStage:    row.ManagementStage,
		HealthStatus:       row.HealthStatus,
		IdentityState:      row.IdentityState,
		LocationDisplay:    row.LocationDisplay,
		FarmID:             row.FarmID,
		ParkID:             row.ParkID,
		ShedID:             row.ShedID,
		CohortID:           row.CohortID,
		Species:            row.Species,
		MergedIntoGoatID:   row.MergedIntoGoatID,
		RowVersion:         row.RowVersion,
	}
}

func sqlcGoatRowFromDisplayID(row identitydb.GetGoatByDisplayIDRow) sqlcGoatRow {
	return sqlcGoatRow{
		GoatID:             row.GoatID,
		DisplayID:          row.DisplayID,
		PrimaryOldTag:      row.PrimaryOldTag,
		RFID:               row.Rfid,
		Breed:              row.Breed,
		Sex:                row.Sex,
		AgeBand:            row.AgeBand,
		LifecycleStatus:    row.LifecycleStatus,
		ReproductiveStatus: row.ReproductiveStatus,
		GrowthCohortTag:    row.GrowthCohortTag,
		ManagementStage:    row.ManagementStage,
		HealthStatus:       row.HealthStatus,
		IdentityState:      row.IdentityState,
		LocationDisplay:    row.LocationDisplay,
		FarmID:             row.FarmID,
		ParkID:             row.ParkID,
		ShedID:             row.ShedID,
		CohortID:           row.CohortID,
		Species:            row.Species,
		MergedIntoGoatID:   row.MergedIntoGoatID,
		RowVersion:         row.RowVersion,
	}
}

func goatSummaryFromSQLC(row sqlcGoatRow) (domain.GoatSummary, string, *string, int) {
	summary := domain.GoatSummary{
		GoatID:             row.GoatID,
		DisplayID:          row.DisplayID,
		PrimaryOldTag:      pgTextPtr(row.PrimaryOldTag),
		RFID:               pgTextPtr(row.RFID),
		Breed:              pgTextPtr(row.Breed),
		Sex:                pgTextPtr(row.Sex),
		AgeBand:            pgTextPtr(row.AgeBand),
		LifecycleStatus:    row.LifecycleStatus,
		ReproductiveStatus: pgTextPtr(row.ReproductiveStatus),
		GrowthCohortTag:    pgTextPtr(row.GrowthCohortTag),
		ManagementStage:    pgTextPtr(row.ManagementStage),
		HealthStatus:       pgTextPtr(row.HealthStatus),
		IdentityState:      row.IdentityState,
		LocationPath: domain.LocationPath{
			Display:  row.LocationDisplay,
			FarmID:   nonEmptyStringPtr(row.FarmID),
			ParkID:   nonEmptyStringPtr(row.ParkID),
			ShedID:   nonEmptyStringPtr(row.ShedID),
			CohortID: nonEmptyStringPtr(row.CohortID),
		},
		Warnings: []domain.Warning{},
	}
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
