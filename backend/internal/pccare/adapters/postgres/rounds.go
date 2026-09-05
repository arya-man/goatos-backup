package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

const (
	pcCareRoundResourceType = "pc_care_round"
	pcCareRoundCreateScope  = "pc_care.round.create"
	pcCareRoundCreated      = "pc_care.round.created"

	pcCareRemovalPenResourceType  = "pc_care_removal_pen"
	pcCareRemovalPenProofAction   = "pc_care.removal_pen.proof_recorded"
	pcCareRemovalPenVerdictAction = "pc_care.removal_pen.verdict_applied"
)

// penPlan is one validated pen of a round create: its identity, the display label the
// removal card and the verifier item will show, and — once the tasks are inserted — the
// task id that became its bucket.
type penPlan struct {
	pen    domain.RoundPen
	key    string
	label  string
	taskID string
}

// CreateRound plans a round, one pen task per named pen, and — when the round asks for it
// — the round-grain feed & water removal card with one evidence row per pen, ALL in one
// transaction (maintainer decision 2026-09-05).
//
// Atomicity here is a safety property, not tidiness: a deworming that shipped without the
// removal it was planned with would run on animals nobody fasted. The same reasoning
// already governs the per-pen pair in 000254 — this is that rule at round grain.
func (r *Repository) CreateRound(ctx context.Context, p ports.CreateRoundParams) (ports.RoundRow, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	plannedDate := p.PlannedBusinessDate.Format("2006-01-02")

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: begin create round tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	plans, err := resolveRoundPens(ctx, tx, p.TenantID, p.ParkID, p.Pens)
	if err != nil {
		return ports.RoundRow{}, err
	}

	// Both operator lists are checked in ONE pair of set-based reads over their union —
	// never a read per operator, and never a read per pen.
	everyone := append(append([]string{}, p.AssigneeUserIDs...), p.RemovalOperatorUserIDs...)
	distinct := distinctIDs(everyone)
	var activeCount int
	if err := tx.QueryRow(ctx, activeMembersCountSQL, p.TenantID, distinct).Scan(&activeCount); err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: verify round assignees: %w", err)
	}
	if activeCount != len(distinct) {
		return ports.RoundRow{}, ports.ErrInvalidArgument
	}
	if err := assertOperatorsScopedToPark(ctx, tx, p.TenantID, p.ParkID, distinct); err != nil {
		return ports.RoundRow{}, err
	}

	// The fingerprint carries the pen SET in a stable order, so replaying one key with a
	// different pen list is an idempotency conflict rather than a silently different round.
	penKeys := make([]string, 0, len(plans))
	for _, plan := range plans {
		penKeys = append(penKeys, plan.key)
	}
	sort.Strings(penKeys)
	fingerprint := requestFingerprint(
		p.Category, p.ParkID, plannedDate,
		strings.Join(penKeys, ","),
		strings.Join(p.AssigneeUserIDs, ","),
		fmt.Sprintf("%t", p.FeedRemovalRequired),
		strings.Join(p.RemovalOperatorUserIDs, ","),
	)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, pcCareRoundCreateScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.RoundRow{}, err
	}
	if !reservation.proceed {
		// Exact replay: return the original round, run no side effects.
		if err := tx.Commit(ctx); err != nil {
			return ports.RoundRow{}, fmt.Errorf("pccare: commit idempotent round replay: %w", err)
		}
		committed = true
		return r.GetRound(ctx, p.TenantID, reservation.resultID, nil, true)
	}

	var roundID string
	if err := tx.QueryRow(ctx, roundInsertSQL,
		p.TenantID, p.Category, p.ParkID, plannedDate, p.IdempotencyKey, p.CreatedBy,
	).Scan(&roundID); err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: insert round: %w", err)
	}

	shedIDs := make([]string, 0, len(plans))
	partitionLabels := make([]string, 0, len(plans))
	for _, plan := range plans {
		shedIDs = append(shedIDs, plan.pen.ShedID)
		partitionLabels = append(partitionLabels, plan.pen.PartitionLabel)
	}

	// ONE set-based insert for every pen bucket. ON CONFLICT DO NOTHING lets the natural
	// key (one live task per category/pen/day) refuse a pen already planned; anything less
	// than the full pen set coming back means the round cannot be planned whole.
	rows, err := tx.Query(ctx, roundTasksInsertSQL,
		p.TenantID, p.Category, p.ParkID, shedIDs, partitionLabels, plannedDate, roundID, p.CreatedBy)
	if err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: insert round pen tasks: %w", err)
	}
	inserted := map[string]string{}
	for rows.Next() {
		var taskID, shedID, partitionLabel string
		if err := rows.Scan(&taskID, &shedID, &partitionLabel); err != nil {
			rows.Close()
			return ports.RoundRow{}, fmt.Errorf("pccare: scan inserted pen task: %w", err)
		}
		inserted[domain.RoundPen{ShedID: shedID, PartitionLabel: partitionLabel}.PenKey()] = taskID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: iterate inserted pen tasks: %w", err)
	}
	if len(inserted) != len(plans) {
		// At least one pen already carries a live task for this category and date. The
		// WHOLE round rolls back rather than planning a partial round the CEO never saw:
		// a silently shortened round is work nobody knows is missing.
		return ports.RoundRow{}, domain.ErrTaskAlreadyPlanned
	}
	taskIDs := make([]string, 0, len(plans))
	for i := range plans {
		plans[i].taskID = inserted[plans[i].key]
		taskIDs = append(taskIDs, plans[i].taskID)
	}

	// The whole crew works every pen, so the assignee rows are one set-based cross product
	// — never a statement per pen or per operator.
	if _, err := tx.Exec(ctx, roundAssigneesInsertSQL, p.TenantID, taskIDs, p.AssigneeUserIDs); err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: insert round assignees: %w", err)
	}

	actorType := strings.TrimSpace(p.ActorType)
	if actorType == "" {
		actorType = "human"
	}
	recorder := audit.NewTxRecorder(tx)
	penLabels := make([]string, 0, len(plans))
	for _, plan := range plans {
		penLabels = append(penLabels, plan.label)
	}
	if err := recorder.Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       pcCareRoundCreated,
		ResourceType: pcCareRoundResourceType,
		ResourceID:   roundID,
		ScopeType:    "park",
		ScopeID:      p.ParkID,
		AfterState: map[string]any{
			"category":              p.Category,
			"park_id":               p.ParkID,
			"planned_business_date": plannedDate,
			"assignee_user_ids":     p.AssigneeUserIDs,
			"pen_labels":            penLabels,
			"task_ids":              taskIDs,
		},
		Metadata: map[string]any{"source": "pc-care-planner"},
		TraceID:  p.TraceID,
	}); err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: write round create audit: %w", err)
	}

	if p.FeedRemovalRequired {
		// The card's id is not carried out of here: GetRound reads it back off
		// gates_round_id, so there is exactly one place that answers "which removal gates
		// this round" and a caller cannot be handed a stale one.
		if err := createRoundRemoval(ctx, tx, recorder, p, roundID, plans, actorType); err != nil {
			return ports.RoundRow{}, err
		}
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, pcCareRoundCreateScope, p.IdempotencyKey, pcCareRoundResourceType, roundID); err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: complete round create idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: commit create round: %w", err)
	}
	committed = true
	return r.GetRound(ctx, p.TenantID, roundID, nil, true)
}

