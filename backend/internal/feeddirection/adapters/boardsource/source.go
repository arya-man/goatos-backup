// Package boardsource is Feed's contribution to the cross-module Work Board. Feed shows its work
// as ACTIVITY CARDS per park -- packing, direction, transport and wastage -- and the pens are
// listed INSIDE each card as its subtasks (maintainer decision 2026-09-12). It lives INSIDE the
// feeddirection package so the isolation rule holds in both directions: feed reads only its own
// tables here, and the board never reads a feed table.
//
// FOUR ACTIVITIES, DIRECTION ONE CARD PER SESSION (maintainer instruction 2026-09-25: "no need
// per pen or per shed; direction, wastage, packing, transport, four cards only; for feed
// direction, morning and evening separately"). Feed direction is served in sessions and each
// session's bag is filmed and verified on its own, so direction shows one card per session of the
// day's sheet, named by the sheet's own session label ("Feed direction · Morning"). Packing,
// transport and wastage stay one card each. The feed videos do NOT also appear as Verification
// cards (verification/adapters/boardsource leaves source_module 'feed' out): a card's review
// state is carried here, on the card and on its pens, so one piece of feed work is counted once.
//
// EVERY CARD SAYS WHERE EACH PEN IS. A pen is one of: approved; in review (handed in, waiting for
// the verifier); sent back; started (some of its bags filmed, not all); not filmed. The card's
// counts carry all five (done / in_review / needs_attention / not_started, the rest started),
// every surface renders them as the card's count line, and the drawer lists the pens from the
// same pen roll-up, so the card and its drawer can never disagree. The old card said "0/59 done · 59 started" while its
// drawer showed 11 pens not filmed, because it called every unapproved pen started.
//
// READ-ONLY and REPORTING-ONLY. Nothing here materializes, submits, verifies or reworks feed
// work. The mapping from a row status to a board work state, and the roll-up of pens to a card,
// are the ONLY business meaning this file adds.
//
// THE WORK DAY, NOT THE SERVE DAY (maintainer decision 2026-09-12). A board date D shows the
// feed videos DUE TO BE FILMED on D, wherever they are eaten: direction and wastage are filmed
// on the serve day (target_date = D); packing and transport are filmed the day BEFORE the
// serve, so they are the rows whose serve day is D+1 (packing target_date = D+1; a transport
// task's business_date is already the staging day = D).
//
// THE CARD SITS WHERE THE WORK IS (maintainer decision 2026-09-14):
//
//	To do        no pen has started
//	In progress  any pen is in progress or sent back, OR the pens are mixed (some started,
//	             some not) -- work on the card has begun and is not all handed in
//	In review    EVERY pen is handed in and at least one is still with the verifier
//	Done         EVERY pen is completed
package boardsource

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// SourceType is the ref type carried on every feed board row.
const SourceType = "feed_activity"

// activity is one kind of feed card: its stable key, its keyset rank (display and paging order),
// its farm-worded title and clock, whether it splits into one card per sheet session, and the SQL
// that lists its units for the bound tenant/date/park. Every units SQL selects exactly
// (shed_id, partition_key, partition_label, session_no, session_label, st, owner_id) and binds
// $1 tenant, $2 board date, $3 park.
//
// NO OWNER SCOPE (maintainer review 2026-09-25). A feed card is the park crew's work for the day,
// not one person's: filtered to a person, it used to keep only the pens that person filmed plus
// the unfilmed ones, so a card the crew had half done dropped back to To do under "Naveen". The
// card now reads the same under every lens -- an operator's own board and a director's person
// filter alike -- and the owner filter keeps it because it is a pool card.
type activity struct {
	key        string
	rank       int
	title      string
	clock      string
	perSession bool
	units      string
}

// laneRankExpr is the SQL twin of domain.LaneFor over a unit's work state: 3 Done, 2 In
// review, 1 In progress, 0 To do. Units roll up to a pen and pens to a card by rollupRankExpr,
// applied twice.
const laneRankExpr = `CASE st
  WHEN 'completed' THEN 3
  WHEN 'verification_pending' THEN 2
  WHEN 'rejected' THEN 1
  WHEN 'in_progress' THEN 1
  WHEN 'proof_pending' THEN 1
  WHEN 'blocked' THEN 1
  ELSE 0 END`

