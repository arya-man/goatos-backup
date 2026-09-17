// Package postgres persists pen routines and their tasks (migration 000320).
//
// Boundary: this module reads its own pen_routine_* tables, the org tables every module may
// read (locations, workforce_members, shed_partitions, the person-access tables), the
// verification_items table it triggers on, and -- for the occupied-pen flag of the pen
// catalog and the all-pens scope ONLY -- goats + goat_shed_partitions. That last read is an
// EXISTS over live residents of one pen; nothing here resolves an animal's identity.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

const (
	idemScopePresence = "pen_routine.presence"
	idemScopeSubmit   = "pen_routine.submit"
	idemScopeAuthor   = "pen_routine.author"
	resourceType      = "pen_routine_task"
	routineResource   = "pen_routine"
	auditDomain       = "pen_routines"
)

// Repository implements ports.Repository over pgx.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
	now     func() time.Time
}

// NewRepository wires the repository.
func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Repository{pool: pool, timeout: timeout, now: time.Now}
}

// WithClock pins the clock, for tests.
func (r *Repository) WithClock(now func() time.Time) *Repository {
	r.now = now
	return r
}

var _ ports.Repository = (*Repository)(nil)

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// taskColumns is the single projection every task read uses: the task row, the PINNED
// version's renderable fields (name, instruction, evidence, review), the definition's cadence
// (read live, for the reason line), the scope, the park and shed names, and the people who hold
// the routine's roles for the task's park (assignees.go, the ONE resolution) so the row can
// answer "may this caller work it" without a second read.
var taskColumns = sqlRepository1

const taskFrom = sqlRepository2

type taskScan struct {
	t                             domain.Task
	partition, parkName, shedName string
	cadenceKind                   string
	weekdays, monthDays           []int16
	afterWorkKinds                []string
	intervalDays                  *int32
	dueOffsetDays                 int
	evidenceRaw, answersRaw       []byte
	proofsRaw, assigneesRaw       []byte
	enteredBy, submittedBy        *string
	verifiedBy, reworkReason      *string
}

func (s *taskScan) targets() []any {
	t := &s.t
	return []any{
		&t.TaskID, &t.TenantID, &t.RoutineID, &t.RoutineVersion, &t.RoutineName, &t.Instruction,
		&s.evidenceRaw, &t.ReviewKind,
		&s.cadenceKind, &s.weekdays, &s.monthDays, &s.afterWorkKinds, &s.intervalDays, &s.dueOffsetDays,
		&t.ScopeKind, &t.ParkID, &s.parkName, &t.ShedID, &s.shedName, &s.partition,
		&t.TriggerKinds, &t.SourceDate, &t.PlannedDate, &t.DueDate, &t.WorkState, &t.Status,
		&s.answersRaw, &s.proofsRaw,
		&t.EnteredAt, &s.enteredBy, &t.LeftAt, &s.submittedBy, &t.SubmittedAt, &s.verifiedBy, &t.VerifiedAt,
		&s.reworkReason, &t.RolledFwd, &t.DelayedSince, &t.RowVersion, &t.CreatedAt, &t.UpdatedAt,
		&s.assigneesRaw,
	}
}

