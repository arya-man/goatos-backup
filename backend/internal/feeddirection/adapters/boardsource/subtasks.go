package boardsource

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Subtasks of a feed activity card are its PENS, one line each, worst first: the shed's own
// roll-up state (the leftmost lane across that shed's bags/sessions) carried as a subtask so a
// reader opens "Feed packing" and sees which pens are done, in review, sent back or owed.
//
// READ-ONLY and REPORTING-ONLY: nothing here submits, verifies or reworks feed work.
//
// The subtask rank is the SQL twin of the worst-first order: a shed with any rejected bag is
// needs-attention (0), then to do (1), in progress (2), in review (3), done (4). It is the
// leading segment of the keyset so the page is a plain ascending sort on (rank, shed_id).
const shedSubtaskRankExpr = `CASE
  WHEN s.any_rej THEN 0
  WHEN s.lane_rank = 0 THEN 1
  WHEN s.lane_rank = 1 THEN 2
  WHEN s.lane_rank = 2 THEN 3
  ELSE 4 END`

// subtasksSQL builds one activity's per-shed page. It reuses the activity's own units SQL with
// the owner bind ($4) left NULL -- the drawer lists every pen of the card, not just the
// viewer's -- rolls the units up to the shed, ranks each shed, and keyset-pages on
// (rank, shed_id). The whole shed count rides count(*) OVER () before the keyset cut.
//
// projection-review: membership=the activity's rows for ONE tenant, park and work-day (the SAME
// predicate metricsSQL binds); group_key=(shed_id, partition_key) the PEN, pre-aggregated by MIN(laneRank)/BOOL_OR before
// ranking so a pen with several sessions is one line; join_cardinality=locations on its primary
// key (1:1), no fan-out; pagination=keyset on (rank, pen_key) ASC after ($5,$6) with LIMIT $7,
// total by count(*) OVER () computed before the cut; scope=tenant_id($1), business_date($2),
// park_id($3), owner NULL($4).
func subtasksSQL(units string) string {
	return `
WITH units AS (` + units + `),
pen AS (
  SELECT shed_id, partition_key,
         (ARRAY_AGG(partition_label ORDER BY (partition_label = lower(partition_label)), partition_label)
          FILTER (WHERE partition_label <> ''))[1] AS partition_label,
         MIN(` + laneRankExpr + `) AS lane_rank, BOOL_OR(st = 'rejected') AS any_rej
  FROM units GROUP BY shed_id, partition_key
),
ranked AS (
  SELECT s.shed_id, COALESCE(s.partition_label, '') AS partition_label, s.partition_key, s.lane_rank, s.any_rej, ` + shedSubtaskRankExpr + ` AS rank,
         s.shed_id::text || '|' || s.partition_key AS pen_key,
         count(*) OVER () AS total
  FROM pen s
)
SELECT r.shed_id::text, r.partition_label, r.lane_rank, r.any_rej, r.rank, r.pen_key, r.total, COALESCE(loc.name, '')
FROM ranked r
LEFT JOIN locations loc ON loc.tenant_id = $1::uuid AND loc.location_id = r.shed_id
WHERE (r.rank, r.pen_key) > ($5::int, $6::text)
ORDER BY r.rank, r.pen_key
LIMIT $7`
}

// ListSubtasks implements ports.SubtaskSource. The row is named by its source id (the activity
// key); an unknown key or a day/park with no rows returns an empty page with Total 0.
func (s *Source) ListSubtasks(ctx context.Context, q ports.SubtaskQuery) (domain.SubtaskPage, error) {
	a, ok := activityFromSourceID(q.SourceID)
	if !ok {
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
	rows, err := s.pool.Query(ctx, subtasksSQL(a.units),
		q.TenantID, q.BusinessDate, q.ParkID, nullUUID(q.OwnerUserID), afterRank, afterID, limit+1)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("feed boardsource subtasks %s: %w", a.key, err)
	}
	defer rows.Close()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	for rows.Next() {
		st, total, err := scanShedSubtask(rows)
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
		return domain.SubtaskPage{}, fmt.Errorf("feed boardsource subtasks %s rows: %w", a.key, err)
	}
	return page, nil
}

func scanShedSubtask(rows pgx.Rows) (domain.Subtask, int, error) {
	var (
		shedID, partitionLabel, penKey, shedName string
		laneRank, rank, n                        int
		anyRej                                   bool
	)
	if err := rows.Scan(&shedID, &partitionLabel, &laneRank, &anyRej, &rank, &penKey, &n, &shedName); err != nil {
		return domain.Subtask{}, 0, fmt.Errorf("feed boardsource subtask scan: %w", err)
	}
	film := domain.Step{Name: "Film", State: domain.StepTodo}
	verify := domain.Step{Name: "Verify", State: domain.StepLocked}
	state := domain.WorkStateDue
	subtitle := "Not filmed yet"
	switch {
	case laneRank == 3:
		film.State, verify.State = domain.StepDone, domain.StepDone
		state, subtitle = domain.WorkStateCompleted, "Approved"
	case laneRank == 2:
		film.State, verify.State = domain.StepDone, domain.StepInReview
		state, subtitle = domain.WorkStateVerificationPending, "In review"
	case anyRej:
		film.State, verify.State, verify.Detail = domain.StepDone, domain.StepRework, "Sent back"
		state, subtitle = domain.WorkStateRejected, "Sent back"
	case laneRank == 1:
		film.State = domain.StepInProgress
		state, subtitle = domain.WorkStateInProgress, "Started"
	}
	// Pen name is shed + partition via the canonical helper ("Castro 1", "Godel 1 - Part 3",
	// "Yashoda"); never a bare shed base when the pen has a partition (maintainer report 2026-09-14).
	name := oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display()
	if name == "" {
		name = "Pen"
	}
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, penKey), Name: name, Subtitle: subtitle,
		WorkState: state, NeedsAttention: anyRej,
		Steps: []domain.Step{film, verify},
	}.Finalize(), n, nil
}
