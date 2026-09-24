package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

type routineScan struct {
	d                                 domain.Definition
	parkName, notifyTime, instruction string
	weekdays, monthDays               []int16
	intervalDays                      *int32
	evidenceRaw, pensRaw              []byte
	createdBy, updatedBy              *string
	openToday, delayed                int
}

func (s *routineScan) targets() []any {
	d := &s.d
	return []any{
		&d.RoutineID, &d.TenantID, &d.ParkID, &s.parkName, &d.Name, &s.instruction,
		&d.ScopeKind, &d.OccupiedOnly, &d.CadenceKind, &s.weekdays, &s.monthDays, &d.AfterWorkKinds,
		&s.intervalDays, &d.StartDate, &d.AssigneeRoles,
		&d.DueOffsetDays, &s.notifyTime, &d.ReviewKind, &d.Status, &d.CurrentVersion, &s.evidenceRaw,
		&s.pensRaw, &s.createdBy, &s.updatedBy, &d.CreatedAt, &d.UpdatedAt, &d.RowVersion,
		&s.openToday, &s.delayed,
	}
}

type penJSON struct {
	ShedID         string `json:"shed_id"`
	ShedName       string `json:"shed_name"`
	PartitionLabel string `json:"partition_label"`
}

func (s *routineScan) finish() (ports.RoutineListRow, error) {
	d := s.d
	d.ParkName = strings.TrimSpace(s.parkName)
	d.Instruction = s.instruction
	d.NotifyTime = s.notifyTime
	d.Weekdays = toInts(s.weekdays)
	d.MonthDays = toInts(s.monthDays)
	if d.AfterWorkKinds == nil {
		d.AfterWorkKinds = []string{}
	}
	ev, err := domain.ParseEvidence(s.evidenceRaw)
	if err != nil {
		return ports.RoutineListRow{}, fmt.Errorf("pen routine: routine %s evidence: %w", d.RoutineID, err)
	}
	d.Evidence = ev
	var pens []penJSON
	if len(s.pensRaw) > 0 {
		if err := json.Unmarshal(s.pensRaw, &pens); err != nil {
			return ports.RoutineListRow{}, fmt.Errorf("pen routine: routine %s pens: %w", d.RoutineID, err)
		}
	}
	d.Pens = make([]domain.PenRef, 0, len(pens))
	for _, p := range pens {
		partition := strings.TrimSpace(p.PartitionLabel)
		if oploc.NormalizePartition(partition) == oploc.WholeSentinel {
			partition = ""
		}
		d.Pens = append(d.Pens, domain.PenRef{
			ShedID:    p.ShedID,
			Partition: partition,
			ShedName:  strings.TrimSpace(p.ShedName),
			Label:     oploc.OperationalLocation{ShedID: p.ShedID, ShedName: strings.TrimSpace(p.ShedName), PartitionLabel: partition}.Display(),
		})
	}
	if s.intervalDays != nil {
		d.IntervalDays = int(*s.intervalDays)
	}
	d.AssigneeRoles = domain.SortRoles(d.AssigneeRoles)
	if d.AssigneeRoles == nil {
		d.AssigneeRoles = []string{}
	}
	d.People = []domain.Assignee{}
	d.CreatedBy = deref(s.createdBy)
	d.UpdatedBy = deref(s.updatedBy)
	d.CreatedAt = d.CreatedAt.UTC()
	d.UpdatedAt = d.UpdatedAt.UTC()
	return ports.RoutineListRow{Definition: d, OpenToday: s.openToday, Delayed: s.delayed}, nil
}

// readRoutines runs the routine projection with an extra predicate; $1 is the tenant and $2
// the business date the open_today count answers for. The role holders of every routine read
// are attached by ONE batched read (listRoutineAssignees), never per routine.
func (r *Repository) readRoutines(ctx context.Context, q querier, where, lock string, args ...any) ([]ports.RoutineListRow, error) {
	query := fmt.Sprintf(`SELECT %s %s WHERE d.tenant_id = $1::uuid AND %s ORDER BY park.name, lower(v.name), d.routine_id%s`, routineColumns, routineFrom, where, lock)
	bound := sqlbind.MustBind(query, args...)
	rows, err := q.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("pen routine: read routines: %w", err)
	}
	out, err := scanRoutineRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(out))
	for _, row := range out {
		ids = append(ids, row.Definition.RoutineID)
	}
	people, err := listRoutineAssignees(ctx, q, args[0].(string), ids)
	if err != nil {
		return nil, err
	}
	attachRoutinePeople(out, people)
	return out, nil
}

