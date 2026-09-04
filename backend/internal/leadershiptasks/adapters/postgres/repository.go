// Package postgres persists leadership tasks: the per-tenant numbered brief a director
// raises for a CXO, its attachments (pointers into the proof store), the seen stamp the
// drawer badge counts, and the status ladder. Every write is idempotent, audited and
// announced through the outbox in ONE transaction.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const (
	idemScopeRaise   = "leadership_task.raise"
	idemScopeEdit    = "leadership_task.edit"
	idemScopeStatus  = "leadership_task.status"
	idemScopeComment = "leadership_task.comment"

	resourceType = "leadership_task"
)

// Repository is the module's Postgres adapter.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
	now     func() time.Time
}

// NewRepository wires the adapter over the shared pool.
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

// taskColumns is the single projection every task read uses. Column order here and in
// scanTask must move together. Names are resolved in the SAME query (two LEFT JOINs on
// the active roster row, never a per-row lookup); a person who does not resolve reads as
// ” so the screen omits the attribution rather than showing a uuid.
const taskColumns = sqlRepository1

const taskFrom = sqlRepository2

func scanTask(row pgx.Row) (domain.Task, error) {
	var t domain.Task
	err := row.Scan(
		&t.TaskID, &t.TenantID, &t.TaskNo, &t.Title, &t.Body, &t.Status,
		&t.RaisedByUserID, &t.RaisedByName,
		&t.AssigneeUserID, &t.AssigneeName,
		&t.RaisedAt, &t.UpdatedAt, &t.DoneAt, &t.CancelledAt, &t.SeenAt, &t.AssigneeComment, &t.RowVersion,
	)
	if err != nil {
		return domain.Task{}, err
	}
	t.RaisedAt = t.RaisedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	return t, nil
}

