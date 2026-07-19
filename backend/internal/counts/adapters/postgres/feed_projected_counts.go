package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const (
	feedProjectedCountsDefaultLimit = 50
	feedProjectedCountsMaxLimit     = 200
	feedProjectedCountsMaxOffset    = 5000
)

// feedGrainNormSQL is the SQL twin of countAliasNorm (repository.go): lowercase, trim, collapse
// internal whitespace runs to single underscores.
//
// It exists because the two sides of this projection describe the SAME grain in DIFFERENT
// columns. The live herd carries free-text goats.management_stage / goats.breed straight off the
// source sheet; a movement carries shifting_event_impacts.stage_tag / breed_key, where breed_key
// was already run through countAliasNorm at write time. Joining "Boer" to "boer" on raw equality
// would silently fail to match, and a failed match on a FULL OUTER JOIN does not error -- it
// splits one grain into two rows, showing the operator an unchanged current count beside a
// free-floating delta. Normalizing BOTH sides with the same rule is what makes the join land.
//
// countAliasNorm is idempotent (its output is already lowercase and underscore-joined), so
// applying this to an already-normalized breed_key is a no-op rather than a second transform.
const feedGrainNormSQL = `lower(regexp_replace(btrim(COALESCE(%s, '')), '\s+', '_', 'g'))`

