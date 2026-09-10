package boardsource

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// ApprovalsSourceType is the ref type carried on every counts approval board row.
const ApprovalsSourceType = "counts_approval_request"

// ApprovalsSource implements ports.Source over counts_approval_requests: one row per
// birth / pen-move request raised on the business day. The approver is a POOL by design
// (the per-person counts_approver grant), so no row ever names an owner; the raiser is
// carried in the subtitle instead.
//
// WHICH REQUESTS CAN REACH A PARK'S BOARD. counts_approval_requests carries no park column;
// the park lives in the payload -- `destination_park_id` on a shifting request and `park_id`
// on a birth (both validated by the raise handler before they are stored). A DEATH request
// names only its subject animal, so its park is the animal's own `goats.park_id`, read the
// way the counts module already reads it for the park-scoped death decision
// (Repository.ApprovalSubjectPark). That read-through is stable, not a snapshot risk: an
// animal never moves between parks (leaving a park is a terminal exit, maintainer decision
// 2026-07-19), and a death is that terminal exit. The pen is the animal's shed and partition
// at read time for the same reason. Without this the park's approver pool saw births and
// pen moves on the board but never a death (found by the 2026-09-10 E2E).
type ApprovalsSource struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewApprovals constructs the approvals source.
func NewApprovals(pool *pgxpool.Pool, timeout time.Duration) *ApprovalsSource {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &ApprovalsSource{pool: pool, timeout: timeout}
}

func (s *ApprovalsSource) Module() domain.Module { return domain.ModuleCounts }
func (s *ApprovalsSource) SourceType() string    { return ApprovalsSourceType }

// approvalWorkStateSQL is the one place an approval request becomes a board work state.
//
//	pending  -> due       (awaiting the approver pool)
//	approved -> completed (the decision landed and its effect committed with it)
//	rejected -> rejected  (turned down; the raiser is told why)
const approvalWorkStateSQL = `CASE a.status
  WHEN 'pending' THEN 'due'
  WHEN 'approved' THEN 'completed'
  WHEN 'rejected' THEN 'rejected'
  ELSE 'due'
END`

// approvalParkSQL is where a request's park lives in its payload, by request type. A death
// resolves to NULL and so never matches a park bound (see the type comment).
const approvalParkSQL = `CASE a.request_type
  WHEN 'shifting' THEN a.payload->>'destination_park_id'
  WHEN 'birth' THEN a.payload->>'park_id'
  WHEN 'death' THEN g.park_id::text
END`

const approvalShedSQL = `CASE a.request_type
  WHEN 'shifting' THEN a.payload->>'destination_shed_id'
  WHEN 'birth' THEN a.payload->>'shed_id'
  WHEN 'death' THEN g.shed_id::text
END`

const approvalPartitionSQL = `CASE a.request_type
  WHEN 'shifting' THEN a.payload->>'destination_partition_label'
  WHEN 'birth' THEN a.payload->>'partition_label'
  WHEN 'death' THEN gsp.partition_label
END`

// approvalFromSQL joins the subject animal a death names. Both joins are on the animal's
// primary key (goats: tenant_id, goat_id; goat_shed_partitions: tenant_id, goat_id), so a
// request never fans out; a birth or pen move has no subject animal and joins nothing.
const approvalFromSQL = `counts_approval_requests a
  LEFT JOIN goats g ON g.tenant_id = a.tenant_id AND g.goat_id = a.subject_goat_id
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = a.tenant_id AND gsp.goat_id = a.subject_goat_id`

// uuidTextRe guards the payload->uuid cast: a shed id in the payload is validated on raise,
// but a malformed one must degrade that row to a bare title, not fail the whole board read.
const uuidTextRe = `'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'`

