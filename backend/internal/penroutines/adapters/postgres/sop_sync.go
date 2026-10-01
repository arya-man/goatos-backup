package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
)

// PHONE-TASK SOP SYNC (docs/decisions/simple-task-phone-tabs.md): a phone-task SOP published on a
// module's SOP page becomes, in the SAME transaction as the publish, one phone tab and one routine
// per park. The routine engine then runs it unchanged. Every derived row carries sop_code, and a
// row carrying it is never edited from /routines.

// PhoneTaskSOPEvent is one publish or retire of a phone-task SOP version.
type PhoneTaskSOPEvent struct {
	TenantID string
	ActorID  string
	SOPCode  string
	SOPName  string
	// Status is "published" or "retired".
	Status string
	// StillPublished reports, after the flip, whether any version of the SOP is published.
	StillPublished bool
	FormDSL        map[string]any
}

// SyncPhoneTaskSOP writes (or retires) the tab and routines a phone-task SOP derives, inside the
// caller's transaction. An SOP whose version carries no phone_task section is left alone.
func (r *Repository) SyncPhoneTaskSOP(ctx context.Context, tx pgx.Tx, ev PhoneTaskSOPEvent) error {
	if ev.Status != "published" {
		if ev.StillPublished {
			return nil
		}
		// The SOP is no longer published: its tab leaves every bar and its routines raise nothing
		// new. Open tasks are untouched, the same as retiring a routine by hand.
		if err := execBound(ctx, tx, sqlSOPRetireTab, ev.TenantID, ev.SOPCode, nullIfEmpty(ev.ActorID), r.now().UTC()); err != nil {
			return fmt.Errorf("pen routine: retire sop tab: %w", err)
		}
		if err := execBound(ctx, tx, sqlSOPRetireRoutines, ev.TenantID, ev.SOPCode, []string{}, nullIfEmpty(ev.ActorID), r.now().UTC()); err != nil {
			return fmt.Errorf("pen routine: retire sop routines: %w", err)
		}
		return nil
	}
	if !domain.HasPhoneTask(ev.FormDSL) {
		return nil
	}
	doc, err := domain.ParsePhoneTask(ev.SOPCode, ev.SOPName, ev.FormDSL)
	if err != nil {
		return err
	}
	now := r.now().UTC()
	var tabID string
	if err := queryRowBound(ctx, tx, sqlSOPUpsertTab, ev.TenantID, domain.PhoneTabKeyForSOPCode(ev.SOPCode), strings.TrimSpace(ev.SOPName),
		domain.PhoneModuleForSOPCode(ev.SOPCode), doc.Tab.Icon, nonNilStrings(doc.Tab.Filters), ev.SOPCode, nullIfEmpty(ev.ActorID), now).Scan(&tabID); err != nil {
		return fmt.Errorf("pen routine: write sop tab: %w", err)
	}
	existing := map[string]string{}
	bound := sqlbind.MustBind(sqlSOPRoutines, ev.TenantID, ev.SOPCode)
	rows, err := tx.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return fmt.Errorf("pen routine: read sop routines: %w", err)
	}
	for rows.Next() {
		var routineID, parkID string
		if err := rows.Scan(&routineID, &parkID); err != nil {
			rows.Close()
			return err
		}
		existing[parkID] = routineID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	today := r.today()
	parks := make([]string, 0, len(doc.Parks))
	for _, park := range doc.Parks { // scale-guard:ignore: bounded by the tenant's parks (two today) in ONE authored document; each park is one routine write.
		d := doc.DefinitionFor(ev.SOPName, park, today)
		if err := resolveAssignee(ctx, tx, ev.TenantID, d.ParkID, &d); err != nil {
			return err
		}
		evidenceJSON, err := json.Marshal(d.Evidence)
		if err != nil {
			return err
		}
		parks = append(parks, d.ParkID)
		routineID, ok := existing[d.ParkID]
		action := "pen_routine.updated"
		if ok {
			before, err := r.getRoutine(ctx, tx, ev.TenantID, routineID, true)
			if err != nil {
				return err
			}
			next := before.Definition.CurrentVersion + 1
			if err := execBound(ctx, tx, sqlAuthoring2, ev.TenantID, routineID, next, strings.TrimSpace(d.Name), d.Instruction, evidenceJSON, d.ReviewKind, nullIfEmpty(ev.ActorID), now); err != nil {
				return fmt.Errorf("pen routine: sop version: %w", err)
			}
			if err := execBound(ctx, tx, sqlAuthoring3,
				ev.TenantID, routineID, strings.TrimSpace(d.Name), d.ScopeKind, d.OccupiedOnly, d.CadenceKind,
				toInt16s(d.Weekdays), toInt16s(d.MonthDays), domain.SortWorkKinds(d.AfterWorkKinds), d.DueOffsetDays,
				d.NotifyTime, d.ReviewKind, next, nullIfEmpty(ev.ActorID), now, before.Definition.RowVersion,
				intervalArg(d), d.StartDate, domain.SortRoles(d.AssigneeRoles), nullIfEmpty(d.AssigneeUserID),
			); err != nil {
				return mapAuthoringError(err, "sop update")
			}
			if err := execBound(ctx, tx, sqlAuthoring4, ev.TenantID, routineID); err != nil {
				return fmt.Errorf("pen routine: sop clear pens: %w", err)
			}
		} else {
			action = "pen_routine.created"
			if err := queryRowBound(ctx, tx, sqlAuthoring1,
				ev.TenantID, d.ParkID, strings.TrimSpace(d.Name), d.ScopeKind, d.OccupiedOnly, d.CadenceKind,
				toInt16s(d.Weekdays), toInt16s(d.MonthDays), domain.SortWorkKinds(d.AfterWorkKinds), d.DueOffsetDays,
				d.NotifyTime, d.ReviewKind, nullIfEmpty(ev.ActorID), now,
				intervalArg(d), d.StartDate, domain.SortRoles(d.AssigneeRoles), nullIfEmpty(d.AssigneeUserID),
			).Scan(&routineID); err != nil {
				return mapAuthoringError(err, "sop create")
			}
			if err := execBound(ctx, tx, sqlAuthoring2, ev.TenantID, routineID, 1, strings.TrimSpace(d.Name), d.Instruction, evidenceJSON, d.ReviewKind, nullIfEmpty(ev.ActorID), now); err != nil {
				return fmt.Errorf("pen routine: sop first version: %w", err)
			}
		}
		if err := writeMembers(ctx, tx, ev.TenantID, routineID, d); err != nil {
			return err
		}
		if err := execBound(ctx, tx, sqlSOPStampRoutine, ev.TenantID, routineID, tabID, ev.SOPCode); err != nil {
			return fmt.Errorf("pen routine: stamp sop routine: %w", err)
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID: ev.TenantID, ActorID: ev.ActorID, ActorType: "human", Action: action,
			ResourceType: routineResource, ResourceID: routineID, ScopeType: "park", ScopeID: d.ParkID,
			AfterState: routineAuditState(d),
			Metadata:   map[string]any{"domain": auditDomain, "module": auditDomain, "sop_code": ev.SOPCode},
		}); err != nil {
			return fmt.Errorf("pen routine: audit sop routine: %w", err)
		}
	}
	// A park the new version no longer names stops raising work.
	if err := execBound(ctx, tx, sqlSOPRetireRoutines, ev.TenantID, ev.SOPCode, parks, nullIfEmpty(ev.ActorID), now); err != nil {
		return fmt.Errorf("pen routine: retire dropped sop routines: %w", err)
	}
	return nil
}

