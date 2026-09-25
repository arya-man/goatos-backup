package boardsource

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	toxinpg "github.com/vgoats/goatos/backend/internal/toxin/adapters/postgres"
	toxindomain "github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Subtasks of a toxin row, for the board's issue view: ONE subtask per step of the procedure the
// round was opened on (its own sop_version -- a procedure published mid-round never moves the
// steps), in the SOP's own words. A working step is done when its completion exists; a waiting
// row mirrors the step it waits for. The round's own reads (the task, its completions with the
// tester's name, the procedure version) are the toxin module's own repository and procedure
// source, so the board shows exactly what the tester's phone shows.
//
// READ-ONLY and REPORTING-ONLY: nothing here completes a step, gates a wait or casts a verdict.

// projection-review: membership=the ONE toxin_test_tasks row named by task_id that the row read would show on that day for that park (the same baseWhere); group_key=(task_id, step no), one procedure step is one subtask; join_cardinality=none here (the round and its completions are then read by primary key through the toxin repository); pagination=keyset on the rank-led subtask key over the bounded (<= 20 step) procedure, total = the whole procedure; scope=tenant_id, the park code, the day predicate and task_id.
const scopeSQL = `SELECT 1 FROM toxin_test_tasks t WHERE ` + baseWhere + ` AND t.task_id = $4::uuid`

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
	empty := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	var one int
	bound := sqlbind.MustBind(scopeSQL, q.TenantID, q.ParkID, q.BusinessDate, q.SourceID)
	if err := s.pool.QueryRow(ctx, bound.SQL(), bound.Args()...).Scan(&one); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, nil
		}
		return domain.SubtaskPage{}, fmt.Errorf("toxin boardsource subtask scope: %w", err)
	}
	row, err := toxinpg.NewRepository(s.pool, s.timeout).GetTask(ctx, q.TenantID, q.SourceID)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("toxin boardsource subtask task: %w", err)
	}
	proc, err := toxinpg.NewProcedureSource(s.pool).ProcedureVersion(ctx, q.TenantID, row.Task.SOPVersion)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("toxin boardsource subtask procedure: %w", err)
	}
	return domain.PageSubtasks(StepSubtasks(row.Task, row.Completions, proc), q.AfterKey, q.Limit), nil
}

// StepSubtasks lays a round out as its procedure's steps.
func StepSubtasks(task toxindomain.Task, completions []toxindomain.StepCompletion, proc toxindomain.Procedure) []domain.Subtask {
	done := map[int]toxindomain.StepCompletion{}
	for _, c := range completions {
		done[c.StepNo] = c
	}
	next := proc.NextStepNo(completions)
	out := make([]domain.Subtask, 0, len(proc.Steps))
	for _, spec := range proc.Steps {
		stepNo := spec.No
		if spec.Kind == toxindomain.StepKindWait {
			// A waiting row has no state of its own: it mirrors the step it waits for.
			if gated, ok := proc.GatedByWait(spec.No); ok {
				stepNo = gated.No
			}
		}
		work := domain.Step{Name: "Do", State: domain.StepTodo}
		state := domain.WorkStateDue
		subtitle := "Step " + strconv.Itoa(spec.No)
		owner := domain.Owner{}
		if c, ok := done[stepNo]; ok {
			work.State = domain.StepDone
			state = domain.WorkStateCompleted
			if !c.CompletedAt.IsZero() {
				subtitle += " · done " + biztime.FarmDate(c.CompletedAt.In(biztime.DefaultLocation()))
			}
			if spec.Kind != toxindomain.StepKindWait {
				owner.Name = c.CompletedBy
			}
		} else if stepNo == next && task.Status == toxindomain.StatusInProgress {
			work.State = domain.StepInProgress
			state = domain.WorkStateInProgress
		} else if next == 0 || task.Status != toxindomain.StatusInProgress {
			work.State = domain.StepLocked
		}
		steps := []domain.Step{work}
		if spec.Kind == toxindomain.StepKindPhotoReading {
			review := domain.Step{Name: "Review", State: domain.StepLocked}
			switch task.Status {
			case toxindomain.StatusPendingReview:
				review.State = domain.StepInReview
				state = domain.WorkStateVerificationPending
			case toxindomain.StatusAccepted:
				review.State = domain.StepDone
				review.Detail = toxindomain.OutcomeLabel(task.Outcome)
			}
			steps = append(steps, review)
		}
		rank := domain.RankFor(state, false)
		out = append(out, domain.Subtask{
			Key:  domain.SubtaskKey(rank, fmt.Sprintf("%02d", spec.No)),
			Name: spec.Title, Subtitle: subtitle,
			WorkState: state, Owner: owner, Steps: steps,
		}.Finalize())
	}
	return out
}
