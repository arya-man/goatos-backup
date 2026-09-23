package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// The routing half of Health Config: which diagnosis types exist, and which animals reach each
// one (migration 000395).
//
// Every read here is a CONFIG read -- once per screen open, never per animal -- so it is allowed
// to be broader than the serving read in diagnosis_repository.go, which resolves one animal
// against the same tables through a single indexed lookup.

const (
	// The types, with the two facts a screen cannot compose for itself: how many stages point at
	// each one (so a retire can warn instead of surprising), and whether the type can diagnose
	// anything yet.
	//
	// projection-review: membership=health_diagnosis_types for the tenant; group_key=(tenant_id,
	// type_key) which is that table's own unique constraint, so one row per type; joins are two
	// correlated aggregates over health_diagnosis_stage_routes and health_diagnosis_register_versions
	// keyed on (tenant_id, type_key)/(tenant_id, animal_class) -- scalar subqueries, so neither can
	// multiply a type; pagination=none, the type list is a handful of rows by construction and the
	// screen shows all of them; scope=tenant_id equality on every arm.
	sqlDiagnosisTypes = `
SELECT t.type_key, t.label, t.status, t.sort_order, t.is_builtin, t.updated_at,
       (SELECT count(*) FROM health_diagnosis_stage_routes r
         WHERE r.tenant_id = t.tenant_id AND r.type_key = t.type_key),
       EXISTS (SELECT 1 FROM health_diagnosis_register_versions v
                WHERE v.tenant_id = t.tenant_id AND v.animal_class = t.type_key
                  AND v.status = 'published')
FROM health_diagnosis_types t
WHERE t.tenant_id = $1::uuid
ORDER BY t.sort_order, t.type_key`

	// The routes, with the farm's own stage name and the number of animals the route governs.
	//
	// The stage label and the live count both come from LEFT joins because a route may name a
	// stage the catalog no longer holds -- a farm can retire a stage without remembering the
	// route -- and that route must still be visible and deletable rather than silently dropped
	// from the screen.
	//
	// projection-review: membership=health_diagnosis_stage_routes for the tenant; group_key=
	// (tenant_id, age_band, stage_code), that table's unique constraint, so one row per route;
	// animal_stage_lookup is joined on (tenant_id, lower(stage_code)) which the lookup's own
	// uniqueness makes 1:0..1, and the live count is a correlated aggregate over goats rather
	// than a join, so neither can multiply a route; the wildcard row deliberately gets a NULL
	// label and a band-wide count; pagination=none; scope=tenant_id equality on every arm.
	sqlDiagnosisRoutes = `
SELECT r.age_band, r.stage_code, r.type_key, r.sub_stage,
       coalesce(s.name, ''), coalesce(t.label, r.type_key),
       (r.stage_code <> '*' AND s.animal_stage_id IS NULL),
       CASE WHEN r.stage_code = '*'
            THEN (SELECT count(*) FROM goats g
                   WHERE g.tenant_id = r.tenant_id AND g.lifecycle_status = 'alive'
                     AND lower(coalesce(g.age_band, '')) = r.age_band)
            ELSE (SELECT count(*) FROM goats g
                   WHERE g.tenant_id = r.tenant_id AND g.lifecycle_status = 'alive'
                     AND lower(btrim(coalesce(g.management_stage, ''))) = r.stage_code)
       END
FROM health_diagnosis_stage_routes r
LEFT JOIN health_diagnosis_types t
       ON t.tenant_id = r.tenant_id AND t.type_key = r.type_key
LEFT JOIN animal_stage_lookup s
       ON s.tenant_id = r.tenant_id AND lower(btrim(s.stage_code)) = r.stage_code
WHERE r.tenant_id = $1::uuid
ORDER BY r.age_band, (r.stage_code = '*') DESC, r.stage_code`

	// The gap: stages that HOLD LIVE ANIMALS and reach no type.
	//
	// Driven from the animals rather than from animal_stage_lookup, because the stage that
	// actually strands a manager is the one an animal is standing on -- including a code the
	// catalog never had. A stage with no live animals is not a gap worth showing; nobody can be
	// refused on it today.
	//
	// The wildcard is honoured here too: a band with a wildcard route has no gaps by
	// construction, which is why adults report none until a farm deletes theirs.
	//
	// projection-review: membership=alive goats for the tenant whose (age_band, management_stage)
	// pair matches no route; group_key=(age_band, lower(management_stage)) and the count is over
	// that same key set, so numerator and denominator range identically; animal_stage_lookup is
	// joined 1:0..1 on (tenant_id, lower(stage_code)) for the label only and cannot multiply a
	// group; pagination=none, the result is bounded by the number of distinct stages on the farm;
	// scope=tenant_id equality on both the goats scan and the NOT EXISTS probes.
	// The farm's own stages, offered so a stage code is PICKED rather than typed. A typed code
	// that matches no animal makes a route that can never fire, and its only symptom is a zero in
	// a column -- which is exactly what happened on 2026-09-23 (`mothers` typed for `Mother`).
	//
	// Driven from the CATALOG rather than from the animals, because a stage with no animals today
	// is still a legitimate thing to route -- Milking holds none this morning and will tomorrow.
	// The live count rides along so the picker can say which stages actually hold animals.
	//
	// projection-review: membership=active animal_stage_lookup rows for the tenant whose age_band
	// is adult or kid; group_key=(tenant_id, stage_code), that catalog's own key, so one row per
	// stage; the live count and the routed flag are correlated subqueries over goats and
	// health_diagnosis_stage_routes keyed on that same stage, so neither can multiply a stage;
	// pagination=none, a farm has tens of stages; scope=tenant_id equality on every arm.
	sqlAvailableStages = `
SELECT lower(btrim(s.stage_code)), s.name, lower(coalesce(s.age_band, '')),
       (SELECT count(*) FROM goats g
         WHERE g.tenant_id = s.tenant_id AND g.lifecycle_status = 'alive'
           AND lower(btrim(coalesce(g.management_stage, ''))) = lower(btrim(s.stage_code))),
       EXISTS (SELECT 1 FROM health_diagnosis_stage_routes r
                WHERE r.tenant_id = s.tenant_id
                  AND r.age_band = lower(coalesce(s.age_band, ''))
                  AND r.stage_code = lower(btrim(s.stage_code))),
       coalesce((SELECT r.type_key FROM health_diagnosis_stage_routes r
                  WHERE r.tenant_id = s.tenant_id
                    AND r.age_band = lower(coalesce(s.age_band, ''))
                    AND r.stage_code = lower(btrim(s.stage_code))), ''),
       coalesce((SELECT t.label FROM health_diagnosis_stage_routes r
                  JOIN health_diagnosis_types t
                    ON t.tenant_id = r.tenant_id AND t.type_key = r.type_key
                  WHERE r.tenant_id = s.tenant_id
                    AND r.age_band = lower(coalesce(s.age_band, ''))
                    AND r.stage_code = lower(btrim(s.stage_code))), '')
FROM animal_stage_lookup s
WHERE s.tenant_id = $1::uuid AND s.status = 'active'
  AND lower(coalesce(s.age_band, '')) IN ('adult', 'kid')
ORDER BY lower(coalesce(s.age_band, '')), s.sort_order, s.stage_code`

	sqlUnroutedStages = `
SELECT lower(coalesce(g.age_band, '')) AS age_band,
       lower(btrim(coalesce(g.management_stage, ''))) AS stage_code,
       coalesce(max(s.name), ''),
       count(*)
FROM goats g
LEFT JOIN animal_stage_lookup s
       ON s.tenant_id = g.tenant_id
      AND lower(btrim(s.stage_code)) = lower(btrim(coalesce(g.management_stage, '')))
WHERE g.tenant_id = $1::uuid
  AND g.lifecycle_status = 'alive'
  AND lower(coalesce(g.age_band, '')) IN ('adult', 'kid')
  AND NOT EXISTS (
        SELECT 1 FROM health_diagnosis_stage_routes r
         JOIN health_diagnosis_types t
           ON t.tenant_id = r.tenant_id AND t.type_key = r.type_key AND t.status = 'active'
        WHERE r.tenant_id = g.tenant_id
          AND r.age_band = lower(coalesce(g.age_band, ''))
          AND r.stage_code = lower(btrim(coalesce(g.management_stage, ''))))
  AND NOT EXISTS (
        SELECT 1 FROM health_diagnosis_stage_routes r
         JOIN health_diagnosis_types t
           ON t.tenant_id = r.tenant_id AND t.type_key = r.type_key AND t.status = 'active'
        WHERE r.tenant_id = g.tenant_id
          AND r.age_band = lower(coalesce(g.age_band, ''))
          AND r.stage_code = '*')
GROUP BY 1, 2
ORDER BY 4 DESC, 2`

	// sqlLockDiagnosisType
	sqlLockDiagnosisType = `
SELECT is_builtin FROM health_diagnosis_types
WHERE tenant_id=$1::uuid AND type_key=$2 FOR UPDATE`

	// sqlInsertDiagnosisType
	sqlInsertDiagnosisType = `
INSERT INTO health_diagnosis_types (tenant_id, type_key, label, status, sort_order, is_builtin)
VALUES ($1::uuid, $2, $3, $4, $5, false)`

	// sqlCountRoutesForType
	sqlCountRoutesForType = `
SELECT count(*) FROM health_diagnosis_stage_routes
WHERE tenant_id=$1::uuid AND type_key=$2`

	// sqlUpdateDiagnosisType
	sqlUpdateDiagnosisType = `
UPDATE health_diagnosis_types
SET label=$3, status=$4, sort_order=$5, updated_at=now()
WHERE tenant_id=$1::uuid AND type_key=$2`

	// sqlDiagnosisTypeStatus
	// Does this farm actually have this stage? The write refuses one it does not, because a route
	// on an unknown stage matches nothing for ever and reports it as a zero.
	sqlStageExists = `
SELECT EXISTS (
  SELECT 1 FROM animal_stage_lookup
   WHERE tenant_id=$1::uuid AND status='active'
     AND lower(coalesce(age_band,''))=$2
     AND lower(btrim(stage_code))=$3)`

	sqlDiagnosisTypeStatus = `
SELECT status FROM health_diagnosis_types
WHERE tenant_id=$1::uuid AND type_key=$2`

	// sqlUpsertStageRoute
	sqlUpsertStageRoute = `
INSERT INTO health_diagnosis_stage_routes (tenant_id, age_band, stage_code, type_key, sub_stage)
VALUES ($1::uuid, $2, $3, $4, $5)
ON CONFLICT (tenant_id, age_band, stage_code)
DO UPDATE SET type_key = EXCLUDED.type_key, sub_stage = EXCLUDED.sub_stage, updated_at = now()`

	// sqlDeleteStageRoute
	sqlDeleteStageRoute = `
DELETE FROM health_diagnosis_stage_routes
WHERE tenant_id=$1::uuid AND age_band=$2 AND stage_code=$3`
)

