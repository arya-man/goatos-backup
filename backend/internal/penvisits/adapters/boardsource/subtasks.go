package boardsource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	pvdomain "github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Subtasks of a pen visit row, for the board's issue view: the visit has no finer grain (one
// pen, one video), so it drills into itself as ONE subtask with the chain visit -> verify.
// Reads pen_visit_tasks plus workforce_members for the visitor's name.
//
// READ-ONLY and REPORTING-ONLY: nothing here gates a submit or a verdict.

// projection-review: membership=the ONE pen_visit_tasks row named by (tenant_id, park_id, due_business_date, task_id) with canceled excluded and the module split applied, so a row the board would not show is an empty page; group_key=(tenant_id, task_id), one visit is one subtask; join_cardinality=workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1); pagination=none -- one unit, total is 1 or 0; scope=tenant_id, park_id, due_business_date and task_id, the same predicate the row read binds.
func (s *Source) subtaskSQL() string {
	return `
SELECT v.task_id::text, v.reasons, v.source_business_date::text, v.planned_business_date::text,
       v.work_state, v.status, COALESCE(v.rework_reason, ''), v.submitted_at, v.verified_at,
       COALESCE(v.submitted_by::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM pen_visit_tasks v
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = v.submitted_by AND m.status = 'active'
WHERE v.tenant_id = $1::uuid
  AND v.park_id = $2::uuid
  AND v.due_business_date = $3::date
  AND v.task_id = $4::uuid
  AND v.work_state <> 'canceled'`
}

// ListSubtasks implements ports.SubtaskSource.
func (s *Source) ListSubtasks(ctx context.Context, q ports.SubtaskQuery) (domain.SubtaskPage, error) {
	afterRank, _, err := domain.ParseSubtaskKey(q.AfterKey)
	if err != nil {
		return domain.SubtaskPage{}, err
	}
	if err := ports.CheckUUIDSourceID(q.SourceID); err != nil {
		return domain.SubtaskPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	st, err := s.readSubtask(ctx, q)
	if errors.Is(err, pgx.ErrNoRows) {
		return page, nil
	}
	if err != nil {
		return domain.SubtaskPage{}, err
	}
	page.Total = 1
	// One unit: a cursor past it (a later rank than its own) lists nothing more.
	if q.AfterKey != "" {
		rank, _, _ := domain.ParseSubtaskKey(st.Key)
		if afterRank >= rank {
			return page, nil
		}
	}
	page.Subtasks = append(page.Subtasks, st)
	return page, nil
}

func (s *Source) readSubtask(ctx context.Context, q ports.SubtaskQuery) (domain.Subtask, error) {
	var (
		taskID, sourceDate, planned, workState, status, reworkMsg string
		reasons                                                   []string
		submittedAt, verifiedAt                                   *time.Time
		ownerUserID, ownerMemberID, ownerName                     string
	)
	err := s.pool.QueryRow(ctx, s.subtaskSQL(), q.TenantID, q.ParkID, q.BusinessDate, q.SourceID).Scan(
		&taskID, &reasons, &sourceDate, &planned, &workState, &status, &reworkMsg, &submittedAt, &verifiedAt,
		&ownerUserID, &ownerMemberID, &ownerName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subtask{}, err
		}
		return domain.Subtask{}, fmt.Errorf("pen visit boardsource subtask: %w", err)
	}
	visit := domain.Step{Name: "Visit", State: domain.StepTodo}
	verify := domain.Step{Name: "Verify", State: domain.StepLocked}
	state := domain.WorkStateDue
	attention := false
	if submittedAt != nil {
		visit.State = domain.StepDone
		visit.Detail = "Visited " + biztime.FarmDate(submittedAt.In(biztime.DefaultLocation()))
	}
	switch status {
	case pvdomain.StatusRework:
		visit.State = domain.StepTodo
		verify.State, verify.Detail = domain.StepRework, reworkMsg
		state, attention = domain.WorkStateRejected, true
	case pvdomain.StatusCompleted:
		verify.State = domain.StepDone
		if verifiedAt != nil {
			verify.Detail = "Verified " + biztime.FarmDate(verifiedAt.In(biztime.DefaultLocation()))
		}
		state = domain.WorkStateCompleted
	case pvdomain.StatusPendingVerification:
		verify.State = domain.StepInReview
		state = domain.WorkStateVerificationPending
	default:
		if workState == pvdomain.WorkStateDelayed {
			state, attention = domain.WorkStateOverdue, true
			visit.Detail = "Owed " + biztime.FarmDateFromBusinessDate(planned)
		}
	}
	labels := make([]string, 0, len(reasons))
	for i, r := range pvdomain.SortReasons(reasons) {
		l := pvdomain.ReasonLabel(r)
		if i > 0 {
			l = strings.ToLower(l)
		}
		labels = append(labels, l)
	}
	subtitle := "After " + strings.ToLower(strings.Join(labels, ", ")) + " on " + biztime.FarmDateFromBusinessDate(sourceDate)
	if len(labels) == 0 {
		subtitle = "After preventive care on " + biztime.FarmDateFromBusinessDate(sourceDate)
	}
	rank := domain.RankFor(state, attention)
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, taskID), Name: "Pen visit", Subtitle: subtitle,
		WorkState: state, NeedsAttention: attention,
		Owner: domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName},
		Steps: []domain.Step{visit, verify},
	}.Finalize(), nil
}
