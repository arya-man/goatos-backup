// Package postgres persists pen visit tasks (migrations 000277 and 000295).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/penvisits/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

const (
	idemScopeSubmit = "pen_visit.submit"
	resourceType    = "pen_visit_task"

	// verificationCategoryVaccination is the category the sopbridge stamps on a vaccination
	// shed proof item; the pc_* categories are pccare/domain's verification categories. Both are
	// matched by string here rather than imported: this package must not depend on either
	// producer, and the vocabulary is pinned by the materializer's integration test.
	verificationCategoryVaccination = "vaccination_proof"

	// The verification items' source ref types that name a PARENT this visit closes. A
	// vaccination shed submit files its item against the sop_submission; a per-goat vaccination
	// item (ref_type vaccination_goat) is the same submit seen one grain down and is not a
	// second parent. A PC Care task files its item against the task.
	sourceRefTypeSOPSubmission = "sop_submission"
	sourceRefTypePCCareTask    = "pc_care_task"
)

// verificationCategoryReasons maps a verification item's category to the visit reason it
// implies. A category absent from this map (inventory vaccine, feed & water removal, every
// other module's proof) is not pen work and raises no visit.
var verificationCategoryReasons = map[string]string{
	verificationCategoryVaccination: domain.ReasonVaccination,
	"pc_deworming":                  domain.ReasonDeworming,
	"pc_anti_protozoan":             domain.ReasonAntiProtozoan,
	"pc_ticks_removal":              domain.ReasonTicksRemoval,
	"pc_hoof_trimming":              domain.ReasonHoofTrimming,
	"pc_hair_trimming":              domain.ReasonHairTrimming,
}

// sourceKindForRefType maps a verification item's source ref type to the parent kind the link
// table stores; "" means the item names no parent this module links.
func sourceKindForRefType(refType string) string {
	switch refType {
	case sourceRefTypeSOPSubmission:
		return domain.SourceKindVaccinationSubmission
	case sourceRefTypePCCareTask:
		return domain.SourceKindPCCareTask
	}
	return ""
}

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

// taskColumns is the single projection every task read uses. The park and shed names ride the
// row so the list needs no second read; the pen label is composed in Go through oploc; the
// park's configured visitors and the visit's parents ride as arrays so the row can answer
// "may this caller record" and "what does this close" without a second read per row.
const taskColumns = sqlRepository1

const taskFrom = sqlRepository2

// taskScanTargets is the destination list matching taskColumns, so every read that scans a
// task row -- with or without leading extra columns -- binds the same columns in the same order.
type taskScan struct {
	t                             domain.Task
	partition, parkName, shedName string
	visitors, sources             []string
	reworkReason                  *string
}

func (s *taskScan) targets() []any {
	t := &s.t
	return []any{
		&t.TaskID, &t.TenantID, &t.ParkID, &s.parkName, &t.ShedID, &s.shedName, &s.partition,
		&t.Reasons, &t.SourceDate, &t.PlannedDate, &t.DueDate, &t.WorkState, &t.Status,
		&t.ProofRef, &t.SubmittedBy, &t.SubmittedAt, &t.VerifiedBy, &t.VerifiedAt, &s.reworkReason,
		&t.RolledFwd, &t.DelayedSince, &t.RowVersion, &t.CreatedAt, &t.UpdatedAt,
		&s.visitors, &s.sources,
	}
}

func (s *taskScan) finish() domain.Task {
	t := s.t
	t.ParkName = strings.TrimSpace(s.parkName)
	t.ShedName = strings.TrimSpace(s.shedName)
	t.Partition = strings.TrimSpace(s.partition)
	if oploc.NormalizePartition(t.Partition) == oploc.WholeSentinel {
		t.Partition = ""
	}
	t.PenLabel = oploc.OperationalLocation{ShedID: t.ShedID, ShedName: t.ShedName, PartitionLabel: t.Partition}.Display()
	if s.reworkReason != nil {
		t.ReworkReason = strings.TrimSpace(*s.reworkReason)
	}
	t.VisitorIDs = s.visitors
	t.Sources = make([]domain.Source, 0, len(s.sources))
	for _, src := range s.sources {
		kind, ref, ok := strings.Cut(src, ":")
		if !ok || kind == "" || ref == "" {
			continue
		}
		t.Sources = append(t.Sources, domain.Source{Kind: kind, RefID: ref})
	}
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	if t.SubmittedAt != nil {
		v := t.SubmittedAt.UTC()
		t.SubmittedAt = &v
	}
	if t.VerifiedAt != nil {
		v := t.VerifiedAt.UTC()
		t.VerifiedAt = &v
	}
	return t
}

