package boardsource

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Subtasks of an engine workflow row, for the board's issue view: ONE subtask per step of the
// workflow (the SOP's authored steps, in their own words), each with the chain do -> verify when
// the step records proof, or just do when it does not. A workflow carries at most a few dozen
// steps (the SOP compiler bounds a track), so the row's steps are read in one bounded query and
// ranked and paged here on the subtask key.
//
// READ-ONLY and REPORTING-ONLY: nothing here gates an answer, a completion or a verdict.

// projection-review: membership=the workflow_actions of the ONE workflow_instances row named by (tenant_id, park_id, workflow_id) that the row read would show on that day for this lane (the same baseWhere), canceled and skipped steps excluded (a skipped step is off the taken path and never owed, the same rule RecomputeCard counts by); group_key=(workflow_id, action_key), one step is one subtask (workflow_actions_natural_uq); join_cardinality=workforce_members filtered to status='active' on the partial-unique (tenant_id, user_id) index (at most 1); pagination=keyset on the rank-led subtask key, applied in Go over the bounded step list, total = the whole list; scope=tenant_id, park_id, the day predicate, the lane's module set and workflow_id.
func subtaskSQL() string {
	return `
SELECT wa.action_id::text, wa.seq, wa.title, wa.status, wa.due_at, wa.completed_at,
       COALESCE(wa.rework_reason, ''),
       (wa.requires_video OR wa.proof_min_videos > 0 OR wa.proof_min_photos > 0),
       COALESCE(wa.completed_by::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM workflow_instances wi
JOIN workflow_actions wa ON wa.workflow_id = wi.workflow_id AND wa.status NOT IN ('canceled', 'skipped')
LEFT JOIN workforce_members m
  ON m.tenant_id = wi.tenant_id AND m.user_id = wa.completed_by AND m.status = 'active'
WHERE ` + baseWhere() + `
  AND wi.workflow_id = $6::uuid
ORDER BY wa.seq, wa.action_id`
}

// ListSubtasks implements ports.SubtaskSource.
func (s *Source) ListSubtasks(ctx context.Context, q ports.SubtaskQuery) (domain.SubtaskPage, error) {
	if _, _, err := domain.ParseSubtaskKey(q.AfterKey); err != nil {
		return domain.SubtaskPage{}, err
	}
	if err := ports.CheckUUIDSourceID(q.SourceID); err != nil {
		return domain.SubtaskPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	now := s.now()
	bound := sqlbind.MustBind(subtaskSQL(), q.TenantID, q.ParkID, q.BusinessDate, s.modules, s.knownArg(), q.SourceID)
	rows, err := s.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("workflow boardsource subtasks: %w", err)
	}
	defer rows.Close()
	all := []domain.Subtask{}
	for rows.Next() {
		var (
			actionID, title, status, reworkReason string
			seq                                   int
			dueAt, completedAt                    *time.Time
			needsProof                            bool
			ownerUserID, ownerMemberID, ownerName string
		)
		if err := rows.Scan(&actionID, &seq, &title, &status, &dueAt, &completedAt, &reworkReason, &needsProof,
			&ownerUserID, &ownerMemberID, &ownerName); err != nil {
			return domain.SubtaskPage{}, fmt.Errorf("workflow boardsource subtask scan: %w", err)
		}
		all = append(all, stepSubtask(actionID, seq, title, status, dueAt, completedAt, reworkReason, needsProof,
			domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName}, now))
	}
	if err := rows.Err(); err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("workflow boardsource subtask rows: %w", err)
	}
	return domain.PageSubtasks(all, q.AfterKey, q.Limit), nil
}

// stepSubtask turns one engine step into a subtask. The state follows the step's own status;
// a pending step past its due instant is overdue and needs attention, a step sent back is
// rejected and needs attention.
func stepSubtask(actionID string, seq int, title, status string, dueAt, completedAt *time.Time, reworkReason string,
	needsProof bool, owner domain.Owner, now time.Time) domain.Subtask {
	do := domain.Step{Name: "Do", State: domain.StepTodo}
	verify := domain.Step{Name: "Verify", State: domain.StepLocked}
	state := domain.WorkStateDue
	attention := false
	subtitle := ""
	if dueAt != nil {
		subtitle = "Due " + biztime.FarmDate(dueAt.In(biztime.DefaultLocation()))
	}
	switch status {
	case "completed":
		do.State = domain.StepDone
		verify.State = domain.StepDone
		state = domain.WorkStateCompleted
		if completedAt != nil {
			subtitle = "Done " + biztime.FarmDate(completedAt.In(biztime.DefaultLocation()))
		}
	case "in_review":
		do.State = domain.StepDone
		verify.State = domain.StepInReview
		state = domain.WorkStateVerificationPending
	case "rework":
		verify.State, verify.Detail = domain.StepRework, strings.TrimSpace(reworkReason)
		state, attention = domain.WorkStateRejected, true
		if reason := strings.TrimSpace(reworkReason); reason != "" {
			subtitle = "Sent back: " + reason
		}
	default:
		if dueAt != nil && dueAt.Before(now) {
			state, attention = domain.WorkStateOverdue, true
			do.State = domain.StepNeedsAttention
		}
	}
	steps := []domain.Step{do}
	if needsProof {
		steps = append(steps, verify)
	}
	rank := domain.RankFor(state, attention)
	return domain.Subtask{
		Key:  domain.SubtaskKey(rank, fmt.Sprintf("%04d:%s", seq, actionID)),
		Name: title, Subtitle: subtitle,
		WorkState: state, NeedsAttention: attention,
		Owner: owner, Steps: steps,
	}.Finalize()
}
