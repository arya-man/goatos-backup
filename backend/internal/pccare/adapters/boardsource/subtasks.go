package boardsource

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	pcdomain "github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Subtasks of a PC Care task, for the board's issue view: one per ANIMAL scanned on the task
// (pc_care_task_animals), named by the scanned tag verbatim, with the chain scan -> proof ->
// submit -> verify. A task with no scan yet -- or a park-grain task that never carries
// animals (vaccine inventory, feed & water removal) -- drills into itself as one subtask,
// so a live row never shows an empty list. Reads pc_care_tasks and pc_care_task_animals
// plus workforce_members for the scanner's name.
//
// READ-ONLY and REPORTING-ONLY: nothing here gates a scan, a submit, a verdict or a close.

// pccareSubtaskRankSQL is the SQL twin of domain.RankFor, stated once.
//
//	task in rework (the verifier sent it back)          -> 0 needs attention
//	task delayed past its plan and not yet submitted    -> 0 needs attention
//	no scan yet (the task itself)                       -> 1 to do
//	scanned, task still open                            -> 2 in progress
//	task submitted, awaiting the verdict                -> 3 in review
//	task completed or closed                            -> 4 done
const pccareSubtaskRankSQL = `CASE
  WHEN u.task_status = 'rework' THEN 0
  WHEN u.task_status = 'open' AND u.work_state = 'delayed' THEN 0
  WHEN u.task_status IN ('completed') OR u.work_state = 'closed' THEN 4
  WHEN u.task_status = 'pending_verification' THEN 3
  WHEN u.kind = 'task' THEN 1
  ELSE 2
END`

// projection-review: membership=the ONE pc_care_tasks row named by (tenant_id, park_id, due_business_date, task_id) with canceled excluded, then every pc_care_task_animals row of that task (pc_care_task_animals_task_idx on tenant_id, task_id) or one synthetic unit when the task has none; group_key=(tenant_id, unit id) where the unit id is the animal row id or the task id for the synthetic unit, so one scanned animal is one subtask; join_cardinality=the two UNION ALL arms are exclusive on the NOT EXISTS so a task contributes exactly its animals or exactly one unit, and workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1), so nothing fans an animal out and the window total counts each unit once; pagination=keyset on (rank, unit id) ASC after ($5, $6) with LIMIT $7 and the whole count carried by count(*) OVER () computed before the keyset cut; scope=tenant_id, park_id, due_business_date and task_id, the same predicate the row read binds on pc_care_tasks.
const pccareSubtasksSQL = `
WITH task AS (
  SELECT t.task_id, t.category, t.status AS task_status, t.work_state, COALESCE(t.rework_reason, '') AS rework_reason,
         t.required_slot_keys
  FROM pc_care_tasks t
  WHERE t.tenant_id = $1::uuid
    AND t.park_id = $2::uuid
    AND t.due_business_date = $3::date
    AND t.task_id = $4::uuid
    AND t.work_state <> 'canceled'
),
units AS (
  SELECT 'animal'::text AS kind, an.animal_row_id::text AS unit_id, an.scanned_identifier AS name,
         -- FAIL CLOSED: the has-all-keys test is vacuously true against an empty list (source.go).
         an.scanned_at, (cardinality(k.required_slot_keys) > 0 AND an.sop_proofs ?& k.required_slot_keys) AS slots_done,
         (SELECT count(*)::int FROM unnest(k.required_slot_keys) rk WHERE an.sop_proofs ? rk) AS slots_filled,
         cardinality(k.required_slot_keys) AS slots_required,
         (an.submitted_at IS NOT NULL) AS animal_submitted, an.scanned_by AS actor_id,
         k.category, k.task_status, k.work_state, k.rework_reason
  FROM task k
  JOIN pc_care_task_animals an ON an.tenant_id = $1::uuid AND an.task_id = k.task_id
  UNION ALL
  SELECT 'task', k.task_id::text, '', NULL, false, 0, cardinality(k.required_slot_keys), false, NULL,
         k.category, k.task_status, k.work_state, k.rework_reason
  FROM task k
  WHERE NOT EXISTS (SELECT 1 FROM pc_care_task_animals an WHERE an.tenant_id = $1::uuid AND an.task_id = k.task_id)
),
ranked AS (
  SELECT u.*, ` + pccareSubtaskRankSQL + ` AS rank, count(*) OVER () AS total
  FROM units u
)
SELECT r.kind, r.unit_id, r.name, r.scanned_at, r.slots_done, r.slots_filled, r.slots_required, r.animal_submitted,
       r.category, r.task_status, r.work_state, r.rework_reason, r.rank, r.total,
       COALESCE(r.actor_id::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM ranked r
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = r.actor_id AND m.status = 'active'
WHERE (r.rank, r.unit_id) > ($5::int, $6::text)
ORDER BY r.rank, r.unit_id
LIMIT $7`