func (s *taskScan) finish() (domain.Task, error) {
	t := s.t
	t.ParkName = strings.TrimSpace(s.parkName)
	t.ShedName = strings.TrimSpace(s.shedName)
	t.Partition = strings.TrimSpace(s.partition)
	if oploc.NormalizePartition(t.Partition) == oploc.WholeSentinel {
		t.Partition = ""
	}
	if t.ShedID == "" {
		// A whole-park task names no pen: every pen field stays "".
		t.ShedName, t.Partition, t.PenLabel = "", "", ""
	} else {
		t.PenLabel = oploc.OperationalLocation{ShedID: t.ShedID, ShedName: t.ShedName, PartitionLabel: t.Partition}.Display()
	}
	ev, err := domain.ParseEvidence(s.evidenceRaw)
	if err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: task %s pinned evidence: %w", t.TaskID, err)
	}
	t.Evidence = ev
	interval := 0
	if s.intervalDays != nil {
		interval = int(*s.intervalDays)
	}
	t.CadenceLine = domain.CadenceLine(domain.Definition{
		CadenceKind:    s.cadenceKind,
		IntervalDays:   interval,
		Weekdays:       toInts(s.weekdays),
		MonthDays:      toInts(s.monthDays),
		AfterWorkKinds: s.afterWorkKinds,
		DueOffsetDays:  s.dueOffsetDays,
	})
	t.Answers = map[string]any{}
	if len(s.answersRaw) > 0 {
		if err := json.Unmarshal(s.answersRaw, &t.Answers); err != nil {
			return domain.Task{}, fmt.Errorf("pen routine: task %s answers: %w", t.TaskID, err)
		}
	}
	t.Proofs = []domain.ProofItem{}
	if len(s.proofsRaw) > 0 {
		if err := json.Unmarshal(s.proofsRaw, &t.Proofs); err != nil {
			return domain.Task{}, fmt.Errorf("pen routine: task %s proofs: %w", t.TaskID, err)
		}
	}
	t.EnteredBy = deref(s.enteredBy)
	t.SubmittedBy = deref(s.submittedBy)
	t.VerifiedBy = deref(s.verifiedBy)
	t.ReworkReason = strings.TrimSpace(deref(s.reworkReason))
	var people []domain.Assignee
	if len(s.assigneesRaw) > 0 {
		if err := json.Unmarshal(s.assigneesRaw, &people); err != nil {
			return domain.Task{}, fmt.Errorf("pen routine: task %s assignees: %w", t.TaskID, err)
		}
	}
	t.AssigneeIDs = make([]string, 0, len(people))
	t.AssigneeNames = make([]string, 0, len(people))
	for _, p := range people {
		t.AssigneeIDs = append(t.AssigneeIDs, p.UserID)
		t.AssigneeNames = append(t.AssigneeNames, strings.TrimSpace(p.DisplayName))
	}
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	for _, at := range []**time.Time{&t.EnteredAt, &t.LeftAt, &t.SubmittedAt, &t.VerifiedAt} {
		if *at != nil {
			v := (*at).UTC()
			*at = &v
		}
	}
	return t, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toInts(in []int16) []int {
	out := make([]int, 0, len(in))
	for _, v := range in {
		out = append(out, int(v))
	}
	return out
}

func scanTask(row pgx.Row) (domain.Task, error) {
	var s taskScan
	if err := row.Scan(s.targets()...); err != nil {
		return domain.Task{}, err
	}
	return s.finish()
}

func scanTasks(rows pgx.Rows) ([]domain.Task, error) {
	out := []domain.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("pen routine: scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListMine pages the tasks owed on the routines whose roles the caller holds for the park. Within a chip, the
// most recently due first, keyset on (due_business_date, task_id). Counts range over the SAME
// assignee predicate as the rows.
//
// projection-review: membership=pen_routine_tasks rows whose routine's roles the caller holds for the routine's park (assigneePredicate: an IN over the tenant's routines with an EXISTS over the role holders, so a caller holding several matching grants never multiplies a task), one row per task (PK); group_key=(tenant_id, task_id) for the page and work_state for the counts over the SAME predicate; join_cardinality=definition 1:1 (PK), version 1:1 (PK on tenant, routine, version), locations park/shed 1:1 (PK, the shed LEFT JOIN null for a whole-park task), the resolved assignees a jsonb_agg subquery; pagination=keyset on (due_business_date DESC, task_id DESC) with LIMIT, counts whole-list; scope=tenant_id + caller predicate on both reads
func (r *Repository) ListMine(ctx context.Context, p ports.ListParams) (ports.Page, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	states := p.States
	if len(states) == 0 {
		states = []string{domain.WorkStateScheduled, domain.WorkStateDelayed}
	}
	args := []any{p.TenantID, p.UserID, states}
	where := "t.tenant_id = $1 AND " + assigneePredicate + " AND t.work_state = ANY($3::text[])"
	if p.Cursor != "" {
		due, id, ok := decodeCursor(p.Cursor)
		if !ok {
			return ports.Page{}, ports.ErrInvalidArgument
		}
		args = append(args, due, id)
		where += fmt.Sprintf(" AND (t.due_business_date, t.task_id) < ($%d::date, $%d::uuid)", len(args)-1, len(args))
	}
	query := fmt.Sprintf(`SELECT %s %s WHERE %s ORDER BY t.due_business_date DESC, t.task_id DESC LIMIT %d`, taskColumns, taskFrom, where, limit+1)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return ports.Page{}, fmt.Errorf("pen routine: list: %w", err)
	}
	out, err := scanTasks(rows)
	rows.Close()
	if err != nil {
		return ports.Page{}, err
	}
	page := ports.Page{StateCounts: map[string]int{}}
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		page.NextCursor = encodeCursor(last.DueDate, last.TaskID)
	}
	page.Rows = out
	countRows, err := r.pool.Query(ctx, sqlRepository3, p.TenantID, p.UserID)
	if err != nil {
		return ports.Page{}, fmt.Errorf("pen routine: counts: %w", err)
	}
	defer countRows.Close()
	for countRows.Next() {
		var state string
		var n int
		if err := countRows.Scan(&state, &n); err != nil {
			return ports.Page{}, err
		}
		page.StateCounts[state] = n
	}
	return page, countRows.Err()
}

