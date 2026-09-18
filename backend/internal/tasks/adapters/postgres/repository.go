// Package postgres implements the tasks (birth/death workflow) storage port over Postgres.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

const defaultQueryTimeout = 3 * time.Second

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Repository implements ports.Repository over pgx.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration

	// identityTx places a kid when the Record shed fallback step is completed, inside that action
	// write's own transaction. Optional: a repository without it runs every other workflow
	// unchanged. See newborn_placement.go.
	identityTx IdentityTxWriter
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

// ResolveActionPresentations returns backend-authored workflow task titles and recorded answers at
// action_id grain. The
// verifier proof resolver supplies a bounded page of IDs from proof metadata; action_id is the
// workflow_actions primary key, so this is one indexed query rather than one lookup per video.
func (r *Repository) ResolveActionPresentations(ctx context.Context, tenantID string, actionIDs []string) (map[string]string, map[string]string, error) {
	labels := make(map[string]string, len(actionIDs))
	answers := make(map[string]string, len(actionIDs))
	if len(actionIDs) == 0 {
		return labels, answers, nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT action_id::text, title, COALESCE(NULLIF(btrim(answer_value), ''), '')
FROM workflow_actions
WHERE tenant_id = $1::uuid
  AND action_id = ANY($2::uuid[])`, tenantID, actionIDs)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var actionID, title, answer string
		if err := rows.Scan(&actionID, &title, &answer); err != nil {
			return nil, nil, err
		}
		labels[actionID] = title
		if answer = formatRecordedAnswer(answer); answer != "" {
			answers[actionID] = answer
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return labels, answers, nil
}

func formatRecordedAnswer(answer string) string {
	answer = strings.TrimSpace(answer)
	switch strings.ToLower(answer) {
	case "yes":
		return "Yes"
	case "no":
		return "No"
	default:
		return answer
	}
}

// ---------------------------------------------------------------------------
// Open
// ---------------------------------------------------------------------------

// OpenWorkflow inserts the workflow instance and its template's action rows in ONE transaction.
// The instance INSERT is idempotent on the event-grained birth key or goat-grained death key via
// ON CONFLICT DO NOTHING: redelivery and a twin's second attach duplicate no actions, while a
// later litter for the same dam receives a fresh mother workflow.
func (r *Repository) OpenWorkflow(ctx context.Context, cmd ports.OpenWorkflowCommand) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if strings.TrimSpace(cmd.TenantID) == "" || strings.TrimSpace(cmd.SubjectGoatID) == "" || cmd.EventAt.IsZero() {
		return false, domain.ErrMissingRequiredField
	}
	// The kid track carries the Record shed fallback step ONLY when this kid is not already in a
	// kid pen -- read from ground truth, so it is self-healing: once the fallback tags a pen for
	// kids, the park's next birth places automatically and its workflow opens without the step.
	needsShedPlacement := false
	if cmd.TemplateKey == domain.TemplateKeyBirthKid {
		var placementErr error
		needsShedPlacement, placementErr = r.needsShedPlacement(ctx, cmd.TenantID, cmd.SubjectGoatID)
		if placementErr != nil {
			return false, placementErr
		}
	}
	template, sopVersionID, err := r.compileTemplate(ctx, cmd.TenantID, cmd.TemplateKey, domain.CompileOptions{
		EventAt:            cmd.EventAt,
		NeedsShedPlacement: needsShedPlacement,
	})
	if err != nil {
		return false, err
	}
	var birthEventID *string
	if cmd.TemplateKey == domain.TemplateKeyBirthKid || cmd.TemplateKey == domain.TemplateKeyBirthMother {
		childID := cmd.SubjectGoatID
		if cmd.TemplateKey == domain.TemplateKeyBirthMother && cmd.DamGoatID != nil {
			childID = *cmd.DamGoatID
		}
		var eventID string
		err := r.pool.QueryRow(ctx, `SELECT birth_event_id::text FROM goat_births
WHERE tenant_id=$1::uuid AND child_goat_id=$2::uuid`, cmd.TenantID, childID).Scan(&eventID)
		if err == nil {
			birthEventID = &eventID
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
	}

	// Initial card fields come straight from the template (compute-on-write from the very first row):
	// the first visible operator step is next, and the counters cover every visible operator step.
	var first *domain.ActionTemplate
	for i := range template.Actions {
		a := template.Actions[i]
		if a.Type != domain.ActionTypeApproval {
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
  birth_event_id, event_at, event_date, park_id, shed_id, state,
  actions_total, actions_done, next_action_key, next_action_title, next_due_at, sop_version_id, subject_ref_id,
  capture_evidence
) VALUES (
  $1::uuid, $2, $3, $4::uuid, nullif($5::text,'')::uuid,
  nullif($6::text,'')::uuid, $7::timestamptz, $8::date, nullif($9::text,'')::uuid, nullif($10::text,'')::uuid, 'open',
  $11, 0, $12, $13, $14::timestamptz, nullif($15::text,'')::uuid, nullif($16::text,'')::uuid,
  $17::jsonb
)
ON CONFLICT DO NOTHING
RETURNING workflow_id::text`,
		cmd.TenantID, cmd.TemplateKey, template.Module, cmd.SubjectGoatID, deref(cmd.DamGoatID),
		deref(birthEventID), cmd.EventAt.UTC(), biztime.BusinessDate(cmd.EventAt), deref(cmd.ParkID), deref(cmd.ShedID),
		template.OperatorActionCount(), nextKey, nextTitle, nextDue, sopVersionID, deref(cmd.SubjectRefID),
		mustCaptureJSON(cmd.CaptureEvidence),
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

	// One multi-VALUES INSERT for every template step (<= 18 rows) — set-based, never a per-row loop
	// of Execs. Birth-time eligibility keeps the actual row count between 13 and 18.
	var (
		sb   strings.Builder
		args []any
	)
	sb.WriteString(`INSERT INTO workflow_actions (
  tenant_id, workflow_id, action_key, seq, section, action_type, title, detail, requires_video, options, due_at,
  task_type, answer_type, engine_hook, proof_min_videos, proof_min_photos, hard_time_gate, wait_for_all,
  requires_keys, after_action_key, after_offset_seconds, answer_gate
) VALUES `)
	for i, a := range template.Actions {
		if i > 0 {
			sb.WriteString(", ")
		}
		base := len(args)
		sb.WriteString(fmt.Sprintf("($%d::uuid, $%d::uuid, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::jsonb, $%d::timestamptz, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::jsonb, $%d, $%d, $%d::jsonb)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11,
			base+12, base+13, base+14, base+15, base+16, base+17, base+18, base+19, base+20, base+21, base+22))
		var options any
		if len(a.Options) > 0 {
			raw, err := json.Marshal(a.Options)
			if err != nil {
				return false, err
			}
			options = string(raw)
		}
		var dueAt any
		if a.Schedule.IsDependencyTimed() {
			// Dependency-timed: populated atomically when the named step completes.
			dueAt = nil
		} else if a.Schedule != (domain.Schedule{}) {
			dueAt = a.Schedule.DueAt(cmd.EventAt).UTC()
		} else {
			// Immediate steps are due at the event moment itself.
			dueAt = cmd.EventAt.UTC()
		}
		requires, err := json.Marshal(nonNilStrings(a.Requires))
		if err != nil {
			return false, err
		}
		answerKind := a.AnswerKind
		if answerKind == "" {
			answerKind = domain.AnswerKindNone
		}
		var answerGate any
		if a.AnswerGate != nil {
			raw, err := json.Marshal(a.AnswerGate)
			if err != nil {
				return false, err
			}
			answerGate = string(raw)
		}
		args = append(args, cmd.TenantID, workflowID, a.Key, a.Seq, a.Section, a.Type, a.Title, a.Detail, a.RequiresVideo, options, dueAt,
			a.TaskType, answerKind, a.EngineHook, a.Proof.Video, a.Proof.Photo, a.HardTimeGate, a.WaitForAll,
			string(requires), a.Schedule.AfterStepKey, int(a.Schedule.Offset.Seconds()), answerGate)
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
SELECT g.goat_id::text, g.display_id, g.species, g.sex, COALESCE(g.breed, ''),
       g.dob, to_char(g.time_of_birth, 'HH24:MI'), g.lifecycle_status,
       g.park_id::text, g.shed_id::text,
       COALESCE(CASE WHEN gsp.shed_id = g.shed_id AND lower(btrim(gsp.partition_label)) <> 'whole'
                     THEN btrim(gsp.partition_label) END, '')
FROM goats g
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
WHERE g.tenant_id = $1::uuid AND g.goat_id = $2::uuid`, tenantID, goatID).Scan(
		&out.GoatID, &out.DisplayID, &out.Species, &out.Sex, &out.Breed,
		&dob, &timeOfBirth, &out.LifecycleStatus, &parkID, &shedID, &out.PartitionLabel,
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
  g.display_id, g.row_version, g.sex, COALESCE(g.breed, ''),
  COALESCE(tag.identifier_value, ''),
  COALESCE(park.name, ''), COALESCE(shed.name, ''),
  COALESCE(CASE WHEN gsp.shed_id = wi.shed_id AND lower(btrim(gsp.partition_label)) <> 'whole'
                THEN btrim(gsp.partition_label) END, '')`

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
  ON shed.tenant_id = wi.tenant_id AND shed.location_id = wi.shed_id
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = wi.tenant_id AND gsp.goat_id = wi.subject_goat_id`

// ListWorkflows serves one keyset page of cards plus the requested day's chip counts.
//
// projection-review: membership=workflow_instances for tenant module and event_date with card state pre-aggregated on write; group_key=workflow_id and status chip bucket; join_cardinality=goat tag park and shed enrichments are each one-to-at-most-one; pagination=chips aggregate the whole filter while cards use next_due_at and workflow_id keyset; scope=tenant_id module event_date and requested status
// Producer grain = `workflow_actions` unique (workflow_id, action_key); consumer
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
	todayDate := q.TodayDate
	if todayDate == "" {
		todayDate = q.EventDate
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

	// projection-review: producer and consumer both use workflow_instances at one row per workflow.
	// Grouping by event_date cannot multiply cards; WorkflowCount and the existence of a date range
	// over the identical predicate (tenant, module, open, next_due_at < now, event_date < today).
	// The partial workflow_instances_overdue_dates_idx serves this bounded five-date summary.
	rowsOverdue, err := r.pool.Query(ctx, `
SELECT event_date::text, count(*)::integer
FROM workflow_instances
WHERE tenant_id = $1::uuid
  AND module = $2
  AND state = 'open'
  AND next_due_at IS NOT NULL
  AND next_due_at < $3::timestamptz
  AND event_date < $4::date
GROUP BY event_date
ORDER BY event_date DESC
LIMIT 5`, q.TenantID, q.Module, now.UTC(), todayDate)
	if err != nil {
		return domain.WorkflowListPage{}, err
	}
	for rowsOverdue.Next() {
		var item domain.WorkflowOverdueDate
		if err := rowsOverdue.Scan(&item.Date, &item.WorkflowCount); err != nil {
			rowsOverdue.Close()
			return domain.WorkflowListPage{}, err
		}
		page.OverdueDates = append(page.OverdueDates, item)
	}
	if err := rowsOverdue.Err(); err != nil {
		rowsOverdue.Close()
		return domain.WorkflowListPage{}, err
	}
	rowsOverdue.Close()

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

// cardPenLabel is the card's pen in the farm's words: the shed plus the animal's own partition in
// that shed ("Castro 1", "Godel 1 - Part 3"), composed ONLY by oploc so every surface agrees.
func cardPenLabel(shedName, partitionLabel string) string {
	return oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display()
}

type cardScanner interface {
	Scan(dest ...any) error
}

func scanCard(row cardScanner, now time.Time) (domain.WorkflowCard, error) {
	var (
		partitionLabel string
		card           domain.WorkflowCard
		nextKey        *string
		nextTitle      *string
		nextDue        *time.Time
	)
	if err := row.Scan(
		&card.WorkflowID, &card.Module, &card.TemplateKey, &card.Subject.GoatID,
		&card.EventAt, &card.EventDate, &card.State,
		&card.ActionsTotal, &card.ActionsDone,
		&nextKey, &nextTitle, &nextDue, &card.AwaitingVerification,
		&card.Subject.DisplayID, &card.Subject.RowVersion, &card.Subject.Sex, &card.Subject.Breed,
		&card.Subject.Tag,
		&card.ParkLabel, &card.ShedLabel, &partitionLabel,
	); err != nil {
		return domain.WorkflowCard{}, err
	}
	card.ShedLabel = cardPenLabel(card.ShedLabel, partitionLabel)
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
	var damRFID *string
	var litterSize *int
	// projection-review: goat_births produces one row per unique (tenant_id, child_goat_id) and
	// workflow_instances consumes it on (tenant_id, subject_goat_id), so this join is 1:0..1. A goat
	// can carry two active RFID types; the LATERAL side pre-selects one preferred identifier before
	// the join, keeping it 1:0..1 too. This detail read has no ratio or cap key set.
	row := r.pool.QueryRow(ctx, `
SELECT `+cardSelectColumns+`, dam.display_id, dam_rfid.identifier_value, gb.litter_size`+cardJoins+`
LEFT JOIN goats dam
  ON dam.tenant_id = wi.tenant_id AND dam.goat_id = wi.dam_goat_id
LEFT JOIN LATERAL (
  SELECT gi.identifier_value
  FROM goat_identifiers gi
  WHERE gi.tenant_id = wi.tenant_id
    AND gi.goat_id = wi.dam_goat_id
    AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
    AND gi.status = 'active'
  ORDER BY CASE gi.identifier_type WHEN 'animal_identifier_1' THEN 0 ELSE 1 END
  LIMIT 1
) dam_rfid ON true
LEFT JOIN goat_births gb
  ON gb.tenant_id = wi.tenant_id
 AND gb.child_goat_id = wi.subject_goat_id
 AND wi.template_key = 'birth_kid'
WHERE wi.tenant_id = $1::uuid AND wi.workflow_id = $2::uuid`, tenantID, workflowID)
	var (
		partitionLabel string
		card           domain.WorkflowCard
		nextKey        *string
		nextTitle      *string
		nextDue        *time.Time
	)
	err := row.Scan(
		&card.WorkflowID, &card.Module, &card.TemplateKey, &card.Subject.GoatID,
		&card.EventAt, &card.EventDate, &card.State,
		&card.ActionsTotal, &card.ActionsDone,
		&nextKey, &nextTitle, &nextDue, &card.AwaitingVerification,
		&card.Subject.DisplayID, &card.Subject.RowVersion, &card.Subject.Sex, &card.Subject.Breed,
		&card.Subject.Tag,
		&card.ParkLabel, &card.ShedLabel, &partitionLabel,
		&damDisplay,
		&damRFID,
		&litterSize,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WorkflowDetail{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.WorkflowDetail{}, err
	}
	card.ShedLabel = cardPenLabel(card.ShedLabel, partitionLabel)
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

	eventLabel := biztime.FarmDate(card.EventAt) + " · " + card.EventAt.In(biztime.DefaultLocation()).Format("15:04")
	facts := []domain.WorkflowFact{{Label: "Event", Value: eventLabel}}
	if card.ParkLabel != "" {
		facts = append(facts, domain.WorkflowFact{Label: "Park", Value: card.ParkLabel})
	}
	if card.ShedLabel != "" {
		facts = append(facts, domain.WorkflowFact{Label: "Pen", Value: card.ShedLabel})
	}
	if card.TemplateKey == domain.TemplateKeyBirthKid && damRFID != nil && *damRFID != "" {
		facts = append(facts, domain.WorkflowFact{Label: "Mother RFID", Value: *damRFID})
	} else if damDisplay != nil && *damDisplay != "" {
		// On the kid track dam_goat_id is the mother; on the mother track it links back to the
		// (first) kid. Label the fact accordingly.
		label := "Mother"
		if card.TemplateKey == domain.TemplateKeyBirthMother {
			label = "Kid"
		}
		facts = append(facts, domain.WorkflowFact{Label: label, Value: *damDisplay})
	}
	if litterSize != nil {
		facts = append(facts, domain.WorkflowFact{Label: "Litter size", Value: fmt.Sprintf("%d", *litterSize)})
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
       idempotency_key, request_fingerprint, row_version,
       task_type, answer_type, engine_hook, proof_min_videos, proof_min_photos, proof_refs,
       hard_time_gate, wait_for_all, requires_keys, after_action_key, after_offset_seconds, rework_reason, answer_gate
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
			a          domain.WorkflowAction
			options    []byte
			proofRefs  []byte
			requires   []byte
			answerGate []byte
		)
		if err := rows.Scan(
			&a.ActionID, &a.TenantID, &a.WorkflowID, &a.ActionKey, &a.Seq, &a.Section, &a.ActionType,
			&a.Title, &a.Detail, &a.RequiresVideo, &options, &a.DueAt, &a.Status,
			&a.AnswerValue, &a.ProofRef, &a.CompletedBy, &a.CompletedAt, &a.VerificationItemID,
			&a.IdempotencyKey, &a.RequestFingerprint, &a.RowVersion,
			&a.TaskType, &a.AnswerType, &a.EngineHook, &a.ProofMinVideos, &a.ProofMinPhotos, &proofRefs,
			&a.HardTimeGate, &a.WaitForAll, &requires, &a.AfterActionKey, &a.AfterOffsetSeconds, &a.ReworkReason, &answerGate,
		); err != nil {
			return nil, err
		}
		if len(answerGate) > 0 && string(answerGate) != "null" {
			var gate domain.AnswerCondition
			if err := json.Unmarshal(answerGate, &gate); err != nil {
				return nil, err
			}
			a.AnswerGate = &gate
		}
		if len(options) > 0 {
			if err := json.Unmarshal(options, &a.Options); err != nil {
				return nil, err
			}
		}
		if len(proofRefs) > 0 {
			if err := json.Unmarshal(proofRefs, &a.ProofRefs); err != nil {
				return nil, err
			}
		}
		if len(requires) > 0 {
			if err := json.Unmarshal(requires, &a.RequiresKeys); err != nil {
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
// card. `actions_done` / `actions_total` / `next_*` use the same key set: every visible non-approval
// operator action across main and scheduled-colostrum sections (join key workflow_id, 1:N
// pre-aggregated on write in the same txn by domain.RecomputeCard); death workflow state separately
// uses all main actions. Chip counts group
// workflow_instances rows by derived bucket at (tenant_id, module, event_date) grain — numerator
// and denominator both range over the same workflow_instances key set; no join fan-out.
func (r *Repository) workflowMutation(
	ctx context.Context, tenantID, workflowID string,
	// mutate receives the OPEN transaction so a step whose completion has a canonical side effect
	// (today: the Record shed placement) can write that effect in the SAME transaction as the
	// action row. Callbacks that only change action state ignore it.
	mutate func(tx pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error),
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

	changed, replay, err := mutate(tx, &w, actions)
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
		// scale-guard:ignore: bounded — `changed` holds the written action plus its side effects (death sign-off, two video resets, or the branch steps an answer skipped) out of one workflow's <=18 template actions; never a data-sized set
		if _, err := tx.Exec(ctx, `
UPDATE workflow_actions
SET status = $3, answer_value = $4, proof_ref = $5, completed_by = nullif($6::text,'')::uuid,
    completed_at = $7::timestamptz, verification_item_id = nullif($8::text,'')::uuid,
    idempotency_key = $9, request_fingerprint = $10,
    row_version = $11, due_at = $12::timestamptz, proof_refs = $13::jsonb, rework_reason = $14, updated_at = now()
WHERE tenant_id = $1::uuid AND action_id = $2::uuid`,
			tenantID, a.ActionID, a.Status, a.AnswerValue, a.ProofRef, derefPtr(a.CompletedBy),
			a.CompletedAt, derefPtr(a.VerificationItemID), a.IdempotencyKey, a.RequestFingerprint,
			a.RowVersion, a.DueAt, mustProofJSON(a.ProofRefs), a.ReworkReason,
		); err != nil {
			if isUniqueViolation(err) {
				// workflow_actions_idempotency_uq: this client key already claimed a DIFFERENT action write.
				return domain.WorkflowInstance{}, nil, false, domain.ErrIdempotencyConflict
			}
			return domain.WorkflowInstance{}, nil, false, err
		}
	}

	// A mutation may APPEND steps (a capture re-shoot); count them in the recompute.
	for _, a := range changed {
		if findAction(actions, a.ActionID) < 0 {
			actions = append(actions, a)
		}
	}
	recomputed := domain.RecomputeCard(w, actions)
	if _, err := tx.Exec(ctx, `
UPDATE workflow_instances
SET state = $3, actions_total = $4, actions_done = $5,
    next_action_key = $6, next_action_title = $7, next_due_at = $8::timestamptz,
    awaiting_verification = $9, capture_evidence = $10::jsonb, row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`,
		tenantID, workflowID, recomputed.State, recomputed.ActionsTotal, recomputed.ActionsDone,
		recomputed.NextActionKey, recomputed.NextActionTitle, recomputed.NextDueAt,
		recomputed.AwaitingVerification, mustCaptureJSON(recomputed.CaptureEvidence),
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

const instanceSelectColumns = `
SELECT workflow_id::text, tenant_id::text, template_key, module, subject_goat_id::text,
       dam_goat_id::text, event_at, event_date::text, park_id::text, shed_id::text, state,
       actions_total, actions_done, next_action_key, next_action_title, next_due_at,
       awaiting_verification, row_version, COALESCE(capture_evidence, '{}'::jsonb), birth_event_id::text
FROM workflow_instances
WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`

func scanInstance(row pgx.Row) (domain.WorkflowInstance, error) {
	var w domain.WorkflowInstance
	var capture []byte
	err := row.Scan(
		&w.WorkflowID, &w.TenantID, &w.TemplateKey, &w.Module, &w.SubjectGoatID,
		&w.DamGoatID, &w.EventAt, &w.EventDate, &w.ParkID, &w.ShedID, &w.State,
		&w.ActionsTotal, &w.ActionsDone, &w.NextActionKey, &w.NextActionTitle, &w.NextDueAt,
		&w.AwaitingVerification, &w.RowVersion, &capture, &w.BirthEventID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WorkflowInstance{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.WorkflowInstance{}, err
	}
	if len(capture) > 0 {
		if err := json.Unmarshal(capture, &w.CaptureEvidence); err != nil {
			return domain.WorkflowInstance{}, err
		}
	}
	return w, nil
}

func lockInstance(ctx context.Context, tx pgx.Tx, tenantID, workflowID string) (domain.WorkflowInstance, error) {
	return scanInstance(tx.QueryRow(ctx, instanceSelectColumns+` FOR UPDATE`, tenantID, workflowID))
}

// mustCaptureJSON encodes the capture snapshot for the jsonb column ('{}' when empty).
func mustCaptureJSON(e authored.Evidence) string {
	raw, err := json.Marshal(e)
	if err != nil || len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

// AnswerAction answers a question / question_select step under the idempotency contract.
func (r *Repository) AnswerAction(ctx context.Context, cmd domain.AnswerActionCommand) (domain.ActionWriteResult, error) {
	deathApplied, err := r.deathAlreadyApplied(ctx, cmd.TenantID, cmd.WorkflowID)
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	var target domain.WorkflowAction
	w, actions, replay, err := r.workflowMutation(ctx, cmd.TenantID, cmd.WorkflowID,
		func(tx pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			idx := findAction(actions, cmd.ActionID)
			if idx < 0 {
				return nil, false, domain.ErrNotFound
			}
			if domain.OperatorActionBlocked(w.TemplateKey, actions[idx], actions) {
				return nil, false, domain.ErrActionOutOfSequence
			}
			updated, isReplay, err := domain.ApplyAnswer(actions[idx], cmd)
			if err != nil {
				return nil, false, err
			}
			if !isReplay {
				// Birth is reviewed per step: a recorded clip waits for its own verdict.
				updated = domain.HoldStepForReview(w.TemplateKey, updated)
			}
			actions[idx] = updated
			target = updated
			if isReplay {
				return nil, true, nil
			}
			// THE PLACEMENT COMMITS WITH THE ANSWER. Completing Record shed moves the kid into the
			// pen the operator named and tags that pen for kids. Doing it here, inside the action
			// write's own transaction, is what makes the two one atomic unit: a failed placement
			// rolls the step back to pending rather than recording it as done with the kid still in
			// the wrong pen. The idempotency gate is the action row itself -- an exact replay
			// returned above, and a completed action refuses a second write -- so this cannot run
			// twice for the same step.
			if updated.HasHook(domain.EngineHookRecordPen) {
				if err := r.applyRecordedNewbornPen(ctx, tx, w, updated, cmd); err != nil {
					return nil, false, err
				}
			}
			// An answer that finishes the LAST reopened step of an applied death (a re-answered
			// photo question after a rework) reopens the verification gate exactly as a video
			// completion does; the initial round still waits for the admin approval transaction.
			if deathApplied && w.TemplateKey == domain.TemplateKeyDeath && domain.DeathStepsComplete(actions) {
				w.AwaitingVerification = true
			}
			// ANSWER-DRIVEN BRANCHES settle in the same transaction as the answer: every step
			// gated on this question that the answer does not satisfy is skipped now, so the
			// card, the next step and the phone list agree the moment the answer lands.
			changed := []domain.WorkflowAction{updated}
			for _, skippedRow := range domain.ResolveAnswerBranches(updated, actions) {
				for k := range actions {
					if actions[k].ActionID == skippedRow.ActionID {
						actions[k] = skippedRow
					}
				}
				changed = append(changed, skippedRow)
			}
			return changed, false, nil
		})
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	return domain.WriteResult(w, actions, target, replay), nil
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
		func(tx pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			idx := findAction(actions, cmd.ActionID)
			if idx < 0 {
				return nil, false, domain.ErrNotFound
			}
			if domain.OperatorActionBlocked(w.TemplateKey, actions[idx], actions) {
				return nil, false, domain.ErrActionOutOfSequence
			}
			if domain.ActionTimeBlocked(actions[idx], cmd.CompletedAt) {
				return nil, false, domain.ErrActionNotYetDue
			}
			if actions[idx].HasHook(domain.EngineHookTagKid) {
				ready, err := r.hasPermanentIdentifier(ctx, cmd.TenantID, w.SubjectGoatID)
				if err != nil {
					return nil, false, err
				}
				if !ready {
					return nil, false, domain.ErrPermanentIdentifierRequired
				}
			}
			// Evidence rework does not repeat the business operation or restart its dependents.
			reshootingBirthStep := domain.ReviewedPerStep(w.TemplateKey) && actions[idx].Status == domain.ActionStatusRework
			updated, isReplay, err := domain.ApplyComplete(actions[idx], cmd)
			if err != nil {
				return nil, false, err
			}
			if !isReplay {
				// Birth is reviewed per step: a recorded clip waits for its own verdict.
				updated = domain.HoldStepForReview(w.TemplateKey, updated)
			}
			actions[idx] = updated
			target = updated
			if isReplay {
				return nil, true, nil
			}
			changed := []domain.WorkflowAction{updated}
			if updated.CompletedAt != nil && !reshootingBirthStep {
				// Dependency-timed steps: any sibling whose schedule waits on THIS step gets its
				// due time now (the ORS round 2 shape, authored in the SOP; legacy rows carry the
				// backfilled after_action_key).
				for i := range actions {
					dep := actions[i]
					afterKey, offset := dep.AfterActionKey, time.Duration(dep.AfterOffsetSeconds)*time.Second
					if dep.TaskType == "" && dep.ActionKey == domain.ActionKeyORSWater2 && afterKey == "" {
						afterKey, offset = domain.ActionKeyORSWater1, 50*time.Minute
					}
					if afterKey == "" || afterKey != updated.ActionKey || dep.Status != domain.ActionStatusPending {
						continue
					}
					due := updated.CompletedAt.Add(offset)
					actions[i].DueAt = &due
					actions[i].RowVersion++
					changed = append(changed, actions[i])
				}
			}
			// A death capture re-shoot replaces the rejected proof in the workflow's capture snapshot
			// in THIS transaction, so the next bundle carries the new proof exactly once.
			if w.TemplateKey == domain.TemplateKeyDeath && updated.HasHook(domain.EngineHookReshootReport) {
				if idx, ok := domain.ReshootMediaIndex(updated); ok && len(updated.ProofRefs) > 0 {
					w.CaptureEvidence = domain.ReplaceCaptureMedia(w.CaptureEvidence, idx, updated.ProofRefs[0])
				}
			}
			// Only re-shoots after an already-applied death return directly to Verify. The initial
			// upload pair waits for the separate admin approval transaction.
			if deathApplied && w.TemplateKey == domain.TemplateKeyDeath && domain.DeathStepsComplete(actions) {
				w.AwaitingVerification = true
			}
			return changed, false, nil
		})
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	return domain.WriteResult(w, actions, target, replay), nil
}

func (r *Repository) hasPermanentIdentifier(ctx context.Context, tenantID, goatID string) (bool, error) {
	var ready bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM goat_identifiers
  WHERE tenant_id = $1::uuid AND goat_id = $2::uuid
    AND identifier_type = 'animal_identifier_1' AND status = 'active'
)`, tenantID, goatID).Scan(&ready)
	return ready, err
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
// It proves EVERY authored operator step of the death SOP is completed with its proof
// (domain.DeathStepsComplete -- the exactly-two-videos lock is retired, 2026-09-16) and atomically
// opens the workflow verification gate inside the SAME transaction that applies the goat exit
// and approval status.
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
	if !domain.DeathStepsComplete(actions) {
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

// CompleteTagActionForGoat records that the permanent RFID prerequisite is satisfied. Despite the
// legacy method name, promotion no longer completes the task: the operator must still submit the
// tag action's mandatory video. The promote transaction remains the sole identifier writer.
func (r *Repository) CompleteTagActionForGoat(ctx context.Context, tenantID, goatID string, completedAt time.Time) error {
	identifier, err := r.permanentIdentifier(ctx, tenantID, goatID)
	if err != nil {
		return err
	}
	lookupCtx, cancel := r.withTimeout(ctx)
	workflowID, err := func() (string, error) {
		defer cancel()
		var id string
		err := r.pool.QueryRow(lookupCtx, `
SELECT workflow_id::text
FROM workflow_instances
WHERE tenant_id = $1::uuid AND subject_goat_id = $2::uuid
  AND template_key = $3 AND state <> 'canceled'
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
		func(tx pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			if w.AwaitingVerification {
				return nil, true, nil
			}
			for i := range actions {
				if !actions[i].HasHook(domain.EngineHookTagKid) {
					continue
				}
				// A delayed identifier event must not change the version of a recording already
				// submitted for review. Its verdict is fenced by that exact version. Only
				// unfinished operator work needs the identifier prerequisite stamped here.
				if actions[i].Status != domain.ActionStatusPending && actions[i].Status != domain.ActionStatusRework {
					continue
				}
				if actions[i].AnswerValue != nil && *actions[i].AnswerValue == identifier {
					return nil, true, nil
				}
				actions[i].AnswerValue = &identifier
				actions[i].RowVersion++
				return []domain.WorkflowAction{actions[i]}, false, nil
			}
			return nil, true, nil
		})
	return err
}

func (r *Repository) permanentIdentifier(ctx context.Context, tenantID, goatID string) (string, error) {
	var identifier string
	err := r.pool.QueryRow(ctx, `
SELECT identifier_value FROM goat_identifiers
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid
  AND identifier_type = 'animal_identifier_1' AND status = 'active'
ORDER BY valid_from DESC LIMIT 1`, tenantID, goatID).Scan(&identifier)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrPermanentIdentifierRequired
	}
	return identifier, err
}

// DeathEvidenceForVerification returns the approved/in-review death workflow addressed by the
// goat.exited event: the instance (capture snapshot, review round) and EVERY step, so the app
// service composes the bundle through the same builder the completion path uses -- not two
// hardcoded action keys and the legacy single proof_ref column (bug 2, 2026-09-16). Bounded by
// the natural key and template size; no history or tenant-wide scan is involved.
func (r *Repository) DeathEvidenceForVerification(ctx context.Context, tenantID, goatID string) (ports.DeathEvidenceReview, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var workflowID string
	err := r.pool.QueryRow(ctx, `
SELECT workflow_id::text
FROM workflow_instances
WHERE tenant_id = $1::uuid AND subject_goat_id = $2::uuid AND template_key = $3
  AND awaiting_verification = true`, tenantID, goatID, domain.TemplateKeyDeath).Scan(&workflowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.DeathEvidenceReview{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.DeathEvidenceReview{}, err
	}
	w, err := scanInstance(r.pool.QueryRow(ctx, instanceSelectColumns, tenantID, workflowID))
	if err != nil {
		return ports.DeathEvidenceReview{}, err
	}
	actions, err := r.listActions(ctx, r.pool, tenantID, workflowID, false)
	if err != nil {
		return ports.DeathEvidenceReview{}, err
	}
	if !domain.DeathStepsComplete(actions) {
		return ports.DeathEvidenceReview{}, domain.ErrNotFound
	}
	out := ports.DeathEvidenceReview{Workflow: w, Actions: actions}
	// The operator is whoever completed the LAST proof-bearing step (seq order), as before.
	for _, a := range actions {
		if a.ActionType == domain.ActionTypeApproval || a.Status != domain.ActionStatusCompleted || len(a.AllProofRefs()) == 0 {
			continue
		}
		if a.CompletedBy != nil && *a.CompletedBy != "" {
			out.OperatorID = *a.CompletedBy
		}
	}
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

	var workflowID, lifecycle string
	err = tx.QueryRow(ctx, `
SELECT wi.workflow_id::text, g.lifecycle_status
FROM workflow_instances wi
JOIN goats g ON g.tenant_id = wi.tenant_id AND g.goat_id = wi.subject_goat_id
WHERE wi.tenant_id = $1::uuid AND wi.subject_goat_id = $2::uuid AND wi.template_key = $3
FOR UPDATE OF wi`, tenantID, goatID, domain.TemplateKeyDeath).Scan(&workflowID, &lifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	// A rejection cancels the STAGED workflow of a living animal. Once a death is applied the
	// workflow is that death's evidence under review; a later-rejected report (a duplicate left
	// pending beside the approved one) must never cancel it (E2E 2026-09-17).
	if lifecycle != "alive" {
		return tx.Commit(ctx)
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
		func(tx pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
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

// BounceDeathVideosForRework sends every proof-bearing step back to 'rework' (proofs cleared so
// a re-shoot is mandatory, the verifier's words kept, answer-only steps untouched --
// domain.ReopenDeathProofSteps) and closes the current workflow verification gate. Idempotent
// on replay.
func (r *Repository) BounceDeathVideosForRework(ctx context.Context, cmd ports.DeathVerdictCommand) error {
	_, _, _, err := r.workflowMutation(ctx, cmd.TenantID, cmd.WorkflowID,
		func(tx pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			if w.TemplateKey != domain.TemplateKeyDeath {
				return nil, false, domain.ErrNotFound
			}
			w.AwaitingVerification = false
			changed := domain.ReopenDeathProofSteps(actions, cmd.Reason)
			// The report's own proofs were in the rejected bundle too: send each back as a
			// re-shoot step (decision 5). Keyed on the verdict, so a redelivery adds nothing.
			recordingKey := cmd.RecordingKey
			if recordingKey == "" {
				recordingKey = "death-round-" + strconv.Itoa(w.RowVersion)
			}
			added, err := insertCaptureReshootSteps(ctx, tx, cmd.TenantID, w.WorkflowID, w.CaptureEvidence, actions, recordingKey, cmd.Reason)
			if err != nil {
				return nil, false, err
			}
			if len(changed) == 0 && len(added) == 0 {
				return nil, true, nil
			}
			return append(changed, added...), false, nil
		})
	// ErrNotFound propagates for the same reason as the approve path: a redelivered rework is a no-op
	// mutation, so this is only a mis-routed ref_id and must not vanish silently.
	return err
}

// ApplyBirthStepVerdict lands one verdict on one recorded birth step. The item's ref_id is the
// action id, so the owning workflow is resolved with ONE indexed lookup on the action's primary
// key and then mutated under the workflow row lock like every other write, so the card fields are
// recomputed in the same transaction (a rejected step re-opens the card; the last approval
// completes it).
func (r *Repository) ApplyBirthStepVerdict(ctx context.Context, cmd ports.BirthStepVerdictCommand) error {
	lookupCtx, cancel := r.withTimeout(ctx)
	defer cancel()
	var workflowID string
	err := r.pool.QueryRow(lookupCtx, `
SELECT workflow_id::text FROM workflow_actions
WHERE tenant_id = $1::uuid AND action_id = $2::uuid`, cmd.TenantID, cmd.ActionID).Scan(&workflowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	_, _, _, err = r.workflowMutation(ctx, cmd.TenantID, workflowID,
		func(_ pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			if !domain.ReviewedPerStep(w.TemplateKey) {
				return nil, false, domain.ErrNotFound
			}
			idx := findAction(actions, cmd.ActionID)
			if idx < 0 {
				return nil, false, domain.ErrNotFound
			}
			updated, changed := domain.ApplyStepVerdict(actions[idx], cmd.RecordingKey, cmd.Approved, cmd.Reason)
			if !changed {
				return nil, true, nil
			}
			actions[idx] = updated
			return []domain.WorkflowAction{updated}, false, nil
		})
	return err
}

// ApplyBirthSignoffApproved serves the RETIRED whole-workflow bundle (see
// domain.VerificationRefTypeBirthSignoff): it closes the explicit gate a pre-cutover item opened.
func (r *Repository) ApplyBirthSignoffApproved(ctx context.Context, cmd ports.DeathVerdictCommand) error {
	_, _, _, err := r.workflowMutation(ctx, cmd.TenantID, cmd.WorkflowID,
		func(_ pgx.Tx, w *domain.WorkflowInstance, _ []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			if w.TemplateKey != domain.TemplateKeyBirthKid && w.TemplateKey != domain.TemplateKeyBirthMother {
				return nil, false, domain.ErrNotFound
			}
			if !w.AwaitingVerification {
				return nil, true, nil
			}
			w.AwaitingVerification = false
			return nil, false, nil
		})
	return err
}

// BounceBirthVideoForRework serves the RETIRED whole-workflow bundle: every recorded clip of the
// track goes back for a re-shoot, and each re-shoot then enters review on its own.
func (r *Repository) BounceBirthVideoForRework(ctx context.Context, cmd ports.DeathVerdictCommand) error {
	_, _, _, err := r.workflowMutation(ctx, cmd.TenantID, cmd.WorkflowID,
		func(tx pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			if w.TemplateKey != domain.TemplateKeyBirthKid && w.TemplateKey != domain.TemplateKeyBirthMother {
				return nil, false, domain.ErrNotFound
			}
			changed := make([]domain.WorkflowAction, 0, len(actions))
			w.AwaitingVerification = false
			for i := range actions {
				if !actions[i].RequiresVideo ||
					(actions[i].Status != domain.ActionStatusCompleted && actions[i].Status != domain.ActionStatusInReview) {
					continue
				}
				actions[i].Status = domain.ActionStatusRework
				if !actions[i].HasHook(domain.EngineHookTagKid) {
					actions[i].AnswerValue = nil
				}
				actions[i].ProofRef = nil
				actions[i].ProofRefs = nil
				actions[i].CompletedBy = nil
				actions[i].CompletedAt = nil
				actions[i].VerificationItemID = nil
				actions[i].IdempotencyKey = nil
				actions[i].RequestFingerprint = nil
				actions[i].RowVersion++
				changed = append(changed, actions[i])
			}
			if len(changed) == 0 {
				return nil, true, nil
			}
			return changed, false, nil
		})
	return err
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func findAction(actions []domain.WorkflowAction, actionID string) int {
	for i := range actions {
		if actions[i].ActionID == actionID {
			return i
		}
	}
	return -1
}

// FetchShedDetails fetches the shed name and partition label for a given shed ID.
// Returns empty strings if the shed is not found or has no active partition.
func (r *Repository) FetchShedDetails(ctx context.Context, tenantID, shedID string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Canonical resolver: oploc owns the query AND the column choice. Do not inline a
	// SELECT here -- writing a bespoke one is how normalized_label ('3') got rendered in
	// place of partition_label ('Part 3').
	loc, err := oploc.ResolveShedLocation(ctx, r.pool.QueryRow(ctx, oploc.ShedScopedLocationSQL,
		tenantID, shedID))

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Unresolvable shed: caller degrades to its location-less label.
			return "", "", nil
		}
		return "", "", fmt.Errorf("tasks: fetch shed details: %w", err)
	}
	return loc.ShedName, loc.PartitionLabel, nil
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

// ---------------------------------------------------------------------------
// Subject-ref workflows (reconcile, shifting) -- SOP-driven questionnaires on a non-goat subject
// ---------------------------------------------------------------------------

const workflowIDBySubjectRefSQL = `
SELECT workflow_id::text FROM workflow_instances
WHERE tenant_id = $1::uuid AND template_key = $2 AND subject_ref_id = $3::uuid`

// WorkflowIDBySubjectRef reads the (tenant, template_key, subject_ref_id) unique row.
func (r *Repository) WorkflowIDBySubjectRef(ctx context.Context, tenantID, templateKey, subjectRefID string) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	var id string
	err := r.pool.QueryRow(ctx, workflowIDBySubjectRefSQL, tenantID, templateKey, subjectRefID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return id, err
}

// ReopenProofStepsForRework sends every completed proof-bearing operator step back to rework
// with its proofs cleared, so the operator re-shoots exactly what the verifier rejected. Answers
// on proof-less question steps are kept. Idempotent: a workflow with nothing to reopen is a no-op.
func (r *Repository) ReopenProofStepsForRework(ctx context.Context, tenantID, workflowID, reason string) error {
	reason = strings.TrimSpace(reason)
	_, _, _, err := r.workflowMutation(ctx, tenantID, workflowID,
		func(_ pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			var changed []domain.WorkflowAction
			w.AwaitingVerification = false
			for i := range actions {
				a := actions[i]
				if a.ActionType == domain.ActionTypeApproval || a.Status != domain.ActionStatusCompleted {
					continue
				}
				if a.ProofMinVideos == 0 && a.ProofMinPhotos == 0 && !a.RequiresVideo {
					continue
				}
				actions[i].Status = domain.ActionStatusRework
				actions[i].ProofRef = nil
				actions[i].ProofRefs = nil
				actions[i].CompletedAt = nil
				actions[i].CompletedBy = nil
				if reason != "" {
					r := reason
					actions[i].ReworkReason = &r
				}
				actions[i].RowVersion++
				changed = append(changed, actions[i])
			}
			if len(changed) == 0 {
				return nil, true, nil
			}
			return changed, false, nil
		})
	return err
}

// insertCaptureReshootStepsSQL writes every re-shoot step of one verdict in ONE statement; the
// natural key (workflow_id, action_key) makes a redelivered verdict insert nothing.
const insertCaptureReshootStepsSQL = `
INSERT INTO workflow_actions (
  tenant_id, workflow_id, action_key, seq, section, action_type, title, detail, requires_video, options, due_at,
  status, task_type, answer_type, engine_hook, proof_min_videos, proof_min_photos, hard_time_gate, wait_for_all,
  requires_keys, after_action_key, after_offset_seconds, rework_reason
)
SELECT $1::uuid, $2::uuid, s.action_key, s.seq, s.section, s.action_type, s.title, s.detail, s.requires_video, NULL, now(),
  s.status, s.task_type, s.answer_type, s.engine_hook, s.proof_min_videos, s.proof_min_photos, false, false,
  '[]'::jsonb, '', 0, s.rework_reason
FROM unnest($3::text[], $4::int[], $5::text[], $6::text[], $7::text[], $8::text[], $9::bool[], $10::text[], $11::text[],
  $12::text[], $13::text[], $14::int[], $15::int[], $16::text[])
  AS s(action_key, seq, section, action_type, title, detail, requires_video, status, task_type, answer_type, engine_hook,
       proof_min_videos, proof_min_photos, rework_reason)
ON CONFLICT (workflow_id, action_key) DO NOTHING
RETURNING action_id::text, action_key`

// insertCaptureReshootSteps appends the re-shoot steps for a rejected capture inside the caller's
// transaction (one multi-row INSERT, ON CONFLICT on the natural key so a redelivered verdict
// inserts nothing). It returns the rows actually inserted; the caller's recompute counts them
// through the actions slice, which this appends to.
func insertCaptureReshootSteps(ctx context.Context, tx pgx.Tx, tenantID, workflowID string, capture authored.Evidence, actions []domain.WorkflowAction, recordingKey, reason string, indexes ...int) ([]domain.WorkflowAction, error) {
	steps := domain.CaptureReshootSteps(capture, actions, recordingKey, reason, indexes...)
	if len(steps) == 0 {
		return nil, nil
	}
	existing := map[string]bool{}
	for _, a := range actions {
		existing[a.ActionKey] = true
	}
	var (
		pending                                          []domain.WorkflowAction
		keys, sections, types, titles, details, statuses []string
		taskTypes, answerTypes, hooks                    []string
		reasons                                          []*string
		seqs, minVideos, minPhotos                       []int32
		requiresVideo                                    []bool
	)
	for _, step := range steps {
		if existing[step.ActionKey] {
			continue
		}
		pending = append(pending, step)
		keys = append(keys, step.ActionKey)
		seqs = append(seqs, int32(step.Seq))
		sections = append(sections, step.Section)
		types = append(types, step.ActionType)
		titles = append(titles, step.Title)
		details = append(details, step.Detail)
		requiresVideo = append(requiresVideo, step.RequiresVideo)
		statuses = append(statuses, step.Status)
		taskTypes = append(taskTypes, step.TaskType)
		answerTypes = append(answerTypes, step.AnswerType)
		hooks = append(hooks, step.EngineHook)
		minVideos = append(minVideos, int32(step.ProofMinVideos))
		minPhotos = append(minPhotos, int32(step.ProofMinPhotos))
		reasons = append(reasons, step.ReworkReason)
	}
	if len(pending) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, insertCaptureReshootStepsSQL, tenantID, workflowID,
		keys, seqs, sections, types, titles, details, requiresVideo, statuses, taskTypes, answerTypes, hooks,
		minVideos, minPhotos, reasons)
	if err != nil {
		return nil, err
	}
	inserted := map[string]string{}
	for rows.Next() {
		var actionID, actionKey string
		if err := rows.Scan(&actionID, &actionKey); err != nil {
			rows.Close()
			return nil, err
		}
		inserted[actionKey] = actionID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var added []domain.WorkflowAction
	for _, step := range pending {
		actionID, ok := inserted[step.ActionKey]
		if !ok {
			continue // a redelivered verdict: the natural key already existed
		}
		step.ActionID, step.TenantID, step.WorkflowID = actionID, tenantID, workflowID
		added = append(added, step)
	}
	return added, nil
}

// AppendCaptureReshootSteps appends a rejected birth report's re-shoot steps and recomputes the
// card in the same transaction (the track reopens so the operator sees the work).
func (r *Repository) AppendCaptureReshootSteps(ctx context.Context, tenantID, workflowID string, capture authored.Evidence, indexes []int, recordingKey, reason string) error {
	_, _, _, err := r.workflowMutation(ctx, tenantID, workflowID,
		func(tx pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			added, err := insertCaptureReshootSteps(ctx, tx, tenantID, workflowID, capture, actions, recordingKey, reason, indexes...)
			if err != nil {
				return nil, false, err
			}
			if len(added) == 0 {
				return nil, true, nil
			}
			return added, false, nil
		})
	return err
}

// BirthWorkflowIDForEvent: the litter's mother track, else its first kid track (one indexed read).
func (r *Repository) BirthWorkflowIDForEvent(ctx context.Context, tenantID, birthEventID string) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	var id string
	err := r.pool.QueryRow(ctx, `
SELECT workflow_id::text FROM workflow_instances
WHERE tenant_id = $1::uuid AND birth_event_id = $2::uuid AND template_key IN ($3, $4) AND state <> 'canceled'
ORDER BY (template_key = $3) DESC, created_at ASC
LIMIT 1`, tenantID, birthEventID, domain.TemplateKeyBirthMother, domain.TemplateKeyBirthKid).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return id, err
}