// createRoundRemoval writes the round-grain feed & water removal card and its per-pen
// evidence rows. ONE card (one evening, one crew, one submit, one midnight gate) and one
// feed+water slot pair PER PEN, because a single clip stretched over several pens proves
// nothing and the verifier cannot tell which pen was actually emptied — weighing's own
// correction of 2026-09-03, applied here at the same grain.
func createRoundRemoval(
	ctx context.Context,
	tx pgx.Tx,
	recorder audit.TxRecorder,
	p ports.CreateRoundParams,
	roundID string,
	plans []penPlan,
	actorType string,
) error {
	removalDate := p.PlannedBusinessDate.AddDate(0, 0, -1).Format("2006-01-02")

	var removalTaskID string
	err := tx.QueryRow(ctx, roundRemovalInsertSQL,
		p.TenantID, p.ParkID, removalDate, roundID, p.IdempotencyKey+":fasting", p.CreatedBy,
	).Scan(&removalTaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		// A live removal card already gates this round. The pair cannot be planned whole,
		// so the whole create rolls back rather than shipping a deworming with no gate.
		return domain.ErrTaskAlreadyPlanned
	}
	if err != nil {
		return fmt.Errorf("pccare: insert round removal task: %w", err)
	}

	if _, err := tx.Exec(ctx, removalAssigneesInsertSQL, p.TenantID, removalTaskID, p.RemovalOperatorUserIDs); err != nil {
		return fmt.Errorf("pccare: insert round removal assignees: %w", err)
	}

	gatedTaskIDs := make([]string, 0, len(plans))
	penLabels := make([]string, 0, len(plans))
	for _, plan := range plans {
		gatedTaskIDs = append(gatedTaskIDs, plan.taskID)
		penLabels = append(penLabels, plan.label)
	}
	// One set-based insert for every pen's evidence row.
	if _, err := tx.Exec(ctx, removalPenProofsInsertSQL, p.TenantID, removalTaskID, gatedTaskIDs, penLabels); err != nil {
		return fmt.Errorf("pccare: insert round removal pen proofs: %w", err)
	}

	if err := recorder.Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       pcCareCreatedAction,
		ResourceType: pcCareTaskResourceType,
		ResourceID:   removalTaskID,
		ScopeType:    "park",
		ScopeID:      p.ParkID,
		AfterState: map[string]any{
			"category":              domain.CategoryFeedWaterRemoval,
			"park_id":               p.ParkID,
			"planned_business_date": removalDate,
			"assignee_user_ids":     p.RemovalOperatorUserIDs,
			"gates_round_id":        roundID,
			"pen_labels":            penLabels,
		},
		Metadata: map[string]any{"source": "pc-care-planner"},
		TraceID:  p.TraceID,
	}); err != nil {
		return fmt.Errorf("pccare: write round removal create audit: %w", err)
	}
	return nil
}

