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

// feedPartitionKeyExpr is intentionally constant: the live feed projection is exact-shed grain.
// Legacy partition labels may still appear on old movement rows as an input bridge to resolve the
// exact shed id, but once resolved they must not split or display as a second live location axis.
const feedPartitionKeyExpr = `COALESCE(NULL::text, 'whole')`

// scale-guard:ignore: 5k-50k-envelope — canonical indexed read per
// docs/decisions/operational-kernel-5k-50k-scale-envelope.md. The live-herd census and the
// approved-but-unexecuted movement set are both served directly from canonical SQL at the current
// release envelope; this screen earns its own projection only under that ADR's scale-out ladder.
//
// projection-review: membership=canonical goats rows for the tenant with merged_into_goat_id IS NULL and lifecycle_status pinned (default 'alive'), FULL OUTER JOINed to shifting_events restricted to movements that are not yet applied -- either authorization_state='authorized' (ordinary event_status='authorized', plus rollout-compatible authorized+pending_verification) or authorization_state='pending' AND event_status='pending' (raised, not yet approved, per the 2026-08-10 decision); the two branches are disjoint on authorization_state so one movement contributes exactly once, and an already-executed ('applied'), rejected or canceled movement is structurally excluded; group_key=(park_id, shed_id, normalized management_stage, normalized breed, normalized sex) applied identically to both sides via feedGrainNormSQL, with raw labels carried alongside for display only; leg_tag=the SOURCE leg carries the impact's stage_tag (the cohort the animals leave with) while the DESTINATION leg carries the destination shed's own inferred cohort; join_cardinality=shifting_event_impacts is 1:N per event and is PRE-AGGREGATED in the delta CTE before the join to live, so the movement legs cannot fan out the live COUNT; pagination=total_rows is a COUNT window function over the FULL combined set and is invariant to limit/offset; scope=tenant_id on every side plus optional park/shed equality and shed-set predicates
//
// Expanded rationale:
//
//	membership   = the live herd IS the count (maintainer decision 2026-07-19: this farm runs no
//	               physical counting workflow), so the base is canonical goats rather than a
//	               count_base_anchors replay. merged_into_goat_id IS NULL keeps a merged animal
//	               from being counted under both identities.
//
//	               The delta set is every not-yet-applied movement in one of two disjoint states:
//	               Park Head-AUTHORIZED (new rows are 'authorized'; authorized+pending_verification is
//	               retained only for an in-flight pre-000049 row), or RAISED and still awaiting
//	               approval (2026-08-10). 'applied' is already represented by goats; 'rejected' and
//	               'canceled' are excluded, so a movement that is turned down stops feeding a shed.
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
//	$6  business timezone   (biztime.DefaultTimezone)
//	$7  limit
//	$8  offset
//	$9  shed_id SET filter (empty array = no set filter)
//	$10 breed filter, normalized with feedGrainNormSQL by the caller ('' = all breeds)
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
    -- projection-review: membership=canonical live goats for the tenant at exact shed grain; legacy goat_shed_partitions history is deliberately ignored so an exact shed such as Castro 2 cannot be split again by a stale label; group_key=park/shed/stage/breed/sex; pagination=none, the feed sheet is a whole-scope projection consumed in full; scope=tenant plus the caller's park/shed/business-date predicates
    ` + feedPartitionKeyExpr + ` AS partition_key,
    NULL::text AS partition_label_raw,
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
    AND (cardinality($9::uuid[]) = 0 OR g.shed_id = ANY($9::uuid[]))
    AND ($10 = '' OR ` + fmt.Sprintf(feedGrainNormSQL, "g.breed") + ` = $10)
  GROUP BY g.park_id, g.shed_id,
           COALESCE(g.management_stage, ''), COALESCE(g.breed, ''), g.sex,
           ` + feedPartitionKeyExpr + `
),
-- Movements that are NOT yet executed, in two DISJOINT branches keyed on authorization_state, so a
-- movement contributes exactly once as it travels from raised to approved.
--
-- BRANCH 1 -- AUTHORIZED, feed-effective from the authorization business date. Maintainer decision
-- 2026-07-27, unchanged: there is NO lead time and NO priority branch here -- an approved movement is
-- a pending feed input the moment it is authorized (domain.FeedShiftingEffectiveBusinessDate).
-- authorized_at is the approval stamp, deliberately NOT effective_at (an authored intent date).
-- 'applied' is already in current_head_count. The authorized+pending_verification arm is a rollout
-- compatibility shape for a pre-000049 completed row.
--
-- BRANCH 2 -- RAISED, not yet approved, feed-effective from the ACTIONS lead time. Maintainer
-- decision 2026-08-10; see the branch's own comment and
-- domain.FeedShiftingRaisedEffectiveBusinessDate.
--
-- The date is derived in Asia/Kolkata, never UTC. An approval at 20:00 UTC is already the next
-- day in India, and a UTC-derived date would put the movement on the wrong feed day.
pending_event AS (
  SELECT
    se.shifting_event_id,
    se.source_park_id,
    se.source_shed_id,
    se.source_partition_label,
    se.destination_park_id,
    se.destination_shed_id,
    se.destination_partition_label,
    (se.authorized_at AT TIME ZONE $6)::date AS feed_effective_date
  FROM shifting_events se
  WHERE se.tenant_id = $1::uuid
    AND se.authorization_state = 'authorized'
    AND (se.event_status = 'authorized'
         OR (se.event_status = 'pending_verification' AND se.authorization_state = 'authorized'))
    AND se.authorized_at IS NOT NULL
  UNION ALL
  -- RAISED BUT NOT YET APPROVED (maintainer decision 2026-08-10, superseding the approval half of
  -- 2026-07-27). See domain.FeedShiftingRaisedEffectiveBusinessDate for the full rationale.
  --
  -- Approval no longer starts the feed clock; only REJECTION stops it. A movement raised at 09:00 is
  -- due tomorrow, but tomorrow's normal sheet was issued at 07:00 and is already being packed --
  -- waiting for the park head meant the destination pen was packed for the head count it had at
  -- breakfast and the arriving animals had no feed.
  --
  -- authorization_state='pending' is the ONLY state that qualifies: 'rejected' and 'authorized' are
  -- both excluded here (the latter is the branch above, so a movement cannot be counted twice as it
  -- moves from raised to approved). event_status='pending' excludes 'canceled', 'rejected' and
  -- 'unresolved' by construction -- a withdrawn movement stops feeding a shed immediately.
  --
  -- The date is the ACTIONS lead time, not the raise day: low priority raised before 13:30 IST is
  -- due tomorrow, at or after 13:30 the day after, high priority immediately. Derived in
  -- Asia/Kolkata via $6 -- a raise stamped 09:00 UTC is already 14:30 in India and is therefore
  -- AFTER the cutoff, which a UTC-derived comparison would get backwards.
  SELECT
    se.shifting_event_id,
    se.source_park_id,
    se.source_shed_id,
    se.source_partition_label,
    se.destination_park_id,
    se.destination_shed_id,
    se.destination_partition_label,
    CASE
      WHEN se.priority = 'high' THEN (se.raised_at AT TIME ZONE $6)::date
      WHEN (se.raised_at AT TIME ZONE $6)::time < TIME '13:30' THEN (se.raised_at AT TIME ZONE $6)::date + 1
      ELSE (se.raised_at AT TIME ZONE $6)::date + 2
    END AS feed_effective_date
  FROM shifting_events se
  WHERE se.tenant_id = $1::uuid
    AND se.authorization_state = 'pending'
    AND se.event_status = 'pending'
),
-- The destination shed's own operational cohort, so the DESTINATION leg of a movement is tagged
-- with the tag the animals ADOPT on arrival, not the SOURCE stage they leave with. A cross-profile
-- move (K1 -> K2, warmup -> fattening, ...) otherwise projected +N of the SOURCE cohort into the
-- destination shed, over-feeding the wrong ration there while under-representing the real one.
--
-- BRIDGE (2026-07-20): the authoritative per-shed profile is public.shed_profiles.animal_stage_id,
-- but that table is not yet populated, so the destination tag is inferred from the destination
-- shed's existing residents -- the SAME resident-inference bridge the completion path
-- (identity.resolveDestinationTag) uses, so the pending projection agrees with the eventual
-- completed row. A shed with exactly one distinct non-blank management_stage yields that stage; an
-- empty or mixed shed yields NULL and the leg falls back to the source stage_tag (the pre-fix
-- behaviour) until shed_profiles is seeded and both paths switch to it.
--
-- Set-based: ONE GROUP BY over goats per shed, never a per-movement subquery -- the same shape as
-- the live CTE, so it adds a scan, not an N+1.
--
-- projection-review: membership=live goats grouped per shed (same tenant/merged/exited/lifecycle filter as the live CTE), non-blank management_stage only; group_key=shed_id, exactly one row per shed with cohort_stage = its single distinct management_stage or NULL when empty/mixed; join_cardinality=strict 1:{0,1} per shed, LEFT JOINed onto the destination movement leg by shed_id so it can only relabel that leg's stage and never fans it out (a shed appears at most once); pagination=not itself paged -- it is a bounded per-shed cohort lookup consumed by the delta CTE, which pre-aggregates and is the paged grain set; scope=tenant_id on goats; the destination leg it feeds still carries the same optional park_id/shed_id and shed-SET predicates as the live side
dest_cohort AS (
  SELECT
    g.shed_id,
    -- projection-review: membership=the destination-cohort side of the feed projection, drawn from the same exact-shed live-goat set; group_key=shed only, so stale partition history cannot invent a second destination cohort inside an exact shed; pagination=none, the projection is consumed whole; scope=tenant plus the caller's park/shed/business-date predicates
    ` + feedPartitionKeyExpr + ` AS partition_key,
    NULL::text AS partition_label_raw,
    CASE WHEN count(DISTINCT COALESCE(g.management_stage, '')) = 1
         THEN min(g.management_stage) END AS cohort_stage
  FROM goats g
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND ($2 = '' OR g.lifecycle_status = $2)
    AND g.management_stage IS NOT NULL
    AND btrim(g.management_stage) <> ''
  GROUP BY g.shed_id, ` + feedPartitionKeyExpr + `
),
-- One row per (movement, impact, direction). Source loses head_count, destination gains it.
--
-- feed_effective_date <= D, never = D: a movement counts on its authorization day and every later
-- day until executed. An OVERDUE movement -- authorized days ago and still not executed -- therefore
-- keeps counting instead of silently dropping out and quietly de-feeding a shed whose animals are
-- still expected. (Overdue itself is the tighter < D-1 test in the delta CTE below; counting here is
-- the wider <= D.)
pending_leg AS (
  SELECT
    p.shifting_event_id,
    p.feed_effective_date,
    p.source_park_id AS park_id,
    COALESCE(src_sp.operational_location_id, p.source_shed_id) AS shed_id,
    'whole' AS partition_key,
    NULL::text AS partition_label_raw,
    COALESCE(i.stage_tag, '') AS stage_label,
    i.breed_label             AS breed_label,
    COALESCE(i.sex, '')       AS sex_label,
    -i.head_count             AS signed_head
  FROM pending_event p
  JOIN shifting_event_impacts i
    ON i.tenant_id = $1::uuid
   AND i.shifting_event_id = p.shifting_event_id
  LEFT JOIN shed_partitions src_sp
    ON src_sp.tenant_id = $1::uuid
   AND src_sp.status = 'active'
   AND (
        src_sp.operational_location_id = p.source_shed_id
        OR (
          src_sp.shed_id = p.source_shed_id
          AND regexp_replace(lower(btrim(src_sp.partition_label)), '^part[[:space:]]+', '') =
              regexp_replace(lower(btrim(COALESCE(p.source_partition_label, 'whole'))), '^part[[:space:]]+', '')
        )
   )
  WHERE p.source_shed_id IS NOT NULL
    AND p.feed_effective_date <= $5::date
  UNION ALL
  SELECT
    p.shifting_event_id,
    p.feed_effective_date,
    p.destination_park_id,
    COALESCE(dst_sp.operational_location_id, p.destination_shed_id),
    'whole',
    NULL::text,
    -- DESTINATION tag, not the source stage_tag: the animals adopt the destination shed's cohort on
    -- arrival. Falls back to the source tag only when the destination shed is empty/mixed and its
    -- cohort cannot be inferred (bridge limitation until shed_profiles is seeded -- see dest_cohort).
    COALESCE(NULLIF(dc.cohort_stage, ''), i.stage_tag, ''),
    i.breed_label,
    COALESCE(i.sex, ''),
    i.head_count
  FROM pending_event p
  JOIN shifting_event_impacts i
    ON i.tenant_id = $1::uuid
   AND i.shifting_event_id = p.shifting_event_id
  LEFT JOIN shed_partitions dst_sp
    ON dst_sp.tenant_id = $1::uuid
   AND dst_sp.status = 'active'
   AND (
        dst_sp.operational_location_id = p.destination_shed_id
        OR (
          dst_sp.shed_id = p.destination_shed_id
          AND regexp_replace(lower(btrim(dst_sp.partition_label)), '^part[[:space:]]+', '') =
              regexp_replace(lower(btrim(COALESCE(p.destination_partition_label, 'whole'))), '^part[[:space:]]+', '')
        )
   )
  LEFT JOIN dest_cohort dc
    ON dc.shed_id = COALESCE(dst_sp.operational_location_id, p.destination_shed_id)
   AND dc.partition_key = 'whole'
  WHERE p.feed_effective_date <= $5::date
),
-- Pre-aggregate the legs to ONE row per grain BEFORE joining the live herd. This is what keeps a
-- multi-impact movement from fanning out and multiplying current_head_count.
delta AS (
  SELECT
    l.park_id,
    l.shed_id,
    l.partition_key,
    ` + fmt.Sprintf(feedGrainNormSQL, "l.stage_label") + ` AS stage_key,
    ` + fmt.Sprintf(feedGrainNormSQL, "l.breed_label") + ` AS breed_key,
    ` + fmt.Sprintf(feedGrainNormSQL, "l.sex_label") + ` AS sex_key,
    -- Aggregated the same way as the live side's min(gsp.partition_label): the rows in this group
    -- all share one partition_key, so min() picks that partition's own raw label rather than
    -- inventing one. The combined CTE COALESCEs live's label ahead of this, so this only supplies a
    -- label for a delta-only row -- a destination shed holding none of the grain today, which is
    -- exactly the row the feed team most needs to see and the one a LEFT JOIN would have dropped.
    min(l.partition_label_raw) AS partition_label_raw,
    min(l.stage_label) AS stage_label,
    min(l.breed_label) AS breed_label,
    min(l.sex_label)   AS sex_label,
    sum(l.signed_head) AS pending_delta,
    -- Overdue = authorized BEFORE the packing day (feed day - 1) and still unexecuted, i.e. pending
    -- across at least one full cycle. A move authorized on the packing day is expected to execute
    -- that same day and is NOT overdue, so a zero-lead projection does not flag every fresh move.
    bool_or(l.feed_effective_date < ($5::date - 1)) AS overdue_pending,
    COALESCE(
      array_agg(DISTINCT l.shifting_event_id::text)
        FILTER (WHERE l.feed_effective_date < ($5::date - 1)),
      ARRAY[]::text[]
    ) AS overdue_event_ids
  FROM pending_leg l
  WHERE ($3 = '' OR l.park_id = NULLIF($3, '')::uuid)
    AND ($4 = '' OR l.shed_id = NULLIF($4, '')::uuid)
    -- Applied to the movement legs too, on the same terms as the live side: a
    -- shed-set page must not show a delta sourced from a shed the page excluded.
    AND (cardinality($9::uuid[]) = 0 OR l.shed_id = ANY($9::uuid[]))
    AND ($10 = '' OR ` + fmt.Sprintf(feedGrainNormSQL, "l.breed_label") + ` = $10)
  GROUP BY 1, 2, 3, 4, 5, 6
),
-- FULL OUTER, not LEFT. A destination shed that holds none of this grain today has no live row at
-- all, and a LEFT JOIN from live would drop the incoming animals entirely -- the exact shed the
-- feed team most needs to see. Both sides are 1:{0,1} on the grain key by construction.
combined AS (
  SELECT
    COALESCE(lv.park_id, d.park_id)                  AS park_id,
    COALESCE(lv.shed_id, d.shed_id)                  AS shed_id,
    COALESCE(lv.partition_key, d.partition_key, 'whole') AS partition_key,
    COALESCE(lv.partition_label_raw, d.partition_label_raw, NULL) AS partition_label_raw,
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
    AND d.partition_key IS NOT DISTINCT FROM lv.partition_key
    AND d.stage_key = lv.stage_key
    AND d.breed_key = lv.breed_key
    AND d.sex_key   = lv.sex_key
)
SELECT
  c.park_id::text,
  COALESCE(NULLIF(park.location_code, ''), park.name, '') AS park_label,
  c.shed_id::text,
  COALESCE(NULLIF(shed.name, ''), shed.location_code, '') AS shed_label,
  COALESCE(c.partition_label_raw, '') AS partition_label,
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
  c.partition_key,
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
  c.partition_key,
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
const feedProjectedCountsPagingSQL = `LIMIT $7 OFFSET $8`

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
			&row.PartitionLabel,
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
