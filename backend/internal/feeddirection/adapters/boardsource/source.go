// Package boardsource is Feed's contribution to the cross-module Work Board. Feed shows ONE
// aggregate card per park per ACTIVITY -- packing, direction (distribution), transport and
// wastage -- and the sheds are listed INSIDE the card as its subtasks (maintainer decision
// 2026-09-12). It lives INSIDE the feeddirection package so the isolation rule holds in both
// directions: feed reads only its own tables here, and the board never reads a feed table.
//
// READ-ONLY and REPORTING-ONLY. Nothing here materializes, submits, verifies or reworks feed
// work. The mapping from a row status to a board work state, and the roll-up of a park's
// sheds to one card, are the ONLY business meaning this file adds.
//
// THE WORK DAY, NOT THE SERVE DAY (maintainer decision 2026-09-12). A board date D shows the
// feed videos DUE TO BE FILMED on D, wherever they are eaten: direction and wastage are filmed
// on the serve day (target_date = D); packing and transport are filmed the day BEFORE the
// serve, so they are the rows whose serve day is D+1 (packing target_date = D+1; a transport
// task's business_date is already the staging day = D). One card therefore gathers "what the
// crew should film today", never "what is eaten today".
//
// THE CARD SITS WHERE THE WORK IS. A card holds many sheds in different states; it lands in
// the LEFTMOST lane that still has a shed -- To do if any shed is unstarted, else In progress,
// else In review, else Done -- so the board always points at outstanding feed work. Each
// shed's own state is carried on its subtask line inside the card.
package boardsource

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// SourceType is the ref type carried on every feed board row. The source id names the
// activity (packing / direction / transport / wastage), one card per park per activity.
const SourceType = "feed_activity"

// activity is one feed card's spec: its stable source id, its keyset rank (display and paging
// order), its farm-worded title and clock, and the SQL that lists its per-shed units for the
// bound tenant/date/park. Every unitsSQL selects exactly (shed_id, st, owner_id) and binds
// $1 tenant, $2 board date, $3 park, $4 owner-or-null.
type activity struct {
	key   string
	rank  int
	title string
	clock string
	units string
}

// laneRankExpr is the SQL twin of domain.LaneFor over a unit's work state: 3 Done, 2 In
// review, 1 In progress, 0 To do. A shed's rank is the MIN over its units (leftmost lane), and
// a card's rank is the MIN over its sheds, so "where the work is" is one MIN chained twice.
const laneRankExpr = `CASE st
  WHEN 'completed' THEN 3
  WHEN 'verification_pending' THEN 2
  WHEN 'rejected' THEN 1
  WHEN 'in_progress' THEN 1
  WHEN 'proof_pending' THEN 1
  WHEN 'blocked' THEN 1
  ELSE 0 END`

// transportUnits lists one unit per transport task (one trip per physical shed per day),
// excluding retired tasks. The owner is the task's operator or, once filmed, the current
// attempt's operator (SubmitTransport records it on the attempt).
const transportUnits = `
  SELECT t.shed_id AS shed_id,
    CASE
      WHEN t.status = 'completed' THEN 'completed'
      WHEN t.status = 'verification_due' THEN 'verification_pending'
      WHEN t.status = 'rework' THEN 'rejected'
      ELSE 'due'
    END AS st,
    COALESCE(t.operator_id, att.operator_id) AS owner_id
  FROM feed_transport_tasks t
  LEFT JOIN feed_transport_attempts att
    ON att.tenant_id = t.tenant_id AND att.attempt_id = t.current_attempt_id
  WHERE t.tenant_id = $1::uuid AND t.business_date = $2::date AND t.park_id = $3::uuid
    AND t.status <> 'retired'
    AND ($4::uuid IS NULL OR COALESCE(t.operator_id, att.operator_id) = $4::uuid
         OR COALESCE(t.operator_id, att.operator_id) IS NULL)`