// resolveRoundPens validates every pen of a round against the park's shed and pen catalog
// and resolves each one's display label, in TWO set-based reads no matter how many pens
// the round names. A read per pen would be the n-plus-one fan-out the scale guard bans,
// and at 100 pens it is 200 serial round trips inside an open write transaction.
func resolveRoundPens(ctx context.Context, tx pgx.Tx, tenantID, parkID string, pens []domain.RoundPen) ([]penPlan, error) {
	shedIDs := make([]string, 0, len(pens))
	seenShed := map[string]struct{}{}
	for _, pen := range pens {
		if _, dup := seenShed[pen.ShedID]; dup {
			continue
		}
		seenShed[pen.ShedID] = struct{}{}
		shedIDs = append(shedIDs, pen.ShedID)
	}

	shedNames := map[string]string{}
	rows, err := tx.Query(ctx, roundShedsInParkSQL, tenantID, parkID, shedIDs)
	if err != nil {
		return nil, fmt.Errorf("pccare: resolve round sheds in park: %w", err)
	}
	for rows.Next() {
		var shedID, name string
		if err := rows.Scan(&shedID, &name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("pccare: scan round shed: %w", err)
		}
		shedNames[shedID] = name
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pccare: iterate round sheds: %w", err)
	}
	if len(shedNames) != len(shedIDs) {
		return nil, ports.ErrShedNotInPark
	}

	// The pen catalog for every named shed in one read. A shed with no catalog row is an
	// undivided shed and may be named only WITHOUT a pen label; a partitioned shed may be
	// named only WITH one that matches the catalog.
	catalog := map[string]map[string]struct{}{}
	prows, err := tx.Query(ctx, roundShedPartitionsSQL, tenantID, shedIDs)
	if err != nil {
		return nil, fmt.Errorf("pccare: resolve round pen catalog: %w", err)
	}
	for prows.Next() {
		var shedID, label string
		if err := prows.Scan(&shedID, &label); err != nil {
			prows.Close()
			return nil, fmt.Errorf("pccare: scan round pen: %w", err)
		}
		if catalog[shedID] == nil {
			catalog[shedID] = map[string]struct{}{}
		}
		catalog[shedID][domain.PartitionMatchKey(label)] = struct{}{}
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return nil, fmt.Errorf("pccare: iterate round pen catalog: %w", err)
	}

	plans := make([]penPlan, 0, len(pens))
	for _, pen := range pens {
		requested := domain.PartitionMatchKey(pen.PartitionLabel)
		pensOfShed, partitioned := catalog[pen.ShedID]
		if partitioned {
			if requested == "whole" {
				return nil, ports.ErrInvalidPartition
			}
			if _, ok := pensOfShed[requested]; !ok {
				return nil, ports.ErrInvalidPartition
			}
		} else if requested != "whole" {
			return nil, ports.ErrInvalidPartition
		}
		plans = append(plans, penPlan{
			pen:   pen,
			key:   pen.PenKey(),
			label: oploc.OperationalLocation{ShedName: shedNames[pen.ShedID], PartitionLabel: pen.PartitionLabel}.Display(),
		})
	}
	return plans, nil
}