func encodeCursor(due, id string) string { return due + "|" + id }

func decodeCursor(c string) (string, string, bool) {
	parts := strings.SplitN(c, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if _, err := time.Parse("2006-01-02", parts[0]); err != nil {
		return "", "", false
	}
	if _, err := uuid.Parse(parts[1]); err != nil {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// GetTask reads one task.
func (r *Repository) GetTask(ctx context.Context, tenantID, taskID string) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.getRow(ctx, r.pool, tenantID, taskID, false)
}

func (r *Repository) getRow(ctx context.Context, q querier, tenantID, taskID string, forUpdate bool) (domain.Task, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE OF t"
	}
	query := fmt.Sprintf(`SELECT %s %s WHERE t.tenant_id = $1 AND t.task_id = $2%s`, taskColumns, taskFrom, lock)
	t, err := scanTask(q.QueryRow(ctx, query, tenantID, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: get: %w", err)
	}
	return t, nil
}

// OpenCount answers the badge: tasks still awaiting work on the routines one person is on.
func (r *Repository) OpenCount(ctx context.Context, tenantID, userID string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var n int
	if err := r.pool.QueryRow(ctx, sqlRepository4, tenantID, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("pen routine: open count: %w", err)
	}
	return n, nil
}

// RecordPresence writes one punch -- enter or leave -- on a task: one transaction with the
// idempotency reservation, the row lock, the domain rule re-run on the locked row, the
// presence row, the denormalized stamps on the task (fenced), and audit. No outbox event: a
// punch is evidence on the task, not a business fact of its own.
func (r *Repository) RecordPresence(ctx context.Context, p ports.PresenceParams) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: begin presence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.Actor.UserID, p.TaskID, p.EventType, p.CapturedAt.UTC().Format(time.RFC3339))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopePresence, p.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.Task{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Task{}, err
		}
		if reservation.resultID == "" {
			return domain.Task{}, ports.ErrIdempotencyConflict
		}
		return r.getRow(ctx, r.pool, p.TenantID, reservation.resultID, false)
	}
	before, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, true)
	if err != nil {
		return domain.Task{}, err
	}
	if err := domain.CheckPresence(before, p.Actor, p.EventType, p.RowVersion); err != nil {
		return domain.Task{}, err
	}
	now := r.now().UTC()
	capturedAt := p.CapturedAt.UTC()
	if capturedAt.IsZero() {
		capturedAt = now
	}
	if err := insertPresence(ctx, tx, p.TenantID, p.TaskID, p.Actor.UserID, p.EventType, capturedAt, now, p.Location, p.Integrity, p.IdempotencyKey); err != nil {
		return domain.Task{}, err
	}
	var tag interface{ RowsAffected() int64 }
	if p.EventType == domain.PresenceEnter {
		tag, err = tx.Exec(ctx, sqlRepository5, p.TenantID, p.TaskID, capturedAt, p.Actor.UserID, now, before.RowVersion)
	} else {
		tag, err = tx.Exec(ctx, sqlRepository6, p.TenantID, p.TaskID, capturedAt, now, before.RowVersion)
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: presence update: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.Task{}, domain.ErrVersionConflict
	}
	after, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, false)
	if err != nil {
		return domain.Task{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.Actor.UserID,
		ActorType:    "human",
		Action:       "pen_routine.presence." + p.EventType,
		ResourceType: resourceType,
		ResourceID:   p.TaskID,
		ScopeType:    "park",
		ScopeID:      after.ParkID,
		BeforeState:  auditState(before),
		AfterState:   auditState(after),
		TraceID:      p.TraceID,
		Metadata: map[string]any{
			"domain":          auditDomain,
			"module":          auditDomain,
			"event_type":      p.EventType,
			"captured_at":     capturedAt.Format(time.RFC3339),
			"location":        p.Location,
			"integrity":       p.Integrity,
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: audit presence: %w", err)
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopePresence, p.IdempotencyKey, resourceType, p.TaskID); err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: complete presence idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: commit presence: %w", err)
	}
	return after, nil
}