// sheetSessionUnits lists one unit per issued sheet pen/session/workflow and overlays the
// completion row when it exists. A missing completion row is real owed work: the pen appears as
// due instead of disappearing from the board.
func sheetSessionUnits(table, dateExpr string) string {
	return `
  WITH issued AS (
    SELECT DISTINCT r.shed_id, r.partition_key, r.session_no, r.workflow
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = i.tenant_id AND r.feed_direction_issue_id = i.feed_direction_issue_id
    WHERE i.tenant_id = $1::uuid AND i.park_id = $3::uuid AND i.feed_day = ` + dateExpr + `
      AND i.state IN ('issued', 'amended', 'locked')
      AND r.tenant_id = $1::uuid AND r.park_id = $3::uuid
  )
  SELECT i.shed_id AS shed_id,
    CASE
      WHEN c.status = 'completed' THEN 'completed'
      WHEN c.status = 'rework' THEN 'rejected'
      WHEN c.status = 'pending_verification' THEN 'verification_pending'
      ELSE 'due'
    END AS st,
    c.completed_by AS owner_id
  FROM issued i
  LEFT JOIN ` + table + ` c
    ON c.tenant_id = $1::uuid AND c.park_id = $3::uuid AND c.target_date = ` + dateExpr + `
   AND c.shed_id = i.shed_id AND c.partition_key = i.partition_key
   AND c.session_no = i.session_no AND c.workflow = i.workflow
  WHERE ($4::uuid IS NULL OR c.completed_by = $4::uuid OR c.completed_by IS NULL)`
}

// wastageUnits lists one unit per issued experiment pen and overlays the wastage completion when
// it exists. Wastage is one pen-day, not one session, so it rolls the issued experiment sheet to
// shed/partition before joining the completion table.
const wastageUnits = `
  WITH issued AS (
    SELECT DISTINCT r.shed_id, r.partition_key
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = i.tenant_id AND r.feed_direction_issue_id = i.feed_direction_issue_id
    WHERE i.tenant_id = $1::uuid AND i.park_id = $3::uuid AND i.feed_day = $2::date
      AND i.workflow = 'experiment' AND i.state IN ('issued', 'amended', 'locked')
      AND r.tenant_id = $1::uuid AND r.park_id = $3::uuid AND r.workflow = 'experiment'
  )
  SELECT i.shed_id AS shed_id,
    CASE
      WHEN c.status = 'completed' THEN 'completed'
      WHEN c.status = 'rework' THEN 'rejected'
      WHEN c.status = 'pending_verification' THEN 'verification_pending'
      ELSE 'due'
    END AS st,
    c.completed_by AS owner_id
  FROM issued i
  LEFT JOIN feed_wastage_completions c
    ON c.tenant_id = $1::uuid AND c.park_id = $3::uuid AND c.target_date = $2::date
   AND c.shed_id = i.shed_id AND c.partition_key = i.partition_key
   AND c.workflow = 'experiment'
  WHERE ($4::uuid IS NULL OR c.completed_by = $4::uuid OR c.completed_by IS NULL)`

// activities are the four feed cards, in display and keyset order.
//
// projection-review: membership=the activity's own rows for ONE tenant, park and work-day
// (transport feed_transport_tasks by business_date excluding retired, one row per shed per day by
// feed_transport_tasks_daily_shed_uq, and packing/direction/wastage by target_date one row per
// (shed, session, workflow) by each table's natural key); group_key=shed_id, units pre-aggregated
// to the shed by MIN(laneRank) and BOOL_OR(rejected) BEFORE the card MINs over sheds, so a pen
// with two sessions is one shed line and never fans the card's shed count; join_cardinality=
// feed_transport_attempts on the task's current_attempt_id (1:{0,1}) and the park name a scalar
// subquery on locations' primary key (1:1), so the card counts each shed once; pagination=at most
// four cards per park, ordered by activity rank and keyset after the activity key, never
// row-paged; scope=tenant_id($1), business_date($2), park_id($3) and the optional owner predicate
// ($4), repeated verbatim in the count and subtask reads.
var activities = []activity{
	{key: "packing", rank: 0, title: "Feed packing", clock: "Packed today for tomorrow", units: sheetSessionUnits("feed_packing_completions", "($2::date + 1)")},
	{key: "direction", rank: 1, title: "Feed direction", clock: "Served today", units: sheetSessionUnits("feed_distribution_completions", "$2::date")},
	{key: "transport", rank: 2, title: "Feed transport", clock: "Staged today by 15:00 for tomorrow", units: transportUnits},
	{key: "wastage", rank: 3, title: "Feed wastage", clock: "Measured today", units: wastageUnits},
}