const roundInsertSQL = `
INSERT INTO pc_care_rounds (tenant_id, category, park_id, planned_business_date, idempotency_key, created_by)
VALUES ($1::uuid, $2, $3::uuid, $4::date, $5, $6::uuid)
RETURNING round_id::text`

// roundTasksInsertSQL inserts every pen bucket of a round in ONE statement. The two text
// arrays are positional partners of one pen list, built together in the same loop, so an
// index can never pair one pen's shed with another pen's label.
const roundTasksInsertSQL = `
INSERT INTO pc_care_tasks (
  tenant_id, category, park_id, shed_id, partition_label,
  planned_business_date, due_business_date, round_id, idempotency_key, created_by
)
SELECT
  $1::uuid, $2, $3::uuid, pen.shed_id, nullif(btrim(pen.partition_label), ''),
  $6::date, $6::date, $7::uuid,
  $7::text || ':' || pen.shed_id::text || ':' || coalesce(nullif(btrim(pen.partition_label), ''), 'whole'),
  $8::uuid
FROM unnest($4::uuid[], $5::text[]) AS pen(shed_id, partition_label)
ON CONFLICT (tenant_id, category, park_id, shed_id, partition_key, planned_business_date)
  WHERE work_state <> 'canceled'
DO NOTHING
RETURNING task_id::text, shed_id::text, coalesce(partition_label, '')`

// roundAssigneesInsertSQL is the crew x pens cross product in one statement.
const roundAssigneesInsertSQL = `
INSERT INTO pc_care_task_assignees (tenant_id, task_id, operator_user_id)
SELECT $1::uuid, t.task_id, o.operator_user_id
FROM unnest($2::uuid[]) AS t(task_id)
CROSS JOIN unnest($3::uuid[]) AS o(operator_user_id)`

// roundRemovalInsertSQL creates the round-grain removal card: no shed (it covers several
// pens; the pens it covers are named by pc_care_removal_pen_proofs), gates_round_id set.
const roundRemovalInsertSQL = `
INSERT INTO pc_care_tasks (
  tenant_id, category, park_id, planned_business_date, due_business_date,
  gates_round_id, idempotency_key, created_by
) VALUES (
  $1::uuid, 'feed_water_removal', $2::uuid, $3::date, $3::date,
  $4::uuid, $5, $6::uuid
)
ON CONFLICT (tenant_id, gates_round_id) WHERE gates_round_id IS NOT NULL
DO NOTHING
RETURNING task_id::text`

const removalPenProofsInsertSQL = `
INSERT INTO pc_care_removal_pen_proofs (tenant_id, removal_task_id, gated_task_id, pen_label)
SELECT $1::uuid, $2::uuid, pen.gated_task_id, pen.pen_label
FROM unnest($3::uuid[], $4::text[]) AS pen(gated_task_id, pen_label)`

const roundShedsInParkSQL = `
SELECT shed.location_id::text, shed.name
FROM locations shed
WHERE shed.tenant_id = $1::uuid
  AND shed.parent_location_id = $2::uuid
  AND shed.location_type = 'shed'
  AND shed.status = 'active'
  AND shed.retired_at IS NULL
  AND shed.location_id = ANY($3::uuid[])`

