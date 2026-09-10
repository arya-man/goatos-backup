package boardsource

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Subtasks of a weighing work item, for the board's issue view.
//
// An INDIVIDUAL bucket's subtasks are its scanned animals: one per weighing_observations row,
// the tag string VERBATIM (weighing knows a scanned string and a weight and never asks what
// animal that is), the weight, and the chain scan -> submit -> verify -> close. A WHOLE-PEN
// bucket's subtask is the pen itself: its one open weighing_shed_observations row, or -- when
// nothing has been weighed yet -- the pen as one to-do, so a live row never drills into an
// empty list.
//
// READ-ONLY and REPORTING-ONLY: nothing here gates a scan, a submit, a verdict or a close.
// Reads weighing_work_items, weighing_campaign_sheds, weighing_observations and
// weighing_shed_observations, plus workforce_members for the recorder's name.

// subtaskRankSQL is the SQL twin of domain.RankFor, stated once: needs attention (rework)
// first, then to do, in progress, in review, done.
//
//	rework                                        -> 0 needs attention
//	nothing weighed yet (the whole-pen to-do)     -> 1 to do
//	pending, bucket still capturing               -> 2 in progress
//	pending, submitted (bucket completed/closed)  -> 3 in review
//	verified                                      -> 4 done
const subtaskRankSQL = `CASE
  WHEN u.verification_status = 'rework' THEN 0
  WHEN u.kind = 'pen_todo' THEN 1
  WHEN u.verification_status = 'verified' THEN 4
  WHEN u.submitted OR u.bucket_status IN ('completed', 'closed') THEN 3
  ELSE 2
END`