func activityByKey(key string) (activity, bool) {
	for _, a := range activities {
		if a.key == key {
			return a, true
		}
	}
	return activity{}, false
}

func sourceID(parkID string, a activity) string {
	return parkID + ":" + a.key
}

func activityFromSourceID(id string) (activity, bool) {
	if _, key, ok := strings.Cut(id, ":"); ok {
		return activityByKey(key)
	}
	return activityByKey(id)
}

// metricsSQL rolls one activity's units up to its card: the shed count, done/pending/attention
// shed tallies, the card's lane rank (MIN over sheds, -1 when the card has no shed), whether
// the leftmost lane holds a rejected shed, and the park name.
//
// projection-review: membership=one activity's units for ONE tenant, park and work-day (the units
// SQL's own predicate); group_key=shed_id, units pre-aggregated to the shed by MIN(laneRank) and
// BOOL_OR(rejected) so a pen with several sessions is one shed row before the card counts;
// join_cardinality=no join in the roll-up (park name is a scalar subquery on locations' primary
// key, 1:1), so each shed is counted once; pagination=one aggregate row per call, never row-paged
// (the four cards are keyset-ordered by activity rank in ListRows); scope=tenant_id($1),
// business_date($2), park_id($3) and the optional owner predicate ($4).
func metricsSQL(units string) string {
	return `
WITH units AS (` + units + `),
shed AS (
  SELECT shed_id, MIN(` + laneRankExpr + `) AS lane_rank, BOOL_OR(st = 'rejected') AS any_rej
  FROM units GROUP BY shed_id
)
SELECT
  count(*)::int,
  count(*) FILTER (WHERE lane_rank = 3)::int,
  count(*) FILTER (WHERE lane_rank < 3)::int,
  count(*) FILTER (WHERE any_rej)::int,
  COALESCE(MIN(lane_rank), -1)::int,
  COALESCE(BOOL_OR(any_rej AND lane_rank = 1), false),
  (SELECT COALESCE(name, '') FROM locations WHERE tenant_id = $1::uuid AND location_id = $3::uuid)
FROM shed`
}

// Source implements ports.Source: feed's aggregate activity cards.
type Source struct {
	pool       *pgxpool.Pool
	timeout    time.Duration
	cacheMu    sync.Mutex
	cardCache  map[string]cachedCard
	cacheUntil func() time.Time
}

// New constructs the source.
func New(pool *pgxpool.Pool, timeout time.Duration) *Source {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Source{pool: pool, timeout: timeout, cardCache: map[string]cachedCard{}, cacheUntil: time.Now}
}

func (s *Source) Module() domain.Module { return domain.ModuleFeed }
func (s *Source) SourceType() string    { return SourceType }

type cardMetrics struct {
	sheds, done, pending, attention, cardRank int
	rejAtProgress                             bool
	parkName                                  string
}

type cachedCard struct {
	metrics cardMetrics
	expires time.Time
}