// recordRoutingAudit writes the routing audit trail.
//
// It does NOT reuse recordAudit: that one stamps ResourceType "health_protocol_version" and a
// UUID resource id, and a type key or a band/stage pair is neither. Passing one through produced
// `invalid input syntax for type uuid` -- a 500 on a perfectly good request -- so the identity
// that is not a UUID rides the AfterState, where it is readable, and the resource id is left
// empty rather than filled with something that only looks like an id.
func recordRoutingAudit(ctx context.Context, tx pgx.Tx, tenantID, actorID, action, resourceType string, after map[string]any) error {
	return audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "admin",
		Action:       action,
		ResourceType: resourceType,
		AfterState:   after,
	})
}

// DiagnosisRouting reads the whole Types screen.
func (r *Repository) DiagnosisRouting(ctx context.Context, tenantID string) (domain.DiagnosisRoutingView, error) {
	var out domain.DiagnosisRoutingView
	out.Types = []domain.DiagnosisType{}
	out.Routes = []domain.StageRouteRow{}
	out.UnroutedStages = []domain.UnroutedStage{}
	out.Stages = []domain.AvailableStage{}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	typesBound := sqlbind.MustBind(sqlDiagnosisTypes, tenantID)
	rows, err := r.pool.Query(ctx, typesBound.SQL(), typesBound.Args()...)
	if err != nil {
		return out, fmt.Errorf("health: read diagnosis types: %w", err)
	}
	for rows.Next() {
		var t domain.DiagnosisType
		if err := rows.Scan(&t.TypeKey, &t.Label, &t.Status, &t.SortOrder, &t.IsBuiltin,
			&t.UpdatedAt, &t.RouteCount, &t.HasPublishedRegister); err != nil {
			rows.Close()
			return out, fmt.Errorf("health: scan diagnosis type: %w", err)
		}
		out.Types = append(out.Types, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("health: read diagnosis types: %w", err)
	}

	routesBound := sqlbind.MustBind(sqlDiagnosisRoutes, tenantID)
	rrows, err := r.pool.Query(ctx, routesBound.SQL(), routesBound.Args()...)
	if err != nil {
		return out, fmt.Errorf("health: read diagnosis routes: %w", err)
	}
	for rrows.Next() {
		var row domain.StageRouteRow
		if err := rrows.Scan(&row.AgeBand, &row.StageCode, &row.TypeKey, &row.SubStage,
			&row.StageLabel, &row.TypeLabel, &row.StageRetired, &row.LiveAnimals); err != nil {
			rrows.Close()
			return out, fmt.Errorf("health: scan diagnosis route: %w", err)
		}
		row.IsWildcard = row.StageCode == domain.StageWildcard
		out.Routes = append(out.Routes, row)
	}
	rrows.Close()
	if err := rrows.Err(); err != nil {
		return out, fmt.Errorf("health: read diagnosis routes: %w", err)
	}

	stageBound := sqlbind.MustBind(sqlAvailableStages, tenantID)
	srows, err := r.pool.Query(ctx, stageBound.SQL(), stageBound.Args()...)
	if err != nil {
		return out, fmt.Errorf("health: read stages: %w", err)
	}
	for srows.Next() {
		var a domain.AvailableStage
		if err := srows.Scan(&a.StageCode, &a.StageLabel, &a.AgeBand, &a.LiveAnimals,
			&a.Routed, &a.RoutedTypeKey, &a.RoutedTypeLabel); err != nil {
			srows.Close()
			return out, fmt.Errorf("health: scan stage: %w", err)
		}
		out.Stages = append(out.Stages, a)
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return out, fmt.Errorf("health: read stages: %w", err)
	}

	gapBound := sqlbind.MustBind(sqlUnroutedStages, tenantID)
	grows, err := r.pool.Query(ctx, gapBound.SQL(), gapBound.Args()...)
	if err != nil {
		return out, fmt.Errorf("health: read unrouted stages: %w", err)
	}
	for grows.Next() {
		var g domain.UnroutedStage
		if err := grows.Scan(&g.AgeBand, &g.StageCode, &g.StageLabel, &g.LiveAnimals); err != nil {
			grows.Close()
			return out, fmt.Errorf("health: scan unrouted stage: %w", err)
		}
		g.ClinicalPlacement = domain.IsClinicalPlacementStage(g.StageCode)
		out.UnroutedStages = append(out.UnroutedStages, g)
	}
	grows.Close()
	if err := grows.Err(); err != nil {
		return out, fmt.Errorf("health: read unrouted stages: %w", err)
	}

	return out, nil
}

// SaveDiagnosisType creates a type or relabels/retires an existing one.
//
// The KEY IS IMMUTABLE once the row exists: `health_diagnosis_register_versions.animal_class`
// and every stored run carry it, so rewriting it would make old proposals unreadable. An update
// therefore touches label, status and sort order only, and a create is the only path that sets
// a key.
func (r *Repository) SaveDiagnosisType(ctx context.Context, cmd domain.SaveDiagnosisTypeCommand) (domain.DiagnosisType, error) {
	var zero domain.DiagnosisType
	if err := cmd.Validate(); err != nil {
		return zero, err
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return zero, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var existingBuiltin bool
	err = tx.QueryRow(ctx, sqlLockDiagnosisType, cmd.TenantID, cmd.TypeKey).Scan(&existingBuiltin)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// A create. is_builtin is never set from a request: only migration 000395 mints those,
		// because being built-in means "this type's register is the committed rulebook", which a
		// farm cannot make true by ticking a box.
		if _, err := tx.Exec(ctx, sqlInsertDiagnosisType,
			cmd.TenantID, cmd.TypeKey, cmd.Label, cmd.Status, cmd.SortOrder); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return zero, domain.ErrTypeKeyInUse
			}
			return zero, fmt.Errorf("health: create diagnosis type: %w", err)
		}
	case err != nil:
		return zero, fmt.Errorf("health: read diagnosis type: %w", err)
	default:
		if cmd.Status == "retired" {
			if existingBuiltin {
				return zero, domain.ErrBuiltinTypeNotRetirable
			}
			// Refused while stages still point here, because the animals on those stages would
			// start being refused with "its stage is not mapped" -- true, but naming the wrong
			// cause. Moving the stages first is one extra act and a legible one.
			var routed int
			if err := tx.QueryRow(ctx, sqlCountRoutesForType, cmd.TenantID, cmd.TypeKey).Scan(&routed); err != nil {
				return zero, fmt.Errorf("health: count routes: %w", err)
			}
			if routed > 0 {
				return zero, domain.ErrTypeStillRouted
			}
		}
		if _, err := tx.Exec(ctx, sqlUpdateDiagnosisType,
			cmd.TenantID, cmd.TypeKey, cmd.Label, cmd.Status, cmd.SortOrder); err != nil {
			return zero, fmt.Errorf("health: update diagnosis type: %w", err)
		}
	}

	if err := recordRoutingAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "health.diagnosis_type.saved",
		"health_diagnosis_type", map[string]any{
			"type_key": cmd.TypeKey, "label": cmd.Label, "status": cmd.Status,
		}); err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	committed = true

	return domain.DiagnosisType{
		TypeKey: cmd.TypeKey, Label: cmd.Label, Status: cmd.Status,
		SortOrder: cmd.SortOrder, IsBuiltin: existingBuiltin,
	}, nil
}