// ListAssignees lists every person a task may be raised for: an ACTIVE roster member whose
// own mobile ticks carry the Tasks module at Oversee (maintainer decision 2026-09-04: "keep it
// optional -- if they are selected there, only for them"). The CXO role decides nothing here;
// the /people access editor does, and unticking someone takes them off this list at once.
func (r *Repository) ListAssignees(ctx context.Context, tenantID string) ([]ports.Assignee, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlListAssignees, tenantID, permissions.SurfaceMobile, leadershipTasksModuleKey, permissions.LevelOversee)
	if err != nil {
		return nil, fmt.Errorf("leadership task: list assignees: %w", err)
	}
	defer rows.Close()
	out := make([]ports.Assignee, 0, 8)
	for rows.Next() {
		var a ports.Assignee
		if err := rows.Scan(&a.UserID, &a.Name); err != nil {
			return nil, fmt.Errorf("leadership task: scan assignee: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListTasks pages the caller's tasks by keyset, newest first, and batch-loads each page
// row's attachments in ONE query.
//
// projection-review: membership=leadership_tasks at its task_id key (one row per task) filtered by the party predicate raised_by = $user OR assignee_user_id = $user, which the page rows, the status counts AND the unseen count all share so a chip never advertises a row the list hides; group_key=status for the chip counts, over that same party predicate; join_cardinality=leadership_task_attachments is at most 12 rows per task and is fetched as a second bounded query keyed by the page's task ids (never multiplied into the page query), and the two workforce_members name LEFT JOINs are 1:1 on workforce_members_active_user_unique_idx; pagination=keyset on (raised_at DESC, task_id DESC) with counts computed whole-list never page-local; scope=tenant_id on every branch, party predicate on every read
func (r *Repository) ListTasks(ctx context.Context, p ports.ListParams) (ports.Page, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	args := []any{p.TenantID, p.UserID}
	where := "t.tenant_id = $1 AND (t.raised_by = $2::uuid OR t.assignee_user_id = $2::uuid)"
	if len(p.Statuses) > 0 {
		args = append(args, p.Statuses)
		where += fmt.Sprintf(" AND t.status = ANY($%d)", len(args))
	}
	if p.Cursor != "" {
		raisedAt, taskID, err := decodeCursor(p.Cursor)
		if err != nil {
			return ports.Page{}, err
		}
		args = append(args, raisedAt, taskID)
		where += fmt.Sprintf(" AND (t.raised_at, t.task_id) < ($%d::timestamptz, $%d::uuid)", len(args)-1, len(args))
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	query := fmt.Sprintf(`SELECT %s %s WHERE %s ORDER BY t.raised_at DESC, t.task_id DESC LIMIT %d`, taskColumns, taskFrom, where, limit+1)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return ports.Page{}, fmt.Errorf("leadership task: list: %w", err)
	}
	defer rows.Close()
	tasks := make([]domain.Task, 0, limit)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return ports.Page{}, fmt.Errorf("leadership task: list scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return ports.Page{}, fmt.Errorf("leadership task: list rows: %w", err)
	}
	page := ports.Page{StatusCounts: map[string]int{}}
	if len(tasks) > limit {
		last := tasks[limit-1]
		page.NextCursor = encodeCursor(last.RaisedAt, last.TaskID)
		tasks = tasks[:limit]
	}
	if err := r.attachTo(ctx, r.pool, p.TenantID, tasks); err != nil {
		return ports.Page{}, err
	}
	page.Rows = tasks

	countRows, err := r.pool.Query(ctx, sqlRepository4, p.TenantID, p.UserID)
	if err != nil {
		return ports.Page{}, fmt.Errorf("leadership task: status counts: %w", err)
	}
	defer countRows.Close()
	for countRows.Next() {
		var status string
		var n int
		if err := countRows.Scan(&status, &n); err != nil {
			return ports.Page{}, fmt.Errorf("leadership task: status counts scan: %w", err)
		}
		page.StatusCounts[status] = n
	}
	if err := countRows.Err(); err != nil {
		return ports.Page{}, err
	}
	unseen, err := r.unseenCount(ctx, r.pool, p.TenantID, p.UserID)
	if err != nil {
		return ports.Page{}, err
	}
	page.UnseenCount = unseen
	return page, nil
}

// attachTo loads the attachments of a bounded task set in one query and fills them in.
func (r *Repository) attachTo(ctx context.Context, q querier, tenantID string, tasks []domain.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids := make([]string, 0, len(tasks))
	index := make(map[string]int, len(tasks))
	for i := range tasks {
		ids = append(ids, tasks[i].TaskID)
		index[tasks[i].TaskID] = i
		tasks[i].Attachments = nil
	}
	rows, err := q.Query(ctx, sqlRepository5, tenantID, ids)
	if err != nil {
		return fmt.Errorf("leadership task: list attachments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var taskID string
		var a domain.Attachment
		if err := rows.Scan(&taskID, &a.AttachmentID, &a.ProofID, &a.Kind, &a.MimeType, &a.FileName, &a.SizeBytes, &a.DurationMS, &a.Position); err != nil {
			return fmt.Errorf("leadership task: attachments scan: %w", err)
		}
		if i, ok := index[taskID]; ok {
			tasks[i].Attachments = append(tasks[i].Attachments, a)
		}
	}
	for i := range tasks {
		tasks[i].AttachmentCount = len(tasks[i].Attachments)
	}
	return rows.Err()
}

func encodeCursor(raisedAt time.Time, taskID string) string {
	return raisedAt.UTC().Format(time.RFC3339Nano) + "|" + taskID
}

func decodeCursor(cursor string) (string, string, error) {
	parts := strings.SplitN(cursor, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("%w: bad cursor", ports.ErrInvalidArgument)
	}
	if _, err := time.Parse(time.RFC3339Nano, parts[0]); err != nil {
		return "", "", fmt.Errorf("%w: bad cursor timestamp: %v", ports.ErrInvalidArgument, err)
	}
	if !uuidutil.IsUUIDString(parts[1]) {
		return "", "", fmt.Errorf("%w: bad cursor task id", ports.ErrInvalidArgument)
	}
	return parts[0], parts[1], nil
}

// GetTask reads one task with its attachments.
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
		return domain.Task{}, fmt.Errorf("leadership task: get: %w", err)
	}
	tasks := []domain.Task{t}
	if err := r.attachTo(ctx, q, tenantID, tasks); err != nil {
		return domain.Task{}, err
	}
	return tasks[0], nil
}

// Raise records a new task, mints its number, stores its attachments, audits it and
// announces it -- one transaction.
func (r *Repository) Raise(ctx context.Context, p ports.RaiseParams) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: begin raise: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.ActorID, p.AssigneeUserID, p.Title, p.Body, attachmentFingerprint(p.Attachments))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeRaise, p.IdempotencyKey, fingerprint)
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

	// The assignee must be assignable NOW, by the same tick the picker reads, checked inside
	// the write: the picker is a read that can go stale between the form opening and the send.
	var assignable bool
	if err := tx.QueryRow(ctx, sqlAssigneeIsTicked, p.TenantID, p.AssigneeUserID, permissions.SurfaceMobile, leadershipTasksModuleKey, permissions.LevelOversee).Scan(&assignable); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: check assignee: %w", err)
	}
	if !assignable {
		return domain.Task{}, domain.ErrAssigneeNotCXO
	}

	// The running number is minted under a per-tenant advisory lock so two directors raising
	// at once cannot both take #12. The lock is transaction-scoped and released at commit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('leadership_tasks:' || $1::text))`, p.TenantID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: number lock: %w", err)
	}
	now := r.now().UTC()
	var taskID string
	if err := tx.QueryRow(ctx, sqlRepository7,
		p.TenantID, p.Title, p.Body, domain.StatusOpen, p.ActorID, p.AssigneeUserID, now,
	).Scan(&taskID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: insert: %w", err)
	}
	if err := insertAttachments(ctx, tx, p.TenantID, taskID, p.Attachments); err != nil {
		return domain.Task{}, err
	}
	task, err := r.getRow(ctx, tx, p.TenantID, taskID, false)
	if err != nil {
		return domain.Task{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    "human",
		Action:       "leadership_task.raised",
		ResourceType: resourceType,
		ResourceID:   taskID,
		AfterState:   auditState(task),
		Metadata: map[string]any{
			"domain":           "leadership_tasks",
			"module":           "leadership_tasks",
			"task_no":          task.TaskNo,
			"assignee_user_id": p.AssigneeUserID,
			"attachment_count": len(p.Attachments),
			"idempotency_key":  p.IdempotencyKey,
			"operation_id":     p.IdempotencyKey,
		},
	}); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: audit raise: %w", err)
	}
	if err := emitEvent(ctx, tx, EventTaskRaised, task, "", p.ActorID, p.IdempotencyKey, now); err != nil {
		return domain.Task{}, err
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeRaise, p.IdempotencyKey, resourceType, taskID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: complete raise idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: commit raise: %w", err)
	}
	return task, nil
}