func insertPresence(ctx context.Context, tx pgx.Tx, tenantID, taskID, userID, eventType string, capturedAt, recordedAt time.Time, loc domain.PresenceLocation, integ domain.PresenceIntegrity, idempotencyKey string) error {
	locJSON, err := json.Marshal(loc)
	if err != nil {
		return err
	}
	integJSON, err := json.Marshal(integ)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, sqlRepository7, tenantID, taskID, userID, eventType, capturedAt, recordedAt, locJSON, integJSON, idempotencyKey); err != nil {
		return fmt.Errorf("pen routine: presence row: %w", err)
	}
	return nil
}

// Submit records the answers and captures and applies the routine's review outcome -- one
// transaction: idempotency reservation, row lock, answers validated against the PINNED
// version's evidence, the submit rule re-run on the locked row, the update fenced on the row
// version, the auto-leave when the submitter is still checked in, audit, outbox. A routine
// reviewed by the verifier locks the row pending; one with no review completes it on BOTH
// dimensions here.
func (r *Repository) Submit(ctx context.Context, p ports.SubmitParams) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: begin submit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	answersFingerprint, err := canonicalJSON(p.Answers)
	if err != nil {
		return domain.Task{}, fmt.Errorf("%w: answers", ports.ErrInvalidArgument)
	}
	proofsFingerprint, _ := json.Marshal(p.Proofs)
	fingerprint := requestFingerprint(p.Actor.UserID, p.TaskID, answersFingerprint, string(proofsFingerprint))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeSubmit, p.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.Task{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Task{}, err
		}
		if reservation.resultID == "" {
			return domain.Task{}, ports.ErrIdempotencyConflict
		}
		return r.getRow(ctx, r.pool, p.TenantID, reservation.resultID, false)
	}

	before, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, true)
	if err != nil {
		return domain.Task{}, err
	}
	answers, err := domain.CheckAnswers(before.Evidence, p.Answers)
	if err != nil {
		return domain.Task{}, err
	}
	if err := domain.CheckSubmit(before, p.Actor, p.Proofs, p.RowVersion); err != nil {
		return domain.Task{}, err
	}
	now := r.now().UTC()
	submittedAt := p.CapturedAt.UTC()
	if submittedAt.IsZero() || submittedAt.After(now) {
		submittedAt = now
	}
	workState, status := domain.SubmitOutcome(before.ReviewKind)
	if workState == "" {
		workState = before.WorkState
	}
	answersJSON, err := json.Marshal(answers)
	if err != nil {
		return domain.Task{}, err
	}
	proofs := p.Proofs
	if proofs == nil {
		proofs = []domain.ProofItem{}
	}
	proofsJSON, err := json.Marshal(proofs)
	if err != nil {
		return domain.Task{}, err
	}
	// The submit is the leave when the submitter is still in the pen: the row records it and
	// the task's left_at is stamped in the same statement.
	leaving := before.InPen(p.Actor)
	tag, err := tx.Exec(ctx, sqlRepository8, p.TenantID, p.TaskID, workState, status, answersJSON, proofsJSON, p.Actor.UserID, submittedAt, now, before.RowVersion, leaving)
	if err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: submit update: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.Task{}, domain.ErrVersionConflict
	}
	if leaving {
		if err := insertPresence(ctx, tx, p.TenantID, p.TaskID, p.Actor.UserID, domain.PresenceLeave, submittedAt, now, p.Location, p.Integrity, p.IdempotencyKey+":leave"); err != nil {
			return domain.Task{}, err
		}
	}
	after, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, false)
	if err != nil {
		return domain.Task{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.Actor.UserID,
		ActorType:    "human",
		Action:       "pen_routine.submitted",
		ResourceType: resourceType,
		ResourceID:   p.TaskID,
		ScopeType:    "park",
		ScopeID:      after.ParkID,
		BeforeState:  auditState(before),
		AfterState:   auditState(after),
		TraceID:      p.TraceID,
		Metadata: map[string]any{
			"domain":          auditDomain,
			"module":          auditDomain,
			"review_kind":     after.ReviewKind,
			"proof_refs":      proofs,
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: audit submit: %w", err)
	}
	// The submitted event's idempotency key carries the row version so a redo after a rework
	// mints a fresh verification item while a retry of the same submit collapses.
	if err := emitEvent(ctx, tx, EventRoutineSubmitted, after, p.Actor.UserID, "human", fmt.Sprintf("%s:%d", p.TaskID, after.RowVersion), now); err != nil {
		return domain.Task{}, err
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeSubmit, p.IdempotencyKey, resourceType, p.TaskID); err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: complete submit idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("pen routine: commit submit: %w", err)
	}
	return after, nil
}

