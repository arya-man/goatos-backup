package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// workedPen is one pen that had work of a mapped kind submitted on the business date.
type workedPen struct {
	parkID    string
	shedID    string
	partition string
	kinds     map[string]bool
}

// taskRow is one row of the set-based upsert.
type taskRow struct {
	routineID                             string
	version                               int
	parkID, shedID, partition             string
	kindsCSV                              string
	planned, due, workState, delayedSince string
	rolledForward                         int
}

// Materialize is ONE pass per business date: for every ACTIVE routine of the tenant, the pens
// that raise a task on that date -- the calendar cadences from Definition.RaisesOn, the
// after_work routines from the verification items created in that IST day whose category maps
// to one of the routine's kinds -- resolved through the routine's scope (all active pens of
// the park, occupied only by default, or the ticked list), written in ONE set-based upsert on
// the natural key. Idempotent: a replay inserts nothing and widens nothing. A task born late
// (worker outage, routine created mid-day) is due the LATER of its planned date and today,
// delayed with the date it was owed. A routine with no assignee raises nothing and is named.
//
// projection-review: membership=(active routine x pen) pairs for ONE business date, where the pen set is the routine's scope (pen catalog or ticked pens) intersected with the day's worked pens for after_work; group_key=(tenant_id, routine_id, shed_id, partition_key, planned_business_date) -- the natural unique constraint pen_routine_tasks_natural_uq, folded in Go before the UNNEST so no two rows of one statement share a key; join_cardinality=the worked-pen read joins nothing (DISTINCT over verification_items in the IST window on verification_items_created_pen_idx), the catalog read is the shed/partition LEFT JOIN keyed on the catalog PK, the upsert is one INSERT ... ON CONFLICT per statement; pagination=none -- bounded by routines x pens of one day (tens to low hundreds), never paged; scope=tenant, then per routine through its park and assignees, a routine with no assignee is skipped and named
func (r *Repository) Materialize(ctx context.Context, tenantID, businessDate, today string, now time.Time) (ports.MaterializeResult, error) {
	result := ports.MaterializeResult{BusinessDate: businessDate}
	if _, err := time.Parse("2006-01-02", businessDate); err != nil {
		return result, fmt.Errorf("pen routine: materialize: bad business date %q: %w", businessDate, err)
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("pen routine: begin materialize: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Every active routine of the tenant (a handful per park), with its pens and people.
	routines, err := r.readRoutines(ctx, tx, "d.status = $3", "", tenantID, today, domain.StatusActive)
	if err != nil {
		return result, err
	}
	if len(routines) == 0 {
		return result, tx.Commit(ctx)
	}
	needWorked, needCatalog := false, false
	for _, row := range routines {
		if row.Definition.CadenceKind == domain.CadenceAfterWork {
			needWorked = true
		}
		if row.Definition.ScopeKind == domain.ScopeAllPens {
			needCatalog = true
		}
	}

	// 2. The pens worked on the business date, for the after_work routines. Every submit of a
	// mapped kind already raises a verification item carrying park, shed and pen, so this ONE
	// read covers every kind; the category says which. Half-open instant window, sargable over
	// verification_items_created_pen_idx. Bounded by one day's proof items.
	worked := map[string]*workedPen{}
	if needWorked {
		categories := make([]string, 0, len(domain.WorkKinds))
		for _, kind := range domain.WorkKinds {
			categories = append(categories, domain.WorkKindCategory(kind))
		}
		wRows, err := tx.Query(ctx, sqlMaterialize1, tenantID, businessDate, categories)
		if err != nil {
			return result, fmt.Errorf("pen routine: materialize: worked pens: %w", err)
		}
		for wRows.Next() {
			var parkID, shedID, partition, category string
			if err := wRows.Scan(&parkID, &shedID, &partition, &category); err != nil {
				wRows.Close()
				return result, err
			}
			kind := domain.WorkKindForCategory(category)
			if kind == "" {
				continue
			}
			partition = strings.TrimSpace(partition)
			if oploc.NormalizePartition(partition) == oploc.WholeSentinel {
				partition = ""
			}
			key := parkID + "|" + domain.PenKey(shedID, partition)
			pen, ok := worked[key]
			if !ok {
				pen = &workedPen{parkID: parkID, shedID: shedID, partition: partition, kinds: map[string]bool{}}
				worked[key] = pen
			}
			pen.kinds[kind] = true
		}
		wRows.Close()
		if err := wRows.Err(); err != nil {
			return result, err
		}
	}

	// 3. The pen catalog of every park, for the all_pens routines (tens of rows per park).
	catalog := map[string][]catalogPen{}
	if needCatalog {
		pens, err := r.readPens(ctx, tx, tenantID, nil)
		if err != nil {
			return result, err
		}
		for _, p := range pens {
			catalog[p.ParkID] = append(catalog[p.ParkID], p)
		}
	}

	// 4. Resolve each routine's rows for the date and fold them on the natural key.
	rows := []taskRow{}
	seen := map[string]bool{}
	for _, row := range routines {
		d := row.Definition
		planned, err := d.PlannedDateFor(businessDate)
		if err != nil {
			return result, err
		}
		due, workState, delayedSince, rolled := planned, domain.WorkStateScheduled, "", 0
		if today > planned {
			due, workState, delayedSince = today, domain.WorkStateDelayed, planned
			if t, err := time.Parse("2006-01-02", today); err == nil {
				if p, err := time.Parse("2006-01-02", planned); err == nil {
					rolled = int(t.Sub(p).Hours() / 24)
				}
			}
		}
		type penHit struct {
			shedID, partition string
			kinds             []string
		}
		hits := []penHit{}
		switch d.CadenceKind {
		case domain.CadenceAfterWork:
			for _, pen := range worked {
				if pen.parkID != d.ParkID {
					continue
				}
				kinds := []string{}
				for _, k := range domain.SortWorkKinds(d.AfterWorkKinds) {
					if pen.kinds[k] {
						kinds = append(kinds, k)
					}
				}
				if len(kinds) == 0 || !inScope(d, pen.shedID, pen.partition) {
					continue
				}
				hits = append(hits, penHit{shedID: pen.shedID, partition: pen.partition, kinds: kinds})
			}
		default:
			if !d.RaisesOn(businessDate) {
				continue
			}
			switch d.ScopeKind {
			case domain.ScopeSelectedPens:
				for _, p := range d.Pens {
					hits = append(hits, penHit{shedID: p.ShedID, partition: p.Partition})
				}
			default:
				for _, p := range catalog[d.ParkID] {
					if d.OccupiedOnly && !p.Occupied {
						continue
					}
					hits = append(hits, penHit{shedID: p.ShedID, partition: p.Partition})
				}
			}
		}
		if len(d.AssigneeIDs) == 0 {
			// Loud, and no fallback: a routine nobody is assigned to raises nothing.
			if len(hits) > 0 {
				result.RoutinesWithoutAssignee = append(result.RoutinesWithoutAssignee, ports.RoutineRef{RoutineID: d.RoutineID, Name: d.Name, ParkID: d.ParkID})
				result.PensSkipped += len(hits)
			}
			continue
		}
		for _, h := range hits {
			key := d.RoutineID + "|" + domain.PenKey(h.shedID, h.partition) + "|" + planned
			if seen[key] {
				continue
			}
			seen[key] = true
			rows = append(rows, taskRow{
				routineID: d.RoutineID, version: d.CurrentVersion,
				parkID: d.ParkID, shedID: h.shedID, partition: h.partition,
				kindsCSV: strings.Join(h.kinds, ","),
				planned:  planned, due: due, workState: workState, delayedSince: delayedSince, rolledForward: rolled,
			})
		}
	}
	if len(rows) == 0 {
		return result, tx.Commit(ctx)
	}

	// 5. ONE set-based upsert for every row of the day.
	routineIDs := make([]string, 0, len(rows))
	versions := make([]int32, 0, len(rows))
	parkIDs := make([]string, 0, len(rows))
	shedIDs := make([]string, 0, len(rows))
	partitions := make([]string, 0, len(rows))
	kindsCSV := make([]string, 0, len(rows))
	planneds := make([]string, 0, len(rows))
	dues := make([]string, 0, len(rows))
	states := make([]string, 0, len(rows))
	delayedSinces := make([]string, 0, len(rows))
	rolleds := make([]int32, 0, len(rows))
	for _, row := range rows {
		routineIDs = append(routineIDs, row.routineID)
		versions = append(versions, int32(row.version))
		parkIDs = append(parkIDs, row.parkID)
		shedIDs = append(shedIDs, row.shedID)
		partitions = append(partitions, row.partition)
		kindsCSV = append(kindsCSV, row.kindsCSV)
		planneds = append(planneds, row.planned)
		dues = append(dues, row.due)
		states = append(states, row.workState)
		delayedSinces = append(delayedSinces, row.delayedSince)
		rolleds = append(rolleds, int32(row.rolledForward))
	}
	uRows, err := tx.Query(ctx, sqlMaterialize2, tenantID, routineIDs, versions, parkIDs, shedIDs, partitions, kindsCSV, businessDate, planneds, dues, states, delayedSinces, rolleds)
	if err != nil {
		return result, fmt.Errorf("pen routine: materialize: upsert: %w", err)
	}
	createdIDs := []string{}
	for uRows.Next() {
		var taskID string
		var inserted bool
		if err := uRows.Scan(&taskID, &inserted); err != nil {
			uRows.Close()
			return result, err
		}
		if inserted {
			createdIDs = append(createdIDs, taskID)
			result.Created++
		} else {
			result.Widened++
		}
	}
	uRows.Close()
	if err := uRows.Err(); err != nil {
		return result, err
	}

	// 6. Announce each created task: ONE batched read, then the audit row and the outbox
	// emit per task inside the same transaction.
	if len(createdIDs) > 0 {
		query := fmt.Sprintf(`SELECT %s %s WHERE t.tenant_id = $1::uuid AND t.task_id = ANY($2::uuid[]) ORDER BY t.task_id`, taskColumns, taskFrom)
		cRows, err := tx.Query(ctx, query, tenantID, createdIDs)
		if err != nil {
			return result, fmt.Errorf("pen routine: materialize: read created: %w", err)
		}
		created, err := scanTasks(cRows)
		cRows.Close()
		if err != nil {
			return result, err
		}
		recorder := audit.NewTxRecorder(tx)
		// scale-guard:ignore: bounded by the tasks THIS pass created (routines x pens of one day, tens to low hundreds); one audit row and one outbox emit per created task, inside the one materialize transaction.
		for _, task := range created {
			if err := recorder.Record(ctx, audit.Event{
				TenantID:     tenantID,
				ActorType:    "system",
				Action:       "pen_routine.created",
				ResourceType: resourceType,
				ResourceID:   task.TaskID,
				ScopeType:    "park",
				ScopeID:      task.ParkID,
				AfterState:   auditState(task),
				Metadata: map[string]any{
					"domain":        auditDomain,
					"module":        auditDomain,
					"business_date": businessDate,
				},
			}); err != nil {
				return result, fmt.Errorf("pen routine: audit created: %w", err)
			}
			if err := emitEvent(ctx, tx, EventRoutineCreated, task, "", "system_rule", "pen-routine:created:"+task.TaskID, now); err != nil {
				return result, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("pen routine: commit materialize: %w", err)
	}
	return result, nil
}

// inScope reports whether a worked pen falls inside an after_work routine's scope.
func inScope(d domain.Definition, shedID, partition string) bool {
	if d.ScopeKind != domain.ScopeSelectedPens {
		return true
	}
	want := domain.PenKey(shedID, partition)
	for _, p := range d.Pens {
		if domain.PenKey(p.ShedID, p.Partition) == want {
			return true
		}
	}
	return false
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	// The after_work read: pens with mapped work submitted on one IST business date. Half-open
	// instant window (sargable over verification_items_created_pen_idx), never a function of
	// the column. created_at is the SUBMIT instant.
	sqlMaterialize1 = `
SELECT DISTINCT vi.park_id::text, vi.shed_id::text, COALESCE(vi.partition_label, ''), vi.category
FROM verification_items vi
WHERE vi.tenant_id = $1::uuid
  AND vi.shed_id IS NOT NULL
  AND vi.park_id IS NOT NULL
  AND vi.created_at >= ($2::date::timestamp AT TIME ZONE 'Asia/Kolkata')
  AND vi.created_at <  (($2::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata')
  AND vi.category = ANY($3::text[])`
	// Set-based upsert on the natural key. An existing open task for the same pen and planned
	// date gains any new trigger kind (a pen dewormed after the first tick raised it for
	// vaccination); one with the verifier, verified or canceled is left alone; a calendar task
	// (empty kinds) never widens; (xmax = 0) says whether the row was inserted by THIS statement.
	sqlMaterialize2 = `
INSERT INTO pen_routine_tasks (
  tenant_id, routine_id, routine_version, park_id, shed_id, partition_label, trigger_kinds,
  source_business_date, planned_business_date, due_business_date, work_state,
  delayed_since_business_date, rolled_forward_count
)
SELECT $1::uuid, p.routine_id, p.routine_version, p.park_id, p.shed_id, NULLIF(p.partition_label, ''),
       CASE WHEN p.kinds_csv = '' THEN '{}'::text[] ELSE string_to_array(p.kinds_csv, ',') END,
       $8::date, p.planned::date, p.due::date, p.work_state, NULLIF(p.delayed_since, '')::date, p.rolled_forward
FROM unnest($2::uuid[], $3::integer[], $4::uuid[], $5::uuid[], $6::text[], $7::text[], $9::text[], $10::text[], $11::text[], $12::text[], $13::integer[])
  AS p(routine_id, routine_version, park_id, shed_id, partition_label, kinds_csv, planned, due, work_state, delayed_since, rolled_forward)
ON CONFLICT ON CONSTRAINT pen_routine_tasks_natural_uq DO UPDATE
SET trigger_kinds = (SELECT COALESCE(array_agg(DISTINCT k ORDER BY k), '{}'::text[]) FROM unnest(pen_routine_tasks.trigger_kinds || EXCLUDED.trigger_kinds) AS k),
    updated_at = now(),
    row_version = pen_routine_tasks.row_version + 1
WHERE pen_routine_tasks.work_state IN ('scheduled', 'delayed')
  AND pen_routine_tasks.status IN ('open', 'rework')
  AND NOT (pen_routine_tasks.trigger_kinds @> EXCLUDED.trigger_kinds)
RETURNING task_id::text, (xmax = 0) AS inserted`
)