// Edit replaces the brief and the attachment list of a task the caller raised, under the
// row lock and the row version the screen loaded with.
func (r *Repository) Edit(ctx context.Context, p ports.EditParams) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: begin edit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.TaskID, p.ActorID, p.Title, p.Body, fmt.Sprintf("%d", p.RowVersion), attachmentFingerprint(p.Attachments))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeEdit, p.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.Task{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Task{}, err
		}
		return r.getRow(ctx, r.pool, p.TenantID, p.TaskID, false)
	}
	before, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, true)
	if err != nil {
		return domain.Task{}, err
	}
	actor := domain.Actor{UserID: p.ActorID, CanRaise: true}
	if !before.IsRaiser(actor) {
		return domain.Task{}, domain.ErrNotRaiser
	}
	if !domain.IsOpenForWork(before.Status) {
		return domain.Task{}, domain.ErrTaskClosed
	}
	if before.RowVersion != p.RowVersion {
		return domain.Task{}, ports.ErrVersionConflict
	}
	now := r.now().UTC()
	if _, err := tx.Exec(ctx, sqlRepository8, p.TenantID, p.TaskID, p.Title, p.Body, now); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: update brief: %w", err)
	}
	// The attachment list is REPLACED: the phone sends the full list it shows, and the
	// server keeps exactly that. Removed rows go; kept rows keep their ids; new rows are
	// inserted. Proof bytes are never deleted here -- the proof store owns retention.
	if _, err := tx.Exec(ctx, sqlRepository9,
		p.TenantID, p.TaskID, proofIDs(p.Attachments)); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: prune attachments: %w", err)
	}
	if err := insertAttachments(ctx, tx, p.TenantID, p.TaskID, p.Attachments); err != nil {
		return domain.Task{}, err
	}
	after, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, false)
	if err != nil {
		return domain.Task{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    "human",
		Action:       "leadership_task.edited",
		ResourceType: resourceType,
		ResourceID:   p.TaskID,
		BeforeState:  auditState(before),
		AfterState:   auditState(after),
		Metadata: map[string]any{
			"domain":          "leadership_tasks",
			"module":          "leadership_tasks",
			"task_no":         after.TaskNo,
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: audit edit: %w", err)
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeEdit, p.IdempotencyKey, resourceType, p.TaskID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: complete edit idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: commit edit: %w", err)
	}
	return after, nil
}

// ChangeStatus moves the task along its ladder under the row lock. The rule is
// domain.CheckTransition -- the same function that composes status_options -- so the
// buttons the screen showed are exactly the writes this accepts.
func (r *Repository) ChangeStatus(ctx context.Context, p ports.StatusParams) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: begin status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.TaskID, p.Actor.UserID, p.Status, fmt.Sprintf("%d", p.RowVersion))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeStatus, p.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.Task{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Task{}, err
		}
		return r.getRow(ctx, r.pool, p.TenantID, p.TaskID, false)
	}
	before, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, true)
	if err != nil {
		return domain.Task{}, err
	}
	if err := domain.CheckTransition(before, p.Actor, p.Status); err != nil {
		return domain.Task{}, err
	}
	if before.RowVersion != p.RowVersion {
		return domain.Task{}, ports.ErrVersionConflict
	}
	now := r.now().UTC()
	if _, err := tx.Exec(ctx, sqlRepository10, p.TenantID, p.TaskID, p.Status, now); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: update status: %w", err)
	}
	after, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, false)
	if err != nil {
		return domain.Task{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.Actor.UserID,
		ActorType:    "human",
		Action:       "leadership_task.status_changed",
		ResourceType: resourceType,
		ResourceID:   p.TaskID,
		BeforeState:  auditState(before),
		AfterState:   auditState(after),
		Metadata: map[string]any{
			"domain":          "leadership_tasks",
			"module":          "leadership_tasks",
			"task_no":         after.TaskNo,
			"from":            before.Status,
			"to":              after.Status,
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: audit status: %w", err)
	}
	if err := emitEvent(ctx, tx, EventTaskStatusChanged, after, before.Status, p.Actor.UserID, p.IdempotencyKey, now); err != nil {
		return domain.Task{}, err
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeStatus, p.IdempotencyKey, resourceType, p.TaskID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: complete status idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: commit status: %w", err)
	}
	return after, nil
}

// SetComment records the assignee's note under the row lock. No version fence: a note is
// its owner's own text and the latest one wins, the way a text field works.
func (r *Repository) SetComment(ctx context.Context, p ports.CommentParams) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: begin comment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.TaskID, p.Actor.UserID, p.Comment)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeComment, p.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.Task{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Task{}, err
		}
		return r.getRow(ctx, r.pool, p.TenantID, p.TaskID, false)
	}
	before, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, true)
	if err != nil {
		return domain.Task{}, err
	}
	if !before.IsAssignee(p.Actor) {
		return domain.Task{}, domain.ErrNotAssignee
	}
	if !before.CanComment(p.Actor) {
		return domain.Task{}, domain.ErrTaskClosed
	}
	if _, err := tx.Exec(ctx, sqlSetComment, p.TenantID, p.TaskID, p.Comment, r.now().UTC()); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: update comment: %w", err)
	}
	after, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, false)
	if err != nil {
		return domain.Task{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.Actor.UserID,
		ActorType:    "human",
		Action:       "leadership_task.commented",
		ResourceType: resourceType,
		ResourceID:   p.TaskID,
		BeforeState:  map[string]any{"comment": before.AssigneeComment},
		AfterState:   map[string]any{"comment": after.AssigneeComment},
		Metadata: map[string]any{
			"domain":          "leadership_tasks",
			"module":          "leadership_tasks",
			"task_no":         after.TaskNo,
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: audit comment: %w", err)
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeComment, p.IdempotencyKey, resourceType, p.TaskID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: complete comment idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: commit comment: %w", err)
	}
	return after, nil
}

// MarkSeen stamps seen_at once for the assignee. A replay updates nothing (the WHERE
// keeps it a set-if-null), so no idempotency key is needed. Audited only when it changed.
func (r *Repository) MarkSeen(ctx context.Context, tenantID, taskID, userID string) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: begin seen: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := r.now().UTC()
	tag, err := tx.Exec(ctx, sqlRepository11,
		tenantID, taskID, userID, now)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: mark seen: %w", err)
	}
	if tag.RowsAffected() == 1 {
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     tenantID,
			ActorID:      userID,
			ActorType:    "human",
			Action:       "leadership_task.seen",
			ResourceType: resourceType,
			ResourceID:   taskID,
			Metadata:     map[string]any{"domain": "leadership_tasks", "module": "leadership_tasks"},
		}); err != nil {
			return domain.Task{}, fmt.Errorf("leadership task: audit seen: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: commit seen: %w", err)
	}
	return r.getRow(ctx, r.pool, tenantID, taskID, false)
}

