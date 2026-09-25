package boardsource

import (
	"context"
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Subtasks for the Counts vertical's two board sources, for the board's issue view.
//
// An APPROVAL REQUEST has no finer grain than itself: its one subtask is the request, with
// the children a birth names or the animals a pen move names as the subtitle, and the chain
// raise -> approve -> apply (the decision and its effect commit together, so apply lands
// with approve). A MILK FEEDING SESSION is likewise one subtask per task: the session, with
// the chain prepare -> feed -> submit -> verify derived from the task's own status.
//
// READ-ONLY and REPORTING-ONLY: nothing here decides a request or verifies a session.

// approvalSubtaskRankSQL is the SQL twin of domain.RankFor for a request, stated once.
//
//	pending  -> 1 to do (awaiting the approver pool)
//	(a rejected request is off the board -- see approvals.go -- so it has no subtasks)
//	approved -> 4 done
const approvalSubtaskRankSQL = `CASE
  WHEN a.status = 'approved' AND a.request_type = 'shifting' AND se.event_status = 'rejected' THEN 0
  WHEN a.status = 'approved' AND a.request_type = 'shifting' AND se.event_status = 'authorized' THEN 2
  WHEN a.status = 'approved' AND a.request_type = 'shifting' AND se.event_status = 'pending_verification' THEN 3
  WHEN a.status = 'approved' THEN 4
  ELSE 1
END`

// projection-review: membership=the ONE counts_approval_requests row named by (tenant_id, approval_request_id) whose park resolves to the requested park and whose raised_at falls in the requested IST business day, so the row is exactly the one the row read served; group_key=(tenant_id, approval_request_id) as one subtask; join_cardinality=a pen move's shifting event resolves through shifting_events on its primary key (1:1, absent for birth and death) and a death's subject animal resolves through goats on its primary key (1:1, absent for birth and pen move) and the raiser through workforce_members on the partial-unique active (tenant_id,user_id) index (at most 1), the children and animal counts are jsonb_array_length over the row's own payload, so nothing fans the request out and the total is 1 or 0; pagination=keyset on (rank, request id) ASC after ($6, $7) with LIMIT $8 over the single unit; scope=tenant_id, park, day range and approval_request_id, the same predicate the row read binds.
const approvalSubtasksSQL = `
SELECT a.approval_request_id::text, a.request_type, a.status, COALESCE(a.decision_reason, ''),
       a.raised_at, a.decided_at, COALESCE(se.event_status, ''),
       CASE WHEN jsonb_typeof(a.payload->'children') = 'array' THEN jsonb_array_length(a.payload->'children') ELSE 0 END AS children,
       CASE WHEN jsonb_typeof(a.payload->'goat_ids') = 'array' THEN jsonb_array_length(a.payload->'goat_ids') ELSE 0 END AS animals,
       ` + approvalSubtaskRankSQL + ` AS rank,
       COALESCE((SELECT m.display_name FROM workforce_members m
                  WHERE m.tenant_id = $1::uuid AND m.user_id = a.raised_by_user_id
                  ORDER BY (m.status = 'active') DESC, m.workforce_member_id LIMIT 1), '')
FROM counts_approval_requests a
LEFT JOIN shifting_events se ON se.tenant_id = a.tenant_id AND se.shifting_event_id = a.shifting_event_id
LEFT JOIN goats g ON g.tenant_id = a.tenant_id AND g.goat_id = a.subject_goat_id
WHERE a.tenant_id = $1::uuid
  AND a.approval_request_id = $5::uuid
  AND a.status = ANY(ARRAY['pending','approved'])
  AND a.request_type = ANY(ARRAY['birth','shifting','death'])
  AND a.raised_at >= $3::timestamptz AND a.raised_at < $4::timestamptz
  AND ` + approvalParkSQL + ` = $2::text
  AND (` + approvalSubtaskRankSQL + `, a.approval_request_id::text) > ($6::int, $7::text)
ORDER BY 10, 1
LIMIT $8`

// ListSubtasks implements ports.SubtaskSource.
func (s *ApprovalsSource) ListSubtasks(ctx context.Context, q ports.SubtaskQuery) (domain.SubtaskPage, error) {
	start, end, err := businessDayBounds(q.BusinessDate)
	if err != nil {
		return domain.SubtaskPage{}, err
	}
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
	bound, err := sqlbind.Bind(approvalSubtasksSQL, q.TenantID, q.ParkID, start, end, q.SourceID, afterRank, afterID, limit+1)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("counts boardsource subtasks bind: %w", err)
	}
	rows, err := s.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("counts boardsource subtasks: %w", err)
	}
	defer rows.Close()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	for rows.Next() {
		st, err := scanApprovalSubtask(rows)
		if err != nil {
			return domain.SubtaskPage{}, err
		}
		page.Subtasks = append(page.Subtasks, st)
	}
	if err := rows.Err(); err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("counts boardsource subtasks rows: %w", err)
	}
	page.Total = len(page.Subtasks)
	return page, nil
}