// scanRoutineRows reads rows of the routineColumns projection.
func scanRoutineRows(rows pgx.Rows) ([]ports.RoutineListRow, error) {
	out := []ports.RoutineListRow{}
	for rows.Next() {
		var s routineScan
		if err := rows.Scan(s.targets()...); err != nil {
			return nil, fmt.Errorf("pen routine: scan routine: %w", err)
		}
		row, err := s.finish()
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pen routine: read routines: %w", err)
	}
	return out, nil
}

// attachRoutinePeople hangs each routine's role-holder preview off it, keyed by routine id.
func attachRoutinePeople(out []ports.RoutineListRow, people map[string][]domain.Assignee) {
	for i := range out {
		if list, ok := people[out[i].Definition.RoutineID]; ok {
			out[i].Definition.People = list
		}
	}
}

// ListRoutinesAndParks lists the routines of one park (or every park), with the two counts the
// web table shows and their role holders, plus the tenant's active park options -- three
// independent statements on ONE pgx.Batch, one round trip (P10). The role holders are keyed by the
// SAME tenant/park predicate as the routine read instead of waiting for its ids.
//
// projection-review: membership=pen_routine_definitions rows of ONE tenant (optionally one park), one row per routine (PK); group_key=(tenant_id, routine_id); join_cardinality=current version 1:1 on (tenant, routine, current_version) PK, park 1:1, pens as a jsonb_agg subquery, the role holders one batched read keyed by routine (attached in Go, never joined), the two counts correlated subqueries over pen_routine_tasks_routine_idx; pagination=none -- bounded by the authored routine estate (a handful per park); scope=tenant_id + optional park_id
func (r *Repository) ListRoutinesAndParks(ctx context.Context, p ports.RoutineListParams) ([]ports.RoutineListRow, []ports.Park, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	park := nullIfEmpty(p.ParkID)
	var (
		out    []ports.RoutineListRow
		people map[string][]domain.Assignee
		parks  []ports.Park
	)
	batch := &pgx.Batch{}
	// scale-guard:ignore: bounded by the authored routine estate of a tenant (a handful per park); no page needed.
	queueBound(batch, sqlListRoutines, p.TenantID, p.Today, park).Query(func(rows pgx.Rows) error {
		var err error
		out, err = scanRoutineRows(rows)
		return err
	})
	queueBound(batch, sqlRoutineAssigneesForScope, p.TenantID, park, domain.AssignableRoles).Query(func(rows pgx.Rows) error {
		var err error
		people, err = scanRoutineAssignees(rows)
		return err
	})
	batch.Queue(sqlAuthoring8, p.TenantID).Query(func(rows pgx.Rows) error {
		var err error
		parks, err = scanParks(rows)
		return err
	})
	if err := r.pool.SendBatch(ctx, batch).Close(); err != nil {
		return nil, nil, err
	}
	attachRoutinePeople(out, people)
	return out, parks, nil
}

// sqlListRoutines is the routine projection over one tenant ($1) and optional park ($3); $2 is
// the business date open_today answers for.
var sqlListRoutines = fmt.Sprintf(`SELECT %s %s WHERE d.tenant_id = $1::uuid AND ($3::uuid IS NULL OR d.park_id = $3::uuid) ORDER BY park.name, lower(v.name), d.routine_id`, routineColumns, routineFrom)

// GetRoutine reads one routine.
func (r *Repository) GetRoutine(ctx context.Context, tenantID, routineID string) (domain.Definition, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	row, err := r.getRoutine(ctx, r.pool, tenantID, routineID, false)
	return row.Definition, err
}

