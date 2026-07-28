// Package postgres implements the tasks (birth/death workflow) storage port over Postgres.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

const defaultQueryTimeout = 3 * time.Second

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Repository implements ports.Repository over pgx.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

var _ ports.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.timeout)
}

// ---------------------------------------------------------------------------
// Open
// ---------------------------------------------------------------------------

// OpenWorkflow inserts the workflow instance and its template's action rows in ONE transaction.
// The instance INSERT is idempotent on the natural key (tenant, template, subject goat) via
// ON CONFLICT DO NOTHING: a redelivered event, or a twin's second attach to the shared mother
// workflow, inserts nothing and duplicates no actions.
func (r *Repository) OpenWorkflow(ctx context.Context, cmd ports.OpenWorkflowCommand) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	template, ok := domain.TemplateByKey(cmd.TemplateKey)
	if !ok {
		return false, domain.ErrUnknownTemplate
	}
	if strings.TrimSpace(cmd.TenantID) == "" || strings.TrimSpace(cmd.SubjectGoatID) == "" || cmd.EventAt.IsZero() {
		return false, domain.ErrMissingRequiredField
	}

	// Initial card fields come straight from the template (compute-on-write from the very first row):
	// the first main step is the next action, and the counters cover main steps only.
	var first *domain.ActionTemplate
	for i := range template.Actions {
		a := template.Actions[i]
		if a.Section == domain.SectionMain {
			if first == nil || a.Seq < first.Seq {
				first = &template.Actions[i]
			}
		}
	}
	var nextKey, nextTitle any
	var nextDue any
	if first != nil {
		nextKey = first.Key
		nextTitle = first.Title
		nextDue = first.Schedule.DueAt(cmd.EventAt).UTC()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var workflowID string
	err = tx.QueryRow(ctx, `
INSERT INTO workflow_instances (
  tenant_id, template_key, module, subject_goat_id, dam_goat_id,
  event_at, event_date, park_id, shed_id, state,
  actions_total, actions_done, next_action_key, next_action_title, next_due_at
) VALUES (
  $1::uuid, $2, $3, $4::uuid, nullif($5::text,'')::uuid,
  $6::timestamptz, $7::date, nullif($8::text,'')::uuid, nullif($9::text,'')::uuid, 'open',
  $10, 0, $11, $12, $13::timestamptz
)
ON CONFLICT (tenant_id, template_key, subject_goat_id) DO NOTHING
RETURNING workflow_id::text`,
		cmd.TenantID, cmd.TemplateKey, template.Module, cmd.SubjectGoatID, deref(cmd.DamGoatID),
		cmd.EventAt.UTC(), biztime.BusinessDate(cmd.EventAt), deref(cmd.ParkID), deref(cmd.ShedID),
		template.MainActionCount(), nextKey, nextTitle, nextDue,
	).Scan(&workflowID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Natural-key conflict: the workflow already exists. Do not touch its actions.
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		committed = true
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// One multi-VALUES INSERT for every template step (<= 13 rows) — set-based, never a per-row loop
	// of Execs.
	var (
		sb   strings.Builder
		args []any
	)
	sb.WriteString(`INSERT INTO workflow_actions (
  tenant_id, workflow_id, action_key, seq, section, action_type, title, detail, requires_video, options, due_at
) VALUES `)
	for i, a := range template.Actions {
		if i > 0 {
			sb.WriteString(", ")
		}
		base := len(args)
		sb.WriteString(fmt.Sprintf("($%d::uuid, $%d::uuid, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::jsonb, $%d::timestamptz)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11))
		var options any
		if len(a.Options) > 0 {
			raw, err := json.Marshal(a.Options)
			if err != nil {
				return false, err
			}
			options = string(raw)
		}
		var dueAt any
		if a.Schedule != (domain.Schedule{}) {
			dueAt = a.Schedule.DueAt(cmd.EventAt).UTC()
		} else {
			// Immediate steps are due at the event moment itself.
			dueAt = cmd.EventAt.UTC()
		}
		args = append(args, cmd.TenantID, workflowID, a.Key, a.Seq, a.Section, a.Type, a.Title, a.Detail, a.RequiresVideo, options, dueAt)
	}
	if _, err := tx.Exec(ctx, sb.String(), args...); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}

// ---------------------------------------------------------------------------
// Canonical goat reads
// ---------------------------------------------------------------------------

// GoatWorkflowFacts reads the canonical goat row by PK — one indexed lookup, no fan-out.
func (r *Repository) GoatWorkflowFacts(ctx context.Context, tenantID, goatID string) (ports.GoatWorkflowFacts, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var (
		out         ports.GoatWorkflowFacts
		dob         *time.Time
		timeOfBirth *string
		parkID      *string
		shedID      *string
	)
	err := r.pool.QueryRow(ctx, `
SELECT goat_id::text, display_id, species, sex, COALESCE(breed, ''),
       dob, to_char(time_of_birth, 'HH24:MI'), lifecycle_status,
       park_id::text, shed_id::text
FROM goats
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, tenantID, goatID).Scan(
		&out.GoatID, &out.DisplayID, &out.Species, &out.Sex, &out.Breed,
		&dob, &timeOfBirth, &out.LifecycleStatus, &parkID, &shedID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.GoatWorkflowFacts{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.GoatWorkflowFacts{}, err
	}
	out.DOB = dob
	out.TimeOfBirth = timeOfBirth
	out.ParkID = parkID
	out.ShedID = shedID
	return out, nil
}

// ResolveDamGoat resolves a free-text dam reference: a UUID is a direct goat PK; anything else is
// matched against goat_identifiers.normalized_value (identity's normalization is upper+trim).
func (r *Repository) ResolveDamGoat(ctx context.Context, tenantID, damRef string) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	damRef = strings.TrimSpace(damRef)
	if damRef == "" {
		return "", domain.ErrNotFound
	}
	var goatID string
	if uuidPattern.MatchString(damRef) {
		err := r.pool.QueryRow(ctx, `
SELECT goat_id::text FROM goats WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
			tenantID, damRef).Scan(&goatID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domain.ErrNotFound
		}
		return goatID, err
	}
	err := r.pool.QueryRow(ctx, `
SELECT goat_id::text
FROM goat_identifiers
WHERE tenant_id = $1::uuid AND normalized_value = $2
LIMIT 1`, tenantID, strings.ToUpper(damRef)).Scan(&goatID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return goatID, err
}

// ---------------------------------------------------------------------------
// List / detail reads
// ---------------------------------------------------------------------------

// cardSelectColumns is shared by the list page and the detail header so both render the same card.
const cardSelectColumns = `
  wi.workflow_id::text, wi.module, wi.template_key, wi.subject_goat_id::text,
  wi.event_at, wi.event_date::text, wi.state,
  wi.actions_total, wi.actions_done,
  wi.next_action_key, wi.next_action_title, wi.next_due_at, wi.awaiting_verification,
  g.display_id, g.sex, COALESCE(g.breed, ''),
  COALESCE(tag.identifier_value, ''),
  COALESCE(park.name, ''), COALESCE(shed.name, '')`

const cardJoins = `
FROM workflow_instances wi
JOIN goats g
  ON g.tenant_id = wi.tenant_id AND g.goat_id = wi.subject_goat_id
LEFT JOIN LATERAL (
  SELECT gi.identifier_value
  FROM goat_identifiers gi
  WHERE gi.tenant_id = wi.tenant_id AND gi.goat_id = wi.subject_goat_id AND gi.status = 'active'
  ORDER BY (gi.identifier_type = 'animal_identifier_1') DESC,
           (gi.identifier_type = 'temporary_tag') DESC,
           gi.valid_from DESC
  LIMIT 1
) tag ON true
LEFT JOIN locations park
  ON park.tenant_id = wi.tenant_id AND park.location_id = wi.park_id
LEFT JOIN locations shed
  ON shed.tenant_id = wi.tenant_id AND shed.location_id = wi.shed_id`

// ListWorkflows serves one keyset page of cards plus the requested day's chip counts.
//
// projection-review: producer grain = `workflow_actions` unique (workflow_id, action_key); consumer
// card grain = `workflow_instances` unique (tenant_id, template_key, subject_goat_id) = 1 row per
// card. The card counters were pre-aggregated ON WRITE in the action-write transaction, so this
// read touches workflow_instances alone for state — the joins here (goat PK, one LATERAL tag row,
// two location PK joins) are all 1:1 display enrichments that cannot fan a card out. Chip counts
// group the SAME (tenant_id, module, event_date) workflow_instances key set the page reads —
// numerator and denominator range over identical rows; Completed excludes awaiting_verification,
// making Completed and Awaiting video mutually exclusive; page size never changes the chips. Keyset
// over (next_due_at ASC NULLS LAST, workflow_id ASC) matches workflow_instances_list_idx.
func (r *Repository) ListWorkflows(ctx context.Context, q domain.WorkflowListQuery) (domain.WorkflowListPage, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	pageSize := q.PageSize
	if pageSize <= 0 || pageSize > domain.MaxWorkflowPageSize {
		pageSize = domain.MaxWorkflowPageSize
	}
	now := q.Now
	if now.IsZero() {
		now = time.Now()
	}

	var page domain.WorkflowListPage
	err := r.pool.QueryRow(ctx, `
SELECT
  count(*) FILTER (WHERE state <> 'canceled'),
  count(*) FILTER (WHERE state = 'open' AND next_due_at IS NOT NULL AND next_due_at < $4::timestamptz),
  count(*) FILTER (WHERE state = 'open' AND (next_due_at IS NULL OR next_due_at >= $4::timestamptz)),
  count(*) FILTER (WHERE state = 'completed' AND NOT awaiting_verification),
  count(*) FILTER (WHERE awaiting_verification)
FROM workflow_instances
WHERE tenant_id = $1::uuid AND module = $2 AND event_date = $3::date`,
		q.TenantID, q.Module, q.EventDate, now.UTC()).Scan(
		&page.Chips.All, &page.Chips.Overdue, &page.Chips.Due, &page.Chips.Completed, &page.Chips.AwaitingVideo)
	if err != nil {
		return domain.WorkflowListPage{}, err
	}

	// The "now" bind is appended ONLY by the filters that actually reference it. Binding it
	// unconditionally left an unreferenced parameter in the statement for the All / Completed /
	// Awaiting-video filters, and Postgres cannot infer an unused parameter's type — every default
	// list load failed with "could not determine data type of parameter $4" (SQLSTATE 42P18).
	args := []any{q.TenantID, q.Module, q.EventDate}
	filterSQL := ""
	switch q.Filter {
	case "", domain.FilterAll:
		filterSQL = ` AND wi.state <> 'canceled'`
	case domain.FilterOverdue:
		args = append(args, now.UTC())
		filterSQL = fmt.Sprintf(` AND wi.state = 'open' AND wi.next_due_at IS NOT NULL AND wi.next_due_at < $%d::timestamptz`, len(args))
	case domain.FilterDue:
		args = append(args, now.UTC())
		filterSQL = fmt.Sprintf(` AND wi.state = 'open' AND (wi.next_due_at IS NULL OR wi.next_due_at >= $%d::timestamptz)`, len(args))
	case domain.FilterCompleted:
		filterSQL = ` AND wi.state = 'completed' AND NOT wi.awaiting_verification`
	case domain.FilterAwaitingVideo:
		filterSQL = ` AND wi.awaiting_verification`
	default:
		return domain.WorkflowListPage{}, domain.ErrInvalidCursor
	}

	cursorSQL := ""
	if q.Cursor != nil {
		if q.Cursor.DueIsNull {
			args = append(args, q.Cursor.WorkflowID)
			cursorSQL = fmt.Sprintf(` AND wi.next_due_at IS NULL AND wi.workflow_id > $%d::uuid`, len(args))
		} else {
			args = append(args, q.Cursor.NextDueAt.UTC())
			dueArg := len(args)
			args = append(args, q.Cursor.WorkflowID)
			idArg := len(args)
			cursorSQL = fmt.Sprintf(` AND (wi.next_due_at IS NULL OR wi.next_due_at > $%d::timestamptz OR (wi.next_due_at = $%d::timestamptz AND wi.workflow_id > $%d::uuid))`,
				dueArg, dueArg, idArg)
		}
	}
	args = append(args, pageSize+1)

	rows, err := r.pool.Query(ctx, `
SELECT `+cardSelectColumns+cardJoins+`
WHERE wi.tenant_id = $1::uuid AND wi.module = $2 AND wi.event_date = $3::date`+filterSQL+cursorSQL+`
ORDER BY wi.next_due_at ASC NULLS LAST, wi.workflow_id ASC
LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return domain.WorkflowListPage{}, err
	}
	defer rows.Close()

	items := make([]domain.WorkflowCard, 0, pageSize)
	for rows.Next() {
		card, err := scanCard(rows, now)
		if err != nil {
			return domain.WorkflowListPage{}, err
		}
		items = append(items, card)
	}
	if err := rows.Err(); err != nil {
		return domain.WorkflowListPage{}, err
	}
	if len(items) > pageSize {
		items = items[:pageSize]
		last := items[len(items)-1]
		cursor := domain.WorkflowCursor{WorkflowID: last.WorkflowID, DueIsNull: last.NextDueAt == nil}
		if last.NextDueAt != nil {
			cursor.NextDueAt = *last.NextDueAt
		}
		encoded := domain.EncodeWorkflowCursor(cursor)
		page.NextCursor = &encoded
	}
	page.Items = items
	return page, nil
}

type cardScanner interface {
	Scan(dest ...any) error
}

func scanCard(row cardScanner, now time.Time) (domain.WorkflowCard, error) {
	var (
		card      domain.WorkflowCard
		nextKey   *string
		nextTitle *string
		nextDue   *time.Time
	)
	if err := row.Scan(
		&card.WorkflowID, &card.Module, &card.TemplateKey, &card.Subject.GoatID,
		&card.EventAt, &card.EventDate, &card.State,
		&card.ActionsTotal, &card.ActionsDone,
		&nextKey, &nextTitle, &nextDue, &card.AwaitingVerification,
		&card.Subject.DisplayID, &card.Subject.Sex, &card.Subject.Breed,
		&card.Subject.Tag,
		&card.ParkLabel, &card.ShedLabel,
	); err != nil {
		return domain.WorkflowCard{}, err
	}
	card.Subject.RoleLabel = domain.RoleLabelForTemplate(card.TemplateKey)
	card.NextDueAt = nextDue
	if nextKey != nil {
		card.NextAction = &domain.WorkflowNextAction{
			Key:     *nextKey,
			Title:   valueOr(nextTitle, ""),
			DueAt:   nextDue,
			Overdue: nextDue != nil && nextDue.Before(now),
		}
	}
	return card, nil
}

// GetWorkflow serves the full detail: card header + facts + all action rows (<= 13, ordered by seq).
func (r *Repository) GetWorkflow(ctx context.Context, tenantID, workflowID string, now time.Time) (domain.WorkflowDetail, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if now.IsZero() {
		now = time.Now()
	}
	var detail domain.WorkflowDetail
	var damDisplay *string
	row := r.pool.QueryRow(ctx, `
SELECT `+cardSelectColumns+`, dam.display_id`+cardJoins+`
LEFT JOIN goats dam
  ON dam.tenant_id = wi.tenant_id AND dam.goat_id = wi.dam_goat_id
WHERE wi.tenant_id = $1::uuid AND wi.workflow_id = $2::uuid`, tenantID, workflowID)
	var (
		card      domain.WorkflowCard
		nextKey   *string
		nextTitle *string
		nextDue   *time.Time
	)
	err := row.Scan(
		&card.WorkflowID, &card.Module, &card.TemplateKey, &card.Subject.GoatID,
		&card.EventAt, &card.EventDate, &card.State,
		&card.ActionsTotal, &card.ActionsDone,
		&nextKey, &nextTitle, &nextDue, &card.AwaitingVerification,
		&card.Subject.DisplayID, &card.Subject.Sex, &card.Subject.Breed,
		&card.Subject.Tag,
		&card.ParkLabel, &card.ShedLabel,
		&damDisplay,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WorkflowDetail{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.WorkflowDetail{}, err
	}
	card.Subject.RoleLabel = domain.RoleLabelForTemplate(card.TemplateKey)
	card.NextDueAt = nextDue
	if nextKey != nil {
		card.NextAction = &domain.WorkflowNextAction{
			Key:     *nextKey,
			Title:   valueOr(nextTitle, ""),
			DueAt:   nextDue,
			Overdue: nextDue != nil && nextDue.Before(now),
		}
	}
	detail.Card = card

	actions, err := r.listActions(ctx, r.pool, tenantID, workflowID, false)
	if err != nil {
		return domain.WorkflowDetail{}, err
	}
	detail.Actions = actions

	eventLabel := card.EventAt.In(biztime.DefaultLocation()).Format("02 Jan 2006 · 15:04")
	facts := []domain.WorkflowFact{{Label: "Event", Value: eventLabel}}
	if card.ParkLabel != "" {
		facts = append(facts, domain.WorkflowFact{Label: "Park", Value: card.ParkLabel})
	}
	if card.ShedLabel != "" {
		facts = append(facts, domain.WorkflowFact{Label: "Shed", Value: card.ShedLabel})
	}
	if damDisplay != nil && *damDisplay != "" {
		// On the kid track dam_goat_id is the mother; on the mother track it links back to the
		// (first) kid. Label the fact accordingly.
		label := "Mother"
		if card.TemplateKey == domain.TemplateKeyBirthMother {
			label = "Kid"
		}
		facts = append(facts, domain.WorkflowFact{Label: label, Value: *damDisplay})
	}
	detail.Facts = facts
	return detail, nil
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (r *Repository) listActions(ctx context.Context, q queryer, tenantID, workflowID string, forUpdate bool) ([]domain.WorkflowAction, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	rows, err := q.Query(ctx, `
SELECT action_id::text, tenant_id::text, workflow_id::text, action_key, seq, section, action_type,
       title, COALESCE(detail, ''), requires_video, options, due_at, status,
       answer_value, proof_ref, completed_by::text, completed_at, verification_item_id::text,
       idempotency_key, request_fingerprint, row_version
FROM workflow_actions
WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid
ORDER BY seq ASC`+lock, tenantID, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.WorkflowAction
	for rows.Next() {
		var (
			a       domain.WorkflowAction
			options []byte
		)
		if err := rows.Scan(
			&a.ActionID, &a.TenantID, &a.WorkflowID, &a.ActionKey, &a.Seq, &a.Section, &a.ActionType,
			&a.Title, &a.Detail, &a.RequiresVideo, &options, &a.DueAt, &a.Status,
			&a.AnswerValue, &a.ProofRef, &a.CompletedBy, &a.CompletedAt, &a.VerificationItemID,
			&a.IdempotencyKey, &a.RequestFingerprint, &a.RowVersion,
		); err != nil {
			return nil, err
		}
		if len(options) > 0 {
			if err := json.Unmarshal(options, &a.Options); err != nil {
				return nil, err
			}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Action writes (answer / complete / verdicts) — card fields maintained in the SAME transaction
// ---------------------------------------------------------------------------

// workflowMutation is the shared write transaction: lock the instance, lock its (<= 13) actions,
// run the pure domain mutation, persist the changed rows, and recompute the card fields — all in
// ONE transaction so a card can never disagree with its steps.
//
// projection-review: producer grain = `workflow_actions` unique (workflow_id, action_key); consumer
// card grain = `workflow_instances` unique (tenant_id, template_key, subject_goat_id) = 1 row per
// card. `actions_done` / `actions_total` / `next_*` use the same key set: that workflow's main,
// non-approval operator actions (join key workflow_id, 1:N pre-aggregated on write in the same txn
// by domain.RecomputeCard); workflow state uses all main actions. Chip counts group
// workflow_instances rows by derived bucket at (tenant_id, module, event_date) grain — numerator
// and denominator both range over the same workflow_instances key set; no join fan-out.
func (r *Repository) workflowMutation(
	ctx context.Context, tenantID, workflowID string,
	mutate func(w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error),
) (domain.WorkflowInstance, []domain.WorkflowAction, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.WorkflowInstance{}, nil, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	w, err := lockInstance(ctx, tx, tenantID, workflowID)
	if err != nil {
		return domain.WorkflowInstance{}, nil, false, err
	}
	actions, err := r.listActions(ctx, tx, tenantID, workflowID, true)
	if err != nil {
		return domain.WorkflowInstance{}, nil, false, err
	}

	changed, replay, err := mutate(&w, actions)
	if err != nil {
		return domain.WorkflowInstance{}, nil, false, err
	}
	if replay {
		// Exact idempotent replay: return current state, mutate nothing.
		if err := tx.Commit(ctx); err != nil {
			return domain.WorkflowInstance{}, nil, false, err
		}
		committed = true
		return w, actions, true, nil
	}

	for _, a := range changed {
		// scale-guard:ignore: bounded — `changed` holds at most 3 template actions (written action + death sign-off / two video resets) out of <=13 per workflow; never a data-sized set
		if _, err := tx.Exec(ctx, `
UPDATE workflow_actions
SET status = $3, answer_value = $4, proof_ref = $5, completed_by = nullif($6::text,'')::uuid,
    completed_at = $7::timestamptz, verification_item_id = nullif($8::text,'')::uuid,
    idempotency_key = $9, request_fingerprint = $10,
    row_version = $11, updated_at = now()
WHERE tenant_id = $1::uuid AND action_id = $2::uuid`,
			tenantID, a.ActionID, a.Status, a.AnswerValue, a.ProofRef, derefPtr(a.CompletedBy),
			a.CompletedAt, derefPtr(a.VerificationItemID), a.IdempotencyKey, a.RequestFingerprint,
			a.RowVersion,
		); err != nil {
			if isUniqueViolation(err) {
				// workflow_actions_idempotency_uq: this client key already claimed a DIFFERENT action write.
				return domain.WorkflowInstance{}, nil, false, domain.ErrIdempotencyConflict
			}
			return domain.WorkflowInstance{}, nil, false, err
		}
	}

	recomputed := domain.RecomputeCard(w, actions)
	if _, err := tx.Exec(ctx, `
UPDATE workflow_instances
SET state = $3, actions_total = $4, actions_done = $5,
    next_action_key = $6, next_action_title = $7, next_due_at = $8::timestamptz,
    awaiting_verification = $9, row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`,
		tenantID, workflowID, recomputed.State, recomputed.ActionsTotal, recomputed.ActionsDone,
		recomputed.NextActionKey, recomputed.NextActionTitle, recomputed.NextDueAt,
		recomputed.AwaitingVerification,
	); err != nil {
		return domain.WorkflowInstance{}, nil, false, err
	}
	recomputed.RowVersion = w.RowVersion + 1

	if err := tx.Commit(ctx); err != nil {
		return domain.WorkflowInstance{}, nil, false, err
	}
	committed = true
	return recomputed, actions, false, nil
}

func lockInstance(ctx context.Context, tx pgx.Tx, tenantID, workflowID string) (domain.WorkflowInstance, error) {
	var w domain.WorkflowInstance
	err := tx.QueryRow(ctx, `
SELECT workflow_id::text, tenant_id::text, template_key, module, subject_goat_id::text,
       dam_goat_id::text, event_at, event_date::text, park_id::text, shed_id::text, state,
       actions_total, actions_done, next_action_key, next_action_title, next_due_at,
       awaiting_verification, row_version
FROM workflow_instances
WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid
FOR UPDATE`, tenantID, workflowID).Scan(
		&w.WorkflowID, &w.TenantID, &w.TemplateKey, &w.Module, &w.SubjectGoatID,
		&w.DamGoatID, &w.EventAt, &w.EventDate, &w.ParkID, &w.ShedID, &w.State,
		&w.ActionsTotal, &w.ActionsDone, &w.NextActionKey, &w.NextActionTitle, &w.NextDueAt,
		&w.AwaitingVerification, &w.RowVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WorkflowInstance{}, domain.ErrNotFound
	}
	return w, err
}

// AnswerAction answers a question / question_select step under the idempotency contract.
func (r *Repository) AnswerAction(ctx context.Context, cmd domain.AnswerActionCommand) (domain.ActionWriteResult, error) {
	var target domain.WorkflowAction
	w, actions, replay, err := r.workflowMutation(ctx, cmd.TenantID, cmd.WorkflowID,
		func(_ *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			idx := findAction(actions, cmd.ActionID)
			if idx < 0 {
				return nil, false, domain.ErrNotFound
			}
			if domain.OperatorActionBlocked(actions[idx], actions) {
				return nil, false, domain.ErrActionOutOfSequence
			}
			updated, isReplay, err := domain.ApplyAnswer(actions[idx], cmd)
			if err != nil {
				return nil, false, err
			}
			actions[idx] = updated
			target = updated
			if isReplay {
				return nil, true, nil
			}
			return []domain.WorkflowAction{updated}, false, nil
		})
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	return buildWriteResult(w, actions, target, replay), nil
}

// CompleteAction completes an "action" step. A requires_video completion without a proof_ref fails
// with domain.ErrProofRequired (HTTP 422 proof_required). Initial death uploads remain staged while
// the goat is alive (admin approval has not applied the exit). After an approved death, a verifier
// rework leaves the goat dead; completing the second re-shot video therefore reopens the workflow's
// verification gate and re-enqueues without requiring a second admin approval.
func (r *Repository) CompleteAction(ctx context.Context, cmd domain.CompleteActionCommand) (domain.ActionWriteResult, error) {
	deathApplied, err := r.deathAlreadyApplied(ctx, cmd.TenantID, cmd.WorkflowID)
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	var target domain.WorkflowAction
	w, actions, replay, err := r.workflowMutation(ctx, cmd.TenantID, cmd.WorkflowID,
		func(w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			idx := findAction(actions, cmd.ActionID)
			if idx < 0 {
				return nil, false, domain.ErrNotFound
			}
			if domain.OperatorActionBlocked(actions[idx], actions) {
				return nil, false, domain.ErrActionOutOfSequence
			}
			updated, isReplay, err := domain.ApplyComplete(actions[idx], cmd)
			if err != nil {
				return nil, false, err
			}
			actions[idx] = updated
			target = updated
			if isReplay {
				return nil, true, nil
			}
			changed := []domain.WorkflowAction{updated}
			// Only re-shoots after an already-applied death return directly to Verify. The initial
			// upload pair waits for the separate admin approval transaction.
			if deathApplied && w.TemplateKey == domain.TemplateKeyDeath && updated.RequiresVideo && domain.DeathVideosComplete(actions) {
				w.AwaitingVerification = true
			}
			return changed, false, nil
		})
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	return buildWriteResult(w, actions, target, replay), nil
}

// deathAlreadyApplied uses the canonical goat lifecycle, not a client/workflow flag. A racing
// initial upload that reads alive stays staged; the subsequent goat.exited event releases it after
// approval. Once dead, the state is terminal, so verifier re-shoots reliably return to review.
func (r *Repository) deathAlreadyApplied(ctx context.Context, tenantID, workflowID string) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	var lifecycle string
	err := r.pool.QueryRow(ctx, `
SELECT g.lifecycle_status
FROM workflow_instances wi
JOIN goats g ON g.tenant_id = wi.tenant_id AND g.goat_id = wi.subject_goat_id
WHERE wi.tenant_id = $1::uuid AND wi.workflow_id = $2::uuid`, tenantID, workflowID).Scan(&lifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, domain.ErrNotFound
	}
	return lifecycle == "dead", err
}

// PrepareDeathEvidenceForApprovalInTx is the counts approval adapter's transaction-scoped seam.
// It proves both mandatory videos exist and atomically opens the workflow verification gate inside
// the SAME transaction that applies the goat exit and approval status.
// Returning ready=false keeps the approval pending and the live count unchanged.
func (r *Repository) PrepareDeathEvidenceForApprovalInTx(
	ctx context.Context, tx pgx.Tx, tenantID, goatID string,
) (ready bool, err error) {
	var workflowID string
	err = tx.QueryRow(ctx, `
SELECT workflow_id::text
FROM workflow_instances
WHERE tenant_id = $1::uuid AND subject_goat_id = $2::uuid AND template_key = $3
FOR UPDATE`, tenantID, goatID, domain.TemplateKeyDeath).Scan(&workflowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	w, err := lockInstance(ctx, tx, tenantID, workflowID)
	if err != nil {
		return false, err
	}
	actions, err := r.listActions(ctx, tx, tenantID, workflowID, true)
	if err != nil {
		return false, err
	}
	if !domain.DeathVideosComplete(actions) || len(domain.DeathProofRefs(actions)) != 2 {
		return false, nil
	}

	if w.AwaitingVerification {
		return true, nil
	}
	w.AwaitingVerification = true
	recomputed := domain.RecomputeCard(w, actions)
	if _, err := tx.Exec(ctx, `
UPDATE workflow_instances
SET state = $3, actions_total = $4, actions_done = $5,
    next_action_key = $6, next_action_title = $7, next_due_at = $8::timestamptz,
    awaiting_verification = $9, row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`,
		tenantID, workflowID, recomputed.State, recomputed.ActionsTotal, recomputed.ActionsDone,
		recomputed.NextActionKey, recomputed.NextActionTitle, recomputed.NextDueAt,
		recomputed.AwaitingVerification); err != nil {
		return false, err
	}
	return true, nil
}

// CompleteTagActionForGoat completes a pending tag_the_kid step on the goat's open birth_kid
// workflow. Called by the goat.identifier.added consumer when the permanent RFID lands through the
// promote flow. Idempotent no-op when there is no open workflow or the step is already completed.
func (r *Repository) CompleteTagActionForGoat(ctx context.Context, tenantID, goatID string, completedAt time.Time) error {
	lookupCtx, cancel := r.withTimeout(ctx)
	workflowID, err := func() (string, error) {
		defer cancel()
		var id string
		err := r.pool.QueryRow(lookupCtx, `
SELECT workflow_id::text
FROM workflow_instances
WHERE tenant_id = $1::uuid AND subject_goat_id = $2::uuid
  AND template_key = $3 AND state = 'open'
LIMIT 1`, tenantID, goatID, domain.TemplateKeyBirthKid).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domain.ErrNotFound
		}
		return id, err
	}()
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	_, _, _, err = r.workflowMutation(ctx, tenantID, workflowID,
		func(_ *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			for i := range actions {
				if actions[i].ActionKey != domain.ActionKeyTagTheKid {
					continue
				}
				if actions[i].Status == domain.ActionStatusCompleted {
					return nil, true, nil
				}
				at := completedAt
				actions[i].Status = domain.ActionStatusCompleted
				actions[i].CompletedAt = &at
				actions[i].RowVersion++
				return []domain.WorkflowAction{actions[i]}, false, nil
			}
			return nil, true, nil
		})
	return err
}

// DeathEvidenceForVerification returns the approved/in-review evidence bundle addressed by the
// goat.exited event. The workflow and its actions are bounded by the natural key and template
// size; no history or tenant-wide scan is involved.
func (r *Repository) DeathEvidenceForVerification(ctx context.Context, tenantID, goatID string) (ports.DeathEvidenceReview, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var (
		out                   ports.DeathEvidenceReview
		parkID, shedID        *string
		deathProof, postProof string
		operatorID            *string
	)
	err := r.pool.QueryRow(ctx, `
SELECT wi.workflow_id::text, wi.event_date::text, wi.park_id::text, wi.shed_id::text,
       death.proof_ref, postmortem.proof_ref,
       COALESCE(postmortem.completed_by, death.completed_by)::text,
       wi.row_version
FROM workflow_instances wi
JOIN workflow_actions death
  ON death.workflow_id = wi.workflow_id AND death.action_key = $3 AND death.status = 'completed'
JOIN workflow_actions postmortem
  ON postmortem.workflow_id = wi.workflow_id AND postmortem.action_key = $4 AND postmortem.status = 'completed'
WHERE wi.tenant_id = $1::uuid AND wi.subject_goat_id = $2::uuid AND wi.template_key = $5
  AND wi.awaiting_verification = true`,
		tenantID, goatID, domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo,
		domain.TemplateKeyDeath).Scan(
		&out.WorkflowID, &out.EventDate, &parkID, &shedID, &deathProof, &postProof,
		&operatorID, &out.Round,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.DeathEvidenceReview{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.DeathEvidenceReview{}, err
	}
	out.ParkID = deref(parkID)
	out.ShedID = deref(shedID)
	out.OperatorID = deref(operatorID)
	out.ProofRefs = []string{deathProof, postProof}
	return out, nil
}

// CancelDeathWorkflowForGoat closes staged work after an admin rejection. The set is bounded to
// one workflow and its three template actions, and repeated rejection-event delivery is a no-op.
func (r *Repository) CancelDeathWorkflowForGoat(ctx context.Context, tenantID, goatID string, _ time.Time) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var workflowID string
	err = tx.QueryRow(ctx, `
SELECT workflow_id::text
FROM workflow_instances
WHERE tenant_id = $1::uuid AND subject_goat_id = $2::uuid AND template_key = $3
FOR UPDATE`, tenantID, goatID, domain.TemplateKeyDeath).Scan(&workflowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE workflow_actions
SET status = 'canceled', proof_ref = NULL, completed_by = NULL, completed_at = NULL,
    row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid AND status <> 'canceled'`, tenantID, workflowID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE workflow_instances
SET state = 'canceled', actions_done = 0, next_action_key = NULL, next_action_title = NULL,
    next_due_at = NULL, awaiting_verification = false,
    row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid AND state <> 'canceled'`, tenantID, workflowID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ApplyDeathSignoffApproved closes the workflow verification gate after approval. Idempotent under
// redelivery.
func (r *Repository) ApplyDeathSignoffApproved(ctx context.Context, cmd ports.DeathVerdictCommand) error {
	_, _, _, err := r.workflowMutation(ctx, cmd.TenantID, cmd.WorkflowID,
		func(w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			if w.TemplateKey != domain.TemplateKeyDeath {
				return nil, false, domain.ErrNotFound
			}
			if !w.AwaitingVerification {
				return nil, true, nil
			}
			w.AwaitingVerification = false
			return nil, false, nil
		})
	// ErrNotFound propagates: an already-applied verdict is a no-op mutation (not ErrNotFound), so
	// this only fires when the verdict addresses a workflow that does not exist or is not a death
	// workflow — a mis-routed ref_id. The app layer logs it and acks; swallowing it here would make it
	// indistinguishable from a benign replay.
	return err
}

// BounceDeathVideosForRework resets both video actions to 'rework' (clearing their proofs, so a
// re-shoot is mandatory) and closes the current workflow verification gate. Idempotent on replay.
func (r *Repository) BounceDeathVideosForRework(ctx context.Context, cmd ports.DeathVerdictCommand) error {
	_, _, _, err := r.workflowMutation(ctx, cmd.TenantID, cmd.WorkflowID,
		func(w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			if w.TemplateKey != domain.TemplateKeyDeath {
				return nil, false, domain.ErrNotFound
			}
			var changed []domain.WorkflowAction
			w.AwaitingVerification = false
			for i := range actions {
				switch actions[i].ActionKey {
				case domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo:
					if actions[i].Status == domain.ActionStatusCompleted || actions[i].Status == domain.ActionStatusInReview {
						actions[i].Status = domain.ActionStatusRework
						actions[i].ProofRef = nil
						actions[i].CompletedAt = nil
						actions[i].CompletedBy = nil
						actions[i].RowVersion++
						changed = append(changed, actions[i])
					}
				}
			}
			if len(changed) == 0 {
				return nil, true, nil
			}
			return changed, false, nil
		})
	// ErrNotFound propagates for the same reason as the approve path: a redelivered rework is a no-op
	// mutation, so this is only a mis-routed ref_id and must not vanish silently.
	return err
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func buildWriteResult(w domain.WorkflowInstance, actions []domain.WorkflowAction, target domain.WorkflowAction, replay bool) domain.ActionWriteResult {
	result := domain.ActionWriteResult{Workflow: w, Action: target, Replayed: replay}
	if w.TemplateKey == domain.TemplateKeyDeath && w.AwaitingVerification && domain.DeathVideosComplete(actions) {
		// Computed from state, so a replay after a failed enqueue re-reports it and the retry heals.
		result.NeedsVerificationEnqueue = true
		result.DeathProofRefs = domain.DeathProofRefs(actions)
		result.DeathReviewRound = w.RowVersion
	}
	return result
}

func findAction(actions []domain.WorkflowAction, actionID string) int {
	for i := range actions {
		if actions[i].ActionID == actionID {
			return i
		}
	}
	return -1
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func derefPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func valueOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