// approvalBaseWhere binds every read to one tenant, one business day of raised_at and one
// park. The business day is a half-open [day start, next day start) instant range computed
// in Go from Asia/Kolkata, so the predicate stays SARGable on raised_at.
//
// Index: counts_approval_requests_status_queue_idx (tenant_id, status, request_type,
// raised_at DESC, approval_request_id DESC). tenant_id is an equality, status and
// request_type are bounded array conditions on the next two columns, and raised_at is then a
// range on the fourth -- so one tenant-day of requests is read from the index and the park
// predicate (a payload read for birth and pen move, the subject animal's own park for a
// death; unindexed by design) filters inside that slice.
//
// The owner filter: no row is owned, so an owner lens selects nothing here. An approver's
// own board never lists the pool as "mine"; a raiser's board does not either -- raising a
// request is not owning the work of deciding it.
const approvalBaseWhere = `
  a.tenant_id = $1::uuid
  AND a.status = ANY(ARRAY['pending','approved','rejected'])
  AND a.request_type = ANY(ARRAY['birth','shifting','death'])
  AND a.raised_at >= $3::timestamptz AND a.raised_at < $4::timestamptz
  AND ` + approvalParkSQL + ` = $2::text
  AND $5::uuid IS NULL`

// projection-review: membership=counts_approval_requests rows of ONE tenant whose park resolves to the requested park and whose created_at falls in the half-open IST business day, one row per request (primary key); group_key=(tenant_id, approval_request_id) for the list and the derived board_state for the count; join_cardinality=a death's subject animal resolves through goats and goat_shed_partitions on their primary key (tenant_id, goat_id; 1:1, absent for birth and pen move), the request's park/shed columns resolve through locations on their primary key (1:1) and the raiser through workforce_members on the partial-unique active (tenant_id,user_id) index (at most 1), so no join fans a request out; pagination=keyset on the request id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park, day range and the optional owner predicate, repeated verbatim in approvalCountSQL.
const approvalListSQL = `
WITH reqs AS (
  SELECT a.approval_request_id, a.request_type, a.status, a.raised_by_user_id, a.raised_at,
         ` + approvalShedSQL + ` AS shed_text,
         COALESCE(` + approvalPartitionSQL + `, '') AS partition_label,
         CASE WHEN jsonb_typeof(a.payload->'goat_ids') = 'array' THEN jsonb_array_length(a.payload->'goat_ids') ELSE 0 END AS animal_count,
         ` + approvalWorkStateSQL + ` AS board_state
  FROM ` + approvalFromSQL + `
  WHERE ` + approvalBaseWhere + `
    AND ($6::uuid IS NULL OR a.approval_request_id > $6::uuid)
)
SELECT r.approval_request_id::text, r.request_type, r.status, r.board_state, r.raised_at,
       COALESCE(park.name, ''),
       COALESCE(r.shed_text, ''), COALESCE(shed.name, ''), r.partition_label, r.animal_count,
       COALESCE((SELECT m.display_name FROM workforce_members m
                  WHERE m.tenant_id = $1::uuid AND m.user_id = r.raised_by_user_id
                  ORDER BY (m.status = 'active') DESC, m.workforce_member_id LIMIT 1), '')
FROM reqs r
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = $2::uuid
LEFT JOIN locations shed
  ON shed.tenant_id = $1::uuid
 AND shed.location_id = CASE WHEN r.shed_text ~ ` + uuidTextRe + ` THEN r.shed_text::uuid END
WHERE ($7::text[] IS NULL OR r.board_state = ANY($7::text[]))
ORDER BY r.approval_request_id
LIMIT $8`

// projection-review: membership=counts_approval_requests rows of ONE tenant whose park resolves to the requested park and whose created_at falls in the half-open IST business day, one row per request (primary key); group_key=(tenant_id, approval_request_id) (the count query groups by the SAME derived board_state over the SAME membership); join_cardinality=a death's subject animal resolves through goats and goat_shed_partitions on their primary key (tenant_id, goat_id; 1:1, absent for birth and pen move), the request's park/shed columns resolve through locations on their primary key (1:1) and the raiser through workforce_members on the partial-unique active (tenant_id,user_id) index (at most 1), so no join fans a request out; pagination=keyset on the request id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park, day range and the optional owner predicate, repeated verbatim in approvalCountSQL.
const approvalCountSQL = `
SELECT board_state, count(*)
FROM (
  SELECT ` + approvalWorkStateSQL + ` AS board_state
  FROM ` + approvalFromSQL + `
  WHERE ` + approvalBaseWhere + `
) x
WHERE ($6::text[] IS NULL OR board_state = ANY($6::text[]))
GROUP BY board_state`