func (r *Repository) getRoutine(ctx context.Context, q querier, tenantID, routineID string, forUpdate bool) (ports.RoutineListRow, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE OF d"
	}
	rows, err := r.readRoutines(ctx, q, "d.routine_id = $3::uuid", lock, tenantID, r.today(), routineID)
	if err != nil {
		return ports.RoutineListRow{}, err
	}
	if len(rows) == 0 {
		return ports.RoutineListRow{}, ports.ErrRoutineNotFound
	}
	return rows[0], nil
}

func (r *Repository) today() string {
	return r.now().In(timeLocation()).Format("2006-01-02")
}

// CreateRoutine writes the definition (with its assignee roles), version 1 and the pens in one
// transaction under the author's idempotency key.
func (r *Repository) CreateRoutine(ctx context.Context, w ports.WriteParams, d domain.Definition) (domain.Definition, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: begin create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	evidenceJSON, err := json.Marshal(d.Evidence)
	if err != nil {
		return domain.Definition{}, err
	}
	reservation, err := reserveIdempotency(ctx, tx, w.TenantID, idemScopeAuthor, w.IdempotencyKey, requestFingerprint("create", w.ActorID, d.ParkID, d.Name, d.ScopeKind, d.CadenceKind, fmt.Sprint(d.IntervalDays), d.StartDate, strings.Join(d.AssigneeRoles, ","), string(evidenceJSON)))
	if err != nil {
		return domain.Definition{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Definition{}, err
		}
		if reservation.resultID == "" {
			return domain.Definition{}, ports.ErrIdempotencyConflict
		}
		return r.GetRoutine(ctx, w.TenantID, reservation.resultID)
	}
	now := r.now().UTC()
	var routineID string
	err = tx.QueryRow(ctx, sqlAuthoring1,
		w.TenantID, d.ParkID, strings.TrimSpace(d.Name), d.ScopeKind, d.OccupiedOnly, d.CadenceKind,
		toInt16s(d.Weekdays), toInt16s(d.MonthDays), domain.SortWorkKinds(d.AfterWorkKinds), d.DueOffsetDays,
		d.NotifyTime, d.ReviewKind, nullIfEmpty(w.ActorID), now,
		intervalArg(d), d.StartDate, domain.SortRoles(d.AssigneeRoles),
	).Scan(&routineID)
	if err != nil {
		return domain.Definition{}, mapAuthoringError(err, "create")
	}
	if _, err := tx.Exec(ctx, sqlAuthoring2, w.TenantID, routineID, 1, strings.TrimSpace(d.Name), d.Instruction, evidenceJSON, d.ReviewKind, nullIfEmpty(w.ActorID), now); err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: create version: %w", err)
	}
	if err := writeMembers(ctx, tx, w.TenantID, routineID, d); err != nil {
		return domain.Definition{}, err
	}
	after, err := r.getRoutine(ctx, tx, w.TenantID, routineID, false)
	if err != nil {
		return domain.Definition{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     w.TenantID,
		ActorID:      w.ActorID,
		ActorType:    "human",
		Action:       "pen_routine.created",
		ResourceType: routineResource,
		ResourceID:   routineID,
		ScopeType:    "park",
		ScopeID:      d.ParkID,
		AfterState:   routineAuditState(after.Definition),
		TraceID:      w.TraceID,
		Metadata:     map[string]any{"domain": auditDomain, "module": auditDomain, "idempotency_key": w.IdempotencyKey, "operation_id": w.IdempotencyKey},
	}); err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: audit create: %w", err)
	}
	if err := completeIdempotency(ctx, tx, w.TenantID, idemScopeAuthor, w.IdempotencyKey, routineResource, routineID); err != nil {
		return domain.Definition{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: commit create: %w", err)
	}
	return after.Definition, nil
}