const roundShedPartitionsSQL = `
SELECT shed_id::text, coalesce(nullif(btrim(partition_label), ''), 'whole')
FROM shed_partitions
WHERE tenant_id = $1::uuid
  AND shed_id = ANY($2::uuid[])
  AND status = 'active'
  AND coalesce(nullif(btrim(partition_label), ''), 'whole') <> 'whole'`

// GetRound reads one round with every pen bucket it holds. The pen rows are the SAME
// ports.TaskRow every other PC Care read serves, scanned through the same column list, so a
// pen cannot describe itself one way inside a round and another way outside it.
//
// projection-review: membership=pc_care_tasks WHERE round_id = the round; group_key=task_id
// (the pen rows ARE the grain); join_cardinality=pc_care_rounds 1:1 by PK, locations 1:1 by
// PK, assignees/animals pre-aggregated 1:0..1 laterals; the removal card is a single 1:0..1
// lookup on the partial unique index (tenant_id, gates_round_id); pagination=bounded by
// MaxPensPerRound, never by herd size; scope=tenant_id + park clamp.
func (r *Repository) GetRound(ctx context.Context, tenantID, roundID string, authorizedParkIDs []string, tenantWide bool) (ports.RoundRow, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	var round ports.RoundRow
	// scale-guard:ignore: single-round read by primary key with one 1:0..1 removal lookup on pc_care_tasks_gates_round_uq.
	err := r.pool.QueryRow(ctx, `
SELECT r.round_id::text, r.category, r.park_id::text, park.name,
       r.planned_business_date::text, r.created_at,
       coalesce(removal.task_id::text, ''), coalesce(removal.status, '')
FROM pc_care_rounds r
JOIN locations park ON park.tenant_id = r.tenant_id AND park.location_id = r.park_id
LEFT JOIN pc_care_tasks removal
  ON removal.tenant_id = r.tenant_id AND removal.gates_round_id = r.round_id
WHERE r.tenant_id = $1::uuid AND r.round_id = $2::uuid
  AND ($3::bool OR r.park_id = ANY($4::uuid[]))`,
		tenantID, roundID, tenantWide, authorizedParkIDs,
	).Scan(&round.RoundID, &round.Category, &round.ParkID, &round.ParkName,
		&round.PlannedBusinessDate, &round.CreatedAt, &round.RemovalTaskID, &round.RemovalStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.RoundRow{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: get round: %w", err)
	}

	// scale-guard:ignore: one round's pen buckets, covered by pc_care_tasks_round_idx (tenant_id, round_id, task_id) and bounded by domain.MaxPensPerRound.
	rows, err := r.pool.Query(ctx, `
SELECT`+taskSelectColumns+taskFromJoins+`
WHERE t.tenant_id = $1::uuid AND t.round_id = $2::uuid
ORDER BY shed.name, t.partition_label NULLS FIRST, t.task_id`,
		tenantID, roundID)
	if err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: list round pens: %w", err)
	}
	defer rows.Close()
	statuses := make([]string, 0, 8)
	for rows.Next() {
		pen, err := scanTaskRow(rows)
		if err != nil {
			return ports.RoundRow{}, fmt.Errorf("pccare: scan round pen: %w", err)
		}
		round.Pens = append(round.Pens, pen)
		statuses = append(statuses, pen.Status)
	}
	if err := rows.Err(); err != nil {
		return ports.RoundRow{}, fmt.Errorf("pccare: iterate round pens: %w", err)
	}
	round.PenCount = int32(len(round.Pens))
	// The card's ONE status is composed here, backend-side. Clients render it and never
	// re-derive their own from the pen list; two surfaces deriving it independently is how
	// they come to disagree about whether a round is finished.
	round.Status = domain.RoundStatusRollup(statuses)
	return round, nil
}