func scanTask(row pgx.Row) (domain.Task, error) {
	var s taskScan
	if err := row.Scan(s.targets()...); err != nil {
		return domain.Task{}, err
	}
	return s.finish(), nil
}

// ListMine pages the visits owed in the parks the caller is configured to visit: open first
// is the chip's job; within a chip, the most recently due first, keyset on
// (due_business_date, task_id) over pen_visit_tasks_park_idx. Counts range over the SAME
// visitor-park predicate as the rows.
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
	where := "t.tenant_id = $1 AND " + visitorParkPredicate + " AND t.work_state = ANY($3::text[])"
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
		return ports.Page{}, fmt.Errorf("pen visit: list: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Task, 0, limit+1)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return ports.Page{}, fmt.Errorf("pen visit: scan: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
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
		return ports.Page{}, fmt.Errorf("pen visit: counts: %w", err)
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
		return domain.Task{}, fmt.Errorf("pen visit: get: %w", err)
	}
	return t, nil
}

// ForSources reads the visit each parent owes, keyed by the parent's ref id. A parent with no
// visit yet (the materializer runs after the day boundary) is simply absent from the map.
func (r *Repository) ForSources(ctx context.Context, tenantID, sourceKind string, refIDs []string) (map[string]domain.Task, error) {
	out := map[string]domain.Task{}
	if len(refIDs) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// projection-review: membership=pen_visit_task_sources rows for (tenant, kind, ref) -- unique per parent by pen_visit_task_sources_parent_uq, so the join to pen_visit_tasks is 1:1 and each parent maps to at most ONE visit; group_key=source_ref_id; join_cardinality=1:1 (sources -> tasks on task_id PK); pagination=none -- bounded by the caller's page of parents (one batched read per page); scope=tenant + explicit ref ids
	query := fmt.Sprintf(`SELECT s.source_ref_id::text, %s %s JOIN pen_visit_task_sources s ON s.tenant_id = t.tenant_id AND s.task_id = t.task_id
WHERE s.tenant_id = $1::uuid AND s.source_kind = $2 AND s.source_ref_id = ANY($3::uuid[])`, taskColumns, taskFrom)
	rows, err := r.pool.Query(ctx, query, tenantID, sourceKind, refIDs)
	if err != nil {
		return nil, fmt.Errorf("pen visit: for sources: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var s taskScan
		if err := rows.Scan(append([]any{&ref}, s.targets()...)...); err != nil {
			return nil, fmt.Errorf("pen visit: for sources scan: %w", err)
		}
		out[ref] = s.finish()
	}
	return out, rows.Err()
}

// OpenCount answers the badge: visits still awaiting a recording in the parks one person is
// configured to visit.
func (r *Repository) OpenCount(ctx context.Context, tenantID, userID string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var n int
	if err := r.pool.QueryRow(ctx, sqlRepository4, tenantID, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("pen visit: open count: %w", err)
	}
	return n, nil
}

// OpenReasons reads the reasons of every visit one person still has to record.
func (r *Repository) OpenReasons(ctx context.Context, tenantID, userID string) ([][]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// scale-guard:ignore: bounded by the open visits of ONE person's parks (pens worked yesterday, tens of rows), on pen_visit_tasks_park_idx; a badge read, no page.
	rows, err := r.pool.Query(ctx, sqlRepository14, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("pen visit: open reasons: %w", err)
	}
	defer rows.Close()
	out := [][]string{}
	for rows.Next() {
		var reasons []string
		if err := rows.Scan(&reasons); err != nil {
			return nil, err
		}
		out = append(out, reasons)
	}
	return out, rows.Err()
}

// Submit records the visit's video and hands the visit to the verifier -- one transaction:
// idempotency reservation, row lock, the domain rule re-run on the locked row, the update,
// audit, outbox. The kernel clock is untouched: the visit is 'pending_verification' on the
// gate and keeps its scheduled/delayed work_state until the verdict.
func (r *Repository) Submit(ctx context.Context, p ports.SubmitParams) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("pen visit: begin submit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.Actor.UserID, p.TaskID, p.ProofRef)
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
	if err := domain.CheckSubmit(before, p.Actor, p.ProofRef, p.RowVersion); err != nil {
		return domain.Task{}, err
	}
	now := r.now().UTC()
	tag, err := tx.Exec(ctx, sqlRepository5, p.TenantID, p.TaskID, p.ProofRef, p.Actor.UserID, now, before.RowVersion)
	if err != nil {
		return domain.Task{}, fmt.Errorf("pen visit: submit update: %w", err)
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
		Action:       "pen_visit.submitted",
		ResourceType: resourceType,
		ResourceID:   p.TaskID,
		ScopeType:    "park",
		ScopeID:      after.ParkID,
		BeforeState:  auditState(before),
		AfterState:   auditState(after),
		TraceID:      p.TraceID,
		Metadata: map[string]any{
			"domain":          "pen_visits",
			"module":          "pen_visits",
			"proof_ref":       p.ProofRef,
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return domain.Task{}, fmt.Errorf("pen visit: audit submit: %w", err)
	}
	// The submitted event's idempotency key carries the row version so a re-shoot after a
	// rework mints a fresh verification item while a retry of the same submit collapses.
	if err := emitEvent(ctx, tx, EventVisitSubmitted, after, p.Actor.UserID, "human", fmt.Sprintf("%s:%d", p.TaskID, after.RowVersion), now); err != nil {
		return domain.Task{}, err
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeSubmit, p.IdempotencyKey, resourceType, p.TaskID); err != nil {
		return domain.Task{}, fmt.Errorf("pen visit: complete submit idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("pen visit: commit submit: %w", err)
	}
	return after, nil
}

// ApplyVerified flips an approved visit pending_verification -> completed on BOTH dimensions
// (the gate and the kernel clock agree in one transaction) and stamps verified_by/at. Runs
// from the verification.verdict.approved consumer, never from a phone. IDEMPOTENT: a
// re-delivered verdict on a row no longer pending applies nothing.
func (r *Repository) ApplyVerified(ctx context.Context, p ports.VerdictParams) (ports.VerdictResult, error) {
	return r.applyVerdict(ctx, p, true)
}

// BounceForRework flips a rejected visit pending_verification -> rework with the verifier's
// reason. The clip stays on the row as history; the next submit replaces it.
func (r *Repository) BounceForRework(ctx context.Context, p ports.VerdictParams) (ports.VerdictResult, error) {
	return r.applyVerdict(ctx, p, false)
}

func (r *Repository) applyVerdict(ctx context.Context, p ports.VerdictParams, approve bool) (ports.VerdictResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.VerdictResult{}, fmt.Errorf("pen visit: begin verdict: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, true)
	if errors.Is(err, ports.ErrTaskNotFound) {
		// A stale/foreign verdict: nothing of ours to apply.
		return ports.VerdictResult{}, tx.Commit(ctx)
	}
	if err != nil {
		return ports.VerdictResult{}, err
	}
	if before.Status != domain.StatusPendingVerification {
		return ports.VerdictResult{Task: before}, tx.Commit(ctx)
	}
	now := r.now().UTC()
	action := "pen_visit.verified"
	query := sqlRepository11
	args := []any{p.TenantID, p.TaskID, strings.TrimSpace(p.VerifiedBy), now, before.RowVersion}
	if !approve {
		action = "pen_visit.rework"
		query = sqlRepository12
		args = []any{p.TenantID, p.TaskID, strings.TrimSpace(p.Reason), now, before.RowVersion}
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return ports.VerdictResult{}, fmt.Errorf("pen visit: apply verdict: %w", err)
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
			"domain": "pen_visits",
			"module": "pen_visits",
			"source": "pen-visit-verification",
			"reason": strings.TrimSpace(p.Reason),
		},
	}); err != nil {
		return ports.VerdictResult{}, fmt.Errorf("pen visit: audit verdict: %w", err)
	}
	if approve {
		if err := emitEvent(ctx, tx, EventVisitVerified, after, strings.TrimSpace(p.VerifiedBy), "human", fmt.Sprintf("%s:%d", p.TaskID, after.RowVersion), now); err != nil {
			return ports.VerdictResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.VerdictResult{}, fmt.Errorf("pen visit: commit verdict: %w", err)
	}
	return ports.VerdictResult{Applied: true, Task: after}, nil
}

func auditState(t domain.Task) map[string]any {
	proof := ""
	if t.ProofRef != nil {
		proof = *t.ProofRef
	}
	sources := make([]string, 0, len(t.Sources))
	for _, s := range t.Sources {
		sources = append(sources, s.Kind+":"+s.RefID)
	}
	return map[string]any{
		"park_id":              t.ParkID,
		"shed_id":              t.ShedID,
		"partition_label":      t.Partition,
		"pen_label":            t.PenLabel,
		"reasons":              domain.SortReasons(t.Reasons),
		"source_business_date": t.SourceDate,
		"due_business_date":    t.DueDate,
		"work_state":           t.WorkState,
		"status":               t.Status,
		"sources":              sources,
		"proof_ref":            proof,
		"rework_reason":        t.ReworkReason,
		"row_version":          t.RowVersion,
	}
}

// penWork is one pen that had preventive-care work submitted on the source date.
type penWork struct {
	parkID    string
	shedID    string
	partition string
	reasons   map[string]bool
	sources   map[domain.Source]bool
}

// Materialize writes one visit per pen worked on sourceDate, for every park with at least one
// configured visitor, due on the later of sourceDate+1 and today, and links each visit to the
// parents (PC Care tasks, vaccination submissions) whose submit raised it. One transaction;
// idempotent on the natural key (a replay inserts nothing and returns no digest); a second
// submit in a pen that already has an open visit for that day only widens its reasons and its
// parent links.
func (r *Repository) Materialize(ctx context.Context, tenantID string, sourceDate, today string, now time.Time) (ports.MaterializeResult, []ports.CreatedDigest, error) {
	result := ports.MaterializeResult{SourceDate: sourceDate}
	src, err := time.Parse("2006-01-02", sourceDate)
	if err != nil {
		return result, nil, fmt.Errorf("pen visit: materialize: bad source date %q: %w", sourceDate, err)
	}
	plannedDate := src.AddDate(0, 0, 1).Format("2006-01-02")
	dueDate := plannedDate
	workState := domain.WorkStateScheduled
	delayedSince := ""
	rolledForwardCount := 0
	if today > plannedDate {
		dueDate = today
		workState = domain.WorkStateDelayed
		delayedSince = plannedDate
		if parsedToday, err := time.Parse("2006-01-02", today); err == nil {
			rolledForwardCount = int(parsedToday.Sub(src.AddDate(0, 0, 1)).Hours() / 24)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, nil, fmt.Errorf("pen visit: begin materialize: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Which parks have someone configured to visit. A park absent here gets no task and is
	// reported loudly.
	configured := map[string]bool{}
	aRows, err := tx.Query(ctx, sqlRepository6, tenantID)
	if err != nil {
		return result, nil, fmt.Errorf("pen visit: materialize: assignees: %w", err)
	}
	for aRows.Next() {
		var parkID string
		if err := aRows.Scan(&parkID); err != nil {
			aRows.Close()
			return result, nil, err
		}
		configured[parkID] = true
	}
	aRows.Close()
	if err := aRows.Err(); err != nil {
		return result, nil, err
	}

	// 2. The pens worked on the source date. Every vaccination shed submit and every PC Care
	// submit already raises a verification item carrying park, shed and pen, so this ONE read
	// covers both triggers; the category says which, and the source ref names the parent.
	// Bounded by one day's proof items.
	// projection-review: membership=verification_items rows created in the IST day window with shed_id and park_id set and a pen-work category (one row per submitted proof item, unique on (tenant_id, idempotency_key)); group_key=(park_id, shed_id, normalized partition_label) folded in Go from the DISTINCT read -- the consumer's pen grain, the same key the natural unique constraint pen_visit_tasks_natural_uq enforces; join_cardinality=none -- the read joins nothing, many items per pen collapse to one pen row and one task, and each item's (ref_type, ref_id) becomes one parent link, unique per parent by pen_visit_task_sources_parent_uq; pagination=none -- one day's pen set, bounded by pens worked in a day, never paged; scope=per tenant across parks, then per park through pen_visit_park_assignees, a park with no row is skipped and named
	wRows, err := tx.Query(ctx, sqlRepository7, tenantID, sourceDate)
	if err != nil {
		return result, nil, fmt.Errorf("pen visit: materialize: pens: %w", err)
	}
	pens := map[string]*penWork{}
	order := []string{}
	for wRows.Next() {
		var parkID, shedID, partition, category, refType, refID string
		if err := wRows.Scan(&parkID, &shedID, &partition, &category, &refType, &refID); err != nil {
			wRows.Close()
			return result, nil, err
		}
		reason, ok := verificationCategoryReasons[category]
		if !ok {
			continue
		}
		partition = strings.TrimSpace(partition)
		if oploc.NormalizePartition(partition) == oploc.WholeSentinel {
			partition = ""
		}
		key := parkID + "|" + shedID + "|" + oploc.NormalizePartition(partition)
		pen, seen := pens[key]
		if !seen {
			pen = &penWork{parkID: parkID, shedID: shedID, partition: partition, reasons: map[string]bool{}, sources: map[domain.Source]bool{}}
			pens[key] = pen
			order = append(order, key)
		}
		pen.reasons[reason] = true
		if kind := sourceKindForRefType(refType); kind != "" && refID != "" {
			pen.sources[domain.Source{Kind: kind, RefID: refID}] = true
		}
	}
	wRows.Close()
	if err := wRows.Err(); err != nil {
		return result, nil, err
	}

	// 3. One set-based upsert for every pen in a configured park.
	parkIDs, shedIDs, partitions, reasonCSV := []string{}, []string{}, []string{}, []string{}
	missing := map[string]bool{}
	kept := []*penWork{}
	for _, key := range order {
		pen := pens[key]
		if !configured[pen.parkID] {
			missing[pen.parkID] = true
			result.PensSkipped++
			continue
		}
		reasons := make([]string, 0, len(pen.reasons))
		for reason := range pen.reasons {
			reasons = append(reasons, reason)
		}
		parkIDs = append(parkIDs, pen.parkID)
		shedIDs = append(shedIDs, pen.shedID)
		partitions = append(partitions, pen.partition)
		reasonCSV = append(reasonCSV, strings.Join(domain.SortReasons(reasons), ","))
		kept = append(kept, pen)
	}
	for parkID := range missing {
		result.ParksWithoutAssignee = append(result.ParksWithoutAssignee, parkID)
	}
	if len(parkIDs) == 0 {
		return result, nil, tx.Commit(ctx)
	}
	uRows, err := tx.Query(ctx, sqlRepository8, tenantID, parkIDs, shedIDs, partitions, reasonCSV, sourceDate, plannedDate, dueDate, workState, delayedSince, rolledForwardCount)
	if err != nil {
		return result, nil, fmt.Errorf("pen visit: materialize: upsert: %w", err)
	}
	createdIDs := []string{}
	for uRows.Next() {
		var taskID string
		var inserted bool
		if err := uRows.Scan(&taskID, &inserted); err != nil {
			uRows.Close()
			return result, nil, err
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
		return result, nil, err
	}

	// 3b. Link every parent to its pen's visit for the day -- inserted, widened or untouched
	// alike, so a late parent (an offline phone syncing after the tick) still finds the visit
	// it closes on. ON CONFLICT DO NOTHING on the link's own key; the parent uniqueness index
	// keeps one visit per parent.
	srcPark, srcShed, srcPartition, srcKind, srcRef := []string{}, []string{}, []string{}, []string{}, []string{}
	for _, pen := range kept {
		for s := range pen.sources {
			srcPark = append(srcPark, pen.parkID)
			srcShed = append(srcShed, pen.shedID)
			srcPartition = append(srcPartition, pen.partition)
			srcKind = append(srcKind, s.Kind)
			srcRef = append(srcRef, s.RefID)
		}
	}
	if len(srcRef) > 0 {
		if _, err := tx.Exec(ctx, sqlRepository13, tenantID, srcPark, srcShed, srcPartition, srcKind, srcRef, sourceDate); err != nil {
			return result, nil, fmt.Errorf("pen visit: materialize: link sources: %w", err)
		}
	}

	// 4. Announce each created task and build the per-park digests the notifier pushes.
	digests := map[string]*ports.CreatedDigest{}
	digestOrder := []string{}
	for _, taskID := range createdIDs {
		task, err := r.getRow(ctx, tx, tenantID, taskID, false)
		if err != nil {
			return result, nil, err
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     tenantID,
			ActorType:    "system",
			Action:       "pen_visit.created",
			ResourceType: resourceType,
			ResourceID:   taskID,
			ScopeType:    "park",
			ScopeID:      task.ParkID,
			AfterState:   auditState(task),
			Metadata: map[string]any{
				"domain":               "pen_visits",
				"module":               "pen_visits",
				"source_business_date": sourceDate,
			},
		}); err != nil {
			return result, nil, fmt.Errorf("pen visit: audit created: %w", err)
		}
		if err := emitEvent(ctx, tx, EventVisitCreated, task, "", "system_rule", "pen-visit:created:"+taskID, now); err != nil {
			return result, nil, err
		}
		d, ok := digests[task.ParkID]
		if !ok {
			d = &ports.CreatedDigest{ParkID: task.ParkID, ParkName: task.ParkName, VisitorIDs: task.VisitorIDs, DueDate: task.DueDate}
			digests[task.ParkID] = d
			digestOrder = append(digestOrder, task.ParkID)
		}
		d.Tasks = append(d.Tasks, task)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, nil, fmt.Errorf("pen visit: commit materialize: %w", err)
	}
	out := make([]ports.CreatedDigest, 0, len(digestOrder))
	for _, parkID := range digestOrder {
		out = append(out, *digests[parkID])
	}
	return result, out, nil
}

// DueDigestsForSourceDate rebuilds the same per-park notification digest from existing open
// tasks. It is the retry path for "materialize committed, queueing the push failed": the next tick
// no longer inserts rows, but the calendar notification queue is idempotent by event key/device, so
// re-offering the digest is safe and bounded by that source day's open pen visits.
func (r *Repository) DueDigestsForSourceDate(ctx context.Context, tenantID, sourceDate string) ([]ports.CreatedDigest, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlRepository10, tenantID, sourceDate)
	if err != nil {
		return nil, fmt.Errorf("pen visit: due digests: %w", err)
	}
	defer rows.Close()
	return scanDigests(rows)
}

func scanDigests(rows pgx.Rows) ([]ports.CreatedDigest, error) {
	digests := map[string]*ports.CreatedDigest{}
	order := []string{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("pen visit: due digest scan: %w", err)
		}
		key := task.ParkID + "|" + task.DueDate
		d, ok := digests[key]
		if !ok {
			d = &ports.CreatedDigest{
				ParkID:     task.ParkID,
				ParkName:   task.ParkName,
				VisitorIDs: task.VisitorIDs,
				DueDate:    task.DueDate,
			}
			digests[key] = d
			order = append(order, key)
		}
		d.Tasks = append(d.Tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ports.CreatedDigest, 0, len(order))
	for _, key := range order {
		out = append(out, *digests[key])
	}
	return out, nil
}

// SweepRollForward moves visits still awaiting a recording whose due date has passed to today
// as 'delayed' -- the PC Care kernel shape: chunked, FOR UPDATE SKIP LOCKED, capped. A visit
// sitting with the verifier is not late: the gate reads submitted_at, never review state.
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
		// scale-guard:ignore: bounded chunked claim over pen_visit_tasks_sweep_due_idx (tenant_id, work_state, due_business_date, task_id) with FOR UPDATE SKIP LOCKED; each pass touches at most chunkSize rows and the loop is capped at maxChunks.
		tag, err := r.pool.Exec(ctxChunk, sqlRepository9, tenantID, today, chunkSize)
		cancel()
		if err != nil {
			return result, fmt.Errorf("pen visit: sweep roll-forward: %w", err)
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

// visitorParkPredicate admits the rows in parks the caller ($2) is configured to visit.
const visitorParkPredicate = `t.park_id IN (SELECT a.park_id FROM pen_visit_park_assignees a WHERE a.tenant_id = t.tenant_id AND a.user_id = $2::uuid)`

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlRepository1 = `
t.task_id::text, t.tenant_id::text, t.park_id::text, COALESCE(park.name, ''),
t.shed_id::text, COALESCE(NULLIF(shed.name, ''), shed.location_code, ''), COALESCE(t.partition_label, ''),
t.reasons, t.source_business_date::text, t.planned_business_date::text, t.due_business_date::text,
t.work_state, t.status,
t.proof_ref::text, t.submitted_by::text, t.submitted_at, t.verified_by::text, t.verified_at, t.rework_reason,
t.rolled_forward_count, t.delayed_since_business_date::text, t.row_version, t.created_at, t.updated_at,
COALESCE((SELECT array_agg(a.user_id::text ORDER BY a.user_id) FROM pen_visit_park_assignees a WHERE a.tenant_id = t.tenant_id AND a.park_id = t.park_id), '{}'::text[]),
COALESCE((SELECT array_agg(s.source_kind || ':' || s.source_ref_id::text ORDER BY s.source_kind, s.source_ref_id) FROM pen_visit_task_sources s WHERE s.tenant_id = t.tenant_id AND s.task_id = t.task_id), '{}'::text[])`
	sqlRepository2 = `
FROM pen_visit_tasks t
LEFT JOIN locations park ON park.tenant_id = t.tenant_id AND park.location_id = t.park_id
LEFT JOIN locations shed ON shed.tenant_id = t.tenant_id AND shed.location_id = t.shed_id`
	sqlRepository3 = `
SELECT t.work_state, count(*)::int
FROM pen_visit_tasks t
WHERE t.tenant_id = $1 AND ` + visitorParkPredicate + `
GROUP BY t.work_state`
	sqlRepository4 = `
SELECT count(*)::int
FROM pen_visit_tasks t
WHERE t.tenant_id = $1 AND ` + visitorParkPredicate + `
  AND t.work_state IN ('scheduled', 'delayed') AND t.status IN ('open', 'rework')`
	// Submit: the gate flips to pending_verification; the kernel clock is untouched. A rework
	// resubmit clears the verifier's old reason -- the new clip supersedes it.
	sqlRepository5 = `
UPDATE pen_visit_tasks
SET status = 'pending_verification',
    proof_ref = $3::uuid,
    submitted_by = $4::uuid,
    submitted_at = $5::timestamptz,
    rework_reason = NULL,
    updated_at = $5::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2::uuid
  AND work_state IN ('scheduled', 'delayed')
  AND status IN ('open', 'rework')
  AND row_version = $6`
	sqlRepository6 = `
SELECT DISTINCT park_id::text
FROM pen_visit_park_assignees
WHERE tenant_id = $1::uuid`
	// The materializer's read: pens with preventive-care work submitted on one IST business
	// date, and the parent each item was filed against. Half-open instant window (sargable over
	// verification_items_created_pen_idx), never a function of the column. created_at is the
	// SUBMIT instant; captured_at is when the animal was handled and can be an earlier day.
	sqlRepository7 = `
SELECT DISTINCT vi.park_id::text, vi.shed_id::text, COALESCE(vi.partition_label, ''), vi.category,
       vi.source_ref_type, vi.source_ref_id::text
FROM verification_items vi
WHERE vi.tenant_id = $1::uuid
  AND vi.shed_id IS NOT NULL
  AND vi.park_id IS NOT NULL
  AND vi.created_at >= ($2::date::timestamp AT TIME ZONE 'Asia/Kolkata')
  AND vi.created_at <  (($2::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata')
  AND vi.category IN ('vaccination_proof', 'pc_deworming', 'pc_anti_protozoan', 'pc_ticks_removal', 'pc_hoof_trimming', 'pc_hair_trimming')`
	// Set-based upsert on the natural key. An existing visit for the same pen and day that
	// still awaits a recording gains any new reason (second submit, other category); one with
	// the verifier or already verified is left alone; the (xmax = 0) column says whether the
	// row was inserted by THIS statement.
	sqlRepository8 = `
INSERT INTO pen_visit_tasks (
  tenant_id, park_id, shed_id, partition_label, reasons, source_business_date,
  planned_business_date, due_business_date, work_state,
  delayed_since_business_date, rolled_forward_count
)
SELECT $1::uuid, p.park_id, p.shed_id, NULLIF(p.partition_label, ''), string_to_array(p.reasons_csv, ','),
       $6::date, $7::date, $8::date, $9::text, NULLIF($10::text, '')::date, $11::integer
FROM unnest($2::uuid[], $3::uuid[], $4::text[], $5::text[])
  AS p(park_id, shed_id, partition_label, reasons_csv)
ON CONFLICT ON CONSTRAINT pen_visit_tasks_natural_uq DO UPDATE
SET reasons = (SELECT array_agg(DISTINCT r ORDER BY r) FROM unnest(pen_visit_tasks.reasons || EXCLUDED.reasons) AS r),
    updated_at = now(),
    row_version = pen_visit_tasks.row_version + 1
WHERE pen_visit_tasks.work_state IN ('scheduled', 'delayed')
  AND pen_visit_tasks.status IN ('open', 'rework')
  AND NOT (pen_visit_tasks.reasons @> EXCLUDED.reasons)
RETURNING task_id::text, (xmax = 0) AS inserted`
	sqlRepository9 = `
	UPDATE pen_visit_tasks t
SET work_state = 'delayed',
    delayed_since_business_date = COALESCE(t.delayed_since_business_date, t.due_business_date),
    due_business_date = $2::date,
    rolled_forward_count = t.rolled_forward_count + 1,
    updated_at = now(),
    row_version = t.row_version + 1
FROM (
  SELECT task_id
  FROM pen_visit_tasks
  WHERE tenant_id = $1::uuid
    AND work_state IN ('scheduled', 'delayed')
    AND status IN ('open', 'rework')
    AND due_business_date < $2::date
  ORDER BY due_business_date, task_id
  LIMIT $3
  FOR UPDATE SKIP LOCKED
	) claim
	WHERE t.tenant_id = $1::uuid AND t.task_id = claim.task_id`
	sqlRepository10 = `
	SELECT ` + taskColumns + ` ` + taskFrom + `
	WHERE t.tenant_id = $1::uuid
	  AND t.source_business_date = $2::date
	  AND t.work_state IN ('scheduled', 'delayed')
	  AND t.status IN ('open', 'rework')
	ORDER BY t.park_id, t.due_business_date, t.shed_id, t.partition_key, t.task_id`
	// Approve: both dimensions complete in one statement, fenced on the row version the
	// consumer locked.
	sqlRepository11 = `
UPDATE pen_visit_tasks
SET status = 'completed',
    work_state = 'completed',
    verified_by = NULLIF($3::text, '')::uuid,
    verified_at = $4::timestamptz,
    updated_at = $4::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2::uuid
  AND status = 'pending_verification'
  AND row_version = $5`
	// Reject: back to the visitor with the verifier's words; the kernel clock keeps running.
	sqlRepository12 = `
UPDATE pen_visit_tasks
SET status = 'rework',
    rework_reason = NULLIF($3::text, ''),
    updated_at = $4::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2::uuid
  AND status = 'pending_verification'
  AND row_version = $5`
	sqlRepository14 = `
SELECT t.reasons
FROM pen_visit_tasks t
WHERE t.tenant_id = $1 AND ` + visitorParkPredicate + `
  AND t.work_state IN ('scheduled', 'delayed') AND t.status IN ('open', 'rework')
LIMIT 500`
	// Link parents to the pen's visit for the source day. Resolved through the natural key so a
	// widened or untouched visit links exactly like an inserted one.
	sqlRepository13 = `
INSERT INTO pen_visit_task_sources (tenant_id, task_id, source_kind, source_ref_id)
SELECT t.tenant_id, t.task_id, p.source_kind, p.source_ref_id
FROM unnest($2::uuid[], $3::uuid[], $4::text[], $5::text[], $6::uuid[])
  AS p(park_id, shed_id, partition_label, source_kind, source_ref_id)
JOIN pen_visit_tasks t
  ON t.tenant_id = $1::uuid
 AND t.park_id = p.park_id
 AND t.shed_id = p.shed_id
 AND t.partition_key = CASE WHEN btrim(p.partition_label) = '' THEN 'whole' ELSE lower(btrim(p.partition_label)) END
 AND t.source_business_date = $7::date
ON CONFLICT DO NOTHING`
)
