package boardsource

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	pidomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Subtasks of a vaccination drive pen, for the board's issue view: one per ANIMAL in the pen
// for that drive -- its obligation -- named by the animal's tag, with the chain vaccinate ->
// verify derived from the obligation's status and its latest completion.
//
// The board row is a process-integrity grain (batch x rule x pen x day, or one unbatched
// obligation), and its row id carries exactly the keys this read binds on. The membership
// predicates below MIRROR the process-integrity canonical read (repository.go, the `raw` and
// `located` CTEs): the same status set, the same shed resolution (the animal's own shed, else
// the obligation's shed scope), the same partition resolution (the animal's partition, else
// the assignment's, else whole), and the same execution date (the assignment's planned date,
// else the batch's, else the obligation's own due date). Process integrity is the module that
// already reads obligation, completion, protocol and herd tables to serve vaccination
// cross-module; this read adds no table it does not already name.
//
// READ-ONLY and REPORTING-ONLY: nothing here schedules, submits or verifies a dose.

// ErrSubtasksNeedPool is returned when the source was built without a database pool: the
// wrapped row read goes through the process-integrity repository, but the per-animal drill
// is this source's own SQL and needs a connection.
var ErrSubtasksNeedPool = fmt.Errorf("vaccination boardsource: subtasks need a database pool")

// WithPool enables the per-animal subtask read.
func (s *Source) WithPool(pool *pgxpool.Pool, timeout time.Duration) *Source {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	s.pool = pool
	s.timeout = timeout
	return s
}

// vaccinationSubtaskRankSQL is the SQL twin of domain.RankFor, stated once.
//
//	latest completion rejected, or obligation missed   -> 0 needs attention
//	completion accepted, or obligation completed       -> 4 done
//	completion recorded and not yet verified           -> 3 in review
//	obligation in_progress                             -> 2 in progress
//	scheduled / due / deferred / waived                -> 1 to do
const vaccinationSubtaskRankSQL = `CASE
  WHEN u.completion_status = 'rejected' OR u.obligation_status = 'missed' THEN 0
  WHEN u.completion_status = 'accepted' OR u.obligation_status = 'completed' THEN 4
  WHEN u.completion_status = 'recorded' THEN 3
  WHEN u.obligation_status = 'in_progress' THEN 2
  ELSE 1
END`