// ListRemovalPenProofs reads a removal card's per-pen evidence rows in pen-label order.
func (r *Repository) ListRemovalPenProofs(ctx context.Context, tenantID, removalTaskID string) ([]ports.RemovalPenProofRow, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	// scale-guard:ignore: one removal card's evidence rows, covered by pc_care_removal_pen_proofs_task_idx and bounded by domain.MaxPensPerRound.
	rows, err := r.pool.Query(ctx, `
SELECT removal_pen_id::text, gated_task_id::text, pen_label,
       coalesce(feed_proof_ref, ''), coalesce(water_proof_ref, ''),
       status, coalesce(rework_reason, ''), row_version
FROM pc_care_removal_pen_proofs
WHERE tenant_id = $1::uuid AND removal_task_id = $2::uuid
ORDER BY pen_label, removal_pen_id`, tenantID, removalTaskID)
	if err != nil {
		return nil, fmt.Errorf("pccare: list removal pen proofs: %w", err)
	}
	defer rows.Close()
	out := make([]ports.RemovalPenProofRow, 0, 8)
	for rows.Next() {
		var p ports.RemovalPenProofRow
		if err := rows.Scan(&p.RemovalPenID, &p.GatedTaskID, &p.PenLabel,
			&p.FeedProofRef, &p.WaterProofRef, &p.Status, &p.ReworkReason, &p.RowVersion); err != nil {
			return nil, fmt.Errorf("pccare: scan removal pen proof: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pccare: iterate removal pen proofs: %w", err)
	}
	return out, nil
}

// RegisterRemovalPenProof stores ONE pen's feed or water video on a round-grain removal
// card. The pen is addressed by the WORK TASK it gates, and the row must already exist —
// it was created with the round — so a client cannot invent a pen the round does not
// cover, and cannot aim one round's evidence at another round's pen.
func (r *Repository) RegisterRemovalPenProof(ctx context.Context, p ports.RegisterRemovalPenProofParams) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	slot := strings.TrimSpace(p.SlotKey)
	if slot != domain.SlotFeedVideo && slot != domain.SlotWaterVideo {
		return domain.ErrInvalidSlotForCategory
	}
	if strings.TrimSpace(p.ProofRef) == "" {
		return ports.ErrProofRequired
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("pccare: begin removal pen proof tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// The card must be a round-grain removal that is still open for capture. Reusing the
	// shared capture lock keeps "which states accept a video" in ONE place: a submitted card
	// refuses a late clip here exactly as a submitted task does.
	category, err := lockTaskForCapture(ctx, tx, p.TenantID, p.RemovalTaskID)
	if err != nil {
		return err
	}
	if category != domain.CategoryFeedWaterRemoval {
		return domain.ErrInvalidSlotForCategory
	}

	fingerprint := requestFingerprint(p.RemovalTaskID, p.GatedTaskID, slot, p.ProofRef)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, pcCareSlotIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("pccare: commit idempotent removal pen proof replay: %w", err)
		}
		committed = true
		return nil
	}

	column := "feed_proof_ref"
	if slot == domain.SlotWaterVideo {
		column = "water_proof_ref"
	}
	// A re-shoot REPLACES the pen's clip for that slot rather than adding a second one: the
	// verifier judges the video the operator stands behind, and the pair CHECK on the table
	// admits only one of each.
	var removalPenID string
	err = tx.QueryRow(ctx, `
UPDATE pc_care_removal_pen_proofs
SET `+column+` = $4,
    status = 'open',
    rework_reason = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND removal_task_id = $2::uuid AND gated_task_id = $3::uuid
RETURNING removal_pen_id::text`,
		p.TenantID, p.RemovalTaskID, p.GatedTaskID, strings.TrimSpace(p.ProofRef)).Scan(&removalPenID)
	if errors.Is(err, pgx.ErrNoRows) {
		// No evidence row means this pen is not part of the gated round. Refusing beats
		// inserting one: an invented row would put a pen in the verifier's queue that nobody
		// planned and that gates no work.
		return domain.ErrRemovalPenNotInRound
	}
	if err != nil {
		return fmt.Errorf("pccare: store removal pen proof: %w", err)
	}

	actorType := strings.TrimSpace(p.ActorType)
	if actorType == "" {
		actorType = "operator"
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       pcCareRemovalPenProofAction,
		ResourceType: pcCareRemovalPenResourceType,
		ResourceID:   removalPenID,
		ScopeType:    "task",
		ScopeID:      p.RemovalTaskID,
		AfterState: map[string]any{
			"removal_task_id": p.RemovalTaskID,
			"gated_task_id":   p.GatedTaskID,
			"slot_key":        slot,
			"captured_by":     p.CapturedBy,
		},
		Metadata: map[string]any{"source": "pc-care-removal-capture"},
		TraceID:  p.TraceID,
	}); err != nil {
		return fmt.Errorf("pccare: write removal pen proof audit: %w", err)
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, pcCareSlotIdemScope, p.IdempotencyKey, pcCareRemovalPenResourceType, removalPenID); err != nil {
		return fmt.Errorf("pccare: complete removal pen proof idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pccare: commit removal pen proof: %w", err)
	}
	committed = true
	return nil
}

