package boardsource

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Subtasks of a health treatment session, for the board's issue view: one per TREATMENT
// STEP of the session (a medicine with its dose and route, or a plain action), named by
// what is given or done, with the chain give -> verify. A session that has no materialised
// steps drills into itself as one subtask, so a live row never shows an empty list. Reads
// health_treatment_sessions, health_cases and health_session_steps plus workforce_members
// for the name of whoever completed the session. Reads NO animal table.
//
// READ-ONLY and REPORTING-ONLY: nothing here completes a step or casts a verdict.

// healthSubtaskRankSQL is the SQL twin of domain.RankFor, stated once.
//
//	step guarded, or the session in rework / held  -> 0 needs attention
//	step pending, session not started               -> 1 to do
//	step pending, session in progress               -> 2 in progress
//	step completed, session completed unverified    -> 4 done (health verification is post-task
//	                                                   review, never a completion gate)
//	step completed                                  -> 4 done
const healthSubtaskRankSQL = `CASE
  WHEN u.step_status = 'guarded' OR u.session_status IN ('rework', 'held_death_review') THEN 0
  WHEN u.step_status = 'completed' THEN 4
  WHEN u.session_status = 'in_progress' THEN 2
  ELSE 1
END`

// projection-review: membership=the ONE health_treatment_sessions row named by (tenant_id, health_session_id) whose case park is the requested park and whose business_date is the requested day with canceled excluded, then every health_session_steps row of that session (health_session_steps_order_idx on health_session_id, seq) or one synthetic unit when the session has none; group_key=(tenant_id, unit id) where the unit id is the step id or the session id for the synthetic unit, so one step is one subtask; join_cardinality=health_cases on its primary key (1:1), the two UNION ALL arms are exclusive on the NOT EXISTS so a session contributes exactly its steps or exactly one unit, and workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1), so nothing fans a step out and the window total counts each unit once; pagination=keyset on (rank, unit id) ASC after ($5, $6) with LIMIT $7 and the whole count carried by count(*) OVER () computed before the keyset cut; scope=tenant_id, park_id, business_date and health_session_id, the same predicate the row read binds.
const healthSubtasksSQL = `
WITH session AS (
  SELECT s.health_session_id, s.status AS session_status, s.completed_by, s.verified_at,
         c.disease_name
  FROM health_treatment_sessions s
  JOIN health_cases c ON c.tenant_id = s.tenant_id AND c.health_case_id = s.health_case_id
  WHERE s.tenant_id = $1::uuid
    AND s.business_date = $3::date
    AND c.park_id = $2::uuid
    AND s.health_session_id = $4::uuid
    AND s.status NOT IN ('canceled_death', 'canceled')
),
units AS (
  SELECT 'step'::text AS kind, st.health_session_step_id::text AS unit_id, st.seq,
         st.record_type, COALESCE(st.medicine_name, '') AS medicine_name, COALESCE(st.dosage_text, '') AS dosage_text,
         COALESCE(st.medicine_route, '') AS medicine_route, COALESCE(st.instruction, '') AS instruction,
         COALESCE(st.critical_action_type, '') AS critical_action_type,
         st.status AS step_status, x.session_status, x.completed_by, (x.verified_at IS NOT NULL) AS verified, x.disease_name
  FROM session x
  JOIN health_session_steps st ON st.tenant_id = $1::uuid AND st.health_session_id = x.health_session_id
  UNION ALL
  SELECT 'session', x.health_session_id::text, 0, 'session', '', '', '', '', '',
         CASE WHEN x.session_status = 'completed' THEN 'completed' ELSE 'pending' END,
         x.session_status, x.completed_by, (x.verified_at IS NOT NULL), x.disease_name
  FROM session x
  WHERE NOT EXISTS (SELECT 1 FROM health_session_steps st WHERE st.tenant_id = $1::uuid AND st.health_session_id = x.health_session_id)
),
ranked AS (
  SELECT u.*, ` + healthSubtaskRankSQL + ` AS rank, count(*) OVER () AS total,
         lpad(u.seq::text, 4, '0') || ':' || u.unit_id AS sort_id
  FROM units u
)
SELECT r.kind, r.sort_id, r.record_type, r.medicine_name, r.dosage_text, r.medicine_route, r.instruction,
       r.critical_action_type, r.step_status, r.session_status, r.verified, r.disease_name, r.rank, r.total,
       COALESCE(r.completed_by::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM ranked r
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = r.completed_by AND m.status = 'active'
WHERE (r.rank, r.sort_id) > ($5::int, $6::text)
ORDER BY r.rank, r.sort_id
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
	rows, err := s.pool.Query(ctx, healthSubtasksSQL, q.TenantID, q.ParkID, q.BusinessDate, q.SourceID, afterRank, afterID, limit+1)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("health boardsource subtasks: %w", err)
	}
	defer rows.Close()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	for rows.Next() {
		st, total, err := scanHealthSubtask(rows)
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
		return domain.SubtaskPage{}, fmt.Errorf("health boardsource subtasks rows: %w", err)
	}
	return page, nil
}

func scanHealthSubtask(rows pgx.Rows) (domain.Subtask, int, error) {
	var (
		kind, sortID, recordType, medicine, dosage, route, instruction, critical string
		stepStatus, sessionStatus, disease                                       string
		verified                                                                 bool
		rank, total                                                              int
		ownerUserID, ownerMemberID, ownerName                                    string
	)
	if err := rows.Scan(&kind, &sortID, &recordType, &medicine, &dosage, &route, &instruction, &critical,
		&stepStatus, &sessionStatus, &verified, &disease, &rank, &total, &ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Subtask{}, 0, fmt.Errorf("health boardsource subtask scan: %w", err)
	}
	name, subtitle, verb := stepCopy(kind, recordType, medicine, dosage, route, instruction, critical, disease)

	give := domain.Step{Name: verb, State: domain.StepTodo}
	verify := domain.Step{Name: "Verify", State: domain.StepLocked}
	state := domain.WorkStateScheduled
	attention := false
	switch stepStatus {
	case "completed":
		give.State = domain.StepDone
		state = domain.WorkStateCompleted
		// Health verification is post-task evidence review: a completed session is done on
		// the board, and the review step reports whether the verifier has looked yet.
		verify.State = domain.StepInReview
		if verified {
			verify.State = domain.StepDone
		}
	case "guarded":
		give.State, give.Detail = domain.StepLocked, "Held by a critical action"
		state, attention = domain.WorkStateBlocked, true
	default:
		switch sessionStatus {
		case "in_progress":
			give.State = domain.StepInProgress
			state = domain.WorkStateInProgress
		case "due":
			state = domain.WorkStateDue
		}
	}
	switch sessionStatus {
	case "rework":
		verify.State, verify.Detail = domain.StepRework, "Sent back"
		state, attention = domain.WorkStateRejected, true
	case "held_death_review":
		give.State, give.Detail = domain.StepLocked, "Held while the death is reviewed"
		state, attention = domain.WorkStateBlocked, true
	}
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, sortID), Name: name, Subtitle: subtitle,
		WorkState: state, NeedsAttention: attention,
		Owner: domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName},
		Steps: []domain.Step{give, verify},
	}.Finalize(), total, nil
}

// stepCopy names a step by what it is: the medicine (with dose and route beneath), the
// action's instruction, or the critical action's kind. A raw type token is never shown.
func stepCopy(kind, recordType, medicine, dosage, route, instruction, critical, disease string) (name, subtitle, verb string) {
	if kind == "session" {
		return disease + " treatment", "No steps recorded", "Treat"
	}
	switch recordType {
	case "medication":
		name = strings.TrimSpace(medicine)
		if name == "" {
			name = "Medicine"
		}
		parts := []string{}
		if d := strings.TrimSpace(dosage); d != "" {
			parts = append(parts, d)
		}
		if r := strings.TrimSpace(route); r != "" {
			parts = append(parts, humanize(r))
		}
		return name, strings.Join(parts, " · "), "Give"
	case "critical_action":
		name = humanize(critical)
		if name == "" {
			name = "Critical action"
		}
		return name, strings.TrimSpace(instruction), "Do"
	default:
		name = strings.TrimSpace(instruction)
		if name == "" {
			name = "Action"
		}
		return name, "", "Do"
	}
}

// humanize spaces and capitalises a config token ("intramuscular", "isolate_animal") for a
// screen; it is the last resort, never the first choice.
func humanize(token string) string {
	words := strings.Fields(strings.ReplaceAll(strings.TrimSpace(token), "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