// projection-review: membership=the ONE weighing_work_items row named by (tenant_id, park_id, due_business_date, work_item_id) with canceled excluded, then for an individual bucket every weighing_observations row of its campaign_shed_id and for a whole-pen bucket its single open weighing_shed_observations row (withdrawn_at IS NULL, unique per bucket by weighing_shed_observations_one_open_scope_uidx) or one synthetic to-do when there is none; group_key=(tenant_id, unit id) where the unit id is the observation id or the shed observation id or the campaign_shed_id for the synthetic to-do, so one unit is one subtask; join_cardinality=weighing_campaign_sheds on its primary key (1:1), the three UNION ALL arms are mutually exclusive on weighing_category and the NOT EXISTS so a bucket contributes exactly one arm, and workforce_members filtered to status='active' whose (tenant_id,user_id) is unique by the partial active index (at most 1), so nothing fans a unit out and the window total counts each unit once; pagination=keyset on (rank, unit id) ASC after ($5, $6) with LIMIT $7 and the whole count carried by count(*) OVER () computed before the keyset cut; scope=tenant_id, park_id, due_business_date and work_item_id, the same predicate the row read binds on weighing_work_items.
const subtasksSQL = `
WITH item AS (
  SELECT w.work_item_id, w.campaign_shed_id, w.weighing_category, b.status AS bucket_status
  FROM weighing_work_items w
  JOIN weighing_campaign_sheds b
    ON b.tenant_id = w.tenant_id AND b.campaign_shed_id = w.campaign_shed_id
  WHERE w.tenant_id = $1::uuid
    AND w.park_id = $2::uuid
    AND w.due_business_date = $3::date
    AND w.work_item_id = $4::uuid
    AND w.work_state <> 'canceled'
),
units AS (
  SELECT 'animal'::text AS kind, o.observation_id::text AS unit_id, o.scanned_identifier AS name,
         o.weight_kg::float8 AS weight_kg, 0::int AS animal_count,
         o.verification_status, COALESCE(o.rework_reason, '') AS rework_reason,
         (o.submitted_at IS NOT NULL) AS submitted, i.bucket_status, o.recorded_by AS actor_id
  FROM item i
  JOIN weighing_observations o ON o.tenant_id = $1::uuid AND o.campaign_shed_id = i.campaign_shed_id
  WHERE i.weighing_category = 'individual_animal'
  UNION ALL
  SELECT 'pen', so.shed_observation_id::text, '', so.weight_kg::float8, so.animal_count,
         so.verification_status, COALESCE(so.rework_reason, ''),
         true, i.bucket_status, so.recorded_by
  FROM item i
  JOIN weighing_shed_observations so
    ON so.tenant_id = $1::uuid AND so.campaign_shed_id = i.campaign_shed_id AND so.withdrawn_at IS NULL
  WHERE i.weighing_category <> 'individual_animal'
  UNION ALL
  SELECT 'pen_todo', i.campaign_shed_id::text, '', 0, 0, '', '', false, i.bucket_status, NULL
  FROM item i
  WHERE i.weighing_category <> 'individual_animal'
    AND NOT EXISTS (SELECT 1 FROM weighing_shed_observations so
                     WHERE so.tenant_id = $1::uuid AND so.campaign_shed_id = i.campaign_shed_id AND so.withdrawn_at IS NULL)
),
ranked AS (
  SELECT u.*, ` + subtaskRankSQL + ` AS rank, count(*) OVER () AS total
  FROM units u
)
SELECT r.kind, r.unit_id, r.name, r.weight_kg, r.animal_count, r.verification_status, r.rework_reason,
       r.submitted, r.bucket_status, r.rank, r.total,
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
	rows, err := s.pool.Query(ctx, subtasksSQL, q.TenantID, q.ParkID, q.BusinessDate, q.SourceID, afterRank, afterID, limit+1)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("weighing boardsource subtasks: %w", err)
	}
	defer rows.Close()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	for rows.Next() {
		st, total, err := scanSubtask(rows)
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
		return domain.SubtaskPage{}, fmt.Errorf("weighing boardsource subtasks rows: %w", err)
	}
	return page, nil
}

func scanSubtask(rows pgx.Rows) (domain.Subtask, int, error) {
	var (
		kind, unitID, name, status, reworkReason, bucketStatus string
		weightKg                                               float64
		animalCount, rank, total                               int
		submitted                                              bool
		ownerUserID, ownerMemberID, ownerName                  string
	)
	if err := rows.Scan(&kind, &unitID, &name, &weightKg, &animalCount, &status, &reworkReason,
		&submitted, &bucketStatus, &rank, &total, &ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Subtask{}, 0, fmt.Errorf("weighing boardsource subtask scan: %w", err)
	}
	submitted = submitted || bucketStatus == "completed" || bucketStatus == "closed"
	closed := bucketStatus == "closed"

	// The chain: scan -> submit -> verify -> close. Each link states the fact the row holds.
	first := domain.Step{Name: "Scan", State: domain.StepDone, Detail: kgText(weightKg)}
	if kind != "animal" {
		first.Name = "Weigh"
		if animalCount > 0 {
			first.Detail += " · " + animalsText(animalCount)
		}
	}
	submit := domain.Step{Name: "Submit", State: domain.StepTodo}
	verify := domain.Step{Name: "Verify", State: domain.StepLocked}
	closeStep := domain.Step{Name: "Close", State: domain.StepLocked}
	state := domain.WorkStateInProgress
	attention := false
	switch {
	case kind == "pen_todo":
		first.State, first.Detail = domain.StepTodo, "Not weighed yet"
		state = domain.WorkStateDue
	case status == "rework":
		submit.State = domain.StepDone
		verify.State, verify.Detail = domain.StepRework, reworkReason
		state, attention = domain.WorkStateRejected, true
	case status == "verified":
		// The verdict landed: the animal's work is done. The bucket-grain close is the row's
		// own clock and is reported on the last link, never held against the animal.
		submit.State, verify.State = domain.StepDone, domain.StepDone
		closeStep.State = domain.StepTodo
		if closed {
			closeStep.State = domain.StepDone
		}
		state = domain.WorkStateCompleted
	case submitted:
		submit.State, verify.State = domain.StepDone, domain.StepInReview
		state = domain.WorkStateVerificationPending
	}
	if closed && state != domain.WorkStateCompleted && status != "rework" {
		// CLOSE writes only the bucket; a pending observation under a closed bucket is
		// history the verifier never reached, shown as the close it got.
		closeStep.State = domain.StepDone
	}

	subtitle := kgText(weightKg)
	if kind == "animal" {
		if name == "" {
			name = "Unknown tag"
		}
	} else {
		name = "Whole pen"
		if kind == "pen_todo" {
			subtitle = "Not weighed yet"
		} else if animalCount > 0 {
			subtitle += " · " + animalsText(animalCount)
		}
	}
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, unitID), Name: name, Subtitle: subtitle,
		WorkState: state, NeedsAttention: attention,
		Owner: domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName},
		Steps: []domain.Step{first, submit, verify, closeStep},
	}.Finalize(), total, nil
}

func animalsText(n int) string {
	if n == 1 {
		return "1 animal"
	}
	return strconv.Itoa(n) + " animals"
}

// kgText renders a weight the way the verifier's subject label does: one decimal, "kg".
func kgText(kg float64) string {
	return strconv.FormatFloat(kg, 'f', 1, 64) + " kg"
}
