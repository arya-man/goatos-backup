package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// PHONE TABS (docs/decisions/simple-task-phone-tabs.md): where a routine appears on the phone.
// The tab row carries the placement; a routine points at it through pen_routine_definitions.tab_id.

const (
	idemScopeTab = "pen_routine.tab"
	tabResource  = "pen_routine_tab"
)

// ListTabs lists every tab of the tenant with the routines placed on it.
func (r *Repository) ListTabs(ctx context.Context, tenantID string) ([]domain.Tab, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.readTabs(ctx, r.pool, "", "", tenantID)
}

// GetTabByKey reads one tab by its route key.
func (r *Repository) GetTabByKey(ctx context.Context, tenantID, key string) (domain.Tab, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tabs, err := r.readTabs(ctx, r.pool, " AND tb.tab_key = $2", "", tenantID, strings.TrimSpace(key))
	if err != nil {
		return domain.Tab{}, err
	}
	if len(tabs) == 0 {
		return domain.Tab{}, ports.ErrTabNotFound
	}
	return tabs[0], nil
}

func (r *Repository) getTab(ctx context.Context, q querier, tenantID, tabID string, forUpdate bool) (domain.Tab, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE OF tb"
	}
	tabs, err := r.readTabs(ctx, q, " AND tb.tab_id = $2::uuid", lock, tenantID, tabID)
	if err != nil {
		return domain.Tab{}, err
	}
	if len(tabs) == 0 {
		return domain.Tab{}, ports.ErrTabNotFound
	}
	return tabs[0], nil
}