// removalPenSubmitRefs is the round-grain removal's submit readiness check, run inside the
// submit transaction. EVERY pen must carry BOTH videos: a card submitted with one pen
// unfilmed would tell the midnight gate that pen's animals were fasted when they were not.
func removalPenSubmitRefs(ctx context.Context, tx pgx.Tx, tenantID, removalTaskID string) ([]ports.RemovalPenRef, []ports.LabeledRef, error) {
	rows, err := tx.Query(ctx, `
SELECT removal_pen_id::text, gated_task_id::text, pen_label,
       coalesce(feed_proof_ref, ''), coalesce(water_proof_ref, ''), row_version
FROM pc_care_removal_pen_proofs
WHERE tenant_id = $1::uuid AND removal_task_id = $2::uuid
ORDER BY pen_label, removal_pen_id
FOR UPDATE`, tenantID, removalTaskID)
	if err != nil {
		return nil, nil, fmt.Errorf("pccare: read removal pen proofs for submit: %w", err)
	}
	defer rows.Close()
	pens := make([]ports.RemovalPenRef, 0, 8)
	media := make([]ports.LabeledRef, 0, 16)
	for rows.Next() {
		var pen ports.RemovalPenRef
		if err := rows.Scan(&pen.RemovalPenID, &pen.GatedTaskID, &pen.PenLabel,
			&pen.FeedProofRef, &pen.WaterProofRef, &pen.RowVersion); err != nil {
			return nil, nil, fmt.Errorf("pccare: scan removal pen proof for submit: %w", err)
		}
		if strings.TrimSpace(pen.FeedProofRef) == "" || strings.TrimSpace(pen.WaterProofRef) == "" {
			return nil, nil, domain.ErrRemovalProofIncomplete
		}
		pens = append(pens, pen)
		// The parent card's media set is still every clip, so a reader of the card sees the
		// whole evening; the per-pen split is what the verifier's items are built from.
		media = append(media,
			ports.LabeledRef{ProofRef: pen.FeedProofRef, Label: pen.PenLabel + " · Feed removal video"},
			ports.LabeledRef{ProofRef: pen.WaterProofRef, Label: pen.PenLabel + " · Water removal video"},
		)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("pccare: iterate removal pen proofs for submit: %w", err)
	}
	if len(pens) == 0 {
		// A round-grain removal card always has pens; none means the round was planned wrong.
		return nil, nil, domain.ErrRemovalProofIncomplete
	}
	if _, err := tx.Exec(ctx, `
UPDATE pc_care_removal_pen_proofs
SET status = 'pending_verification', updated_at = now()
WHERE tenant_id = $1::uuid AND removal_task_id = $2::uuid`, tenantID, removalTaskID); err != nil {
		return nil, nil, fmt.Errorf("pccare: flip removal pens pending: %w", err)
	}
	return pens, media, nil
}

// ApplyVerifiedRemovalPen flips ONE pen's evidence to completed and rolls the parent card up.
// The parent becomes completed only when EVERY pen is: one approved pen is not an approved
// evening.
func (r *Repository) ApplyVerifiedRemovalPen(ctx context.Context, p ports.ApplyRemovalPenVerdictParams) (bool, error) {
	return r.applyRemovalPenVerdict(ctx, p, domain.StatusCompleted)
}