// UpdateRoutine writes a NEW version row (the health-protocol rule: never edited in place),
// bumps current_version, replaces the pens and the people, fenced on the row version the
// screen loaded with. Open tasks keep the version they pinned.
func (r *Repository) UpdateRoutine(ctx context.Context, w ports.WriteParams, d domain.Definition) (domain.Definition, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: begin update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	evidenceJSON, err := json.Marshal(d.Evidence)
	if err != nil {
		return domain.Definition{}, err
	}
	reservation, err := reserveIdempotency(ctx, tx, w.TenantID, idemScopeAuthor, w.IdempotencyKey, requestFingerprint("update", w.ActorID, d.RoutineID, fmt.Sprint(d.RowVersion), d.Name, d.ScopeKind, d.CadenceKind, fmt.Sprint(d.IntervalDays), d.StartDate, strings.Join(d.AssigneeRoles, ","), string(evidenceJSON)))
	if err != nil {
		return domain.Definition{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Definition{}, err
		}
		if reservation.resultID == "" {
			return domain.Definition{}, ports.ErrIdempotencyConflict
		}
		return r.GetRoutine(ctx, w.TenantID, reservation.resultID)
	}
	before, err := r.getRoutine(ctx, tx, w.TenantID, d.RoutineID, true)
	if err != nil {
		return domain.Definition{}, err
	}
	if d.RowVersion != 0 && d.RowVersion != before.Definition.RowVersion {
		return domain.Definition{}, ports.ErrRoutineVersionConflict
	}
	if d.ParkID != "" && d.ParkID != before.Definition.ParkID {
		return domain.Definition{}, ports.ErrParkImmutable
	}
	now := r.now().UTC()
	nextVersion := before.Definition.CurrentVersion + 1
	if _, err := tx.Exec(ctx, sqlAuthoring2, w.TenantID, d.RoutineID, nextVersion, strings.TrimSpace(d.Name), d.Instruction, evidenceJSON, d.ReviewKind, nullIfEmpty(w.ActorID), now); err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: update version: %w", err)
	}
	tag, err := tx.Exec(ctx, sqlAuthoring3,
		w.TenantID, d.RoutineID, strings.TrimSpace(d.Name), d.ScopeKind, d.OccupiedOnly, d.CadenceKind,
		toInt16s(d.Weekdays), toInt16s(d.MonthDays), domain.SortWorkKinds(d.AfterWorkKinds), d.DueOffsetDays,
		d.NotifyTime, d.ReviewKind, nextVersion, nullIfEmpty(w.ActorID), now, before.Definition.RowVersion,
		intervalArg(d), d.StartDate, domain.SortRoles(d.AssigneeRoles),
	)
	if err != nil {
		return domain.Definition{}, mapAuthoringError(err, "update")
	}
	if tag.RowsAffected() != 1 {
		return domain.Definition{}, ports.ErrRoutineVersionConflict
	}
	if _, err := tx.Exec(ctx, sqlAuthoring4, w.TenantID, d.RoutineID); err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: clear pens: %w", err)
	}
	if err := writeMembers(ctx, tx, w.TenantID, d.RoutineID, d); err != nil {
		return domain.Definition{}, err
	}
	after, err := r.getRoutine(ctx, tx, w.TenantID, d.RoutineID, false)
	if err != nil {
		return domain.Definition{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     w.TenantID,
		ActorID:      w.ActorID,
		ActorType:    "human",
		Action:       "pen_routine.updated",
		ResourceType: routineResource,
		ResourceID:   d.RoutineID,
		ScopeType:    "park",
		ScopeID:      before.Definition.ParkID,
		BeforeState:  routineAuditState(before.Definition),
		AfterState:   routineAuditState(after.Definition),
		TraceID:      w.TraceID,
		Metadata:     map[string]any{"domain": auditDomain, "module": auditDomain, "new_version": nextVersion, "idempotency_key": w.IdempotencyKey, "operation_id": w.IdempotencyKey},
	}); err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: audit update: %w", err)
	}
	if err := completeIdempotency(ctx, tx, w.TenantID, idemScopeAuthor, w.IdempotencyKey, routineResource, d.RoutineID); err != nil {
		return domain.Definition{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: commit update: %w", err)
	}
	return after.Definition, nil
}