// rollupRankExpr folds a group's member ranks (MIN and MAX of laneRankExpr) into the group's own
// rank under the 2026-09-14 rule: all done -> 3; everything handed in with one still in review
// -> 2; nothing started -> 0; anything else (a member in progress, or a mix of started and
// unstarted) -> 1. The same expression rolls units to a pen and pens to a card.
func rollupRankExpr(minCol, maxCol string) string {
	return `CASE
  WHEN ` + minCol + ` = 3 THEN 3
  WHEN ` + minCol + ` >= 2 THEN 2
  WHEN ` + maxCol + ` = 0 THEN 0
  ELSE 1 END`
}

// cardUnitsCTE wraps an activity's units with the CARD they belong to: the session number for a
// per-session activity (direction), 0 for the others.
func cardUnitsCTE(a activity) string {
	cardNo := "0"
	if a.perSession {
		cardNo = "u.session_no"
	}
	return `raw AS (` + a.units + `),
units AS (SELECT u.*, ` + cardNo + ` AS card_no FROM raw u)`
}

// penCTE rolls one activity's units up to pens keyed by (card_no, shed_id, partition_key): the
// pen's rank by rollupRankExpr, whether any bag was sent back, the human partition label (the
// first non-empty one, capitalised spelling preferred) and the session label. Shared by the card
// metrics and the subtask list so both read one number.
var penCTE = `pen AS (
  SELECT card_no, shed_id, partition_key,
         (ARRAY_AGG(partition_label ORDER BY (partition_label = lower(partition_label)), partition_label)
          FILTER (WHERE partition_label <> ''))[1] AS partition_label,
         (ARRAY_AGG(session_label ORDER BY session_label) FILTER (WHERE session_label <> ''))[1] AS session_label,
         ` + rollupRankExpr("MIN("+laneRankExpr+")", "MAX("+laneRankExpr+")") + ` AS lane_rank,
         BOOL_OR(st = 'rejected') AS any_rej
  FROM units GROUP BY card_no, shed_id, partition_key
)`

// transportUnits lists one unit per transport task (one trip per physical shed per day),
// excluding retired tasks. The owner is the task's operator or, once filmed, the current
// attempt's operator (SubmitTransport records it on the attempt).
const transportUnits = `
  SELECT t.shed_id AS shed_id,
    'whole' AS partition_key,
    COALESCE(t.partition_label, '') AS partition_label,
    0 AS session_no,
    '' AS session_label,
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
    AND t.status <> 'retired'`

// sheetSessionUnits lists one unit per issued sheet pen/session/workflow and overlays the
// completion row when it exists. A missing completion row is real owed work: the pen appears as
// due instead of disappearing from the board.
func sheetSessionUnits(table, dateExpr string) string {
	return `
  WITH issued AS (
    SELECT r.shed_id, r.partition_key, COALESCE(r.partition_label, '') AS partition_label, r.session_no,
           MAX(COALESCE(r.session_label, '')) AS session_label, r.workflow
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = i.tenant_id AND r.feed_direction_issue_id = i.feed_direction_issue_id
    WHERE i.tenant_id = $1::uuid AND i.park_id = $3::uuid AND i.feed_day = ` + dateExpr + `
      AND i.state IN ('issued', 'amended', 'locked')
      AND r.tenant_id = $1::uuid AND r.park_id = $3::uuid
    GROUP BY r.shed_id, r.partition_key, COALESCE(r.partition_label, ''), r.session_no, r.workflow
  )
  SELECT i.shed_id AS shed_id,
    i.partition_key AS partition_key,
    i.partition_label AS partition_label,
    i.session_no AS session_no,
    i.session_label AS session_label,
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
   AND c.session_no = i.session_no AND c.workflow = i.workflow`
}

// wastageUnits lists one unit per issued experiment pen and overlays the wastage completion when
// it exists. Wastage is one pen-day, not one session, so it rolls the issued experiment sheet to
// shed/partition before joining the completion table.
const wastageUnits = `
  WITH issued AS (
    SELECT DISTINCT r.shed_id, r.partition_key, COALESCE(r.partition_label, '') AS partition_label
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = i.tenant_id AND r.feed_direction_issue_id = i.feed_direction_issue_id
    WHERE i.tenant_id = $1::uuid AND i.park_id = $3::uuid AND i.feed_day = $2::date
      AND i.workflow = 'experiment' AND i.state IN ('issued', 'amended', 'locked')
      AND r.tenant_id = $1::uuid AND r.park_id = $3::uuid AND r.workflow = 'experiment'
  )
  SELECT i.shed_id AS shed_id,
    i.partition_key AS partition_key,
    i.partition_label AS partition_label,
    0 AS session_no,
    '' AS session_label,
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
   AND c.workflow = 'experiment'`

