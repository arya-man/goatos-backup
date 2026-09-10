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
// names only its subject animal, and resolving that animal's park means reading `goats`,
// which this source may not do. Death requests are therefore NOT on the board today; they
// stay on /approvals. Putting them here needs the counts module to snapshot the park onto
// the request at raise time (the 000123 shape), which is a counts decision, not a board one.
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
END`

const approvalShedSQL = `CASE a.request_type
  WHEN 'shifting' THEN a.payload->>'destination_shed_id'
  WHEN 'birth' THEN a.payload->>'shed_id'
END`

const approvalPartitionSQL = `CASE a.request_type
  WHEN 'shifting' THEN a.payload->>'destination_partition_label'
  WHEN 'birth' THEN a.payload->>'partition_label'
END`

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
// predicate (a payload read, unindexed by design) filters inside that slice. request_type is
// restricted to the two kinds whose payload carries a park; a death can never match.
//
// The owner filter: no row is owned, so an owner lens selects nothing here. An approver's
// own board never lists the pool as "mine"; a raiser's board does not either -- raising a
// request is not owning the work of deciding it.
const approvalBaseWhere = `
  a.tenant_id = $1::uuid
  AND a.status = ANY(ARRAY['pending','approved','rejected'])
  AND a.request_type = ANY(ARRAY['birth','shifting'])
  AND a.raised_at >= $3::timestamptz AND a.raised_at < $4::timestamptz
  AND ` + approvalParkSQL + ` = $2::text
  AND $5::uuid IS NULL`

const approvalListSQL = `
WITH reqs AS (
  SELECT a.approval_request_id, a.request_type, a.status, a.raised_by_user_id, a.raised_at,
         ` + approvalShedSQL + ` AS shed_text,
         COALESCE(` + approvalPartitionSQL + `, '') AS partition_label,
         CASE WHEN jsonb_typeof(a.payload->'goat_ids') = 'array' THEN jsonb_array_length(a.payload->'goat_ids') ELSE 0 END AS animal_count,
         ` + approvalWorkStateSQL + ` AS board_state
  FROM counts_approval_requests a
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

const approvalCountSQL = `
SELECT board_state, count(*)
FROM (
  SELECT ` + approvalWorkStateSQL + ` AS board_state
  FROM counts_approval_requests a
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