// SaveStageRoute points one stage, or a whole age band, at a type.
func (r *Repository) SaveStageRoute(ctx context.Context, cmd domain.SaveStageRouteCommand) (domain.StageRouteRow, error) {
	var zero domain.StageRouteRow
	if err := cmd.Validate(); err != nil {
		return zero, err
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return zero, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// The STAGE must be one this farm actually uses. A route on a stage no animal is ever on
	// matches nothing, for ever, and says so only as a zero in a column -- a dead rule that looks
	// authored. The wildcard is exempt: it names no stage by design.
	if cmd.StageCode != domain.StageWildcard {
		var exists bool
		if err := tx.QueryRow(ctx, sqlStageExists, cmd.TenantID, cmd.AgeBand, cmd.StageCode).Scan(&exists); err != nil {
			return zero, fmt.Errorf("health: check stage: %w", err)
		}
		if !exists {
			return zero, fmt.Errorf("%w: %q is not a stage this farm's %s are on", domain.ErrStageUnknown, cmd.StageCode, cmd.AgeBand)
		}
	}

	// The type must exist AND be active. The table's foreign key already refuses an unknown key,
	// but it cannot see status, and routing an animal at a retired type would refuse it with a
	// message naming the stage rather than the type -- so this is checked here where the author
	// can be told the real reason.
	var status string
	err = tx.QueryRow(ctx, sqlDiagnosisTypeStatus, cmd.TenantID, cmd.TypeKey).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return zero, domain.ErrRouteTypeUnknown
	}
	if err != nil {
		return zero, fmt.Errorf("health: read diagnosis type: %w", err)
	}

	if _, err := tx.Exec(ctx, sqlUpsertStageRoute,
		cmd.TenantID, cmd.AgeBand, cmd.StageCode, cmd.TypeKey, cmd.SubStage); err != nil {
		return zero, fmt.Errorf("health: save stage route: %w", err)
	}

	if err := recordRoutingAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "health.diagnosis_route.saved",
		"health_diagnosis_stage_route", map[string]any{
			"age_band": cmd.AgeBand, "stage_code": cmd.StageCode,
			"type_key": cmd.TypeKey, "sub_stage": cmd.SubStage,
		}); err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	committed = true

	return domain.StageRouteRow{
		AgeBand: cmd.AgeBand, StageCode: cmd.StageCode, TypeKey: cmd.TypeKey,
		SubStage: cmd.SubStage, IsWildcard: cmd.StageCode == domain.StageWildcard,
	}, nil
}

// DeleteStageRoute removes one route.
func (r *Repository) DeleteStageRoute(ctx context.Context, cmd domain.DeleteStageRouteCommand) error {
	if err := cmd.Validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	tag, err := tx.Exec(ctx, sqlDeleteStageRoute,
		cmd.TenantID, cmd.AgeBand, cmd.StageCode)
	if err != nil {
		return fmt.Errorf("health: delete stage route: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}

	if err := recordRoutingAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "health.diagnosis_route.deleted",
		"health_diagnosis_stage_route", map[string]any{
			"age_band": cmd.AgeBand, "stage_code": cmd.StageCode,
		}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}