func (s *Source) readCard(ctx context.Context, a activity, q ports.SourceQuery) (cardMetrics, error) {
	key := strings.Join([]string{q.TenantID, q.ParkID, q.BusinessDate, q.OwnerUserID, a.key}, "\x00")
	now := s.cacheUntil()
	s.cacheMu.Lock()
	if cached, ok := s.cardCache[key]; ok && now.Before(cached.expires) {
		s.cacheMu.Unlock()
		return cached.metrics, nil
	}
	s.cacheMu.Unlock()
	var m cardMetrics
	err := s.pool.QueryRow(ctx, metricsSQL(a.units), q.TenantID, q.BusinessDate, q.ParkID, nullUUID(q.OwnerUserID)).
		Scan(&m.sheds, &m.done, &m.pending, &m.attention, &m.cardRank, &m.rejAtProgress, &m.parkName)
	if err != nil {
		return m, err
	}
	s.cacheMu.Lock()
	s.cardCache[key] = cachedCard{metrics: m, expires: now.Add(30 * time.Second)}
	if len(s.cardCache) > 256 {
		for k, cached := range s.cardCache {
			if !now.Before(cached.expires) {
				delete(s.cardCache, k)
			}
		}
	}
	s.cacheMu.Unlock()
	return m, err
}

// cardState maps a card's lane rank (MIN over sheds) back to one board work state.
func cardState(m cardMetrics) domain.WorkState {
	switch m.cardRank {
	case 3:
		return domain.WorkStateCompleted
	case 2:
		return domain.WorkStateVerificationPending
	case 1:
		if m.rejAtProgress {
			return domain.WorkStateRejected
		}
		return domain.WorkStateInProgress
	default:
		return domain.WorkStateDue
	}
}

func (s *Source) buildRow(a activity, q ports.SourceQuery, m cardMetrics) domain.Row {
	state := cardState(m)
	return domain.Row{
		Module: domain.ModuleFeed, SourceType: SourceType, SourceID: sourceID(q.ParkID, a),
		ParkID: q.ParkID, ParkName: m.parkName,
		BusinessDate: q.BusinessDate, ClockLabel: a.clock,
		WorkState: state, Severity: domain.SeverityOK,
		Title:    a.title,
		Subtitle: fmt.Sprintf("%d %s · %d done", m.sheds, pluralPens(m.sheds), m.done),
		Counts:   domain.Counts{Done: m.done, Pending: m.pending, NeedsAttention: m.attention},
		Href:     "/feed/analytics",
	}.Finalize()
}

func pluralPens(n int) string {
	if n == 1 {
		return "pen"
	}
	return "pens"
}

// ListRows implements ports.Source. It emits up to four cards, in activity rank order, after
// the keyset boundary and matching the state filter, each rolled up from its sheds.
func (s *Source) ListRows(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	afterRank := -1
	if q.AfterSourceID != "" {
		a, ok := activityFromSourceID(q.AfterSourceID)
		if !ok {
			return nil, domain.ErrInvalidCursor
		}
		afterRank = a.rank
	}
	limit := q.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}
	want := stateSet(q.WorkStates)
	out := make([]domain.Row, 0, len(activities))
	for _, a := range activities {
		if a.rank <= afterRank {
			continue
		}
		if len(out) >= limit {
			break
		}
		m, err := s.readCard(ctx, a, q)
		if err != nil {
			return nil, fmt.Errorf("feed boardsource list %s: %w", a.key, err)
		}
		if m.cardRank < 0 {
			continue // no sheds today for this activity -> no card
		}
		row := s.buildRow(a, q, m)
		if want != nil {
			if _, ok := want[row.WorkState]; !ok {
				continue
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// CountByState implements ports.Source: the whole-filter card count per work state.
func (s *Source) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	want := stateSet(q.WorkStates)
	out := map[domain.WorkState]int{}
	for _, a := range activities {
		m, err := s.readCard(ctx, a, q)
		if err != nil {
			return nil, fmt.Errorf("feed boardsource count %s: %w", a.key, err)
		}
		if m.cardRank < 0 {
			continue
		}
		st := cardState(m)
		if want != nil {
			if _, ok := want[st]; !ok {
				continue
			}
		}
		out[st]++
	}
	return out, nil
}

func nullUUID(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func stateSet(states []domain.WorkState) map[domain.WorkState]struct{} {
	if len(states) == 0 {
		return nil
	}
	out := make(map[domain.WorkState]struct{}, len(states))
	for _, s := range states {
		out[s] = struct{}{}
	}
	return out
}