// activities are the four kinds of feed card, in display and keyset order.
//
// projection-review: membership=the activity's own rows for ONE tenant, park and work-day
// (transport feed_transport_tasks by business_date excluding retired, one row per shed per day by
// feed_transport_tasks_daily_shed_uq, and packing/direction/wastage by target_date one row per
// (shed, session, workflow) by each table's natural key); group_key=(card_no, shed_id, partition_key)
// the PEN of one card (card_no is the session for direction, 0 otherwise): units pre-aggregated to
// the pen by rollupRankExpr and BOOL_OR(rejected) BEFORE the card rolls up over pens, so a pen
// with two bags is one pen line and never fans the card's pen count; join_cardinality=
// feed_transport_attempts on the task's current_attempt_id (1:{0,1}), the issued sheet rows
// grouped to (pen, session, workflow) before the completion join (1:{0,1} on its natural key), and
// the park name a scalar subquery on locations' primary key (1:1), so the card counts each pen
// once; pagination=at most one card per activity per session per park, ordered by (activity rank,
// session) and keyset after that pair, never row-paged; scope=tenant_id($1), business_date($2),
// park_id($3), repeated verbatim in the count and subtask reads; no owner predicate (a pool card).
var activities = []activity{
	{key: "packing", rank: 0, title: "Feed packing", clock: "Packed today for tomorrow", units: sheetSessionUnits("feed_packing_completions", "($2::date + 1)")},
	{key: "direction", rank: 1, title: "Feed direction", clock: "Served today", perSession: true, units: sheetSessionUnits("feed_distribution_completions", "$2::date")},
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

// cardID names one card: its activity and, for a per-session activity, the session.
type cardID struct {
	activity activity
	cardNo   int
}

// less is the keyset order: activity rank, then session.
func (c cardID) less(o cardID) bool {
	if c.activity.rank != o.activity.rank {
		return c.activity.rank < o.activity.rank
	}
	return c.cardNo < o.cardNo
}

// sourceID is "<park>:<activity>" for a one-card activity and "<park>:<activity>:<session>" for a
// per-session one.
func sourceID(parkID string, c cardID) string {
	id := parkID + ":" + c.activity.key
	if c.activity.perSession {
		id += ":" + strconv.Itoa(c.cardNo)
	}
	return id
}

// parseSourceID reads a card id back ("<park>:<activity>[:<session>]"). A per-session activity
// must name its session; a one-card activity must not.
func parseSourceID(id string) (cardID, bool) {
	parts := strings.Split(id, ":")
	if len(parts) < 2 || parts[0] == "" {
		return cardID{}, false
	}
	parts = parts[1:] // drop the park
	a, ok := activityByKey(parts[0])
	if !ok {
		return cardID{}, false
	}
	if !a.perSession {
		if len(parts) != 1 {
			return cardID{}, false
		}
		return cardID{activity: a}, true
	}
	if len(parts) != 2 {
		return cardID{}, false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 0 {
		return cardID{}, false
	}
	return cardID{activity: a, cardNo: n}, true
}

// metricsSQL rolls one activity's units up to its cards -- ONE row per card (per session for
// direction), in session order: the card number, its session label, the pen count, the pens
// approved / in review / started / not filmed / sent back, the card's rank (rollupRankExpr over
// its pens), whether any pen was sent back, and the park name.
//
// projection-review: membership=one activity's units for ONE tenant, park and work-day (the units
// SQL's own predicate); group_key=(card_no, shed_id, partition_key) the PEN, then card_no the CARD:
// units pre-aggregated by rollupRankExpr and BOOL_OR(rejected) so a pen with several bags is one
// pen row before the card counts, and the five pen buckets are disjoint (approved lane 3, in review
// lane 2, sent back any_rej, started lane 1 without a rejection, not filmed lane 0) so they add up
// to the pen count; join_cardinality=no join in the roll-up (park name is a scalar subquery on
// locations' primary key, 1:1), so each pen is counted once; pagination=one aggregate row per card,
// never row-paged; scope=tenant_id($1), business_date($2), park_id($3); no owner predicate.
func metricsSQL(a activity) string {
	return `
WITH ` + cardUnitsCTE(a) + `,
` + penCTE + `
SELECT
  card_no,
  COALESCE((ARRAY_AGG(session_label ORDER BY session_label) FILTER (WHERE session_label <> ''))[1], ''),
  count(*)::int,
  count(*) FILTER (WHERE lane_rank = 3)::int,
  count(*) FILTER (WHERE lane_rank = 2)::int,
  count(*) FILTER (WHERE lane_rank = 1 AND NOT any_rej)::int,
  count(*) FILTER (WHERE lane_rank = 0)::int,
  count(*) FILTER (WHERE any_rej)::int,
  ` + rollupRankExpr("MIN(lane_rank)", "MAX(lane_rank)") + `::int,
  COALESCE(BOOL_OR(any_rej), false),
  (SELECT name FROM locations WHERE tenant_id = $1::uuid AND location_id = $3::uuid)
FROM pen
GROUP BY card_no
ORDER BY card_no`
}

// Source implements ports.Source: feed's activity cards.
type Source struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// New constructs the source.
func New(pool *pgxpool.Pool, timeout time.Duration) *Source {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Source{pool: pool, timeout: timeout}
}

func (s *Source) Module() domain.Module { return domain.ModuleFeed }
func (s *Source) SourceType() string    { return SourceType }

type cardMetrics struct {
	id                                                           cardID
	sessionLabel                                                 string
	pens, approved, inReview, started, notFilmed, sentBack, rank int
	anyRejected                                                  bool
	parkName                                                     string
}

type cardReadKey struct {
	source             *Source
	tenant, park, date string
}

// readCards reads every feed card of a park-day as ONE pgx batch (one statement per activity):
// one pool connection and one round trip instead of four parallel statements. State and page
// filters are applied after metrics; every lane in the request shares the same
// tenant/park/day/owner facts, so the batch is memoized per request.
func (s *Source) readCards(ctx context.Context, q ports.SourceQuery) ([]cardMetrics, error) {
	key := cardReadKey{s, q.TenantID, q.ParkID, q.BusinessDate}
	return ports.RequestRead(ctx, key, func(ctx context.Context) ([]cardMetrics, error) {
		return s.readCardsFresh(ctx, q)
	})
}

// PrimeStatements implements ports.PrimingSource: the same activity statements readCardsFresh
// sends, handed to the board's first batch. The last reader seeds the request memo readCards
// looks up, so the whole set is seeded only when every activity was read.
func (s *Source) PrimeStatements(ctx context.Context, q ports.SourceQuery) ([]ports.Statement, error) {
	if !ports.HasRequestReadMemo(ctx) {
		return nil, nil
	}
	key := cardReadKey{s, q.TenantID, q.ParkID, q.BusinessDate}
	perActivity := make([][]cardMetrics, len(activities))
	stmts := make([]ports.Statement, 0, len(activities))
	unseedable := false
	for n, a := range activities {
		idx, a := n, a
		last := n == len(activities)-1
		stmts = append(stmts, ports.Statement{
			Query: sqlbind.MustBind(metricsSQL(a), q.TenantID, q.BusinessDate, q.ParkID),
			Read: func(rows ports.ResultRows) error {
				cards, seedable, err := scanCards(rows, a)
				if err != nil {
					return fmt.Errorf("%s: %w", a.key, err)
				}
				if !seedable {
					unseedable = true
				}
				perActivity[idx] = cards
				if last && !unseedable {
					// scale-guard:ignore: an in-memory memo store after the ONE shared batch's last activity was read; no round trip.
					ports.SeedRequestRead(ctx, key, flatten(perActivity))
				}
				return nil
			},
		})
	}
	return stmts, nil
}

// scanCards reads one activity's card rows. A NULL park name (the park's location row is
// missing) is a row readCardsFresh rejects: the prime reports it unseedable instead of failing
// the shared batch, and readCards then reads (and fails) on its own, exactly as without priming.
func scanCards(rows ports.ResultRows, a activity) ([]cardMetrics, bool, error) {
	out := []cardMetrics{}
	seedable := true
	for rows.Next() {
		var m cardMetrics
		var parkName *string
		if err := rows.Scan(&m.id.cardNo, &m.sessionLabel, &m.pens, &m.approved, &m.inReview, &m.started,
			&m.notFilmed, &m.sentBack, &m.rank, &m.anyRejected, &parkName); err != nil {
			return nil, false, err
		}
		m.id.activity = a
		if parkName == nil {
			seedable = false
			continue
		}
		m.parkName = *parkName
		out = append(out, m)
	}
	return out, seedable, rows.Err()
}

func flatten(perActivity [][]cardMetrics) []cardMetrics {
	out := []cardMetrics{}
	for _, cards := range perActivity {
		out = append(out, cards...)
	}
	return out
}

var errMissingParkName = errors.New("park name missing")

func (s *Source) readCardsFresh(ctx context.Context, q ports.SourceQuery) ([]cardMetrics, error) {
	batch := &pgx.Batch{}
	for _, a := range activities {
		bound := sqlbind.MustBind(metricsSQL(a), q.TenantID, q.BusinessDate, q.ParkID)
		batch.Queue(bound.SQL(), bound.Args()...)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	perActivity := make([][]cardMetrics, len(activities))
	for n, a := range activities {
		// scale-guard:ignore: drains the ONE batch's queued results in order; no round trip per iteration
		rows, err := br.Query()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.key, err)
		}
		cards, seedable, err := scanCards(rows, a)
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.key, err)
		}
		if !seedable {
			return nil, fmt.Errorf("%s: %w", a.key, errMissingParkName)
		}
		perActivity[n] = cards
	}
	return flatten(perActivity), br.Close()
}