// businessDayBounds returns the [start, next start) instants of a YYYY-MM-DD business date in
// the operational calendar.
func businessDayBounds(businessDate string) (time.Time, time.Time, error) {
	day, err := time.ParseInLocation("2006-01-02", businessDate, biztime.DefaultLocation())
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("counts boardsource: %w", domain.ErrInvalidQuery)
	}
	start := biztime.BusinessDayStart(day)
	return start, start.AddDate(0, 0, 1), nil
}

// ListRows implements ports.Source.
func (s *ApprovalsSource) ListRows(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	start, end, err := businessDayBounds(q.BusinessDate)
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}
	rows, err := s.pool.Query(ctx, approvalListSQL,
		q.TenantID, q.ParkID, start, end, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), statesArg(q.WorkStates), limit)
	if err != nil {
		return nil, fmt.Errorf("counts boardsource list: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Row, 0, limit)
	for rows.Next() {
		r, err := scanApprovalRow(rows, q.ParkID)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("counts boardsource list rows: %w", err)
	}
	return out, nil
}

// CountByState implements ports.Source.
func (s *ApprovalsSource) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	start, end, err := businessDayBounds(q.BusinessDate)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, approvalCountSQL, q.TenantID, q.ParkID, start, end, nullUUID(q.OwnerUserID), statesArg(q.WorkStates))
	if err != nil {
		return nil, fmt.Errorf("counts boardsource count: %w", err)
	}
	defer rows.Close()
	return scanCounts(rows, "counts boardsource count")
}

// approvalTypeLabel is the farm word for each request type. "Pen move", never "Shifting" or
// "Shed move" (pen-not-shed vocabulary lock).
func approvalTypeLabel(requestType string) string {
	switch requestType {
	case "birth":
		return "Birth"
	case "death":
		return "Death"
	case "shifting":
		return "Pen move"
	}
	return "Request"
}

func scanApprovalRow(rows pgx.Rows, parkID string) (domain.Row, error) {
	var (
		requestID, requestType, status, boardState string
		raisedAt                                   time.Time
		parkName, shedID, shedName, partitionLabel string
		animalCount                                int
		raisedByName                               string
	)
	if err := rows.Scan(&requestID, &requestType, &status, &boardState, &raisedAt,
		&parkName, &shedID, &shedName, &partitionLabel, &animalCount, &raisedByName); err != nil {
		return domain.Row{}, fmt.Errorf("counts boardsource scan: %w", err)
	}
	loc := oploc.OperationalLocation{ParkID: parkID, ParkName: parkName, ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel}
	pen := domain.Pen{ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel, Display: loc.Display()}
	title := approvalTypeLabel(requestType)
	if pen.Display != "" {
		title += " · " + pen.Display
	}
	// The raiser's name is carried in copy, not as the owner: a fact whose name cannot be
	// resolved is DROPPED from the line rather than rendered as an id.
	subtitle := ""
	if raisedByName != "" {
		subtitle = "Raised by " + raisedByName
	}
	if requestType == "shifting" && animalCount > 0 {
		animals := strconv.Itoa(animalCount) + " animals"
		if animalCount == 1 {
			animals = "1 animal"
		}
		if subtitle != "" {
			subtitle += " · "
		}
		subtitle += animals
	}
	raised := raisedAt.In(biztime.DefaultLocation())

	state := domain.WorkState(boardState)
	counts := domain.Counts{}
	severity := domain.SeverityOK
	switch state {
	case domain.WorkStateCompleted:
		counts.Done = 1
	case domain.WorkStateRejected:
		counts.Pending = 1
		counts.NeedsAttention = 1
		severity = domain.SeverityWatch
	default:
		counts.Pending = 1
	}
	return domain.Row{
		Module: domain.ModuleCounts, SourceType: ApprovalsSourceType, SourceID: requestID,
		ParkID: parkID, ParkName: parkName, Pen: pen,
		BusinessDate: biztime.BusinessDate(raisedAt), ClockLabel: "Raised " + raised.Format("15:04"),
		WorkState: state, Severity: severity,
		Owner: domain.Owner{}, OwnerState: domain.OwnerStatePool,
		Title: title, Subtitle: subtitle, Counts: counts,
	}.Finalize(), nil
}