// execBound and queryRowBound run a package-level statement through the bind-contract check.
func execBound(ctx context.Context, tx pgx.Tx, query string, args ...any) error {
	bound := sqlbind.MustBind(query, args...)
	_, err := tx.Exec(ctx, bound.SQL(), bound.Args()...)
	return err
}

func queryRowBound(ctx context.Context, tx pgx.Tx, query string, args ...any) pgx.Row {
	bound := sqlbind.MustBind(query, args...)
	return tx.QueryRow(ctx, bound.SQL(), bound.Args()...)
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlSOPUpsertTab = `
INSERT INTO pen_routine_tabs (tenant_id, tab_key, label, module_key, icon_key, filters, sop_code, status, created_by, created_at, updated_by, updated_at)
VALUES ($1::uuid, $2, $3, $4, $5, $6::text[], $7, 'active', $8::uuid, $9, $8::uuid, $9)
ON CONFLICT (tenant_id, sop_code) WHERE sop_code IS NOT NULL DO UPDATE
SET label = EXCLUDED.label, module_key = EXCLUDED.module_key, icon_key = EXCLUDED.icon_key, filters = EXCLUDED.filters,
    status = 'active', updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at, row_version = pen_routine_tabs.row_version + 1
RETURNING tab_id::text`
	sqlSOPRoutines = `
SELECT routine_id::text, park_id::text FROM pen_routine_definitions
WHERE tenant_id = $1::uuid AND sop_code = $2
FOR UPDATE`
	sqlSOPStampRoutine = `
UPDATE pen_routine_definitions SET tab_id = $3::uuid, sop_code = $4, status = 'active'
WHERE tenant_id = $1::uuid AND routine_id = $2::uuid`
	sqlSOPRetireRoutines = `
UPDATE pen_routine_definitions
SET status = 'retired', updated_by = $4::uuid, updated_at = $5, row_version = row_version + 1
WHERE tenant_id = $1::uuid AND sop_code = $2 AND status <> 'retired' AND NOT (park_id = ANY($3::uuid[]))`
	sqlSOPRetireTab = `
UPDATE pen_routine_tabs
SET status = 'retired', updated_by = $3::uuid, updated_at = $4, row_version = row_version + 1
WHERE tenant_id = $1::uuid AND sop_code = $2 AND status <> 'retired'`
)
