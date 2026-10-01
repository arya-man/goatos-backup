package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// PHONE TABS (docs/decisions/simple-task-phone-tabs.md): where a routine appears on the phone.
// The tab row carries the placement; a routine points at it through pen_routine_definitions.tab_id.

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

// nonNilStrings keeps an empty list an empty array: pgx binds a nil slice as NULL, and a tab with
// no filters is a real choice ("just the list"), not a missing value.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
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