// BounceRemovalPenForRework flips ONE pen's evidence to rework with the verifier's reason and
// puts the parent card back in rework so the crew can re-shoot THAT pen. The other pens keep
// their own verdicts — that is the point of per-pen review.
func (r *Repository) BounceRemovalPenForRework(ctx context.Context, p ports.ApplyRemovalPenVerdictParams) (bool, error) {
	return r.applyRemovalPenVerdict(ctx, p, domain.StatusRework)
}

func (r *Repository) applyRemovalPenVerdict(ctx context.Context, p ports.ApplyRemovalPenVerdictParams, target string) (bool, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("pccare: begin removal pen verdict tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// Only a pen still awaiting a verdict moves. A replayed or late verdict finds no row and
	// is reported as "nothing happened" rather than overwriting a decision already recorded.
	var removalTaskID string
	err = tx.QueryRow(ctx, `
UPDATE pc_care_removal_pen_proofs
SET status = $3,
    rework_reason = nullif($4::text, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND removal_pen_id = $2::uuid AND status = 'pending_verification'
RETURNING removal_task_id::text`,
		p.TenantID, p.RemovalPenID, target, strings.TrimSpace(p.Reason)).Scan(&removalTaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("pccare: apply removal pen verdict: %w", err)
	}

	// The parent roll-up, computed from the pens themselves in the same transaction so the
	// card can never disagree with the rows it is a summary of. A card that is still holding
	// an undecided pen stays pending; work_state follows only a COMPLETED card, because a
	// bounced pen is work the crew still owes.
	if _, err := tx.Exec(ctx, `
WITH rollup AS (
  SELECT count(*) FILTER (WHERE status = 'completed') AS completed,
         count(*) FILTER (WHERE status = 'rework')    AS rework,
         count(*)                                     AS total
  FROM pc_care_removal_pen_proofs
  WHERE tenant_id = $1::uuid AND removal_task_id = $2::uuid
)
UPDATE pc_care_tasks t
SET status = CASE
      WHEN rollup.rework > 0             THEN 'rework'
      WHEN rollup.completed = rollup.total THEN 'completed'
      ELSE 'pending_verification'
    END,
    work_state = CASE
      WHEN rollup.rework = 0 AND rollup.completed = rollup.total THEN 'completed'
      ELSE t.work_state
    END,
    terminal_at = CASE
      WHEN rollup.rework = 0 AND rollup.completed = rollup.total THEN now()
      ELSE t.terminal_at
    END,
    verified_by = CASE
      WHEN rollup.rework = 0 AND rollup.completed = rollup.total THEN nullif($3::text, '')::uuid
      ELSE t.verified_by
    END,
    verified_at = CASE
      WHEN rollup.rework = 0 AND rollup.completed = rollup.total THEN now()
      ELSE t.verified_at
    END,
    rework_reason = CASE WHEN rollup.rework > 0 THEN nullif($4::text, '') ELSE NULL END,
    updated_at = now(),
    row_version = t.row_version + 1
FROM rollup
WHERE t.tenant_id = $1::uuid AND t.task_id = $2::uuid`,
		p.TenantID, removalTaskID, strings.TrimSpace(p.VerifiedBy), strings.TrimSpace(p.Reason)); err != nil {
		return false, fmt.Errorf("pccare: roll up removal card: %w", err)
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      strings.TrimSpace(p.VerifiedBy),
		ActorType:    "human",
		Action:       pcCareRemovalPenVerdictAction,
		ResourceType: pcCareRemovalPenResourceType,
		ResourceID:   p.RemovalPenID,
		ScopeType:    "task",
		ScopeID:      removalTaskID,
		AfterState: map[string]any{
			"status":        target,
			"rework_reason": strings.TrimSpace(p.Reason),
		},
		Metadata: map[string]any{"source": "pc-care-removal-verdict"},
		TraceID:  p.TraceID,
	}); err != nil {
		return false, fmt.Errorf("pccare: write removal pen verdict audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("pccare: commit removal pen verdict: %w", err)
	}
	committed = true
	return true, nil
}