// readTabs is the one tab projection.
//
// projection-review: membership=pen_routine_tabs rows of ONE tenant (PK, one row per tab); group_key=(tenant_id, tab_id); join_cardinality=the placed routines are a jsonb_agg subquery over pen_routine_definitions (a handful per park) with the park 1:1 on PK; pagination=none -- bounded by the authored tab estate; scope=tenant_id
func (r *Repository) readTabs(ctx context.Context, q querier, extra, lock string, args ...any) ([]domain.Tab, error) {
	query := sqlTabsSelect + extra + " ORDER BY tb.status, lower(tb.label), tb.tab_key" + lock
	bound := sqlbind.MustBind(query, args...)
	rows, err := q.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("pen routine: read tabs: %w", err)
	}
	defer rows.Close()
	out := []domain.Tab{}
	for rows.Next() {
		var t domain.Tab
		var routinesRaw []byte
		if err := rows.Scan(&t.TabID, &t.TenantID, &t.Key, &t.Label, &t.ModuleKey, &t.IconKey, &t.Filters, &t.Status, &t.RowVersion, &t.UpdatedAt, &routinesRaw); err != nil {
			return nil, fmt.Errorf("pen routine: scan tab: %w", err)
		}
		var routines []struct {
			RoutineID string `json:"routine_id"`
			Name      string `json:"name"`
			ParkName  string `json:"park_name"`
		}
		if err := json.Unmarshal(routinesRaw, &routines); err != nil {
			return nil, fmt.Errorf("pen routine: decode tab routines: %w", err)
		}
		t.Routines = make([]domain.TabRoutine, 0, len(routines))
		t.RoutineIDs = make([]string, 0, len(routines))
		for _, rt := range routines {
			t.Routines = append(t.Routines, domain.TabRoutine{RoutineID: rt.RoutineID, Name: rt.Name, ParkName: rt.ParkName})
			t.RoutineIDs = append(t.RoutineIDs, rt.RoutineID)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateTab writes a tab and places its routines, in one transaction.
func (r *Repository) CreateTab(ctx context.Context, w ports.WriteParams, t domain.Tab) (domain.Tab, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Tab{}, fmt.Errorf("pen routine: begin tab create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	reservation, err := reserveIdempotency(ctx, tx, w.TenantID, idemScopeTab, w.IdempotencyKey, tabFingerprint("create", w.ActorID, "", 0, t))
	if err != nil {
		return domain.Tab{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Tab{}, err
		}
		if reservation.resultID == "" {
			return domain.Tab{}, ports.ErrIdempotencyConflict
		}
		return r.getTab(ctx, r.pool, w.TenantID, reservation.resultID, false)
	}
	key, err := freeTabKey(ctx, tx, w.TenantID, domain.TabKeyBase(t.Label))
	if err != nil {
		return domain.Tab{}, err
	}
	now := r.now().UTC()
	var tabID string
	if err := tx.QueryRow(ctx, sqlTabInsert, w.TenantID, key, t.Label, t.ModuleKey, t.IconKey, t.Filters, nullIfEmpty(w.ActorID), now).Scan(&tabID); err != nil {
		return domain.Tab{}, fmt.Errorf("pen routine: create tab: %w", err)
	}
	if err := placeRoutines(ctx, tx, w.TenantID, tabID, t.RoutineIDs); err != nil {
		return domain.Tab{}, err
	}
	after, err := r.getTab(ctx, tx, w.TenantID, tabID, false)
	if err != nil {
		return domain.Tab{}, err
	}
	if err := recordTabAudit(ctx, tx, w, "pen_routine_tab.created", tabID, nil, &after); err != nil {
		return domain.Tab{}, err
	}
	if err := completeIdempotency(ctx, tx, w.TenantID, idemScopeTab, w.IdempotencyKey, tabResource, tabID); err != nil {
		return domain.Tab{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Tab{}, fmt.Errorf("pen routine: commit tab create: %w", err)
	}
	return after, nil
}

// UpdateTab rewrites a tab and replaces its routines, fenced on the row version.
func (r *Repository) UpdateTab(ctx context.Context, w ports.WriteParams, t domain.Tab) (domain.Tab, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Tab{}, fmt.Errorf("pen routine: begin tab update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	reservation, err := reserveIdempotency(ctx, tx, w.TenantID, idemScopeTab, w.IdempotencyKey, tabFingerprint("update", w.ActorID, t.TabID, t.RowVersion, t))
	if err != nil {
		return domain.Tab{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Tab{}, err
		}
		if reservation.resultID == "" {
			return domain.Tab{}, ports.ErrIdempotencyConflict
		}
		return r.getTab(ctx, r.pool, w.TenantID, reservation.resultID, false)
	}
	before, err := r.getTab(ctx, tx, w.TenantID, t.TabID, true)
	if err != nil {
		return domain.Tab{}, err
	}
	if t.RowVersion != 0 && t.RowVersion != before.RowVersion {
		return domain.Tab{}, ports.ErrTabVersionConflict
	}
	tag, err := tx.Exec(ctx, sqlTabUpdate, w.TenantID, t.TabID, t.Label, t.ModuleKey, t.IconKey, t.Filters, nullIfEmpty(w.ActorID), r.now().UTC(), before.RowVersion)
	if err != nil {
		return domain.Tab{}, fmt.Errorf("pen routine: update tab: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.Tab{}, ports.ErrTabVersionConflict
	}
	if err := placeRoutines(ctx, tx, w.TenantID, t.TabID, t.RoutineIDs); err != nil {
		return domain.Tab{}, err
	}
	after, err := r.getTab(ctx, tx, w.TenantID, t.TabID, false)
	if err != nil {
		return domain.Tab{}, err
	}
	if err := recordTabAudit(ctx, tx, w, "pen_routine_tab.updated", t.TabID, &before, &after); err != nil {
		return domain.Tab{}, err
	}
	if err := completeIdempotency(ctx, tx, w.TenantID, idemScopeTab, w.IdempotencyKey, tabResource, t.TabID); err != nil {
		return domain.Tab{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Tab{}, fmt.Errorf("pen routine: commit tab update: %w", err)
	}
	return after, nil
}

// SetTabStatus retires or restores a tab, fenced on the row version. Its routines stay placed on
// it, so restoring puts the tab back exactly as it was.
func (r *Repository) SetTabStatus(ctx context.Context, w ports.WriteParams, tabID, status string, rowVersion int) (domain.Tab, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Tab{}, fmt.Errorf("pen routine: begin tab status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	reservation, err := reserveIdempotency(ctx, tx, w.TenantID, idemScopeTab, w.IdempotencyKey, requestFingerprint("status", w.ActorID, tabID, status, strconv.Itoa(rowVersion)))
	if err != nil {
		return domain.Tab{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Tab{}, err
		}
		if reservation.resultID == "" {
			return domain.Tab{}, ports.ErrIdempotencyConflict
		}
		return r.getTab(ctx, r.pool, w.TenantID, reservation.resultID, false)
	}
	before, err := r.getTab(ctx, tx, w.TenantID, tabID, true)
	if err != nil {
		return domain.Tab{}, err
	}
	if rowVersion != 0 && rowVersion != before.RowVersion {
		return domain.Tab{}, ports.ErrTabVersionConflict
	}
	if _, err := tx.Exec(ctx, sqlTabStatus, w.TenantID, tabID, status, nullIfEmpty(w.ActorID), r.now().UTC()); err != nil {
		return domain.Tab{}, fmt.Errorf("pen routine: tab status: %w", err)
	}
	after, err := r.getTab(ctx, tx, w.TenantID, tabID, false)
	if err != nil {
		return domain.Tab{}, err
	}
	if err := recordTabAudit(ctx, tx, w, "pen_routine_tab.status_changed", tabID, &before, &after); err != nil {
		return domain.Tab{}, err
	}
	if err := completeIdempotency(ctx, tx, w.TenantID, idemScopeTab, w.IdempotencyKey, tabResource, tabID); err != nil {
		return domain.Tab{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Tab{}, fmt.Errorf("pen routine: commit tab status: %w", err)
	}
	return after, nil
}

// PhoneTabsFor lists the active tabs one person's bar carries.
func (r *Repository) PhoneTabsFor(ctx context.Context, tenantID, userID string) ([]domain.PhoneTab, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	bound := sqlbind.MustBind(sqlPhoneTabsFor, tenantID, userID)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("pen routine: phone tabs: %w", err)
	}
	defer rows.Close()
	out := []domain.PhoneTab{}
	for rows.Next() {
		var t domain.PhoneTab
		if err := rows.Scan(&t.Key, &t.Label, &t.ModuleKey, &t.IconKey); err != nil {
			return nil, fmt.Errorf("pen routine: scan phone tab: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// freeTabKey picks base, base_2, base_3 ... -- the first key the tenant does not use yet.
func freeTabKey(ctx context.Context, tx pgx.Tx, tenantID, base string) (string, error) {
	rows, err := tx.Query(ctx, sqlTabKeysLike, tenantID, base)
	if err != nil {
		return "", fmt.Errorf("pen routine: tab keys: %w", err)
	}
	taken := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return "", err
		}
		taken[k] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	if !taken[base] {
		return base, nil
	}
	for n := 2; n < 1000; n++ {
		candidate := base + "_" + strconv.Itoa(n)
		if !taken[candidate] {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: too many tabs share this name", domain.ErrInvalidTab)
}

// placeRoutines makes routineIDs exactly the tab's routines: the named ones move onto it (from
// whichever tab they were on) and any other routine on it goes back to Routines only. Every named
// routine must exist in the tenant, or the whole write is refused.
func placeRoutines(ctx context.Context, tx pgx.Tx, tenantID, tabID string, routineIDs []string) error {
	if routineIDs == nil {
		routineIDs = []string{}
	}
	if _, err := tx.Exec(ctx, sqlTabClearRoutines, tenantID, tabID, routineIDs); err != nil {
		return fmt.Errorf("pen routine: clear tab routines: %w", err)
	}
	if len(routineIDs) == 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, sqlTabPlaceRoutines, tenantID, tabID, routineIDs)
	if err != nil {
		return fmt.Errorf("pen routine: place tab routines: %w", err)
	}
	if int(tag.RowsAffected()) != len(routineIDs) {
		return fmt.Errorf("%w: a routine on this tab is no longer available; reload and choose again", domain.ErrInvalidTab)
	}
	return nil
}

func tabFingerprint(op, actorID, tabID string, rowVersion int, t domain.Tab) string {
	return requestFingerprint(op, actorID, tabID, strconv.Itoa(rowVersion), t.Label, t.ModuleKey, t.IconKey,
		strings.Join(t.Filters, ","), strings.Join(t.RoutineIDs, ","))
}

func recordTabAudit(ctx context.Context, tx pgx.Tx, w ports.WriteParams, action, tabID string, before, after *domain.Tab) error {
	ev := audit.Event{
		TenantID:     w.TenantID,
		ActorID:      w.ActorID,
		ActorType:    "human",
		Action:       action,
		ResourceType: tabResource,
		ResourceID:   tabID,
		ScopeType:    "tenant",
		ScopeID:      w.TenantID,
		TraceID:      w.TraceID,
		Metadata:     map[string]any{"domain": auditDomain, "module": auditDomain, "idempotency_key": w.IdempotencyKey, "operation_id": w.IdempotencyKey},
	}
	if before != nil {
		ev.BeforeState = tabAuditState(*before)
	}
	if after != nil {
		ev.AfterState = tabAuditState(*after)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, ev); err != nil {
		return fmt.Errorf("pen routine: audit tab: %w", err)
	}
	return nil
}

func tabAuditState(t domain.Tab) map[string]any {
	return map[string]any{
		"tab_key":     t.Key,
		"label":       t.Label,
		"module_key":  t.ModuleKey,
		"icon_key":    t.IconKey,
		"filters":     t.Filters,
		"status":      t.Status,
		"routine_ids": t.RoutineIDs,
	}
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlTabsSelect = `
SELECT tb.tab_id::text, tb.tenant_id::text, tb.tab_key, tb.label, tb.module_key, tb.icon_key, tb.filters, tb.status, tb.row_version, tb.updated_at,
  COALESCE((SELECT jsonb_agg(jsonb_build_object('routine_id', d.routine_id::text, 'name', d.name, 'park_name', COALESCE(park.name, '')) ORDER BY park.name, d.name)
    FROM pen_routine_definitions d
    LEFT JOIN locations park ON park.tenant_id = d.tenant_id AND park.location_id = d.park_id
    WHERE d.tenant_id = tb.tenant_id AND d.tab_id = tb.tab_id AND d.status <> 'retired'), '[]'::jsonb)
FROM pen_routine_tabs tb
WHERE tb.tenant_id = $1::uuid`
	sqlTabInsert = `
INSERT INTO pen_routine_tabs (tenant_id, tab_key, label, module_key, icon_key, filters, created_by, created_at, updated_by, updated_at)
VALUES ($1::uuid, $2, $3, $4, $5, $6::text[], $7::uuid, $8, $7::uuid, $8)
RETURNING tab_id::text`
	sqlTabUpdate = `
UPDATE pen_routine_tabs
SET label = $3, module_key = $4, icon_key = $5, filters = $6::text[], updated_by = $7::uuid, updated_at = $8, row_version = row_version + 1
WHERE tenant_id = $1::uuid AND tab_id = $2::uuid AND row_version = $9`
	sqlTabStatus = `
UPDATE pen_routine_tabs
SET status = $3, updated_by = $4::uuid, updated_at = $5, row_version = row_version + 1
WHERE tenant_id = $1::uuid AND tab_id = $2::uuid`
	sqlTabKeysLike = `
SELECT tab_key FROM pen_routine_tabs
WHERE tenant_id = $1::uuid AND left(tab_key, length($2)) = $2`
	sqlTabClearRoutines = `
UPDATE pen_routine_definitions SET tab_id = NULL
WHERE tenant_id = $1::uuid AND tab_id = $2::uuid AND NOT (routine_id = ANY($3::uuid[]))`
	sqlTabPlaceRoutines = `
UPDATE pen_routine_definitions SET tab_id = $2::uuid
WHERE tenant_id = $1::uuid AND routine_id = ANY($3::uuid[])`
)

// sqlPhoneTabsFor: the active tabs holding at least one un-retired routine the user owes, under
// the SAME person-or-role resolution as the task list (RoleHoldersFromSQL), so the bar never
// offers a tab whose list would be empty by construction.
var sqlPhoneTabsFor = `
SELECT tb.tab_key, tb.label, tb.module_key, tb.icon_key
FROM pen_routine_tabs tb
WHERE tb.tenant_id = $1::uuid AND tb.status = 'active'
  AND EXISTS (
    SELECT 1 FROM pen_routine_definitions prd
    WHERE prd.tenant_id = tb.tenant_id AND prd.tab_id = tb.tab_id AND prd.status <> 'retired'
      AND EXISTS (SELECT 1 ` + RoleHoldersFromSQL("prd.tenant_id", "prd.assignee_roles", "prd.park_id", "prd.assignee_user_id") + `
        AND rg.user_id = $2::uuid))
ORDER BY lower(tb.label), tb.tab_key`
