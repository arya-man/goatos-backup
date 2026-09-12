package boardsource

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Subtasks of a feed transport task, for the board's issue view: one per ATTEMPT (each is
// one filmed trip and one verdict, film -> verify), plus the trip itself as a to-do while no
// attempt exists yet, so a live row never drills into an empty list. Reads
// feed_transport_tasks and feed_transport_attempts plus workforce_members for the name.
//
// READ-ONLY and REPORTING-ONLY: nothing here submits, verifies or reworks a trip.

// transportSubtaskRankSQL is the SQL twin of domain.RankFor, stated once.
//
//	rejected attempt      -> 0 needs attention
//	no attempt yet        -> 1 to do
//	verification_due      -> 3 in review
//	approved              -> 4 done
const transportSubtaskRankSQL = `CASE
  WHEN u.status = 'rejected' THEN 0
  WHEN u.kind = 'trip_todo' THEN 1
  WHEN u.status = 'approved' THEN 4
  ELSE 3
END`

// projection-review: membership=the ONE feed_transport_tasks row named by (tenant_id, park_id, business_date, task_id) with retired excluded, then every feed_transport_attempts row of that task (feed_transport_attempts_task_history_idx on tenant_id, task_id) or one synthetic to-do when the task has none; group_key=(tenant_id, unit id) where the unit id is the attempt id or the task id for the synthetic to-do, so one attempt is one subtask; join_cardinality=the two UNION ALL arms are exclusive on the NOT EXISTS so a task without attempts contributes exactly one unit and a task with attempts contributes exactly its attempts, and workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1), so nothing fans an attempt out and the window total counts each unit once; pagination=keyset on (rank, unit id) ASC after ($5, $6) with LIMIT $7 and the whole count carried by count(*) OVER () computed before the keyset cut; scope=tenant_id, park_id, business_date and task_id, the same predicate the row read binds on feed_transport_tasks.
const transportSubtasksSQL = `
WITH task AS (
  SELECT t.task_id, t.status AS task_status
  FROM feed_transport_tasks t
  WHERE t.tenant_id = $1::uuid
    AND t.business_date = $2::date
    AND t.park_id = $3::uuid
    AND t.task_id = $4::uuid
    AND t.status <> 'retired'
),
units AS (
  SELECT 'attempt'::text AS kind, a.attempt_id::text AS unit_id, a.attempt_no, a.status,
         COALESCE(a.rejection_reason, '') AS rejection_reason, a.submitted_at, a.operator_id AS actor_id
  FROM task k
  JOIN feed_transport_attempts a ON a.tenant_id = $1::uuid AND a.task_id = k.task_id
  UNION ALL
  SELECT 'trip_todo', k.task_id::text, 0, '', '', NULL, NULL
  FROM task k
  WHERE NOT EXISTS (SELECT 1 FROM feed_transport_attempts a WHERE a.tenant_id = $1::uuid AND a.task_id = k.task_id)
),
ranked AS (
  SELECT u.*, ` + transportSubtaskRankSQL + ` AS rank, count(*) OVER () AS total
  FROM units u
)
SELECT r.kind, r.unit_id, r.attempt_no, r.status, r.rejection_reason, r.submitted_at, r.rank, r.total,
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
	rows, err := s.pool.Query(ctx, transportSubtasksSQL, q.TenantID, q.BusinessDate, q.ParkID, q.SourceID, afterRank, afterID, limit+1)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("feed transport boardsource subtasks: %w", err)
	}
	defer rows.Close()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	for rows.Next() {
		st, total, err := scanTransportSubtask(rows)
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
		return domain.SubtaskPage{}, fmt.Errorf("feed transport boardsource subtasks rows: %w", err)
	}
	return page, nil
}

func scanTransportSubtask(rows pgx.Rows) (domain.Subtask, int, error) {
	var (
		kind, unitID, status, reason          string
		attemptNo, rank, total                int
		submittedAt                           *time.Time
		ownerUserID, ownerMemberID, ownerName string
	)
	if err := rows.Scan(&kind, &unitID, &attemptNo, &status, &reason, &submittedAt, &rank, &total,
		&ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Subtask{}, 0, fmt.Errorf("feed transport boardsource subtask scan: %w", err)
	}
	film := domain.Step{Name: "Film the trip", State: domain.StepTodo}
	verify := domain.Step{Name: "Verify", State: domain.StepLocked}
	name := "Trip"
	subtitle := "Not filmed yet"
	state := domain.WorkStateDue
	attention := false
	if kind == "attempt" {
		if attemptNo > 1 {
			name = "Trip · attempt " + strconv.Itoa(attemptNo)
		}
		film.State = domain.StepDone
		if submittedAt != nil {
			film.Detail = "Filmed " + submittedAt.In(biztime.DefaultLocation()).Format("15:04")
			subtitle = film.Detail
		}
		switch status {
		case "approved":
			verify.State = domain.StepDone
			state = domain.WorkStateCompleted
		case "rejected":
			verify.State, verify.Detail = domain.StepRework, reason
			state, attention = domain.WorkStateRejected, true
		default:
			verify.State = domain.StepInReview
			state = domain.WorkStateVerificationPending
		}
	}
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, unitID), Name: name, Subtitle: subtitle,
		WorkState: state, NeedsAttention: attention,
		Owner: domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName},
		Steps: []domain.Step{film, verify},
	}.Finalize(), total, nil
}