// scale-guard:ignore: 5k-50k-envelope — canonical indexed read per
// docs/decisions/operational-kernel-5k-50k-scale-envelope.md. The live-herd census and the
// approved-but-unexecuted movement set are both served directly from canonical SQL at the current
// release envelope; this screen earns its own projection only under that ADR's scale-out ladder.
//
// projection-review: membership=canonical goats rows for the tenant with merged_into_goat_id IS NULL and lifecycle_status pinned (default 'alive'), FULL OUTER JOINed to shifting_events restricted to authorization_state='authorized' AND event_status='authorized' so an already-executed ('applied') movement is structurally excluded and cannot be counted twice; group_key=(park_id, shed_id, normalized management_stage, normalized breed, normalized sex) applied identically to both sides via feedGrainNormSQL, with raw labels carried alongside for display only; join_cardinality=shifting_event_impacts is 1:N per event and is PRE-AGGREGATED in the delta CTE before the join to live, so the movement legs cannot fan out the live COUNT, and locations is joined twice after aggregation on the (tenant_id, location_id) primary key as a strict 1:{0,1} label lookup; pagination=total_rows is a COUNT window function over the FULL combined set and is invariant to limit/offset, and the overdue event-id array is aggregated per grain rather than per page; scope=tenant_id on goats, shifting_events, shifting_event_impacts and both locations joins, plus optional park/shed equality predicates AND an optional shed-SET (= ANY) predicate, every one of them applied to the live side and to BOTH movement legs so a shed-scoped page cannot show a delta sourced from a shed the same filter excluded
//
// Expanded rationale:
//
//	membership   = the live herd IS the count (maintainer decision 2026-07-19: this farm runs no
//	               physical counting workflow), so the base is canonical goats rather than a
//	               count_base_anchors replay. merged_into_goat_id IS NULL keeps a merged animal
//	               from being counted under both identities.
//
//	               The delta set is deliberately NARROW: only movements that are both authorized
//	               AND still unexecuted. 'applied' movements are EXCLUDED, and that exclusion is
//	               the single most important predicate in this statement. A completed shifting has
//	               already relocated its animals in goats (CompleteShiftingEvent does the
//	               relocation and the status flip in one transaction), so it is ALREADY reflected
//	               in current_head_count. Including it would add the same animals a second time
//	               and over-feed the destination shed.
//	group_key    = park x shed x stage x breed x sex, normalized on both sides. See
//	               feedGrainNormSQL for why raw equality is not safe here.
//	join_card    = the danger is shifting_event_impacts: one event has MANY impact rows, so
//	               joining events to live grains directly would multiply the live count by the
//	               impact-row count. The delta CTE aggregates the impact legs to one row per grain
//	               FIRST; the FULL OUTER JOIN to live is then 1:{0,1} on both sides by construction.
//	pagination   = LIMIT/OFFSET walks the PRE-AGGREGATED grain set (distinct park/shed/stage/breed/
//	               sex combinations -- tens to low thousands at this envelope), never canonical
//	               goats rows; the service rejects offset > 5000 outright. total_rows is a window
//	               function over the whole combined set, so it does not move with the page.
//	scope        = tenant_id everywhere, plus the optional park/shed filters. The park/shed filter
//	               is applied to the live side AND to both movement legs, so filtering to one shed
//	               cannot leave that shed showing a delta from a movement the filter excluded.
//
// Bind order:
//
//	$1  tenant_id
//	$2  lifecycle_status ('' = any)
//	$3  park_id filter   ('' = all)
//	$4  shed_id filter   ('' = all)
//	$5  target feed date D (YYYY-MM-DD, business calendar)
//	$6  emergency lead days (domain.FeedShiftingEmergencyLeadDays)
//	$7  standard lead days  (domain.FeedShiftingStandardLeadDays)
//	$8  business timezone   (biztime.DefaultTimezone)
//	$9  limit
//	$10 offset
//	$11 shed_id SET filter (empty array = no set filter)
//	$12 breed filter, normalized with feedGrainNormSQL by the caller ('' = all breeds)
var feedProjectedShedCountsBaseSQL = `
WITH live AS MATERIALIZED (
  SELECT
    g.park_id,
    g.shed_id,
    COALESCE(g.management_stage, '') AS management_stage,
    COALESCE(g.breed, '')            AS breed,
    g.sex                            AS sex,
    ` + fmt.Sprintf(feedGrainNormSQL, "g.management_stage") + ` AS stage_key,
    ` + fmt.Sprintf(feedGrainNormSQL, "g.breed") + ` AS breed_key,
    ` + fmt.Sprintf(feedGrainNormSQL, "g.sex") + ` AS sex_key,
    count(*) AS head_count
  FROM goats g
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND ($2 = '' OR g.lifecycle_status = $2)
    AND ($3 = '' OR g.park_id = NULLIF($3, '')::uuid)
    AND ($4 = '' OR g.shed_id = NULLIF($4, '')::uuid)
    -- Shed-SET filter. The BIND ARRAY carries the cast and the column stays bare,
    -- so the ordinary index on g.shed_id remains usable. Casting the column to
    -- text on the left-hand side instead would disable that index -- the
    -- non-sargable-cast anti-pattern.
    AND (cardinality($11::uuid[]) = 0 OR g.shed_id = ANY($11::uuid[]))
    AND ($12 = '' OR ` + fmt.Sprintf(feedGrainNormSQL, "g.breed") + ` = $12)
  GROUP BY g.park_id, g.shed_id,
           COALESCE(g.management_stage, ''), COALESCE(g.breed, ''), g.sex
),
-- Approved but NOT yet executed movements, with the feed-effective business date the timing rule
-- gives each one. The lead days arrive as BIND PARAMETERS from domain.FeedShiftingLeadDays rather
-- than as SQL literals, so the business rule has exactly one definition (Go) and this statement
-- only has to know which priority string is the fast one.
--
-- authorized_at is the approval stamp -- the moment a park head said the movement MAY happen. It
-- is deliberately NOT raised_at (when someone asked) and NOT effective_at (an authored intent
-- date): the feed lead time runs from the decision, so the clock starts at authorization.
--
-- The date is derived in Asia/Kolkata, never UTC. An approval at 20:00 UTC is already the next
-- day in India, and a UTC-derived date would start the lead a day late.
pending_event AS (
  SELECT
    se.shifting_event_id,
    se.source_park_id,
    se.source_shed_id,
    se.destination_park_id,
    se.destination_shed_id,
    ((se.authorized_at AT TIME ZONE $8)::date
       + (CASE WHEN se.priority = 'emergency' THEN $6::int ELSE $7::int END)) AS feed_effective_date
  FROM shifting_events se
  WHERE se.tenant_id = $1::uuid
    AND se.authorization_state = 'authorized'
    AND se.event_status = 'authorized'
    AND se.authorized_at IS NOT NULL
),
-- One row per (movement, impact, direction). Source loses head_count, destination gains it.
--
-- feed_effective_date <= D, never = D. An OVERDUE movement -- due for feed days ago and still not
-- executed -- therefore keeps counting on every later day instead of silently dropping out of the
-- projection and quietly de-feeding a shed whose animals are still expected.
pending_leg AS (
  SELECT
    p.shifting_event_id,
    p.feed_effective_date,
    p.source_park_id AS park_id,
    p.source_shed_id AS shed_id,
    COALESCE(i.stage_tag, '') AS stage_label,
    i.breed_label             AS breed_label,
    COALESCE(i.sex, '')       AS sex_label,
    -i.head_count             AS signed_head
  FROM pending_event p
  JOIN shifting_event_impacts i
    ON i.tenant_id = $1::uuid
   AND i.shifting_event_id = p.shifting_event_id
  WHERE p.source_shed_id IS NOT NULL
    AND p.feed_effective_date <= $5::date
  UNION ALL
  SELECT
    p.shifting_event_id,
    p.feed_effective_date,
    p.destination_park_id,
    p.destination_shed_id,
    COALESCE(i.stage_tag, ''),
    i.breed_label,
    COALESCE(i.sex, ''),
    i.head_count
  FROM pending_event p
  JOIN shifting_event_impacts i
    ON i.tenant_id = $1::uuid
   AND i.shifting_event_id = p.shifting_event_id
  WHERE p.feed_effective_date <= $5::date
),
-- Pre-aggregate the legs to ONE row per grain BEFORE joining the live herd. This is what keeps a
-- multi-impact movement from fanning out and multiplying current_head_count.
delta AS (
  SELECT
    l.park_id,
    l.shed_id,
    ` + fmt.Sprintf(feedGrainNormSQL, "l.stage_label") + ` AS stage_key,
    ` + fmt.Sprintf(feedGrainNormSQL, "l.breed_label") + ` AS breed_key,
    ` + fmt.Sprintf(feedGrainNormSQL, "l.sex_label") + ` AS sex_key,
    min(l.stage_label) AS stage_label,
    min(l.breed_label) AS breed_label,
    min(l.sex_label)   AS sex_label,
    sum(l.signed_head) AS pending_delta,
    bool_or(l.feed_effective_date < $5::date) AS overdue_pending,
    COALESCE(
      array_agg(DISTINCT l.shifting_event_id::text)
        FILTER (WHERE l.feed_effective_date < $5::date),
      ARRAY[]::text[]
    ) AS overdue_event_ids
  FROM pending_leg l
  WHERE ($3 = '' OR l.park_id = NULLIF($3, '')::uuid)
    AND ($4 = '' OR l.shed_id = NULLIF($4, '')::uuid)
    -- Applied to the movement legs too, on the same terms as the live side: a
    -- shed-set page must not show a delta sourced from a shed the page excluded.
    AND (cardinality($11::uuid[]) = 0 OR l.shed_id = ANY($11::uuid[]))
    AND ($12 = '' OR ` + fmt.Sprintf(feedGrainNormSQL, "l.breed_label") + ` = $12)
  GROUP BY 1, 2, 3, 4, 5
),
-- FULL OUTER, not LEFT. A destination shed that holds none of this grain today has no live row at
-- all, and a LEFT JOIN from live would drop the incoming animals entirely -- the exact shed the
-- feed team most needs to see. Both sides are 1:{0,1} on the grain key by construction.
combined AS (
  SELECT
    COALESCE(lv.park_id, d.park_id)                  AS park_id,
    COALESCE(lv.shed_id, d.shed_id)                  AS shed_id,
    COALESCE(lv.management_stage, d.stage_label, '') AS management_stage,
    COALESCE(lv.breed, d.breed_label, '')            AS breed,
    COALESCE(lv.sex, d.sex_label, '')                AS sex,
    COALESCE(lv.head_count, 0)                       AS current_head_count,
    COALESCE(d.pending_delta, 0)                     AS pending_delta,
    COALESCE(d.overdue_pending, false)               AS overdue_pending,
    COALESCE(d.overdue_event_ids, ARRAY[]::text[])   AS overdue_event_ids
  FROM live lv
  FULL OUTER JOIN delta d
    ON  d.park_id   IS NOT DISTINCT FROM lv.park_id
    AND d.shed_id   IS NOT DISTINCT FROM lv.shed_id
    AND d.stage_key = lv.stage_key
    AND d.breed_key = lv.breed_key
    AND d.sex_key   = lv.sex_key
)
SELECT
  c.park_id::text,
  COALESCE(NULLIF(park.location_code, ''), park.name, '') AS park_label,
  c.shed_id::text,
  COALESCE(NULLIF(shed.name, ''), shed.location_code, '') AS shed_label,
  c.management_stage,
  c.breed,
  c.sex,
  c.current_head_count,
  c.pending_delta,
  -- GREATEST floors the projection at zero. The RAW pending_delta is still returned above and the
  -- clamp is reported as its own flag, so a source grain claiming to lose more animals than it
  -- holds surfaces as a visible data problem rather than being rounded away into a plausible
  -- looking small number.
  GREATEST(c.current_head_count + c.pending_delta, 0) AS projected_head_count,
  (c.current_head_count + c.pending_delta) < 0        AS clamped,
  c.overdue_pending,
  c.overdue_event_ids,
  count(*) OVER () AS total_rows
FROM combined c
LEFT JOIN locations park
       ON park.tenant_id = $1::uuid AND park.location_id = c.park_id
LEFT JOIN locations shed
       ON shed.tenant_id = $1::uuid AND shed.location_id = c.shed_id
`