// SetRoutineStatus pauses, resumes or retires a routine, fenced on the row version. Open
// tasks are untouched by design.
func (r *Repository) SetRoutineStatus(ctx context.Context, w ports.WriteParams, routineID, status string, rowVersion int) (domain.Definition, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: begin status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	reservation, err := reserveIdempotency(ctx, tx, w.TenantID, idemScopeAuthor, w.IdempotencyKey, requestFingerprint("status", w.ActorID, routineID, status, fmt.Sprint(rowVersion)))
	if err != nil {
		return domain.Definition{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Definition{}, err
		}
		if reservation.resultID == "" {
			return domain.Definition{}, ports.ErrIdempotencyConflict
		}
		return r.GetRoutine(ctx, w.TenantID, reservation.resultID)
	}
	before, err := r.getRoutine(ctx, tx, w.TenantID, routineID, true)
	if err != nil {
		return domain.Definition{}, err
	}
	if rowVersion != 0 && rowVersion != before.Definition.RowVersion {
		return domain.Definition{}, ports.ErrRoutineVersionConflict
	}
	now := r.now().UTC()
	tag, err := tx.Exec(ctx, sqlAuthoring5, w.TenantID, routineID, status, nullIfEmpty(w.ActorID), now, before.Definition.RowVersion)
	if err != nil {
		return domain.Definition{}, mapAuthoringError(err, "status")
	}
	if tag.RowsAffected() != 1 {
		return domain.Definition{}, ports.ErrRoutineVersionConflict
	}
	after, err := r.getRoutine(ctx, tx, w.TenantID, routineID, false)
	if err != nil {
		return domain.Definition{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     w.TenantID,
		ActorID:      w.ActorID,
		ActorType:    "human",
		Action:       "pen_routine.status." + status,
		ResourceType: routineResource,
		ResourceID:   routineID,
		ScopeType:    "park",
		ScopeID:      before.Definition.ParkID,
		BeforeState:  routineAuditState(before.Definition),
		AfterState:   routineAuditState(after.Definition),
		TraceID:      w.TraceID,
		Metadata:     map[string]any{"domain": auditDomain, "module": auditDomain, "idempotency_key": w.IdempotencyKey, "operation_id": w.IdempotencyKey},
	}); err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: audit status: %w", err)
	}
	if err := completeIdempotency(ctx, tx, w.TenantID, idemScopeAuthor, w.IdempotencyKey, routineResource, routineID); err != nil {
		return domain.Definition{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Definition{}, fmt.Errorf("pen routine: commit status: %w", err)
	}
	return after.Definition, nil
}

// intervalArg is the stored interval_days: the N of an every_n_days routine, NULL otherwise.
func intervalArg(d domain.Definition) *int32 {
	if d.CadenceKind != domain.CadenceEveryNDays {
		return nil
	}
	v := int32(d.IntervalDays)
	return &v
}

// writeMembers inserts the ticked pens set-based (UNNEST), never per row.
func writeMembers(ctx context.Context, tx pgx.Tx, tenantID, routineID string, d domain.Definition) error {
	if d.ScopeKind == domain.ScopeSelectedPens && len(d.Pens) > 0 {
		sheds := make([]string, 0, len(d.Pens))
		partitions := make([]string, 0, len(d.Pens))
		for _, p := range d.Pens {
			sheds = append(sheds, p.ShedID)
			partitions = append(partitions, strings.TrimSpace(p.Partition))
		}
		if _, err := tx.Exec(ctx, sqlAuthoring6, tenantID, routineID, sheds, partitions); err != nil {
			return fmt.Errorf("pen routine: write pens: %w", err)
		}
	}
	return nil
}

func mapAuthoringError(err error, op string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "pen_routine_definitions_park_name_uq" {
		return ports.ErrNameTaken
	}
	return fmt.Errorf("pen routine: %s: %w", op, err)
}