// canonicalJSON renders the raw answer map with sorted keys, so two submits carrying the same
// answers in a different key order fingerprint alike.
func canonicalJSON(raw map[string]json.RawMessage) (string, error) {
	if raw == nil {
		return "{}", nil
	}
	generic := map[string]any{}
	for k, v := range raw {
		var val any
		if len(v) == 0 {
			continue
		}
		if err := json.Unmarshal(v, &val); err != nil {
			return "", err
		}
		generic[k] = val
	}
	b, err := json.Marshal(generic) // encoding/json sorts map keys
	return string(b), err
}

// ApplyVerified flips an approved task pending_verification -> completed on BOTH dimensions
// and stamps verified_by/at. Runs from the verification.verdict.approved consumer, never from
// a phone. IDEMPOTENT: a re-delivered verdict on a row no longer pending applies nothing.
func (r *Repository) ApplyVerified(ctx context.Context, p ports.VerdictParams) (ports.VerdictResult, error) {
	return r.applyVerdict(ctx, p, true)
}

// BounceForRework flips a rejected task pending_verification -> rework with the verifier's
// reason. The answers and captures stay on the row as history; the next submit replaces them.
func (r *Repository) BounceForRework(ctx context.Context, p ports.VerdictParams) (ports.VerdictResult, error) {
	return r.applyVerdict(ctx, p, false)
}