// projection-review: membership=obligation_instances rows of ONE tenant with target_type goat in the process-integrity status set, bound either to ONE batch and rule (obligation_instances_batch_idx on tenant_id, batch_id, status) and then narrowed to the row's pen (the animal's own shed else the obligation's shed scope, whose parent is the requested park), partition (the animal's partition else the assignment's else whole) and execution date (assignment planned date else batch planned date else the obligation's own due day), or to ONE obligation id (primary key) for an unbatched row; group_key=(tenant_id, obligation_id) so one animal's obligation is one subtask; join_cardinality=protocol_rules, protocol_versions, protocol_definitions, goats, goat_shed_partitions and obligation_batches on their primary keys (1:1), vaccination_drive_assignment_members on its unique (tenant_id, obligation_id) index (1:0..1) and its assignment on the primary key (1:1), the latest completion and the tag are LATERAL LIMIT 1 lookups, and workforce_members on its primary key (1:1), so nothing fans an obligation out and the window total counts each once; pagination=keyset on (rank, obligation id) ASC after ($10, $11) with LIMIT $12 and the whole count carried by count(*) OVER () computed before the keyset cut; scope=tenant_id, park (through the shed's parent), and the row keys parsed from the board row id.
const vaccinationSubtasksSQL = `
WITH members AS (
  SELECT oi.obligation_id, oi.status AS obligation_status, oi.target_id AS goat_id, oi.due_at,
         pr.dose_code, pd.name AS protocol_name,
         COALESCE(vda.operator_id, ob.conducted_by) AS operator_member_id,
         COALESCE(vda.planned_date, ob.planned_date, (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date) AS exec_date,
         CASE WHEN g.shed_id IS NOT NULL THEN g.shed_id WHEN oi.scope_type = 'shed' THEN oi.scope_id END AS shed_uuid,
         COALESCE(NULLIF(lower(btrim(COALESCE(gsp.partition_label, ''))), ''), NULLIF(lower(btrim(COALESCE(vda.partition_label, ''))), ''), 'whole') AS partition_key,
         COALESCE(g.display_id, '') AS display_id, COALESCE(g.management_stage, '') AS stage
  FROM obligation_instances oi
  JOIN protocol_rules pr ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
  JOIN protocol_versions pv ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  LEFT JOIN goats g ON g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id AND g.merged_into_goat_id IS NULL
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = g.shed_id
  LEFT JOIN vaccination_drive_assignment_members vdam ON vdam.tenant_id = oi.tenant_id AND vdam.obligation_id = oi.obligation_id
  LEFT JOIN vaccination_drive_assignments vda ON vda.tenant_id = vdam.tenant_id AND vda.assignment_id = vdam.assignment_id
  LEFT JOIN obligation_batches ob ON ob.tenant_id = oi.tenant_id AND ob.batch_id = oi.batch_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND (
      ($3::uuid IS NOT NULL AND oi.batch_id = $3::uuid AND oi.rule_id = $4::uuid)
      OR ($3::uuid IS NULL AND oi.obligation_id = $5::uuid)
    )
),
scoped AS (
  SELECT m.*
  FROM members m
  JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = m.shed_uuid AND shed.parent_location_id = $2::uuid
  WHERE $3::uuid IS NULL
     OR (m.shed_uuid = $6::uuid AND m.partition_key = $7::text AND m.exec_date = $8::date)
),
units AS (
  SELECT s.*, COALESCE(c.status, '') AS completion_status, COALESCE(c.rejection_reason, '') AS rejection_reason,
         c.administered_at, c.verified_at,
         COALESCE(tag.identifier_value, '') AS tag
  FROM scoped s
  LEFT JOIN LATERAL (
    SELECT vc.status, vc.rejection_reason, vc.administered_at, vc.verified_at
    FROM vaccination_completions vc
    WHERE vc.tenant_id = $1::uuid AND vc.obligation_id = s.obligation_id
    ORDER BY vc.created_at DESC, vc.completion_id DESC
    LIMIT 1
  ) c ON true
  LEFT JOIN LATERAL (
    SELECT gi.identifier_value
    FROM goat_identifiers gi
    WHERE gi.tenant_id = $1::uuid AND gi.goat_id = s.goat_id AND gi.status = 'active'
      AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
    ORDER BY gi.identifier_type, gi.identifier_id
    LIMIT 1
  ) tag ON true
),
ranked AS (
  SELECT u.*, ` + vaccinationSubtaskRankSQL + ` AS rank, count(*) OVER () AS total
  FROM units u
)
SELECT r.obligation_id::text, r.obligation_status, r.completion_status, r.rejection_reason,
       r.administered_at, r.verified_at, r.due_at, r.exec_date::text,
       r.dose_code, r.protocol_name, r.tag, r.display_id, r.stage, r.rank, r.total,
       COALESCE(m.user_id::text, ''), COALESCE(r.operator_member_id::text, ''), COALESCE(m.display_name, '')
FROM ranked r
LEFT JOIN workforce_members m ON m.tenant_id = $1::uuid AND m.workforce_member_id = r.operator_member_id
WHERE (r.rank, r.obligation_id::text) > ($9::int, $10::text)
ORDER BY r.rank, r.obligation_id::text
LIMIT $11`

// rowKeys are the process-integrity row id's parts.
type rowKeys struct {
	batchID, ruleID, obligationID, shedID, partition, date string
}

// parseRowID reads the two row id shapes the wrapped read emits:
//
//	batch:<b>:rule:<r>:protocol_version:<pv>:shed:<s>:partition:<label|whole>:date:<YYYY-MM-DD>
//	obligation:<id>
func parseRowID(rowID string) (rowKeys, bool) {
	rowID = strings.TrimSpace(rowID)
	if strings.HasPrefix(rowID, "obligation:") {
		id := strings.TrimPrefix(rowID, "obligation:")
		if id == "" {
			return rowKeys{}, false
		}
		return rowKeys{obligationID: id}, true
	}
	if !strings.HasPrefix(rowID, "batch:") {
		return rowKeys{}, false
	}
	// The partition label may itself contain ':'? It cannot -- shed_partitions labels are
	// words and digits -- but the date is always the last segment, so parse from the ends.
	parts := strings.Split(rowID, ":")
	// batch b rule r protocol_version pv shed s partition p date d => 12 fields when p has no ':'.
	if len(parts) < 12 || parts[0] != "batch" || parts[2] != "rule" || parts[4] != "protocol_version" || parts[6] != "shed" || parts[8] != "partition" || parts[len(parts)-2] != "date" {
		return rowKeys{}, false
	}
	partition := strings.Join(parts[9:len(parts)-2], ":")
	return rowKeys{
		batchID: parts[1], ruleID: parts[3], shedID: parts[7],
		partition: strings.ToLower(strings.TrimSpace(partition)), date: parts[len(parts)-1],
	}, true
}