func routineAuditState(d domain.Definition) map[string]any {
	return map[string]any{
		"park_id":          d.ParkID,
		"name":             d.Name,
		"scope_kind":       d.ScopeKind,
		"occupied_only":    d.OccupiedOnly,
		"cadence_kind":     d.CadenceKind,
		"weekdays":         d.Weekdays,
		"month_days":       d.MonthDays,
		"interval_days":    d.IntervalDays,
		"start_date":       d.StartDate,
		"after_work_kinds": d.AfterWorkKinds,
		"due_offset_days":  d.DueOffsetDays,
		"notify_time":      d.NotifyTime,
		"review_kind":      d.ReviewKind,
		"status":           d.Status,
		"current_version":  d.CurrentVersion,
		"evidence":         d.Evidence,
		"pens":             d.Pens,
		"assignee_roles":   d.AssigneeRoles,
		"row_version":      d.RowVersion,
	}
}

// scanParks reads (park id, name) rows of sqlAuthoring8: the tenant's active parks, by code
// (CBE before CPT, the all-parks order).
func scanParks(rows pgx.Rows) ([]ports.Park, error) {
	out := []ports.Park{}
	for rows.Next() {
		var p ports.Park
		if err := rows.Scan(&p.ParkID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pen routine: parks: %w", err)
	}
	return out, nil
}

// Catalog lists the active pens of one park and who holds each assignable role there, as ONE
// pgx.Batch (one round trip).
func (r *Repository) Catalog(ctx context.Context, tenantID, parkID string) ([]ports.CatalogPen, []ports.RoleHolders, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var (
		pens  []catalogPen
		roles []ports.RoleHolders
	)
	batch := &pgx.Batch{}
	// scale-guard:ignore: bounded by the authored shed/pen estate of a park (tens of rows); see readPens.
	queueBound(batch, sqlAuthoring9, tenantID, nullIfEmpty(parkID)).Query(func(rows pgx.Rows) error {
		var err error
		pens, err = scanPens(rows)
		return err
	})
	queueBound(batch, sqlRoleHoldersForPark, tenantID, parkID, domain.AssignableRoles).Query(func(rows pgx.Rows) error {
		var err error
		roles, err = scanRoleHolders(rows)
		return err
	})
	if err := r.pool.SendBatch(ctx, batch).Close(); err != nil {
		return nil, nil, err
	}
	out := make([]ports.CatalogPen, 0, len(pens))
	for _, p := range pens {
		out = append(out, p.CatalogPen)
	}
	return out, roles, nil
}

// catalogPen is one active pen with the park it belongs to.
type catalogPen struct {
	ports.CatalogPen
	ParkID string
}

// readPens is the ONE pen-catalog read: every ACTIVE pen of a park (or the tenant) from the
// partition catalog, LEFT JOINed so an undivided shed appears once as itself, legacy alias
// rows suppressed through oploc.PartitionAliasExclusionSQL, with an occupied flag from the
// pen's live residents. partition_label is selected, NEVER normalized_label (rule 5a).
//
// projection-review: membership=active non-alias sheds of the scope LEFT JOIN their active shed_partitions rows (an undivided shed contributes exactly one row, a divided shed one row per catalogued pen); group_key=(shed.location_id, sp.normalized_label); join_cardinality=shed_partitions is keyed (tenant, shed, normalized_label) so the LEFT JOIN cannot repeat a pen, the occupied flag is an EXISTS (never multiplies); pagination=none -- bounded by the authored shed/pen estate (tens per park), not by animals; scope=tenant_id + optional parent park
func (r *Repository) readPens(ctx context.Context, q querier, tenantID string, parkID *string) ([]catalogPen, error) {
	// scale-guard:ignore: bounded by the authored shed/pen estate of a tenant (tens of rows per park), served by the locations parent index and shed_partitions' own key; the occupied flag probes ONE pre-aggregated set of occupied (shed, pen) keys.
	bound := sqlbind.MustBind(sqlAuthoring9, tenantID, parkID)
	rows, err := q.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("pen routine: pens: %w", err)
	}
	defer rows.Close()
	return scanPens(rows)
}