// ListSubtasks implements ports.SubtaskSource.
func (s *Source) ListSubtasks(ctx context.Context, q ports.SubtaskQuery) (domain.SubtaskPage, error) {
	afterRank, afterID, err := domain.ParseSubtaskKey(q.AfterKey)
	if err != nil {
		return domain.SubtaskPage{}, err
	}
	if q.AfterKey == "" {
		afterRank = -1
	}
	limit := domain.BoundSubtaskLimit(q.Limit)
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, pccareSubtasksSQL, q.TenantID, q.ParkID, q.BusinessDate, q.SourceID, afterRank, afterID, limit+1)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("pccare boardsource subtasks: %w", err)
	}
	defer rows.Close()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	for rows.Next() {
		st, total, err := scanPCCareSubtask(rows)
		if err != nil {
			return domain.SubtaskPage{}, err
		}
		page.Total = total
		if len(page.Subtasks) == limit {
			page.NextCursor = page.Subtasks[limit-1].Key
			break
		}
		page.Subtasks = append(page.Subtasks, st)
	}
	if err := rows.Err(); err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("pccare boardsource subtasks rows: %w", err)
	}
	return page, nil
}

func scanPCCareSubtask(rows pgx.Rows) (domain.Subtask, int, error) {
	var (
		kind, unitID, name                         string
		scannedAt                                  *time.Time
		slotsDone, animalSubmitted                 bool
		slotsFilled, slotsRequired, rank, total    int
		category, taskStatus, workState, reworkMsg string
		ownerUserID, ownerMemberID, ownerName      string
	)
	if err := rows.Scan(&kind, &unitID, &name, &scannedAt, &slotsDone, &slotsFilled, &slotsRequired, &animalSubmitted,
		&category, &taskStatus, &workState, &reworkMsg, &rank, &total, &ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Subtask{}, 0, fmt.Errorf("pccare boardsource subtask scan: %w", err)
	}
	scan := domain.Step{Name: "Scan", State: domain.StepTodo}
	proof := domain.Step{Name: "Proof", State: domain.StepLocked}
	submit := domain.Step{Name: "Submit", State: domain.StepLocked}
	verify := domain.Step{Name: "Verify", State: domain.StepLocked}
	subtitle := ""
	if kind == "animal" {
		scan.State = domain.StepDone
		if scannedAt != nil {
			scan.Detail = "Scanned " + scannedAt.In(biztime.DefaultLocation()).Format("15:04")
		}
		// Proof readiness is the SAME rule submit applies: every compulsory slot of the task's
		// pinned card (PC CARE SOP, 2026-09-22) -- one capture for the 2-second jobs, the
		// before / while / after triple for the trimming jobs, whatever the farm authored since.
		switch {
		case slotsDone:
			proof.State = domain.StepDone
		case slotsFilled > 0 && slotsRequired > 1:
			proof.State, proof.Detail = domain.StepInProgress, fmt.Sprintf("%d of %d captures", slotsFilled, slotsRequired)
		default:
			proof.State = domain.StepTodo
		}
		submit.State = domain.StepTodo
		if proof.State != domain.StepDone {
			submit.State = domain.StepLocked
		}
		if name == "" {
			name = "Unknown tag"
		}
	} else {
		name = pcdomain.CategoryLabel(category)
		subtitle = "No animal scanned yet"
	}
	submitted := animalSubmitted || taskStatus == "pending_verification" || taskStatus == "completed" || taskStatus == "rework" || workState == "closed"
	state := domain.WorkStateDue
	if kind == "animal" {
		state = domain.WorkStateInProgress
	}
	attention := false
	if submitted {
		if kind != "animal" {
			scan.State, proof.State = domain.StepDone, domain.StepDone
		}
		proof.State = domain.StepDone
		submit.State = domain.StepDone
	}
	switch {
	case taskStatus == "rework":
		verify.State, verify.Detail = domain.StepRework, reworkMsg
		state, attention = domain.WorkStateRejected, true
	case taskStatus == "completed" || workState == "closed":
		verify.State = domain.StepDone
		state = domain.WorkStateCompleted
	case taskStatus == "pending_verification":
		verify.State = domain.StepInReview
		state = domain.WorkStateVerificationPending
	case workState == "delayed":
		state, attention = domain.WorkStateOverdue, true
	}
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, unitID), Name: name, Subtitle: subtitle,
		WorkState: state, NeedsAttention: attention,
		Owner: domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName},
		Steps: []domain.Step{scan, proof, submit, verify},
	}.Finalize(), total, nil
}