func (r *Repository) applyVerdict(ctx context.Context, p ports.VerdictParams, approve bool) (ports.VerdictResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.VerdictResult{}, fmt.Errorf("pen routine: begin verdict: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, true)
	if errors.Is(err, ports.ErrTaskNotFound) {
		return ports.VerdictResult{}, tx.Commit(ctx)
	}
	if err != nil {
		return ports.VerdictResult{}, err
	}
	if before.Status != domain.StatusPendingVerification {
		return ports.VerdictResult{Task: before}, tx.Commit(ctx)
	}
	now := r.now().UTC()
	action := "pen_routine.verified"
	query := sqlRepository9
	args := []any{p.TenantID, p.TaskID, strings.TrimSpace(p.VerifiedBy), now, before.RowVersion}
	if !approve {
		action = "pen_routine.rework"
		query = sqlRepository10
		args = []any{p.TenantID, p.TaskID, strings.TrimSpace(p.Reason), now, before.RowVersion}
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return ports.VerdictResult{}, fmt.Errorf("pen routine: apply verdict: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ports.VerdictResult{Task: before}, tx.Commit(ctx)
	}
	after, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, false)
	if err != nil {
		return ports.VerdictResult{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      strings.TrimSpace(p.VerifiedBy),
		ActorType:    "verifier",
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   p.TaskID,
		ScopeType:    "park",
		ScopeID:      after.ParkID,
		BeforeState:  auditState(before),
		AfterState:   auditState(after),
		TraceID:      p.TraceID,
		Metadata: map[string]any{
			"domain": auditDomain,
			"module": auditDomain,
			"source": "pen-routine-verification",
			"reason": strings.TrimSpace(p.Reason),
		},
	}); err != nil {
		return ports.VerdictResult{}, fmt.Errorf("pen routine: audit verdict: %w", err)
	}
	if approve {
		if err := emitEvent(ctx, tx, EventRoutineVerified, after, strings.TrimSpace(p.VerifiedBy), "system_rule", fmt.Sprintf("%s:%d", p.TaskID, after.RowVersion), now); err != nil {
			return ports.VerdictResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.VerdictResult{}, fmt.Errorf("pen routine: commit verdict: %w", err)
	}
	return ports.VerdictResult{Applied: true, Task: after}, nil
}

func auditState(t domain.Task) map[string]any {
	return map[string]any{
		"routine_id":            t.RoutineID,
		"routine_version":       t.RoutineVersion,
		"park_id":               t.ParkID,
		"shed_id":               t.ShedID,
		"partition_label":       t.Partition,
		"pen_label":             t.PenLabel,
		"trigger_kinds":         domain.SortWorkKinds(t.TriggerKinds),
		"source_business_date":  t.SourceDate,
		"planned_business_date": t.PlannedDate,
		"due_business_date":     t.DueDate,
		"work_state":            t.WorkState,
		"status":                t.Status,
		"answers":               t.Answers,
		"proof_refs":            t.Proofs,
		"entered_at":            t.EnteredAt,
		"left_at":               t.LeftAt,
		"rework_reason":         t.ReworkReason,
		"row_version":           t.RowVersion,
	}
}

// ListForPark is the web Today table: one park, one due date, keyset on task_id over
// pen_routine_tasks_park_day_idx, with the whole-filter summary by farm bucket.
//
// projection-review: membership=pen_routine_tasks rows of ONE tenant, park and due_business_date (canceled excluded, optional routine filter), one row per task (PK); group_key=(tenant_id, task_id) for the page and the derived bucket for the summary over the SAME predicate; join_cardinality=definition/version/locations 1:1 on their PKs, the resolved assignees a jsonb_agg subquery; pagination=keyset on task_id ASC with LIMIT, summary whole-filter; scope=tenant_id, park_id, due_business_date (+ routine_id) repeated verbatim in both reads
func (r *Repository) ListForPark(ctx context.Context, p ports.ParkListParams) (ports.ParkPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := p.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	var routine, cursor *string
	if strings.TrimSpace(p.RoutineID) != "" {
		v := strings.TrimSpace(p.RoutineID)
		routine = &v
	}
	if strings.TrimSpace(p.Cursor) != "" {
		v := strings.TrimSpace(p.Cursor)
		if _, err := uuid.Parse(v); err != nil {
			return ports.ParkPage{}, ports.ErrInvalidArgument
		}
		cursor = &v
	}
	query := fmt.Sprintf(`SELECT %s %s WHERE %s AND ($5::uuid IS NULL OR t.task_id > $5::uuid) ORDER BY t.task_id LIMIT %d`, taskColumns, taskFrom, parkDayPredicate, limit+1)
	rows, err := r.pool.Query(ctx, query, p.TenantID, p.ParkID, p.BusinessDate, routine, cursor)
	if err != nil {
		return ports.ParkPage{}, fmt.Errorf("pen routine: park list: %w", err)
	}
	out, err := scanTasks(rows)
	rows.Close()
	if err != nil {
		return ports.ParkPage{}, err
	}
	page := ports.ParkPage{}
	if len(out) > limit {
		out = out[:limit]
		page.NextCursor = out[len(out)-1].TaskID
	}
	page.Rows = out
	sRows, err := r.pool.Query(ctx, sqlRepository11, p.TenantID, p.ParkID, p.BusinessDate, routine)
	if err != nil {
		return ports.ParkPage{}, fmt.Errorf("pen routine: park summary: %w", err)
	}
	defer sRows.Close()
	for sRows.Next() {
		var workState, status string
		var n int
		if err := sRows.Scan(&workState, &status, &n); err != nil {
			return ports.ParkPage{}, err
		}
		switch {
		case status == domain.StatusPendingVerification:
			page.Summary.InReview += n
		case status == domain.StatusRework:
			page.Summary.SentBack += n
		case workState == domain.WorkStateCompleted:
			page.Summary.Done += n
		case workState == domain.WorkStateDelayed:
			page.Summary.Delayed += n
		case workState == domain.WorkStateScheduled:
			page.Summary.Due += n
		}
	}
	return page, sRows.Err()
}

// DueDigests reads the open tasks due on dueDate, folded per routine, for the day's push.
//
// projection-review: membership=pen_routine_tasks rows of ONE tenant still awaiting work (scheduled/delayed, open/rework) whose due_business_date is the asked date, on pen_routine_tasks_sweep_due_idx; group_key=routine_id, folded in Go from rows ordered by routine; join_cardinality=definition/version/locations 1:1 on PK, the resolved assignees a jsonb_agg subquery; pagination=none -- bounded by one day's open routine tasks (pens x routines, tens to low hundreds); scope=tenant_id + due date + open states
func (r *Repository) DueDigests(ctx context.Context, tenantID, dueDate string) ([]ports.DueDigest, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlRepository12, tenantID, dueDate)
	if err != nil {
		return nil, fmt.Errorf("pen routine: due digests: %w", err)
	}
	defer rows.Close()
	digests := map[string]*ports.DueDigest{}
	order := []string{}
	for rows.Next() {
		var s taskScan
		var notifyTime string
		if err := rows.Scan(append([]any{&notifyTime}, s.targets()...)...); err != nil {
			return nil, fmt.Errorf("pen routine: due digest scan: %w", err)
		}
		task, err := s.finish()
		if err != nil {
			return nil, err
		}
		d, ok := digests[task.RoutineID]
		if !ok {
			d = &ports.DueDigest{
				RoutineID:   task.RoutineID,
				RoutineName: task.RoutineName,
				ParkID:      task.ParkID,
				ParkName:    task.ParkName,
				NotifyTime:  notifyTime,
				AssigneeIDs: task.AssigneeIDs,
				DueDate:     task.DueDate,
			}
			digests[task.RoutineID] = d
			order = append(order, task.RoutineID)
		}
		d.Tasks = append(d.Tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ports.DueDigest, 0, len(order))
	for _, key := range order {
		out = append(out, *digests[key])
	}
	return out, nil
}

// SweepRollForward moves tasks still awaiting work whose due date has passed to today as
// 'delayed' -- the PC Care / pen-visit kernel shape: chunked, FOR UPDATE SKIP LOCKED, capped.
// A task with the verifier is not late: the gate reads submitted_at, never review state.
func (r *Repository) SweepRollForward(ctx context.Context, tenantID string, asOf time.Time, chunkSize, maxChunks int) (ports.SweepResult, error) {
	if chunkSize < 1 {
		chunkSize = 200
	}
	if maxChunks < 1 {
		maxChunks = 50
	}
	today := biztime.BusinessDate(asOf)
	result := ports.SweepResult{}
	for chunk := 0; chunk < maxChunks; chunk++ {
		ctxChunk, cancel := context.WithTimeout(ctx, r.timeout)
		// scale-guard:ignore: bounded chunked claim over pen_routine_tasks_sweep_due_idx (tenant_id, work_state, due_business_date, task_id) with FOR UPDATE SKIP LOCKED; each pass touches at most chunkSize rows and the loop is capped at maxChunks.
		tag, err := r.pool.Exec(ctxChunk, sqlRepository13, tenantID, today, chunkSize)
		cancel()
		if err != nil {
			return result, fmt.Errorf("pen routine: sweep roll-forward: %w", err)
		}
		moved := int(tag.RowsAffected())
		result.RolledForward += moved
		if moved < chunkSize {
			return result, nil
		}
	}
	result.Truncated = true
	return result, nil
}

// assigneePredicate admits the rows of routines whose roles the caller ($2) holds for the
// routine's park -- the ONE role resolution in assignees.go.
var assigneePredicate = RoutinesHeldBySQL("t", "$1::uuid", "$2::uuid")

// parkDayPredicate binds the web Today table: one tenant, one park, one due date, optional
// routine, canceled excluded.
const parkDayPredicate = `t.tenant_id = $1::uuid AND t.park_id = $2::uuid AND t.due_business_date = $3::date AND t.work_state <> 'canceled' AND ($4::uuid IS NULL OR t.routine_id = $4::uuid)`

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
var sqlRepository1 = `
t.task_id::text, t.tenant_id::text, t.routine_id::text, t.routine_version, v.name, v.instruction,
v.evidence, v.review_kind,
d.cadence_kind, d.weekdays, d.month_days, d.after_work_kinds, d.interval_days, d.due_offset_days,
d.scope_kind, t.park_id::text, COALESCE(park.name, ''),
COALESCE(t.shed_id::text, ''), COALESCE(NULLIF(shed.name, ''), shed.location_code, ''), COALESCE(t.partition_label, ''),
t.trigger_kinds, t.source_business_date::text, t.planned_business_date::text, t.due_business_date::text,
t.work_state, t.status,
t.answers, t.proof_refs,
t.entered_at, t.entered_by::text, t.left_at, t.submitted_by::text, t.submitted_at, t.verified_by::text, t.verified_at,
t.rework_reason, t.rolled_forward_count, t.delayed_since_business_date::text, t.row_version, t.created_at, t.updated_at,
` + assigneesJSONSQL

var (
	sqlRepository3 = `
SELECT t.work_state, count(*)::int
FROM pen_routine_tasks t
WHERE t.tenant_id = $1 AND ` + assigneePredicate + `
GROUP BY t.work_state`
	sqlRepository4 = `
SELECT count(*)::int
FROM pen_routine_tasks t
WHERE t.tenant_id = $1 AND ` + assigneePredicate + `
  AND t.work_state IN ('scheduled', 'delayed') AND t.status IN ('open', 'rework')`
	sqlRepository12 = `
SELECT to_char(d.notify_time, 'HH24:MI'), ` + taskColumns + ` ` + taskFrom + `
WHERE t.tenant_id = $1::uuid
  AND t.work_state IN ('scheduled', 'delayed')
  AND t.due_business_date = $2::date
  AND t.status IN ('open', 'rework')
  AND d.status = 'active'
ORDER BY t.routine_id, t.shed_id, t.partition_key, t.task_id`
)

const (
	sqlRepository2 = `
FROM pen_routine_tasks t
JOIN pen_routine_definitions d ON d.tenant_id = t.tenant_id AND d.routine_id = t.routine_id
JOIN pen_routine_versions v ON v.tenant_id = t.tenant_id AND v.routine_id = t.routine_id AND v.version = t.routine_version
LEFT JOIN locations park ON park.tenant_id = t.tenant_id AND park.location_id = t.park_id
LEFT JOIN locations shed ON shed.tenant_id = t.tenant_id AND shed.location_id = t.shed_id`
	// Enter: stamp the check-in; a fresh enter after a leave clears the old left_at.
	sqlRepository5 = `
UPDATE pen_routine_tasks
SET entered_at = $3::timestamptz,
    entered_by = $4::uuid,
    left_at = NULL,
    updated_at = $5::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2::uuid
  AND work_state IN ('scheduled', 'delayed')
  AND status IN ('open', 'rework')
  AND row_version = $6`
	sqlRepository6 = `
UPDATE pen_routine_tasks
SET left_at = $3::timestamptz,
    updated_at = $4::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2::uuid
  AND work_state IN ('scheduled', 'delayed')
  AND status IN ('open', 'rework')
  AND row_version = $5`
	sqlRepository7 = `
INSERT INTO pen_routine_task_presence (tenant_id, task_id, user_id, event_type, captured_at, recorded_at, location, integrity, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::timestamptz, $6::timestamptz, $7::jsonb, $8::jsonb, $9)
ON CONFLICT ON CONSTRAINT pen_routine_task_presence_idem_uq DO NOTHING`
	// Submit: the gate flips per the review kind; a rework resubmit clears the verifier's old
	// reason. $11 says whether the submitter was still in the pen, so left_at is stamped here.
	sqlRepository8 = `
UPDATE pen_routine_tasks
SET work_state = $3,
    status = $4,
    answers = $5::jsonb,
    proof_refs = $6::jsonb,
    submitted_by = $7::uuid,
    submitted_at = $8::timestamptz,
    left_at = CASE WHEN $11::boolean THEN $8::timestamptz ELSE left_at END,
    rework_reason = NULL,
    updated_at = $9::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2::uuid
  AND work_state IN ('scheduled', 'delayed')
  AND status IN ('open', 'rework')
  AND row_version = $10`
	// Approve: both dimensions complete in one statement, fenced on the row version.
	sqlRepository9 = `
UPDATE pen_routine_tasks
SET status = 'completed',
    work_state = 'completed',
    verified_by = NULLIF($3::text, '')::uuid,
    verified_at = $4::timestamptz,
    updated_at = $4::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2::uuid
  AND status = 'pending_verification'
  AND row_version = $5`
	// Reject: back to the assignee with the verifier's words; the kernel clock keeps running.
	// The check-in stamps are cleared so a redo starts with a fresh pen check-in when the
	// routine asks for one (the presence rows stay as history); a rework re-collects everything.
	sqlRepository10 = `
UPDATE pen_routine_tasks
SET status = 'rework',
    rework_reason = NULLIF($3::text, ''),
    entered_at = NULL,
    entered_by = NULL,
    left_at = NULL,
    updated_at = $4::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2::uuid
  AND status = 'pending_verification'
  AND row_version = $5`
	sqlRepository11 = `
SELECT t.work_state, t.status, count(*)::int
FROM pen_routine_tasks t
WHERE ` + parkDayPredicate + `
GROUP BY t.work_state, t.status`
	sqlRepository13 = `
UPDATE pen_routine_tasks t
SET work_state = 'delayed',
    delayed_since_business_date = COALESCE(t.delayed_since_business_date, t.due_business_date),
    due_business_date = $2::date,
    rolled_forward_count = t.rolled_forward_count + 1,
    updated_at = now(),
    row_version = t.row_version + 1
FROM (
  SELECT task_id
  FROM pen_routine_tasks
  WHERE tenant_id = $1::uuid
    AND work_state IN ('scheduled', 'delayed')
    AND status IN ('open', 'rework')
    AND due_business_date < $2::date
  ORDER BY due_business_date, task_id
  LIMIT $3
  FOR UPDATE SKIP LOCKED
) claim
WHERE t.tenant_id = $1::uuid AND t.task_id = claim.task_id`
)