func scanApprovalSubtask(rows pgx.Rows) (domain.Subtask, error) {
	var (
		requestID, requestType, status, reason string
		raisedAt                               time.Time
		decidedAt                              *time.Time
		children, animals, rank                int
		eventStatus, raisedByName              string
	)
	if err := rows.Scan(&requestID, &requestType, &status, &reason, &raisedAt, &decidedAt, &eventStatus, &children, &animals, &rank, &raisedByName); err != nil {
		return domain.Subtask{}, fmt.Errorf("counts boardsource subtask scan: %w", err)
	}
	raise := domain.Step{Name: "Raise", State: domain.StepDone, Detail: "Raised " + raisedAt.In(biztime.DefaultLocation()).Format("15:04")}
	if raisedByName != "" {
		raise.Detail += " by " + raisedByName
	}
	approve := domain.Step{Name: "Approve", State: domain.StepTodo}
	apply := domain.Step{Name: "Apply", State: domain.StepLocked}
	state := domain.WorkStateDue
	attention := false
	switch status {
	case "approved":
		approve.State, apply.State = domain.StepDone, domain.StepDone
		if decidedAt != nil {
			approve.Detail = "Approved " + decidedAt.In(biztime.DefaultLocation()).Format("15:04")
		}
		state = domain.WorkStateCompleted
		if requestType == "shifting" {
			// Approving a move authorizes it and moves nothing: Apply follows the event.
			switch eventStatus {
			case "authorized":
				apply.State, state = domain.StepTodo, domain.WorkStateInProgress
			case "pending_verification":
				apply.State, state = domain.StepInReview, domain.WorkStateVerificationPending
			case "rejected":
				apply.State, state, attention = domain.StepRework, domain.WorkStateRejected, true
			}
		}
	case "rejected":
		approve.State, approve.Detail = domain.StepRework, reason
		state, attention = domain.WorkStateRejected, true
	}
	subtitle := ""
	switch requestType {
	case "birth":
		subtitle = kidsText(children)
	case "shifting":
		subtitle = animalsText(animals)
	case "death":
		subtitle = "1 animal"
	}
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, requestID), Name: approvalTypeLabel(requestType), Subtitle: subtitle,
		WorkState: state, NeedsAttention: attention,
		// The approver is a pool; the raiser is named on the raise step, not as the owner.
		Owner: domain.Owner{},
		Steps: []domain.Step{raise, approve, apply},
	}.Finalize(), nil
}

// milkSubtaskRankSQL is the SQL twin of domain.RankFor for a session, stated once.
//
//	rework               -> 0 needs attention
//	not_submitted        -> 1 to do
//	pending_verification -> 3 in review
//	completed            -> 4 done
const milkSubtaskRankSQL = `CASE t.status
  WHEN 'rework' THEN 0
  WHEN 'pending_verification' THEN 3
  WHEN 'completed' THEN 4
  ELSE 1
END`