// cardState maps a card's rolled-up rank back to one board work state. A card In progress with
// any pen sent back reads Rejected (same lane, amber) so the board points at the rework.
func cardState(m cardMetrics) domain.WorkState {
	switch m.rank {
	case 3:
		return domain.WorkStateCompleted
	case 2:
		return domain.WorkStateVerificationPending
	case 1:
		if m.anyRejected {
			return domain.WorkStateRejected
		}
		return domain.WorkStateInProgress
	default:
		return domain.WorkStateDue
	}
}

// cardTitle: "Feed direction · Morning" for a per-session card (the sheet's own session name, or
// "Session N" when the sheet carries none), the activity's title otherwise.
func cardTitle(m cardMetrics) string {
	if !m.id.activity.perSession {
		return m.id.activity.title
	}
	label := strings.TrimSpace(m.sessionLabel)
	if label == "" {
		label = "Session " + strconv.Itoa(m.id.cardNo)
	}
	return m.id.activity.title + " · " + label
}

// cardSubtitle names the card's size: "59 pens". Where each pen is (approved / in review / sent
// back / started / not filmed) rides the counts, which every surface renders as the card's count
// line and the drawer's tiles -- saying it again in the subtitle wrote the same split twice on a
// narrow card and cut it off mid-word (browser proof 2026-09-25).
func cardSubtitle(m cardMetrics) string {
	return fmt.Sprintf("%d %s", m.pens, pluralPens(m.pens))
}