// UnseenCount answers the drawer badge: tasks addressed to the person, not yet opened,
// not cancelled.
func (r *Repository) UnseenCount(ctx context.Context, tenantID, userID string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.unseenCount(ctx, r.pool, tenantID, userID)
}

func (r *Repository) unseenCount(ctx context.Context, q querier, tenantID, userID string) (int, error) {
	if !uuidutil.IsUUIDString(userID) {
		return 0, nil
	}
	var n int
	if err := q.QueryRow(ctx, sqlRepository12,
		tenantID, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("leadership task: unseen count: %w", err)
	}
	return n, nil
}

func insertAttachments(ctx context.Context, tx pgx.Tx, tenantID, taskID string, attachments []domain.Attachment) error {
	for _, a := range attachments {
		// scale-guard:ignore: bounded by domain.MaxAttachments (12) per write.
		if _, err := tx.Exec(ctx, sqlRepository13,
			tenantID, taskID, a.ProofID, a.Kind, a.MimeType, a.FileName, a.SizeBytes, a.DurationMS, a.Position); err != nil {
			return fmt.Errorf("leadership task: insert attachment: %w", err)
		}
	}
	return nil
}

func proofIDs(attachments []domain.Attachment) []string {
	out := make([]string, 0, len(attachments))
	for _, a := range attachments {
		out = append(out, a.ProofID)
	}
	return out
}