// ListSubtasks implements ports.SubtaskSource.
func (s *Source) ListSubtasks(ctx context.Context, q ports.SubtaskQuery) (domain.SubtaskPage, error) {
	if s.pool == nil {
		return domain.SubtaskPage{}, ErrSubtasksNeedPool
	}
	keys, ok := parseRowID(q.SourceID)
	if !ok {
		// Not a row id this source emits: nothing to drill into.
		return domain.SubtaskPage{Subtasks: []domain.Subtask{}}, nil
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
	var batchID, ruleID, obligationID, shedID *string
	var date *string
	if keys.batchID != "" {
		batchID, ruleID, shedID, date = &keys.batchID, &keys.ruleID, &keys.shedID, &keys.date
	} else {
		obligationID = &keys.obligationID
	}
	partition := keys.partition
	if partition == "" {
		partition = "whole"
	}
	rows, err := s.pool.Query(ctx, vaccinationSubtasksSQL,
		q.TenantID, q.ParkID, batchID, ruleID, obligationID, shedID, partition, date, afterRank, afterID, limit+1)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("vaccination boardsource subtasks: %w", err)
	}
	defer rows.Close()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	for rows.Next() {
		st, total, err := scanVaccinationSubtask(rows)
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
		return domain.SubtaskPage{}, fmt.Errorf("vaccination boardsource subtasks rows: %w", err)
	}
	return page, nil
}

func scanVaccinationSubtask(rows pgx.Rows) (domain.Subtask, int, error) {
	var (
		obligationID, obligationStatus, completionStatus, rejectionReason string
		administeredAt, verifiedAt                                        *time.Time
		dueAt                                                             time.Time
		execDate, doseCode, protocolName, tag, displayID, stage           string
		rank, total                                                       int
		ownerUserID, ownerMemberID, ownerName                             string
	)
	if err := rows.Scan(&obligationID, &obligationStatus, &completionStatus, &rejectionReason,
		&administeredAt, &verifiedAt, &dueAt, &execDate, &doseCode, &protocolName, &tag, &displayID, &stage,
		&rank, &total, &ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Subtask{}, 0, fmt.Errorf("vaccination boardsource subtask scan: %w", err)
	}
	name := tag
	if name == "" {
		// An animal with no active tag is named by its display id: a fallback, never an
		// identity, and never a uuid.
		name = displayID
	}
	if name == "" {
		name = "Untagged animal"
	}
	// The dose label composes through the single backend-owned label source; the raw dose
	// code and the protocol family name never reach the screen.
	subtitle := pidomain.ControlTowerDoseLabel(protocolName, doseCode)
	if stage != "" {
		subtitle += " · " + humanizeStage(stage)
	}

	vaccinate := domain.Step{Name: "Vaccinate", State: domain.StepTodo, Detail: "Due " + biztime.FarmDateFromBusinessDate(execDate)}
	verify := domain.Step{Name: "Verify", State: domain.StepLocked}
	state := domain.WorkStateDue
	attention := false
	switch completionStatus {
	case "accepted":
		vaccinate.State = domain.StepDone
		verify.State = domain.StepDone
		state = domain.WorkStateCompleted
	case "recorded":
		vaccinate.State = domain.StepDone
		verify.State = domain.StepInReview
		state = domain.WorkStateVerificationPending
	case "rejected":
		vaccinate.State = domain.StepRework
		verify.State, verify.Detail = domain.StepRework, rejectionReason
		state, attention = domain.WorkStateRejected, true
	default:
		switch obligationStatus {
		case "completed":
			vaccinate.State, verify.State = domain.StepDone, domain.StepDone
			state = domain.WorkStateCompleted
		case "in_progress":
			vaccinate.State = domain.StepInProgress
			state = domain.WorkStateInProgress
		case "missed":
			vaccinate.State, vaccinate.Detail = domain.StepNeedsAttention, "Missed"
			state, attention = domain.WorkStateMissed, true
		case "deferred":
			vaccinate.State, vaccinate.Detail = domain.StepLocked, "Deferred for recovery"
			state = domain.WorkStateDeferred
		case "waived":
			vaccinate.State, vaccinate.Detail = domain.StepLocked, "Waived"
			state = domain.WorkStateDeferred
		}
	}
	if administeredAt != nil && vaccinate.State == domain.StepDone {
		vaccinate.Detail = "Given " + biztime.FarmDate(*administeredAt)
	}
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, obligationID), Name: name, Subtitle: subtitle,
		WorkState: state, NeedsAttention: attention,
		Owner: domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName},
		Steps: []domain.Step{vaccinate, verify},
	}.Finalize(), total, nil
}

// humanizeStage spaces and capitalises a management stage token ("non_pregnant") for a
// screen. Stages are farm vocabulary already; this only tidies the separator.
func humanizeStage(stage string) string {
	words := strings.Fields(strings.ReplaceAll(strings.TrimSpace(stage), "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