// scanPens reads rows of the sqlAuthoring9 pen catalog.
func scanPens(rows pgx.Rows) ([]catalogPen, error) {
	out := []catalogPen{}
	for rows.Next() {
		var p catalogPen
		if err := rows.Scan(&p.ParkID, &p.ShedID, &p.ShedName, &p.Partition, &p.Occupied); err != nil {
			return nil, fmt.Errorf("pen routine: scan pen: %w", err)
		}
		p.Partition = strings.TrimSpace(p.Partition)
		if oploc.NormalizePartition(p.Partition) == oploc.WholeSentinel {
			p.Partition = ""
		}
		p.Label = oploc.OperationalLocation{ShedID: p.ShedID, ShedName: p.ShedName, PartitionLabel: p.Partition}.Display()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pen routine: pens: %w", err)
	}
	return out, nil
}

// queueBound queues one sqlbind-checked statement on a batch.
func queueBound(batch *pgx.Batch, query string, args ...any) *pgx.QueuedQuery {
	bound := sqlbind.MustBind(query, args...)
	return batch.Queue(bound.SQL(), bound.Args()...)
}

func nullIfEmpty(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func toInt16s(in []int) []int16 {
	out := make([]int16, 0, len(in))
	for _, v := range in {
		out = append(out, int16(v))
	}
	return out
}

func timeLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return time.UTC
	}
	return loc
}

// routineColumns is the routine projection: the definition (with its cadence parameters and
// assignee roles), its CURRENT version's renderable fields, the park name, the ticked pens as a
// JSON array, and the two counts the web table shows ($2 is the business date open_today
// answers for).
const routineColumns = `
d.routine_id::text, d.tenant_id::text, d.park_id::text, COALESCE(park.name, ''), v.name, v.instruction,
d.scope_kind, d.occupied_only, d.cadence_kind, d.weekdays, d.month_days, d.after_work_kinds,
d.interval_days, d.start_date::text, d.assignee_roles,
d.due_offset_days, to_char(d.notify_time, 'HH24:MI'), d.review_kind, d.status, d.current_version, v.evidence,
COALESCE((SELECT jsonb_agg(jsonb_build_object('shed_id', p.shed_id::text, 'shed_name', COALESCE(NULLIF(s.name, ''), s.location_code, ''), 'partition_label', COALESCE(p.partition_label, '')) ORDER BY s.display_order, s.name, p.partition_key)
          FROM pen_routine_pens p LEFT JOIN locations s ON s.tenant_id = p.tenant_id AND s.location_id = p.shed_id
          WHERE p.tenant_id = d.tenant_id AND p.routine_id = d.routine_id), '[]'::jsonb),
d.created_by::text, d.updated_by::text, d.created_at, d.updated_at, d.row_version,
(SELECT count(*)::int FROM pen_routine_tasks t WHERE t.tenant_id = d.tenant_id AND t.routine_id = d.routine_id AND t.work_state IN ('scheduled', 'delayed') AND t.due_business_date = $2::date),
(SELECT count(*)::int FROM pen_routine_tasks t WHERE t.tenant_id = d.tenant_id AND t.routine_id = d.routine_id AND t.work_state = 'delayed')`