func (s *Source) buildRow(q ports.SourceQuery, m cardMetrics) domain.Row {
	return domain.Row{
		Module: domain.ModuleFeed, SourceType: SourceType, SourceID: sourceID(q.ParkID, m.id),
		ParkID: q.ParkID, ParkName: m.parkName,
		BusinessDate: q.BusinessDate, ClockLabel: m.id.activity.clock,
		WorkState: cardState(m), Severity: domain.SeverityOK,
		// Feed work is the park crew's, not one person's: a claim pool, never "no one assigned".
		OwnerState: domain.OwnerStatePool,
		Title:      cardTitle(m),
		Subtitle:   cardSubtitle(m),
		Counts: domain.Counts{
			Done: m.approved, Pending: m.pens - m.approved, NeedsAttention: m.sentBack,
			InReview: m.inReview, NotStarted: m.notFilmed,
		},
		Href: "/feed/analytics",
	}.Finalize()
}

func pluralPens(n int) string {
	if n == 1 {
		return "pen"
	}
	return "pens"
}

// ListRows implements ports.Source. It emits the park-day's cards in (activity, session) order,
// after the keyset boundary and matching the state filter, each rolled up from its pens.
func (s *Source) ListRows(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var after *cardID
	if q.AfterSourceID != "" {
		c, ok := parseSourceID(q.AfterSourceID)
		if !ok {
			return nil, domain.ErrInvalidCursor
		}
		after = &c
	}
	limit := q.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}
	want := stateSet(q.WorkStates)
	cards, err := s.readCards(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("feed boardsource list: %w", err)
	}
	out := make([]domain.Row, 0, len(cards))
	for _, m := range cards {
		if after != nil && !after.less(m.id) {
			continue
		}
		if len(out) >= limit {
			break
		}
		row := s.buildRow(q, m)
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
	cards, err := s.readCards(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("feed boardsource count: %w", err)
	}
	out := map[domain.WorkState]int{}
	for _, m := range cards {
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