// feedProjectedCountsOrderByHeadCount is the DEFAULT display order: biggest projected shed first.
// Its leading term is derived from current_head_count + pending_delta, which is exactly the value
// a concurrent shifting-event authorization/completion changes -- safe for a single-page UI read,
// unsafe as an OFFSET cursor across a multi-page drain (see StableOrder on FeedProjectedCountQuery).
const feedProjectedCountsOrderByHeadCount = `
ORDER BY
  GREATEST(c.current_head_count + c.pending_delta, 0) DESC,
  COALESCE(c.park_id, '00000000-0000-0000-0000-000000000000'::uuid),
  COALESCE(c.shed_id, '00000000-0000-0000-0000-000000000000'::uuid),
  c.management_stage,
  c.breed,
  c.sex
` + feedProjectedCountsPagingSQL

// feedProjectedCountsOrderByIdentity sorts ONLY by the grain's own identity columns -- no derived
// count anywhere in the key. An existing grain's park/shed/stage/breed/sex do not change when a
// movement changes ITS count (a movement changes the delta CTE's contribution to a grain that
// already exists at a fixed identity), so two reads of the same tenant/scope during the same
// caller-side drain place the same grain at the same OFFSET regardless of concurrent writes. Used
// by CONSISTENT-SNAPSHOT multi-page reads (FeedProjectedCountQuery.StableOrder); see
// feeddirection/adapters/counts.Reader.ProjectedGrainsForSheds.
const feedProjectedCountsOrderByIdentity = `
ORDER BY
  COALESCE(c.park_id, '00000000-0000-0000-0000-000000000000'::uuid),
  COALESCE(c.shed_id, '00000000-0000-0000-0000-000000000000'::uuid),
  c.management_stage,
  c.breed,
  c.sex
` + feedProjectedCountsPagingSQL