func attachmentFingerprint(attachments []domain.Attachment) string {
	parts := make([]string, 0, len(attachments))
	for _, a := range attachments {
		parts = append(parts, a.ProofID+"/"+a.Kind+"/"+a.FileName)
	}
	return strings.Join(parts, ",")
}

func auditState(t domain.Task) map[string]any {
	return map[string]any{
		"task_no":          t.TaskNo,
		"title":            t.Title,
		"status":           t.Status,
		"assignee_user_id": t.AssigneeUserID,
		"attachment_count": len(t.Attachments),
		"row_version":      t.RowVersion,
	}
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlRepository1 = `
	t.task_id::text, t.tenant_id::text, t.task_no, t.title, t.body, t.status,
	t.raised_by::text, COALESCE(rb.display_name, ''),
	t.assignee_user_id::text, COALESCE(asg.display_name, ''),
	t.raised_at, t.updated_at, t.done_at, t.cancelled_at, t.seen_at, t.assignee_comment, t.row_version`
	sqlRepository2 = `
FROM public.leadership_tasks t
LEFT JOIN public.workforce_members rb
       ON rb.tenant_id = t.tenant_id AND rb.user_id = t.raised_by AND rb.status = 'active'
LEFT JOIN public.workforce_members asg
       ON asg.tenant_id = t.tenant_id AND asg.user_id = t.assignee_user_id AND asg.status = 'active'`
	sqlRepository3 = `
SELECT DISTINCT g.user_id::text, m.display_name
FROM public.user_scope_grants g
JOIN public.workforce_members m
  ON m.tenant_id = g.tenant_id AND m.user_id = g.user_id AND m.status = 'active'
WHERE g.tenant_id = $1 AND g.role = $2 AND g.status = 'active'
  AND (g.valid_to IS NULL OR g.valid_to > now())
ORDER BY m.display_name, g.user_id::text
LIMIT 100`
	sqlRepository4 = `
SELECT status, count(*) FROM public.leadership_tasks
WHERE tenant_id = $1 AND (raised_by = $2::uuid OR assignee_user_id = $2::uuid)
GROUP BY status`
	sqlRepository5 = `
SELECT task_id::text, attachment_id::text, proof_id::text, kind, mime_type, file_name, size_bytes, duration_ms, position
FROM public.leadership_task_attachments
WHERE tenant_id = $1 AND task_id = ANY($2::uuid[])
ORDER BY task_id, position, attachment_id`
	sqlRepository6 = `
SELECT EXISTS (
  SELECT 1 FROM public.user_scope_grants g
  WHERE g.tenant_id = $1 AND g.user_id = $2::uuid AND g.role = $3 AND g.status = 'active'
    AND (g.valid_to IS NULL OR g.valid_to > now())
)`
	sqlRepository7 = `
INSERT INTO public.leadership_tasks (
  tenant_id, task_no, title, body, status, raised_by, assignee_user_id, raised_at, updated_at
) VALUES (
  $1::uuid,
  (SELECT COALESCE(MAX(task_no), 0) + 1 FROM public.leadership_tasks WHERE tenant_id = $1::uuid),
  $2, $3, $4, $5::uuid, $6::uuid, $7, $7
)
RETURNING task_id::text`
	sqlRepository8 = `
UPDATE public.leadership_tasks
SET title = $3, body = $4, updated_at = $5, row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2`
	sqlRepository9 = `
DELETE FROM public.leadership_task_attachments
WHERE tenant_id = $1 AND task_id = $2 AND NOT (proof_id = ANY($3::uuid[]))`
	sqlRepository10 = `
UPDATE public.leadership_tasks
SET status = $3::text,
    updated_at = $4::timestamptz,
    done_at = CASE WHEN $3::text = 'done' THEN $4::timestamptz ELSE NULL::timestamptz END,
    cancelled_at = CASE WHEN $3::text = 'cancelled' THEN $4::timestamptz ELSE cancelled_at END,
    row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2`
	sqlRepository11 = `
UPDATE public.leadership_tasks
SET seen_at = $4
WHERE tenant_id = $1 AND task_id = $2 AND assignee_user_id = $3::uuid AND seen_at IS NULL`
	sqlRepository12 = `
SELECT count(*) FROM public.leadership_tasks
WHERE tenant_id = $1 AND assignee_user_id = $2::uuid AND seen_at IS NULL AND status <> 'cancelled'`
	sqlRepository13 = `
INSERT INTO public.leadership_task_attachments (
  tenant_id, task_id, proof_id, kind, mime_type, file_name, size_bytes, duration_ms, position
) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9)
ON CONFLICT (tenant_id, task_id, proof_id) DO UPDATE
SET kind = EXCLUDED.kind, file_name = EXCLUDED.file_name, position = EXCLUDED.position`
)

const sqlSetComment = `
UPDATE public.leadership_tasks
SET assignee_comment = $3::text, updated_at = $4::timestamptz, row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2`

// leadershipTasksModuleKey is the module_key the /people ticks store for this module.
const leadershipTasksModuleKey = "leadership_tasks"

const sqlListAssignees = `
SELECT m.user_id::text, m.display_name
FROM public.person_module_access a
JOIN public.workforce_members m
  ON m.tenant_id = a.tenant_id AND m.workforce_member_id = a.workforce_member_id AND m.status = 'active'
WHERE a.tenant_id = $1 AND a.surface = $2 AND a.module_key = $3 AND $4 = ANY(a.capabilities)
  AND m.user_id IS NOT NULL
ORDER BY m.display_name, m.user_id::text
LIMIT 100`

const sqlAssigneeIsTicked = `
SELECT EXISTS (
  SELECT 1 FROM public.person_module_access a
  JOIN public.workforce_members m
    ON m.tenant_id = a.tenant_id AND m.workforce_member_id = a.workforce_member_id AND m.status = 'active'
  WHERE a.tenant_id = $1 AND m.user_id = $2::uuid
    AND a.surface = $3 AND a.module_key = $4 AND $5 = ANY(a.capabilities)
)`