// projection-review: membership=the ONE milk_feeding_tasks row named by (tenant_id, park_id, feeding_date, task_id) with retired excluded, the same row the row read served; group_key=(tenant_id, task_id) as one subtask; join_cardinality=workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1), so the total is 1 or 0; pagination=keyset on (rank, task id) ASC after ($5, $6) with LIMIT $7 over the single unit; scope=tenant_id, park_id, feeding_date and task_id, the same predicate the row read binds.
const milkSubtasksSQL = `
SELECT t.task_id::text, t.session_no, t.due_at, t.head_count, t.status, COALESCE(t.rework_reason, ''),
       t.submitted_at, t.verified_at, ` + milkSubtaskRankSQL + ` AS rank,
       COALESCE(t.assigned_operator_id::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM milk_feeding_tasks t
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = t.assigned_operator_id AND m.status = 'active'
WHERE t.tenant_id = $1::uuid
  AND t.feeding_date = $3::date
  AND t.park_id = $2::uuid
  AND t.task_id = $4::uuid
  AND t.status <> 'retired'
  AND (` + milkSubtaskRankSQL + `, t.task_id::text) > ($5::int, $6::text)
ORDER BY 9, 1
LIMIT $7`

// ListSubtasks implements ports.SubtaskSource.
func (s *MilkFeedingSource) ListSubtasks(ctx context.Context, q ports.SubtaskQuery) (domain.SubtaskPage, error) {
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
	rows, err := s.pool.Query(ctx, milkSubtasksSQL, q.TenantID, q.ParkID, q.BusinessDate, q.SourceID, afterRank, afterID, limit+1)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("milk boardsource subtasks: %w", err)
	}
	defer rows.Close()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	for rows.Next() {
		st, err := scanMilkSubtask(rows)
		if err != nil {
			return domain.SubtaskPage{}, err
		}
		page.Subtasks = append(page.Subtasks, st)
	}
	if err := rows.Err(); err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("milk boardsource subtasks rows: %w", err)
	}
	page.Total = len(page.Subtasks)
	return page, nil
}

func scanMilkSubtask(rows pgx.Rows) (domain.Subtask, error) {
	var (
		taskID, status, reason                string
		sessionNo, headCount, rank            int
		dueAt                                 time.Time
		submittedAt, verifiedAt               *time.Time
		ownerUserID, ownerMemberID, ownerName string
	)
	if err := rows.Scan(&taskID, &sessionNo, &dueAt, &headCount, &status, &reason, &submittedAt, &verifiedAt, &rank,
		&ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Subtask{}, fmt.Errorf("milk boardsource subtask scan: %w", err)
	}
	due := dueAt.In(biztime.DefaultLocation())
	prepare := domain.Step{Name: "Prepare", State: domain.StepTodo}
	feed := domain.Step{Name: "Feed", State: domain.StepTodo}
	submit := domain.Step{Name: "Submit", State: domain.StepTodo}
	verify := domain.Step{Name: "Verify", State: domain.StepLocked}
	state := domain.WorkStateDue
	attention := false
	// The task records ONE fact about the work -- the submit -- so prepare and feed follow it:
	// they are done once the proof is in, and to do until then.
	switch status {
	case "pending_verification":
		prepare.State, feed.State, submit.State = domain.StepDone, domain.StepDone, domain.StepDone
		verify.State = domain.StepInReview
		state = domain.WorkStateVerificationPending
	case "completed":
		prepare.State, feed.State, submit.State, verify.State = domain.StepDone, domain.StepDone, domain.StepDone, domain.StepDone
		state = domain.WorkStateCompleted
	case "rework":
		prepare.State, feed.State = domain.StepDone, domain.StepDone
		submit.State = domain.StepRework
		verify.State, verify.Detail = domain.StepRework, reason
		state, attention = domain.WorkStateRejected, true
	}
	if submittedAt != nil && submit.State == domain.StepDone {
		submit.Detail = "Submitted " + submittedAt.In(biztime.DefaultLocation()).Format("15:04")
	}
	ownerState := domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName}
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, taskID), Name: "Session " + strconv.Itoa(sessionNo),
		Subtitle:  due.Format("15:04") + " · " + kidsText(headCount),
		WorkState: state, NeedsAttention: attention, Owner: ownerState,
		Steps: []domain.Step{prepare, feed, submit, verify},
	}.Finalize(), nil
}

func animalsText(n int) string {
	if n == 1 {
		return "1 animal"
	}
	return strconv.Itoa(n) + " animals"
}

func kidsText(n int) string {
	if n == 1 {
		return "1 kid"
	}
	return strconv.Itoa(n) + " kids"
}