// feedProjectedShedCountsSQL and feedProjectedShedCountsStableOrderSQL are the two complete
// statements ProjectedShedCountsForFeed chooses between on FeedProjectedCountQuery.StableOrder.
// Both share every CTE and predicate; only the ORDER BY differs.
var (
	feedProjectedShedCountsSQL            = feedProjectedShedCountsBaseSQL + feedProjectedCountsOrderByHeadCount
	feedProjectedShedCountsStableOrderSQL = feedProjectedShedCountsBaseSQL + feedProjectedCountsOrderByIdentity
)

// feedProjectedCountsPagingSQL is split into its own literal so the paging clause carries its own
// justification rather than inheriting one from a 150-line statement.
//
// scale-guard:ignore: OFFSET walks the PRE-AGGREGATED grain set (distinct park/shed/stage/breed/sex combinations, tens to low thousands at this envelope), never canonical goats rows; the service rejects offset > 5000 outright.
const feedProjectedCountsPagingSQL = `LIMIT $9 OFFSET $10`

// ProjectedShedCountsForFeed serves the live-herd feed projection: what each shed grain will hold
// on feed day D, given the movements that are approved but not yet executed.
//
// It is a PARALLEL path to ProjectedCountFor/CountAsOf, not a replacement. Those replay movements
// on top of a physically-counted anchor (count_base_anchors) and remain in use by the
// counts-source import and parity tooling. This method reads neither of those tables: per the
// maintainer decision of 2026-07-19 this farm runs no physical counting workflow, so the live
// goats table is the count and shiftings are the only thing that changes it.
//
// ONE set-based statement. No per-row query, no loop-issued read, no N+1 fan-out.
func (r *Repository) ProjectedShedCountsForFeed(
	ctx context.Context,
	req domain.FeedProjectedCountQuery,
) (domain.FeedProjectedCounts, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	limit := req.Limit
	if limit <= 0 {
		limit = feedProjectedCountsDefaultLimit
	}
	if limit > feedProjectedCountsMaxLimit {
		limit = feedProjectedCountsMaxLimit
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > feedProjectedCountsMaxOffset {
		offset = feedProjectedCountsMaxOffset
	}

	// Default to the live herd so this projection's population matches the Counts Breakdown
	// census and Counts -> Herd Register, which both pin status=alive.
	lifecycle := ptrValue(req.LifecycleStatus)
	if lifecycle == "" {
		lifecycle = "alive"
	}

	targetDate := biztime.BusinessDayStart(req.TargetDate)

	querySQL := feedProjectedShedCountsSQL
	if req.StableOrder {
		querySQL = feedProjectedShedCountsStableOrderSQL
	}

	rows, err := r.pool.Query(ctx, querySQL,
		req.TenantID,
		lifecycle,
		ptrValue(req.ParkID),
		ptrValue(req.ShedID),
		targetDate.Format("2006-01-02"),
		domain.FeedShiftingEmergencyLeadDays,
		domain.FeedShiftingStandardLeadDays,
		biztime.DefaultTimezone,
		limit,
		offset,
		// Never nil: a nil slice would bind as NULL and cardinality(NULL) is NULL,
		// which would make the guard predicate NULL and silently drop every row.
		// An empty array is the honest "no set filter" value.
		append([]string{}, req.ShedIDs...),
		// countAliasNorm is the Go twin of feedGrainNormSQL (see its doc comment
		// above), so an empty/unset filter and an already-normalized breed_key
		// both round-trip as a no-op.
		countAliasNorm(ptrValue(req.Breed)),
	)
	if err != nil {
		return domain.FeedProjectedCounts{}, fmt.Errorf("feed projected shed counts: query: %w", err)
	}
	defer rows.Close()

	out := domain.FeedProjectedCounts{
		Items:       []domain.FeedProjectedCountRow{},
		TargetDate:  targetDate,
		ProjectedAt: time.Now().In(biztime.DefaultLocation()),
	}

	for rows.Next() {
		var row domain.FeedProjectedCountRow
		var totalRows int64
		if err := rows.Scan(
			&row.ParkID,
			&row.ParkLabel,
			&row.ShedID,
			&row.ShedLabel,
			&row.ManagementStage,
			&row.Breed,
			&row.Sex,
			&row.CurrentHeadCount,
			&row.PendingDelta,
			&row.ProjectedHeadCount,
			&row.Clamped,
			&row.OverduePending,
			&row.OverdueShiftingEventIDs,
			&totalRows,
		); err != nil {
			return domain.FeedProjectedCounts{}, fmt.Errorf("feed projected shed counts: scan: %w", err)
		}
		if row.OverdueShiftingEventIDs == nil {
			row.OverdueShiftingEventIDs = []string{}
		}
		// Every row carries the same window total; the last write wins and they agree.
		out.TotalRows = totalRows
		out.Items = append(out.Items, row)
	}
	if err := rows.Err(); err != nil {
		return domain.FeedProjectedCounts{}, fmt.Errorf("feed projected shed counts: iterate: %w", err)
	}

	return out, nil
}