const routineFrom = `
FROM pen_routine_definitions d
JOIN pen_routine_versions v ON v.tenant_id = d.tenant_id AND v.routine_id = d.routine_id AND v.version = d.current_version
LEFT JOIN locations park ON park.tenant_id = d.tenant_id AND park.location_id = d.park_id`

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlAuthoring1 = `
INSERT INTO pen_routine_definitions (
  tenant_id, park_id, name, scope_kind, occupied_only, cadence_kind, weekdays, month_days, after_work_kinds,
  due_offset_days, notify_time, review_kind, status, current_version, created_by, updated_by, created_at, updated_at,
  interval_days, start_date, assignee_roles
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6, $7::smallint[], $8::smallint[], $9::text[],
  $10, $11::time, $12, 'active', 1, $13::uuid, $13::uuid, $14::timestamptz, $14::timestamptz,
  $15::integer, $16::date, $17::text[]
)
RETURNING routine_id::text`
	sqlAuthoring2 = `
INSERT INTO pen_routine_versions (tenant_id, routine_id, version, name, instruction, evidence, review_kind, created_by, created_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::jsonb, $7, $8::uuid, $9::timestamptz)`
	sqlAuthoring3 = `
UPDATE pen_routine_definitions
SET name = $3, scope_kind = $4, occupied_only = $5, cadence_kind = $6, weekdays = $7::smallint[], month_days = $8::smallint[],
    after_work_kinds = $9::text[], due_offset_days = $10, notify_time = $11::time, review_kind = $12,
    current_version = $13, updated_by = $14::uuid, updated_at = $15::timestamptz, row_version = row_version + 1,
    interval_days = $17::integer, start_date = $18::date, assignee_roles = $19::text[]
WHERE tenant_id = $1::uuid AND routine_id = $2::uuid AND row_version = $16`
	sqlAuthoring4 = `
DELETE FROM pen_routine_pens WHERE tenant_id = $1::uuid AND routine_id = $2::uuid`
	sqlAuthoring5 = `
UPDATE pen_routine_definitions
SET status = $3, updated_by = $4::uuid, updated_at = $5::timestamptz, row_version = row_version + 1
WHERE tenant_id = $1::uuid AND routine_id = $2::uuid AND row_version = $6`
	sqlAuthoring6 = `
INSERT INTO pen_routine_pens (tenant_id, routine_id, shed_id, partition_label)
SELECT $1::uuid, $2::uuid, p.shed_id, NULLIF(p.partition_label, '')
FROM unnest($3::uuid[], $4::text[]) AS p(shed_id, partition_label)
ON CONFLICT DO NOTHING`
	sqlAuthoring8 = `
SELECT location_id::text, COALESCE(NULLIF(name, ''), location_code, '')
FROM locations
WHERE tenant_id = $1::uuid AND location_type = 'park' AND status = 'active'
ORDER BY location_code, name`
)

// sqlAuthoring9's occupied flag probes ONE pre-aggregated set of occupied (shed, normalized pen)
// keys. It used to be a correlated EXISTS that re-scanned goat_shed_partitions (and re-ran the
// regexp over every row) once per pen: 67 pens x 746 rows on the OCI clone, 26 ms of a 34 ms read.
// The set is built from the same live goats with the same normalization, so the flag is identical.
// occ is an OCCUPANCY set (which pens hold a live goat), not the partition catalog: the catalog
// rows come from shed_partitions below, so empty partitions still appear with occupied=false.
var sqlAuthoring9 = `
WITH occ AS MATERIALIZED (
  SELECT g.shed_id,
         regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') AS norm
  FROM goats g
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
  WHERE g.tenant_id = $1::uuid
    AND g.lifecycle_status = 'alive' AND g.exited_at IS NULL
    AND g.shed_id IN (SELECT s.location_id FROM locations s
                      WHERE s.tenant_id = $1::uuid AND s.location_type = 'shed'
                        AND ($2::uuid IS NULL OR s.parent_location_id = $2::uuid))
  GROUP BY 1, 2
)
SELECT shed.parent_location_id::text,
       shed.location_id::text,
       COALESCE(NULLIF(shed.name, ''), shed.location_code, ''),
       COALESCE(sp.partition_label, ''),
       EXISTS (
         SELECT 1 FROM occ
         WHERE occ.shed_id = shed.location_id
           AND (sp.normalized_label IS NULL OR occ.norm = sp.normalized_label)
       ) AS occupied
FROM locations shed
LEFT JOIN shed_partitions sp
       ON sp.tenant_id = shed.tenant_id AND sp.shed_id = shed.location_id AND sp.status = 'active'
WHERE shed.tenant_id = $1::uuid
  AND ($2::uuid IS NULL OR shed.parent_location_id = $2::uuid)
  AND shed.location_type = 'shed'
  AND shed.status = 'active'
  AND shed.retired_at IS NULL
  AND ` + aliasExclusion + `
  AND (sp.normalized_label IS NOT NULL
       OR NOT EXISTS (SELECT 1 FROM shed_partitions any_sp WHERE any_sp.tenant_id = shed.tenant_id AND any_sp.shed_id = shed.location_id AND any_sp.status = 'active'))
ORDER BY shed.parent_location_id, shed.display_order, shed.name, shed.location_id, sp.display_order NULLS LAST, sp.normalized_label NULLS FIRST`

var aliasExclusion = oploc.PartitionAliasExclusionSQL("shed")
