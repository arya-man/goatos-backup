package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/readcache"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// growthLookbackDays bounds how far BEFORE the requested period the query looks for a prior
// weigh to pair against the first weigh inside the period. Without a bound, a tenant with years
// of weighing history would force a full-history scan per animal every time leadership opened
// this screen for a one-week window. 400 days comfortably covers a goat's whole growth-tracked
// life (kid to sale) while keeping the scan bounded.
const growthLookbackDays = 400

// Keep a single analytics request from occupying the whole DB pool on a cold-cache page load.
// The admin Weights page already calls several weighing reads in parallel; letting this one fan out
// into a dozen simultaneous aggregate scans made staging tail latency worse under shared API bursts.
const weighingGrowthReadParallelism = 3

func weighingAnalyticsCacheKey(prefix string, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string) string {
	parks := append([]string(nil), parkIDs...)
	sort.Strings(parks)
	return fmt.Sprintf("%s:%s:%s:%s:%s:%s:%s:%s",
		prefix, tenantID, strings.Join(parks, ","), periodStart.UTC().Format(time.RFC3339),
		periodEnd.UTC().Format(time.RFC3339), strings.TrimSpace(sex), strings.TrimSpace(origin), strings.TrimSpace(weighingCategory))
}

// analyticsReadKey scopes a weighing analytics read in the shared read cache: the tenant and the
// park set the read spans (what a write evicts by), plus the full normalized parameter string.
func analyticsReadKey(tenantID string, parkIDs []string, params string) readcache.Key {
	return readcache.Key{Tenant: tenantID, Parks: parkIDs, Params: "weighing:" + params}
}

// commitAndEvict commits a weighing write and evicts the analytics reads it can change: the
// tenant, narrowed to the campaign's CURRENT park plus any extraParks the caller read before its
// update (a campaign moved between parks changes both). The eviction is published INSIDE the
// transaction (pg_notify is delivered on commit only) so sibling API instances drop the same
// keys, and applied locally after commit so this instance reads its own write on the very next
// request. A write with no tenant publishes nothing and clears this process only.
func (r *Repository) commitAndEvict(ctx context.Context, tx pgx.Tx, tenantID, campaignID string, extraParks ...string) error {
	var parks []string
	if tenantID != "" && campaignID != "" {
		park, err := r.campaignParkTx(ctx, tx, tenantID, campaignID)
		if err != nil {
			return err
		}
		if park != "" {
			parks = append(parks, park)
		}
		if len(parks) == 0 {
			// The campaign is gone (or unknown): evict the whole tenant rather than guess.
			extraParks = nil
		}
	}
	for _, p := range extraParks {
		if strings.TrimSpace(p) != "" {
			parks = append(parks, p)
		}
	}
	if err := readcache.NotifyTx(ctx, tx, tenantID, parks...); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if tenantID == "" {
		r.cache.EvictAll(ctx)
		return nil
	}
	r.cache.Evict(ctx, tenantID, parks...)
	return nil
}

// campaignParkTx is the campaign's park as this transaction sees it ("" when there is no row).
func (r *Repository) campaignParkTx(ctx context.Context, tx pgx.Tx, tenantID, campaignID string) (string, error) {
	q := sqlbind.MustBind(`SELECT park_id::text FROM weighing_campaigns WHERE tenant_id = $1::uuid AND campaign_id = $2::uuid`, tenantID, campaignID)
	var park string
	if err := tx.QueryRow(ctx, q.SQL(), q.Args()...).Scan(&park); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return park, nil
}

// commitClaimAndEvict commits a kernel cadence claim (day-start, roll-forward, delayed, carry-over
// merge) and publishes a tenant+park eviction for the work items it claimed: carry-over closes
// buckets and roll-forward moves due dates, both of which the Weights reads show. One statement
// resolves the claimed items' tenants and parks; nothing is published for an empty claim.
func (r *Repository) commitClaimAndEvict(ctx context.Context, tx pgx.Tx, claimed []claimedWorkItem) error {
	if len(claimed) == 0 {
		return tx.Commit(ctx)
	}
	ids := make([]string, 0, len(claimed))
	for _, c := range claimed {
		ids = append(ids, c.WorkItemID)
	}
	q := sqlbind.MustBind(`SELECT tenant_id::text, array_agg(DISTINCT park_id::text ORDER BY park_id::text)
FROM weighing_work_items WHERE work_item_id = ANY($1::uuid[])
GROUP BY tenant_id`, ids)
	rows, err := tx.Query(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return err
	}
	scopes := map[string][]string{}
	for rows.Next() {
		var tenant string
		var parks []string
		if err := rows.Scan(&tenant, &parks); err != nil {
			rows.Close()
			return err
		}
		scopes[tenant] = parks
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for tenant, parks := range scopes { // scale-guard:ignore: one pg_notify per distinct tenant in one claimed chunk (a claim is tenant-scoped, so one)
		if err := readcache.NotifyTx(ctx, tx, tenant, parks...); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for tenant, parks := range scopes { // scale-guard:ignore: in-memory cache eviction per claimed tenant, no I/O
		r.cache.Evict(ctx, tenant, parks...)
	}
	return nil
}

// growthPairsCTE is the shared base: resolve each accepted, non-rejected individual observation
// to an animal BY ITS RAW SCANNED TAG ALONE, then pair each observation with the PRECEDING one
// for the same tag to derive an ADG.
//
// Weighing is FREE-FLOW and SELF-CONTAINED: it reads ONLY weighing tables. It must never join
// goat_identifiers, goats, or any other herd/vaccination table. An earlier version resolved the
// tag to a goat_id through goat_identifiers -- a shared herd table used by 13 other modules --
// which quietly made weighing depend on herd identity data and on whatever is wrong in it.
//
// That rule still holds for THIS file: it names no herd table and resolves nothing itself. What
// changed on 2026-09-07 is that it no longer has to pretend a scanned string IS an animal. An
// animal here can carry two RFIDs, and the operator scans whichever one they can read, so the
// interval between a weigh on one tag and the next weigh on the other was never compared -- on STG
// that silently discarded 211 real comparisons and inflated the animal count by 116.
// identity_scope.go (the fifth recorded herd exception, and the narrowest -- goat_identifiers only)
// resolves that question once and hands this CTE an opaque tag -> canonical-tag map in $8/$9. A tag
// the map does not name keeps its own raw string, so an animal with one tag, an animal the register
// has never heard of, and a farm with no double-tagged animal are byte-for-byte what they were
// before. A genuine re-tag still splits history, and that is still honest. It expects bound parameters, in order:
//
//	$1 tenant_id (uuid)
//	$2 park_ids (uuid[]) -- one park, or every park the caller is authorized to monitor; the
//	   caller (service layer) has already authorization-checked this set, this file only filters
//	   by it
//	$3 lookback_start (timestamptz, inclusive) -- observations before this are never read
//	$4 period_end (timestamptz, exclusive)
//	$6/$7 sex/origin cohort filter: the flag, and the tag list it resolved to
//	$8/$9 the same-animal map: scanned tags, and the canonical tag each one keys under
//
// Per-query parameters therefore start at $10. Every consumer of this CTE binds $1-$9 in the same
// order; the two map binds were inserted at $8 rather than appended so the shared prefix stays
// contiguous, and every query below had its own parameters renumbered in the same change.
//
// Grain proof for the akmap LEFT JOIN, since it sits under COUNT/AVG/percentile aggregates: the
// map's `tag` column is UNIQUE BY CONSTRUCTION -- identity_scope.go builds it from a DISTINCT ON
// (normalized identifier value), so at most one map row matches any observation and the join adds
// NO rows. A map that could name one tag twice would fan every weigh of that animal out and double
// it inside every aggregate below, which is the one-to-many defect this note exists to rule out.
//
// EVERY caller-supplied value reaching this file arrives as a bound parameter, never string-
// concatenated -- see the injection-incident comment in weight_history.go for why that rule is
// non-negotiable here.
//
// Pending/unverified observations are DELIBERATELY included (only 'rejected' is excluded): see
// weight_history.go's "Weights AWAITING VERIFICATION are included" comment, which settled this
// for the sibling weight-history read and applies identically here -- a pending weight is a real
// measurement, not a reason to hide it from an aggregate.
const growthPairsCTE = `
base AS (
  SELECT wo.observation_id, wo.weight_kg::float8 AS weight_kg, wo.accepted_at,
         wo.verification_status, wcs.location_id, wcs.display_name AS shed_name,
         -- The PARK the weigh happened in, carried so a read can cut the herd figure per park
         -- without re-running this whole CTE once per park. Goats never move between parks
         -- (maintainer rule), so a pair is always formed inside one park and attributing it to
         -- the later weigh's park partitions the herd rather than double-counting it.
         wc.park_id,
         COALESCE(wcs.partition_label, '') AS partition_label,
         wcs.weighing_category,
         -- SAME-ANIMAL KEY, not the raw scanned string. An animal carrying two RFIDs scanned on
         -- its primary one week and its secondary the next was two animals with one weigh each, so
         -- the interval between those two weighs was never compared at all. identity_scope.go says
         -- which strings are one animal; this file is handed strings and still names no herd table.
         -- An unmapped tag -- a single-tag animal, or one the register does not know -- keeps its
         -- own raw string, so free-flow capture and the unmerged page are untouched.
         COALESCE(akmap.canonical_tag, lower(btrim(wo.scanned_identifier))) AS animal_key
  FROM weighing_observations wo
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
  -- projection-review: membership=the same-animal map resolved by identity_scope.go; group_key=the normalized scanned tag; join_cardinality=0..1 map rows per observation because the map's tag column is unique by construction (DISTINCT ON over normalized identifier value), so this join adds NO rows and cannot fan an animal's weighs out under the aggregates below; pagination=NONE, the map arrives as two bounded bind arrays; scope=tenant + the same park scope and window the caller resolved the map with
  LEFT JOIN unnest($8::text[], $9::text[]) AS akmap(tag, canonical_tag)
    ON akmap.tag = lower(btrim(wo.scanned_identifier))
  WHERE wo.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wo.verification_status <> 'rejected'
    AND wo.accepted_at >= $3::timestamptz
    AND wo.accepted_at < $4::timestamptz
    -- Sex filter, applied ONCE for every read built on this CTE. $6 is FALSE for the unfiltered
    -- page, so those reads run exactly as they did before the filter existed; when it is on, an
    -- empty tag list correctly matches nothing rather than silently meaning "every kid". Which
    -- tags belong to which sex is sex_scope.go's business: this file is handed strings.
    AND (NOT $6::bool OR lower(btrim(wo.scanned_identifier)) = ANY($7::text[]))
),
ordered AS (
  SELECT *,
    LAG(weight_kg) OVER w AS prev_weight,
    LAG(accepted_at) OVER w AS prev_accepted_at,
    LAG(observation_id) OVER w AS prev_observation_id,
    LAG(verification_status) OVER w AS prev_verification_status
  FROM base
  WINDOW w AS (PARTITION BY animal_key ORDER BY accepted_at, observation_id)
),
pairs AS (
  SELECT animal_key, park_id, location_id, shed_name, partition_label, weighing_category, observation_id, prev_observation_id,
         verification_status, prev_verification_status, accepted_at, prev_accepted_at,
         -- Carried so a caller can state the actual change ("19.0 -> 18.2 kg"), not just a rate.
         weight_kg, prev_weight,
         -- WHOLE BUSINESS DAYS between the two weighs, not elapsed seconds. ADG is a per-DAY
         -- rate, and dividing by a fractional day turns ordinary scale noise into a headline
         -- number: two weighs of one tag 111 seconds apart (15.0 kg then 11.0 kg, a duplicate
         -- scan across two sheds) divided by 0.0013 days and reported -3,108,762 g/day on the
         -- leadership Growth screen. An animal cannot gain or lose meaningfully within a day,
         -- so a same-day pair is a re-weigh, a correction, or a double scan -- never growth.
         ((accepted_at AT TIME ZONE 'Asia/Kolkata')::date
            - (prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date) AS days_between,
         (weight_kg - prev_weight) * 1000.0
           / NULLIF(
               (accepted_at AT TIME ZONE 'Asia/Kolkata')::date
                 - (prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date,
               0
             ) AS adg_g_per_day
  FROM ordered
  WHERE prev_weight IS NOT NULL
),
qualifying AS (
  -- Excludes any pair whose two weighs fall on the SAME business day: there is no whole day
  -- between them to average over, so no daily rate exists to report. Weighing stays free-flow
  -- and uncapped -- an operator may weigh a tag as often as they like and every one of those
  -- rows is kept -- but only pairs that actually span days produce an ADG.
  SELECT * FROM pairs WHERE days_between > 0`

// GetLeadershipGrowthADG is the leadership ADG (Average Daily Gain) read model: herd-level
// growth signal for a park and period, built from individual weighs only (see GrowthLumpSum on
// why lump-sum totals are never used to derive per-animal ADG).
//
// periodStart/periodEnd are Asia/Kolkata business-day boundaries expressed as UTC instants by
// the caller (the service layer), half-open [periodStart, periodEnd).
func (r *Repository) GetLeadershipGrowthADG(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory, sections string, timeScope domain.TimeScope) (domain.GrowthADG, error) {
	sectionSet := growthADGSectionSet(sections)
	cacheKey := weighingAnalyticsCacheKey("growth_adg:"+growthADGSectionKey(sectionSet)+":"+timeScope.CacheKey(), tenantID, parkIDs, periodStart, periodEnd, sex, origin, weighingCategory)
	return readcache.Load(ctx, r.cache, analyticsReadKey(tenantID, parkIDs, cacheKey), func(ctx context.Context) (domain.GrowthADG, error) {
		ctx, cancel := r.timeout(ctx)
		defer cancel()
		return r.getLeadershipGrowthADGUncached(ctx, tenantID, parkIDs, periodStart, periodEnd, sex, origin, weighingCategory, sectionSet, timeScope)
	})
}

func (r *Repository) getLeadershipGrowthADGUncached(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string, sectionSet map[string]bool, timeScope domain.TimeScope) (out domain.GrowthADG, err error) {

	// Resolved ONCE for the whole read: every widget below must talk about the same kids, and
	// resolving per helper would let a slow herd write land between two of them and show a
	// leaderboard whose animals are not the ones the headline counted.
	sexApplied := strings.TrimSpace(sex) != ""
	sexScope := SexScope{}
	if sexApplied {
		resolveSexScope := r.resolveSexScope
		if sectionSet["sale_readiness"] {
			resolveSexScope = r.resolveSexScopeWithAllTime
		}
		var scopeErr error
		sexScope, scopeErr = resolveSexScope(ctx, tenantID, parkIDs, sex, periodStart, periodEnd)
		if scopeErr != nil {
			return domain.GrowthADG{}, scopeErr
		}
	}
	originApplied := strings.TrimSpace(origin) != ""
	originScope := SexScope{}
	if originApplied {
		resolveOriginScope := r.resolveOriginScope
		if sectionSet["sale_readiness"] {
			resolveOriginScope = r.resolveOriginScopeWithAllTime
		}
		var originErr error
		originScope, originErr = resolveOriginScope(ctx, tenantID, parkIDs, origin, periodStart, periodEnd)
		if originErr != nil {
			return domain.GrowthADG{}, originErr
		}
	}
	// The same-animal map, resolved ONCE for the same reason the scopes are: every widget must merge
	// the same animals, and a map resolved per helper would let a herd write land between two of them
	// and show a leaderboard keyed differently from the headline above it.
	//
	resolveIdentityMap := r.resolveAnimalIdentityMap
	if sectionSet["sale_readiness"] {
		resolveIdentityMap = func(ctx context.Context, tenantID string, parkIDs []string, _, _ time.Time) (AnimalIdentityMap, error) {
			return r.resolveAnimalIdentityMapAllTime(ctx, tenantID, parkIDs)
		}
	}
	idMap, idErr := resolveIdentityMap(ctx, tenantID, parkIDs, periodStart.AddDate(0, 0, -growthLookbackDays), periodEnd)
	if idErr != nil {
		return domain.GrowthADG{}, idErr
	}
	scope := IntersectScopes(sexScope, sexApplied, originScope, originApplied)
	sexFiltered := sexApplied || originApplied

	periodLen := periodEnd.Sub(periodStart)
	prevStart := periodStart.Add(-periodLen)
	prevEnd := periodStart
	// BOTH weighs must fall inside the selected period (maintainer decision 2026-09-09).
	// Reaching growthLookbackDays before periodStart let an animal weighed ONCE in the period
	// pair against a weigh up to 400 days old and still count: on the 1-7 Sep view 250 of 349
	// animals qualified on an August weigh, and 62% of the measured growth-days fell outside
	// the window the reader selected -- so narrowing the dates barely moved the number. This
	// matches the lump-sum half, which has always required both its points inside the period.
	// "Twice" is per ANIMAL: the pairs CTE partitions on the identity_scope canonical key, so
	// an animal scanned on animal_identifier_1 then animal_identifier_2 is still one animal
	// with two weighs. The identity MAP stays resolved over the wide window on purpose -- it
	// only says which tags are one animal, and narrowing it would un-merge those pairs.
	lookbackStart := periodStart
	prevLookbackStart := prevStart

	var (
		headline     domain.GrowthADGHeadline
		prevHeadline domain.GrowthADGHeadline
		errs         = make(chan error, 13) // one slot for each optional read; workers never block reporting errors
		wg           sync.WaitGroup
	)
	analyticsSlots := make(chan struct{}, weighingGrowthReadParallelism)
	runGrowthRead := func(wg *sync.WaitGroup, errs chan<- error, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case analyticsSlots <- struct{}{}:
				defer func() { <-analyticsSlots }()
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
			if err := fn(); err != nil {
				errs <- err
			}
		}()
	}
	if sectionSet["headline"] {
		runGrowthRead(&wg, errs, func() error {
			var err error
			headline, err = r.growthHeadlineStats(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, idMap, weighingCategory)
			return err
		})
		if sectionSet["headline_previous"] {
			runGrowthRead(&wg, errs, func() error {
				var err error
				prevHeadline, err = r.growthHeadlineStats(ctx, tenantID, parkIDs, prevLookbackStart, prevStart, prevEnd, sexFiltered, scope, idMap, weighingCategory)
				return err
			})
		}
	}

	// ParkID stays the single-park value only when exactly one park was requested (the
	// pre-existing single-park contract); ParkIDs always carries the full authorized set the
	// service layer resolved, whether that is one park or the caller's whole authorized scope.
	singlePark := ""
	if len(parkIDs) == 1 {
		singlePark = parkIDs[0]
	}

	var (
		rejected      int
		eligibility   domain.GrowthEligibility
		trend         []domain.GrowthTrendPoint
		weeklyGain    []domain.GrowthWeeklyGainPoint
		leaderboard   []domain.GrowthShedLeaderboardRow
		distribution  []domain.GrowthDistributionBucket
		saleReadiness domain.GrowthSaleReadiness
		lumpSum       domain.GrowthLumpSum
		parks         []domain.GrowthPark
		losing        []domain.GrowthLosingAnimal
	)
	run := func(fn func() error) {
		runGrowthRead(&wg, errs, fn)
	}
	// Headline, breakdowns and the per-park cut are independent after scope resolution.
	// Share one three-slot pool across all of them, including the headline queries above,
	// instead of serial headline/breakdown/park waves. Results are read only after wg.Wait.
	// Run them with bounded parallelism:
	// enough to hide remote DB round-trip time, but not enough for one cold-cache request to monopolize
	// the app's DB pool while the Weights page is also fetching shed weights, demographics, and Growth
	// Director data.
	if sectionSet["headline"] || sectionSet["rejected"] {
		run(func() error {
			var err error
			rejected, err = r.growthRejectedCount(ctx, tenantID, parkIDs, periodStart, periodEnd, weighingCategory)
			return err
		})
	}
	if sectionSet["eligibility"] {
		run(func() error {
			var err error
			eligibility, err = r.growthEligibility(ctx, tenantID, parkIDs, periodStart, periodEnd, sexFiltered, scope, idMap, weighingCategory)
			return err
		})
	}
	if sectionSet["trend"] {
		run(func() error {
			var err error
			trend, err = r.growthTrend(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, idMap, weighingCategory)
			return err
		})
	}
	if sectionSet["weekly_gain"] {
		run(func() error {
			var err error
			weeklyGain, err = r.growthWeeklyGain(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, idMap, weighingCategory, timeScope)
			return err
		})
	}
	if sectionSet["shed_leaderboard"] {
		run(func() error {
			var err error
			leaderboard, err = r.growthShedLeaderboard(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, idMap, weighingCategory)
			return err
		})
	}
	if sectionSet["distribution"] {
		run(func() error {
			var err error
			distribution, err = r.growthDistribution(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, idMap, weighingCategory)
			return err
		})
	}
	if sectionSet["sale_readiness"] {
		run(func() error {
			var err error
			saleReadiness, err = r.growthSaleReadiness(ctx, tenantID, parkIDs, sexFiltered, scope, idMap, weighingCategory)
			return err
		})
	}
	if sectionSet["lump_sum"] {
		run(func() error {
			var err error
			lumpSum, err = r.growthLumpSumTrend(ctx, tenantID, parkIDs, periodStart, periodEnd, sexFiltered, scope, weighingCategory)
			return err
		})
	}
	if sectionSet["parks"] {
		run(func() error {
			var err error
			parks, err = r.growthParkNames(ctx, tenantID, parkIDs)
			return err
		})
	}
	if sectionSet["losing_animals"] {
		run(func() error {
			var err error
			losing, err = r.growthLosingAnimals(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, idMap, weighingCategory)
			return err
		})
	}
	// Per-park cut of the SAME headline statistic, and only when there is more than one park to
	// cut: with a single park in scope the headline above already IS that park's figure, so the
	// query would cost a scan to restate a number the response carries twice.
	byPark := []domain.GrowthParkGain{}
	if sectionSet["by_park"] && len(parkIDs) > 1 {
		run(func() error {
			var readErr error
			byPark, readErr = r.growthParkGains(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, idMap, weighingCategory)
			return readErr
		})
	}
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		return domain.GrowthADG{}, err
	}
	if sectionSet["headline"] {
		// ZERO must never stand in for UNKNOWN: Status/PreviousStatus (set inside growthHeadlineStats)
		// are the explicit markers, and AverageADGGPerDay/PositiveADGPercent are already nil there when
		// there is no qualifying pair. The delta and the previous-period figure are derived HERE, and
		// they inherit the same rule -- a delta computed against a nil (unknown) previous average would
		// silently read as a real number derived from a fabricated 0, which is exactly the fake-delta
		// defect this guards against.
		if sectionSet["headline_previous"] {
			headline.PreviousAverageADGGPerDay = prevHeadline.AverageADGGPerDay
			headline.PreviousStatus = prevHeadline.Status
			if headline.AverageADGGPerDay != nil && prevHeadline.AverageADGGPerDay != nil {
				delta := *headline.AverageADGGPerDay - *prevHeadline.AverageADGGPerDay
				headline.DeltaGPerDay = &delta
			}
		}
	}
	if sectionSet["headline"] || sectionSet["rejected"] {
		headline.RejectedObservationCount = rejected
	}
	// The tile drills into this list, so the count it shows must be the length of THIS list.
	if sectionSet["losing_animals"] {
		headline.LosingAnimalCount = len(losing)
	}
	out = domain.GrowthADG{
		ParkID:          singlePark,
		ParkIDs:         parkIDs,
		Parks:           parks,
		ByPark:          byPark,
		LosingAnimals:   growthADGLosingAnimals(sectionSet, losing),
		PeriodStart:     periodStart.Format("2006-01-02"),
		PeriodEnd:       periodEnd.Add(-24 * time.Hour).Format("2006-01-02"),
		Headline:        headline,
		Eligibility:     eligibility,
		Trend:           trend,
		WeeklyGain:      weeklyGain,
		ShedLeaderboard: leaderboard,
		Distribution:    distribution,
		SaleReadiness:   saleReadiness,
		LumpSum:         lumpSum,
	}
	return out, nil
}

func growthADGLosingAnimals(sectionSet map[string]bool, losing []domain.GrowthLosingAnimal) []domain.GrowthLosingAnimal {
	if !sectionSet["losing_animals"] {
		return nil
	}
	return losing
}

func growthADGSectionSet(raw string) map[string]bool {
	all := map[string]bool{
		"headline":          true,
		"headline_previous": true,
		"rejected":          true,
		"eligibility":       true,
		"trend":             true,
		"weekly_gain":       true,
		"shed_leaderboard":  true,
		"distribution":      true,
		"sale_readiness":    true,
		"lump_sum":          true,
		"parks":             true,
		"losing_animals":    true,
		"by_park":           true,
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return all
	}
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		key := strings.TrimSpace(part)
		if all[key] {
			out[key] = true
		}
	}
	return out
}

func growthADGSectionKey(sections map[string]bool) string {
	keys := []string{"headline", "headline_previous", "rejected", "eligibility", "trend", "weekly_gain", "shed_leaderboard", "distribution", "sale_readiness", "lump_sum", "parks", "losing_animals", "by_park"}
	active := make([]string, 0, len(keys))
	for _, key := range keys {
		if sections[key] {
			active = append(active, key)
		}
	}
	return strings.Join(active, ",")
}

func (r *Repository) growthHeadlineStats(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, idMap AnimalIdentityMap, weighingCategory string) (domain.GrowthADGHeadline, error) {
	q := `WITH ` + growthPairsCTE + `),
-- projection-review: membership=weighing_observations; group_key=park_aggregate; join_cardinality=one_to_many; pagination=single_row; scope=park_ids
inperiod AS (
  SELECT * FROM qualifying
  WHERE accepted_at >= $5::timestamptz
    AND ($12::text = '' OR weighing_category = $12::text)
),
-- ONE GAIN PER ANIMAL, which is the grain the gain charts report and therefore the grain the
-- headline must report. inperiod is PAIRS: a kid weighed three times in the window contributes two
-- of them, so a herd average taken over pairs quietly counts the most-handled kids twice. It also
-- kept the headline disagreeing with the by-sex chart even after whole-shed pens were added -- 197
-- male pairs against the chart's 141 male animals.
--
-- The animal's own gain is its TOTAL GRAMS over its TOTAL DAYS across those pairs -- the same
-- sum(movement)/sum(days) shape the whole-shed arm uses, and the same one
-- weight_demographics.go's animal_gain uses. A single-pair animal is simply that pair.
--
-- NOT the median of its per-pair RATES, which is what this was until 2026-09-22. A rate median
-- weights every leg equally however long it was, so one short leg decides the animal: a kid weighed
-- 27.5 kg on 24 Aug, 28.4 on 31 Aug and 29.8 on 1 Sep has a 7-day leg at 129 g/day beside a 1-day
-- leg at 1400 g/day, and the median of two values IS their mean -- 764 g/day reported for an animal
-- that gained 2.3 kg in 8 days, which is 287. A 1-day leg is the shortest the same-day filter lets
-- through (see qualifying), and over one day ordinary scale noise IS the whole rate. Summing the
-- movement and dividing by the days it spans weights each leg by its own length, so a short leg
-- contributes its grams and not a rate.
animal_gain AS (
  SELECT animal_key,
         sum((weight_kg - prev_weight) * 1000.0)::float8 / NULLIF(sum(days_between), 0) AS g
  FROM inperiod GROUP BY animal_key
),
endpoint_ids AS (
  SELECT observation_id AS oid, verification_status AS status FROM inperiod
  UNION
  SELECT prev_observation_id, prev_verification_status FROM inperiod
),
-- WHOLE-SHED PENS COUNT TOWARD THE FARM'S DAILY GAIN (maintainer decision 2026-08-26).
--
-- They did not, and that is the defect this arm closes: the headline was the MEDIAN of
-- individually-scanned pairs ONLY, while the by-breed/by-sex/by-stage gain charts on the same page
-- were the weighted MEAN of those pairs PLUS whole-shed pens (the 2026-08-25 decision). Once the
-- Sex filter made both statements about the identical population, the page showed a reader two
-- different male daily gains at once -- 133 g in the headline above 200 g in the chart. Most of
-- this farm's kids are weighed by the whole shed (339 of 791 in the landing window), so the
-- headline was also answering "how fast is the herd growing" from well under half of the herd.
--
-- One row per PEN, anchored on its first and latest weighed business date INSIDE the selected
-- window -- the same rn=1 shape shed_span and lump_span already use, so every gain number on the
-- page ranges over the same pen set. A pen weighed once in the window has no movement to report and
-- is excluded by latest.d > first.d rather than counted as zero growth.
--
-- Known and accepted: a whole-shed average moves when animals ENTER OR LEAVE the pen, not only when
-- they grow, so this is a coarser measure than a scanned pair. That is the trade the maintainer took
-- rather than report the herd from a minority of it. The pair-based statistics below (positive %,
-- negative pairs, losing animals) deliberately stay individual-only: a shed average has no
-- per-animal sign to contribute, and inventing one would put animals in a losing list nobody weighed.
--
-- projection-review: producer grain is one live weighing_shed_observations row per bucket; consumer
-- grain is one row per (location_id, partition_label) -- guaranteed by latest.rn = 1 joined to
-- first.rn = 1 on that same pair, so sum(animals) ranges over disjoint pens. The weighted mean's
-- numerator and denominator range over the identical row set (same FROM, same WHERE).
shed_span AS (
  SELECT latest.animal_count::float8 AS animals,
         (latest.average_weight_kg - first.average_weight_kg) * 1000.0
           / NULLIF(latest.d - first.d, 0) AS g_per_day
  FROM (
    SELECT cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label,
           o.average_weight_kg, o.animal_count,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id, COALESCE(cs2.partition_label, '') ORDER BY o.accepted_at DESC) AS rn
    FROM weighing_shed_observations o
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id AND cs2.tenant_id = o.tenant_id
    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id AND c2.tenant_id = o.tenant_id
    WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
      AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
      AND o.accepted_at >= $5::timestamptz AND o.accepted_at < $4::timestamptz
      AND ($12::text = '' OR cs2.weighing_category = $12::text)
      -- A whole-shed weigh carries no tag, so under a Sex filter it is claimed only when its pen's
      -- cohort is entirely that sex (sex_scope.go proves it); a mixed pen is claimed by neither
      -- side, because one shed average cannot be split between two cohorts.
      AND (NOT $6::bool OR EXISTS (
        SELECT 1 FROM unnest($10::uuid[], $11::text[]) AS b(loc, part)
        WHERE b.loc = cs2.location_id AND b.part = COALESCE(cs2.partition_label, '')
      ))
  ) latest
  JOIN (
    SELECT cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label,
           o.average_weight_kg,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id, COALESCE(cs2.partition_label, '') ORDER BY o.accepted_at ASC) AS rn
    FROM weighing_shed_observations o
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id AND cs2.tenant_id = o.tenant_id
    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id AND c2.tenant_id = o.tenant_id
    WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
      AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
      AND o.accepted_at >= $5::timestamptz AND o.accepted_at < $4::timestamptz
      AND ($12::text = '' OR cs2.weighing_category = $12::text)
      AND (NOT $6::bool OR EXISTS (
        SELECT 1 FROM unnest($10::uuid[], $11::text[]) AS b(loc, part)
        WHERE b.loc = cs2.location_id AND b.part = COALESCE(cs2.partition_label, '')
      ))
  ) first ON first.location_id = latest.location_id
    AND first.partition_label = latest.partition_label
    AND first.rn = 1
  WHERE latest.rn = 1 AND latest.d > first.d
)
SELECT
  -- The weighted mean the gain charts report, over the same population they report it for: every
  -- scanned pair counts once, and every whole-shed pen counts once PER ANIMAL it holds, so a pen of
  -- 76 kids weighs 76 times as much as one scanned kid. NULLIF keeps an empty period NULL rather
  -- than 0 -- a farm that weighed nothing must not render as a herd that stopped growing.
  ((SELECT COALESCE(sum(g), 0) FROM animal_gain)
     + (SELECT COALESCE(sum(animals * g_per_day), 0) FROM shed_span))
  / NULLIF((SELECT COUNT(*) FROM animal_gain)
     + (SELECT COALESCE(sum(animals), 0) FROM shed_span), 0),
  (SELECT COUNT(*) FROM inperiod),
  -- The denominator behind the headline, so the card can say how many kids it speaks for.
  ((SELECT COUNT(*) FROM animal_gain)
     + (SELECT COALESCE(sum(animals), 0) FROM shed_span))::bigint,
  (SELECT COUNT(*) FILTER (WHERE adg_g_per_day > 0) * 100.0 / NULLIF(COUNT(*), 0) FROM inperiod),
  (SELECT COUNT(*) FROM inperiod WHERE adg_g_per_day < 0),
  (SELECT COUNT(*) FROM endpoint_ids WHERE status = 'pending')`

	var h domain.GrowthADGHeadline
	// median/percent are left as SQL NULL (never COALESCEd to 0) when inperiod is empty, and
	// scanned straight into pointer fields -- this is the ZERO-vs-UNKNOWN fix: a park where every
	// animal was weighed exactly once must come back with these fields absent, not "0".
	bound, bindErr := sqlbind.Bind(q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, idMap.Tags, idMap.CanonicalTags,
		scope.LocationIDs, scope.PartitionLabels, weighingCategory)
	if bindErr != nil {
		return h, fmt.Errorf("weighing: bind growth headline query: %w", bindErr)
	}
	err := r.pool.QueryRow(ctx, bound.SQL(), bound.Args()...).Scan(
		&h.AverageADGGPerDay, &h.PairCount, &h.HeadlineAnimals, &h.PositiveADGPercent, &h.NegativeADGCount,
		&h.UnverifiedObservationCount,
	)
	if err != nil {
		return h, err
	}
	// Keyed on the HEADLINE's own denominator, not on PairCount: a park whose kids are all weighed by
	// the whole shed has zero scanned pairs and a perfectly real daily gain, and calling that
	// "insufficient_data" would blank the one number this screen exists to answer.
	if h.HeadlineAnimals == 0 {
		h.Status = "insufficient_data"
	} else {
		h.Status = "ok"
	}
	return h, nil
}

func (r *Repository) growthRejectedCount(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, weighingCategory string) (int, error) {
	var count int
	// projection-review: membership=weighing_observations + weighing_shed_observations; group_key=park_aggregate; join_cardinality=one_to_many; pagination=single_row; scope=park_ids
	err := r.pool.QueryRow(ctx, `
	-- projection-review: membership=weighing_observations + weighing_shed_observations; group_key=park_aggregate; join_cardinality=one_to_many; pagination=single_row; scope=park_ids
WITH rejected AS (
  SELECT wo.observation_id::text AS rejected_id
  FROM weighing_observations wo
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
  WHERE wo.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wo.verification_status = 'rework'
    AND wo.accepted_at >= $3::timestamptz
    AND wo.accepted_at < $4::timestamptz
    AND ($5::text = '' OR wcs.weighing_category = $5::text)
  UNION ALL
  SELECT wso.shed_observation_id::text AS rejected_id
  FROM weighing_shed_observations wso
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wso.campaign_shed_id AND wcs.tenant_id = wso.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wso.tenant_id
  WHERE wso.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wso.verification_status = 'rework'
    AND wso.accepted_at >= $3::timestamptz
    AND wso.accepted_at < $4::timestamptz
    AND ($5::text = '' OR wcs.weighing_category = $5::text)
)
SELECT COUNT(*) FROM rejected`, tenantID, parkIDs, periodStart, periodEnd, weighingCategory).Scan(&count)
	return count, err
}

func (r *Repository) growthEligibility(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, idMap AnimalIdentityMap, weighingCategory string) (domain.GrowthEligibility, error) {
	var e domain.GrowthEligibility
	// projection-review: membership=weighing_observations; group_key=animal_key; join_cardinality=one_to_many; pagination=single_row; scope=park_ids
	err := r.pool.QueryRow(ctx, `
WITH obs AS (
  -- Same-animal key, not the raw tag: this query's two numbers are "animals weighed twice or more"
  -- and "animals weighed at all", and a double-tagged animal scanned on a different RFID each round
  -- inflated BOTH -- counted twice, and in neither case as an animal with two weighs. On STG this
  -- read 549 animals where the farm has 433.
  SELECT COALESCE(akmap.canonical_tag, lower(btrim(wo.scanned_identifier))) AS animal_key
  FROM weighing_observations wo
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
  -- projection-review: membership=the same-animal map resolved by identity_scope.go; group_key=the normalized scanned tag; join_cardinality=0..1 map rows per observation because the map's tag column is unique by construction (DISTINCT ON over normalized identifier value), so this join adds NO rows and cannot fan an animal's weighs out under the aggregates below; pagination=NONE, the map arrives as two bounded bind arrays; scope=tenant + the same park scope and window the caller resolved the map with
  LEFT JOIN unnest($8::text[], $9::text[]) AS akmap(tag, canonical_tag)
    ON akmap.tag = lower(btrim(wo.scanned_identifier))
  WHERE wo.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wo.verification_status <> 'rejected'
    AND wo.accepted_at >= $3::timestamptz
    AND wo.accepted_at < $4::timestamptz
    AND ($7::text = '' OR wcs.weighing_category = $7::text)
    -- The Sex filter reaches the coverage counters too. This pair is the phone's "70/382" tile --
    -- how many kids have a second weigh out of all weighed -- and leaving it unfiltered reported
    -- the whole herd's coverage under a heading about half of it.
    AND (NOT $5::bool OR lower(btrim(wo.scanned_identifier)) = ANY($6::text[]))
),
per_animal AS (
  SELECT animal_key, COUNT(*) AS n FROM obs GROUP BY animal_key
)
SELECT COUNT(*) FILTER (WHERE n >= 2), COUNT(*) FROM per_animal`,
		tenantID, parkIDs, periodStart, periodEnd, sexFiltered, scope.Tags, weighingCategory, idMap.Tags, idMap.CanonicalTags).Scan(&e.AnimalsWithTwoPlusWeighs, &e.TotalAnimalsWeighed)
	return e, err
}

func (r *Repository) growthTrend(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, idMap AnimalIdentityMap, weighingCategory string) ([]domain.GrowthTrendPoint, error) {
	// projection-review: membership=weighing_observations; group_key=week_start; join_cardinality=one_to_many; pagination=multi_row; scope=park_ids
	q := `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT *, (date_trunc('week', accepted_at AT TIME ZONE 'Asia/Kolkata'))::date AS week_start
  FROM qualifying
  WHERE accepted_at >= $5::timestamptz
    AND ($10::text = '' OR weighing_category = $10::text)
)
SELECT week_start,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_per_day) AS median_adg,
       COUNT(*) AS pair_count
FROM inperiod
GROUP BY week_start
-- This groups whatever dates actually have pairs into calendar weeks purely for display -- it is
-- NOT an assumption that weighing happens weekly. A week with no pairs simply has no row here: it
-- is never interpolated with a fabricated zero or a carried-forward value, and no week is ever
-- flagged as "missed" or "overdue".
ORDER BY week_start`
	bound1, bindErr1 := sqlbind.Bind(q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, idMap.Tags, idMap.CanonicalTags, weighingCategory)
	if bindErr1 != nil {
		return nil, fmt.Errorf("weighing: bind growth query: %w", bindErr1)
	}
	rows, err := r.pool.Query(ctx, bound1.SQL(), bound1.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GrowthTrendPoint{}
	for rows.Next() {
		var p domain.GrowthTrendPoint
		var weekStart time.Time
		if err := rows.Scan(&weekStart, &p.MedianADGGPerDay, &p.PairCount); err != nil {
			return nil, err
		}
		p.WeekStart = weekStart.Format("2006-01-02")
		out = append(out, p)
	}
	return out, rows.Err()
}

// growthWeeklyGain is the headline statistic cut by calendar week.
//
// IT MUST STAY THE SAME STATISTIC AS growthHeadlineStats (maintainer decision 2026-08-26). The
// two arms below mirror that function's animal_gain and shed_span exactly -- an animal-weighted
// mean over scanned kids (each once, at the median of its own pairs) PLUS whole-shed pens (each
// once per animal it holds). growthTrend beside this one is the MEDIAN over SCANNED PAIRS ONLY;
// putting that on a page next to this headline is what the lock forbids, which is why this is a
// separate query rather than a second column on that one.
//
// projection-review:
//
//	producer grain: one weighing_observations row per scan; one weighing_shed_observations row per pen weigh.
//	consumer grain: arm (a) one row per (week_start, animal_key) -- GROUP BY those two columns.
//	                arm (b) one row per (week_start, location_id, partition_label) -- rn = 1 over
//	                that triple, so a pen weighed three times in one week contributes ONE movement,
//	                not two, and sum(animals) ranges over disjoint pens within each week.
//	join_cardinality: both arms pre-aggregate to their own grain before the UNION ALL, so the
//	                final sum() never fans out; the numerator and denominator range over the
//	                identical row set (same FROM, same WHERE) in each arm.
//	ratio key set: numerator sum(total) and denominator sum(animals) both range over `weekly`
//	                grouped by week_start -- one key set, stated identical.
//	pagination: multi_row (bounded by the caller's window; 12 weeks on the Weights analytics page).
//	scope: park_ids, plus the sex/origin scope applied to BOTH arms.
//
// growthWeeklyGainQuery is hoisted to package scope rather than built inside the function so a
// query-plan test can reach it by name, which is what the scale guard is asking for.
const growthWeeklyGainTemplate = `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT *, {{BUCKET_ACCEPTED_AT}} AS week_start
  FROM qualifying
  WHERE accepted_at >= $5::timestamptz
    AND ($12::text = '' OR weighing_category = $12::text)
    -- The selected pen ($13/$14, empty = every pen). Narrowed HERE rather than after the grouping
    -- because the rows stop carrying a pen the moment they are grouped by bucket.
    AND ($13::text = '' OR (location_id::text = $13::text AND partition_label = $14::text))
),
-- Arm (a): ONE GAIN PER ANIMAL PER BUCKET, over ALL the movement inside it -- the animal's total
-- grams divided by the days those legs span. Grouping by pairs instead would count the
-- most-handled kids twice, which is the defect the headline's own animal_gain comment records.
--
-- The SAME shape as arm (b) below, deliberately: a bucket holding two legs must report the movement
-- across both, not one leg or an unweighted average of their rates. It was a median of rates until
-- 2026-09-22 -- see the headline's animal_gain for the worked case the maintainer reported.
animal_gain AS (
  SELECT week_start, animal_key,
         sum((weight_kg - prev_weight) * 1000.0)::float8 / NULLIF(sum(days_between), 0) AS g
  FROM inperiod GROUP BY week_start, animal_key
),
-- Arm (b): whole-shed pens. Every live pen weigh in the window, scoped exactly as the headline
-- scopes it -- a pen is claimed only when the (location, partition) pair is in the resolved list,
-- so a mixed-sex or mixed-origin pen is claimed by neither cohort rather than split.
pen_obs AS (
  SELECT cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label,
         o.average_weight_kg, o.animal_count,
         (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
  FROM weighing_shed_observations o
  JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id AND cs2.tenant_id = o.tenant_id
  JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id AND c2.tenant_id = o.tenant_id
  WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
    AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
    AND o.accepted_at >= $5::timestamptz AND o.accepted_at < $4::timestamptz
    AND ($12::text = '' OR cs2.weighing_category = $12::text)
    AND ($13::text = '' OR (cs2.location_id::text = $13::text AND COALESCE(cs2.partition_label, '') = $14::text))
    AND (NOT $6::bool OR EXISTS (
      SELECT 1 FROM unnest($10::uuid[], $11::text[]) AS b(loc, part)
      WHERE b.loc = cs2.location_id AND b.part = COALESCE(cs2.partition_label, '')))
),
-- CONSECUTIVE pen weighs, not first-vs-latest-in-window. The headline anchors on the window's two
-- ends because it reports ONE number for the whole window; a weekly series must attribute movement
-- to the week it was observed in, so each weigh is paired with the one before it and bucketed by
-- the LATER weigh -- the identical rule arm (a) applies to a scanned pair spanning two weeks.
pen_pairs AS (
  SELECT location_id, partition_label, animal_count, d, average_weight_kg,
         LAG(average_weight_kg) OVER w AS prev_w,
         LAG(d) OVER w AS prev_d
  FROM pen_obs
  WINDOW w AS (PARTITION BY location_id, partition_label ORDER BY d)
),
-- ONE FIGURE PER PEN PER BUCKET, over ALL the movement inside it. A pen weighed three times in one
-- bucket yields two pairs, and counting both would add its head count to that bucket's denominator
-- twice -- the same double-count arm (a) avoids by grouping on animal_key.
--
-- The figure is the pen's grams gained across the bucket divided by the DAYS those legs cover, not
-- the last leg alone. Over a calendar week that is the same number, because a pen is rarely weighed
-- twice inside one -- but over a 30-day bucket (maintainer request 2026-09-21) a weekly-weighed pen
-- has four legs, and reporting only the last one would put one week's gain under a heading that
-- says thirty days. The head count is the pen's at its LATEST weigh in the bucket, counted once.
pen_bucketed AS (
  SELECT {{BUCKET_D}} AS week_start, location_id, partition_label, d,
         animal_count,
         (average_weight_kg - prev_w) * 1000.0 AS gain_g,
         (d - prev_d) AS leg_days
  FROM pen_pairs
  WHERE prev_w IS NOT NULL AND d > prev_d
),
shed_span AS (
  SELECT week_start,
         (array_agg(animal_count ORDER BY d DESC))[1]::float8 AS animals,
         sum(gain_g) / NULLIF(sum(leg_days), 0) AS g_per_day
  FROM pen_bucketed
  GROUP BY week_start, location_id, partition_label
),
-- Each arm is already at its own grain, so this UNION ALL cannot fan out.
weekly AS (
  SELECT week_start, COALESCE(sum(g), 0) AS total, COUNT(*)::float8 AS animals
  FROM animal_gain GROUP BY week_start
  UNION ALL
  SELECT week_start, COALESCE(sum(animals * g_per_day), 0), COALESCE(sum(animals), 0)
  FROM shed_span GROUP BY week_start
)
SELECT week_start, sum(total) / NULLIF(sum(animals), 0), sum(animals)::bigint
FROM weekly
GROUP BY week_start
-- A week with no animals behind it is dropped rather than reported as zero growth: nobody weighed
-- then, which is not the same statement as "the herd did not grow".
HAVING sum(animals) > 0
ORDER BY week_start`

// The two rendered variants. $13/$14 are the selected pen (empty = every pen) and $15 is the
// window's last business date, bound ONLY by the month variant, which is the only one that names it.
var (
	growthWeeklyGainQuery      = bucketedQuery(growthWeeklyGainTemplate, domain.GainBucketWeek, 15)
	growthWeeklyGainMonthQuery = bucketedQuery(growthWeeklyGainTemplate, domain.GainBucketMonth, 15)
)

func (r *Repository) growthWeeklyGain(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, idMap AnimalIdentityMap, weighingCategory string, timeScope domain.TimeScope) ([]domain.GrowthWeeklyGainPoint, error) {
	// The month variant needs the window's last day to count its 30-day blocks back from; the week
	// variant never mentions it, so binding it there would be a parameter the statement does not
	// take. The point's WeekStart therefore carries the START of whichever bucket was asked for.
	query := growthWeeklyGainQuery
	args := []any{tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, idMap.Tags, idMap.CanonicalTags,
		scope.LocationIDs, scope.PartitionLabels, weighingCategory,
		timeScope.PenLocationID, timeScope.PenPartitionLabel}
	if timeScope.Bucket == domain.GainBucketMonth {
		query = growthWeeklyGainMonthQuery
		args = append(args, gainBucketAnchor(periodEnd))
	}
	bound, bindErr := sqlbind.Bind(query, args...)
	if bindErr != nil {
		return nil, fmt.Errorf("weighing: bind weekly gain query: %w", bindErr)
	}
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Initialised, never nil, so the wire carries [] rather than null for a farm with no weighs.
	out := []domain.GrowthWeeklyGainPoint{}
	for rows.Next() {
		var p domain.GrowthWeeklyGainPoint
		var weekStart time.Time
		var animals int64
		if err := rows.Scan(&weekStart, &p.AverageADGGPerDay, &animals); err != nil {
			return nil, err
		}
		p.WeekStart = weekStart.Format("2006-01-02")
		p.Animals = int(animals)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) growthShedLeaderboard(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, idMap AnimalIdentityMap, weighingCategory string) ([]domain.GrowthShedLeaderboardRow, error) {
	// projection-review: membership=weighing_observations; group_key=location_id; join_cardinality=one_to_many; pagination=multi_row; scope=park_ids
	q := `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT * FROM qualifying
  WHERE accepted_at >= $5::timestamptz
    AND ($10::text = '' OR weighing_category = $10::text)
),
period_weights AS (
  SELECT DISTINCT ON (wcs.location_id, COALESCE(wcs.partition_label, ''), COALESCE(akmap.canonical_tag, lower(btrim(wo.scanned_identifier))))
         wcs.location_id, wcs.display_name AS shed_name,
         COALESCE(wcs.partition_label, '') AS partition_label,
         -- The park's SHORT CODE (CBE, CPT) falling back to its full name, which is the
         -- convention the shed-weights rows on this same page already use -- so both series
         -- of the gain chart name a park the same way. 39 shed names exist in BOTH parks, so
         -- an unqualified row on this chart is genuinely ambiguous.
         COALESCE(NULLIF(pk.location_code, ''), pk.name, '') AS park_name,
         wo.weight_kg::float8 AS weight_kg,
         -- Same-animal key ($8/$9). The ADG half of this row already keys through
         -- growthPairsCTE, so the period weight/count half must collapse the same
         -- animal too; otherwise a double-RFID animal shows as one ADG animal and
         -- two animals in the count beside it.
         COALESCE(akmap.canonical_tag, lower(btrim(wo.scanned_identifier))) AS animal_key
  FROM weighing_observations wo
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
  LEFT JOIN locations pk
    ON pk.tenant_id = wc.tenant_id AND pk.location_id = wc.park_id
  -- projection-review: membership=the same-animal map resolved by identity_scope.go; group_key=the normalized scanned tag; join_cardinality=0..1 map rows per observation because the map's tag column is unique by construction (DISTINCT ON over normalized identifier value), so this join adds NO rows and cannot fan the shed leaderboard period weights out; pagination=NONE, the map arrives as two bounded bind arrays; scope=tenant + the same park scope and window the caller resolved the map with
  LEFT JOIN unnest($8::text[], $9::text[]) AS akmap(tag, canonical_tag)
    ON akmap.tag = lower(btrim(wo.scanned_identifier))
  WHERE wo.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wo.verification_status <> 'rejected'
    AND wo.accepted_at >= $5::timestamptz
    AND wo.accepted_at < $4::timestamptz
    AND ($10::text = '' OR wcs.weighing_category = $10::text)
    -- HALF-FILTERED IS WORSE THAN UNFILTERED. This row's daily gain already followed the Sex
    -- filter (its pairs CTE carries the predicate) while its kid count and median weight did not,
    -- so one row showed a male-only gain sitting beside an all-kids count -- two populations, one
    -- line, nothing saying so. Same predicate, same population, one row.
    AND (NOT $6::bool OR lower(btrim(wo.scanned_identifier)) = ANY($7::text[]))
  ORDER BY wcs.location_id, COALESCE(wcs.partition_label, ''), COALESCE(akmap.canonical_tag, lower(btrim(wo.scanned_identifier))),
           wo.accepted_at DESC, wo.observation_id DESC
),
shed_weight AS (
  SELECT location_id, partition_label, MAX(shed_name) AS shed_name, MAX(park_name) AS park_name,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY weight_kg) AS median_weight_kg,
         COUNT(DISTINCT animal_key) AS n
  FROM period_weights
  GROUP BY location_id, partition_label
),
shed_adg AS (
  SELECT location_id, partition_label,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_per_day) AS median_adg,
         COUNT(*) AS pair_count
  FROM inperiod
  GROUP BY location_id, partition_label
),
-- The headline's statistic cut per pen: one gain per animal (its total grams over its total days
-- across the legs ending in this pen), then the mean over those animals. Grouped per animal FIRST
-- so a kid weighed four times counts once, exactly as the headline's animal_gain does.
shed_animal_gain AS (
  SELECT location_id, partition_label, avg(g) AS mean_adg, COUNT(*) AS animals
  FROM (
    SELECT location_id, partition_label, animal_key,
           sum((weight_kg - prev_weight) * 1000.0)::float8 / NULLIF(sum(days_between), 0) AS g
    FROM inperiod
    GROUP BY location_id, partition_label, animal_key
  ) per_animal
  GROUP BY location_id, partition_label
)
SELECT sw.location_id, sw.shed_name, sw.partition_label, sw.park_name, sw.n, sw.median_weight_kg,
       COALESCE(sa.median_adg, 0), COALESCE(sa.pair_count, 0),
       sg.mean_adg, COALESCE(sg.animals, 0)
FROM shed_weight sw
LEFT JOIN shed_adg sa ON sa.location_id = sw.location_id AND COALESCE(sa.partition_label, '') = COALESCE(sw.partition_label, '')
LEFT JOIN shed_animal_gain sg ON sg.location_id = sw.location_id AND COALESCE(sg.partition_label, '') = COALESCE(sw.partition_label, '')
ORDER BY sw.shed_name, sw.partition_label`
	bound2, bindErr2 := sqlbind.Bind(q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, idMap.Tags, idMap.CanonicalTags, weighingCategory)
	if bindErr2 != nil {
		return nil, fmt.Errorf("weighing: bind growth query: %w", bindErr2)
	}
	rows, err := r.pool.Query(ctx, bound2.SQL(), bound2.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GrowthShedLeaderboardRow{}
	for rows.Next() {
		var row domain.GrowthShedLeaderboardRow
		if err := rows.Scan(&row.LocationID, &row.DisplayName, &row.PartitionLabel, &row.ParkName, &row.AnimalCount, &row.MedianWeightKg,
			&row.MedianADGGPerDay, &row.ADGPairCount, &row.AverageADGGPerDay, &row.ADGAnimals); err != nil {
			return nil, err
		}
		// A weighing bucket is often a SYNTHETIC per-partition location whose own name is already
		// "Mandela 1 - Part 5", so this goes through ResolveComposedName rather than Display().
		// This site used to carry its own HasSuffix guard; it now shares the one implementation, so
		// the leaderboard and the operator's card can no longer drift apart on the same pen.
		_, _, row.OperationalLocationDisplay = oploc.ResolveComposedName(row.LocationID, row.DisplayName, row.PartitionLabel)
		out = append(out, row)
	}
	return out, rows.Err()
}

// growthDistributionBinWidth and growthDistributionBinCount define the FIXED histogram shape:
// 12 bins of 25 g/day each (0-300), plus an explicit negative bucket and a "300+" overflow, so
// the same bin edges apply across every park and period and results are visually comparable.
const (
	growthDistributionBinWidth = 25.0
	growthDistributionBinCount = 12
)

func (r *Repository) growthDistribution(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, idMap AnimalIdentityMap, weighingCategory string) ([]domain.GrowthDistributionBucket, error) {
	var negativeCount int
	// projection-review: membership=weighing_observations; group_key=bucket; join_cardinality=one_to_many; pagination=multi_row; scope=park_ids
	if err := r.pool.QueryRow(ctx, `WITH `+growthPairsCTE+`),
inperiod AS (SELECT * FROM qualifying WHERE accepted_at >= $5::timestamptz AND ($10::text = '' OR weighing_category = $10::text))
SELECT COUNT(*) FROM inperiod WHERE adg_g_per_day < 0`,
		tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, idMap.Tags, idMap.CanonicalTags, weighingCategory).Scan(&negativeCount); err != nil {
		return nil, err
	}

	maxEdge := growthDistributionBinWidth * growthDistributionBinCount
	// $6/$7 belong to the shared pairs CTE (the sex flag and its tag list), so this query's own
	// two parameters start at $8. Every consumer of growthPairsCTE binds those two in the same
	// positions, which is what lets the CTE carry one predicate for all of them.
	rows, err := r.pool.Query(ctx, `WITH `+growthPairsCTE+`),
inperiod AS (SELECT * FROM qualifying WHERE accepted_at >= $5::timestamptz AND adg_g_per_day >= 0 AND ($10::text = '' OR weighing_category = $10::text))
SELECT width_bucket(adg_g_per_day, 0, $11::float8, $12::int) AS bucket, COUNT(*)
FROM inperiod
GROUP BY bucket
ORDER BY bucket`,
		tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, idMap.Tags, idMap.CanonicalTags,
		weighingCategory, maxEdge, growthDistributionBinCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[int]int, growthDistributionBinCount)
	for rows.Next() {
		var bucket, count int
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, err
		}
		counts[bucket] += count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]domain.GrowthDistributionBucket, 0, growthDistributionBinCount+2)
	out = append(out, domain.GrowthDistributionBucket{Label: "negative", Count: negativeCount})
	for i := 1; i <= growthDistributionBinCount; i++ {
		lo := float64(i-1) * growthDistributionBinWidth
		hi := float64(i) * growthDistributionBinWidth
		loCopy, hiCopy := lo, hi
		out = append(out, domain.GrowthDistributionBucket{
			Label: fmt.Sprintf("%.0f-%.0f", lo, hi), MinGPerDay: &loCopy, MaxGPerDay: &hiCopy, Count: counts[i],
		})
	}
	// width_bucket returns bin count+1 for any value >= the top edge -- that overflow bin is
	// folded into an explicit "300+" bucket rather than silently dropped or mis-binned into the
	// last regular bucket.
	topEdge := maxEdge
	out = append(out, domain.GrowthDistributionBucket{
		Label: fmt.Sprintf("%.0f+", topEdge), MinGPerDay: &topEdge, Count: counts[growthDistributionBinCount+1],
	})
	return out, nil
}

func (r *Repository) growthSaleReadiness(ctx context.Context, tenantID string, parkIDs []string, sexFiltered bool, scope SexScope, idMap AnimalIdentityMap, weighingCategory string) (domain.GrowthSaleReadiness, error) {
	// FAIL LOUD, never quietly empty. An unresolved AllTimeTags is an empty list, and an empty tag
	// list filters every animal out -- so a caller that resolved the window-only scope would report
	// zero sale-ready kids and look like a farm with nothing to sell. That is the kind of wrong
	// number nobody questions, so it is an error instead.
	if sexFiltered && !scope.allTimeResolved {
		return domain.GrowthSaleReadiness{}, fmt.Errorf(
			"weighing: sale readiness spans all time and needs the all-time sex scope; resolve it with ResolveSexScopeWithAllTime")
	}
	// "Latest weight" here is the animal's LATEST-EVER accepted individual weigh, not bounded to
	// the requested period: sale readiness is a point-in-time fact about the animal today, and
	// bounding it to a reporting window would make an animal that was not weighed this month
	// (but is definitely heavy enough to sell, per its last known weight) invisible to the
	// exact question this section answers.
	// projection-review: membership=weighing_observations; group_key=animal_key; join_cardinality=one_to_many; pagination=single_row; scope=park_ids
	rows, err := r.pool.Query(ctx, `
WITH obs AS (
  -- Same-animal key. DISTINCT ON below picks each animal's latest-ever weigh, and a double-tagged
  -- animal keyed by the raw string appeared TWICE with two different "latest" weights -- so it
  -- could be counted once as sale-ready and once as not. The map bound here is the ALL-TIME one,
  -- for the same reason the tag list is (see AllTimeTags below).
  SELECT wo.observation_id, wo.weight_kg::float8 AS weight_kg, wo.accepted_at,
         COALESCE(akmap.canonical_tag, lower(btrim(wo.scanned_identifier))) AS animal_key
  FROM weighing_observations wo
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
  -- projection-review: membership=the same-animal map resolved by identity_scope.go; group_key=the normalized scanned tag; join_cardinality=0..1 map rows per observation because the map's tag column is unique by construction (DISTINCT ON over normalized identifier value), so this join adds NO rows and cannot fan an animal's weighs out under the aggregates below; pagination=NONE, the map arrives as two bounded bind arrays; scope=tenant + the same park scope and window the caller resolved the map with
  LEFT JOIN unnest($6::text[], $7::text[]) AS akmap(tag, canonical_tag)
    ON akmap.tag = lower(btrim(wo.scanned_identifier))
  WHERE wo.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wo.verification_status <> 'rejected'
    -- The Sex filter narrows WHICH KIDS, never the time bound. This block deliberately reads each
    -- animal's latest-EVER weigh rather than the selected window (see the note above), and that is
    -- untouched here -- but a reader on Male must still be told how many MALE kids are heavy enough
    -- to sell. The parameters were previously not even passed, so this block answered about the
    -- whole herd beside a headline about half of it.
    --
    -- AllTimeTags, NOT Tags: the ordinary scope is bounded by the selected window plus the 90-day
    -- gain lookback, and using it here narrowed a latest-EVER count to "weighed recently" -- an
    -- animal last weighed a year ago and long since heavy enough to sell would have dropped out of
    -- the denominator. This farm's data cannot show that today because every weigh in it falls
    -- inside the lookback, which is exactly why the regression test below reaches outside it.
    AND (NOT $3::bool OR lower(btrim(wo.scanned_identifier)) = ANY($4::text[]))
    AND ($5::text = '' OR $5::text = 'individual_animal')
),
latest AS (
  SELECT DISTINCT ON (animal_key) animal_key, weight_kg
  FROM obs
  ORDER BY animal_key, accepted_at DESC, observation_id DESC
)
SELECT COUNT(*), COUNT(*) FILTER (WHERE weight_kg >= 30), COUNT(*) FILTER (WHERE weight_kg >= 35)
FROM latest`, tenantID, parkIDs, sexFiltered, scope.AllTimeTags, weighingCategory, idMap.Tags, idMap.CanonicalTags)
	var s domain.GrowthSaleReadiness
	if err != nil {
		return s, err
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&s.AnimalsConsidered, &s.AtOrAbove30Kg, &s.AtOrAbove35Kg); err != nil {
			return s, err
		}
	}
	return s, rows.Err()
}

func (r *Repository) growthLumpSumTrend(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, weighingCategory string) (domain.GrowthLumpSum, error) {
	// Lump-sum shed totals are read HERE, entirely separately from the individual-observation
	// queries above -- see the GrowthLumpSum domain comment for why a per-animal ADG must never
	// be derived from the delta between two shed-level averages.
	// projection-review: membership=weighing_shed_observations; group_key=location_id_week; join_cardinality=one_to_many; pagination=multi_row; scope=park_ids
	rows, err := r.pool.Query(ctx, `
SELECT wcs.location_id, wcs.display_name, COALESCE(wcs.partition_label, ''),
       (date_trunc('week', wso.accepted_at AT TIME ZONE 'Asia/Kolkata'))::date AS week_start,
       AVG(wso.average_weight_kg::float8) AS avg_weight_kg,
       SUM(wso.animal_count) AS head_count
FROM weighing_shed_observations wso
JOIN weighing_campaign_sheds wcs
  ON wcs.campaign_shed_id = wso.campaign_shed_id AND wcs.tenant_id = wso.tenant_id
JOIN weighing_campaigns wc
  ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wso.tenant_id
WHERE wso.tenant_id = $1::uuid
  AND wc.park_id = ANY($2::uuid[])
  AND wso.verification_status <> 'rejected'
  AND wso.withdrawn_at IS NULL
  AND wso.accepted_at >= $3::timestamptz
  AND wso.accepted_at < $4::timestamptz
  AND ($8::text = '' OR wcs.weighing_category = $8::text)
  -- The Sex filter reaches this trend too. It did NOT, and the parameters were accepted and
  -- silently ignored: every other block of this read narrowed to the selected half of the herd
  -- while the whole-shed trend kept reporting all ten pen-weeks, so a reader on Female saw a
  -- female headline above a trend of pens that hold no females at all. A whole-shed weigh carries
  -- no tag, so it is claimed only when its pen's cohort is entirely that sex (sex_scope.go proves
  -- it); a mixed pen is claimed by neither side, because one shed average cannot be split between
  -- two cohorts.
  AND (NOT $5::bool OR EXISTS (
    SELECT 1 FROM unnest($6::uuid[], $7::text[]) AS b(loc, part)
    WHERE b.loc = wcs.location_id AND b.part = COALESCE(wcs.partition_label, '')
  ))
GROUP BY wcs.location_id, wcs.display_name, COALESCE(wcs.partition_label, ''), week_start
ORDER BY wcs.display_name, COALESCE(wcs.partition_label, ''), week_start`,
		tenantID, parkIDs, periodStart, periodEnd, sexFiltered, scope.LocationIDs, scope.PartitionLabels, weighingCategory)
	if err != nil {
		return domain.GrowthLumpSum{}, err
	}
	defer rows.Close()
	out := domain.GrowthLumpSum{ShedWeekTrend: []domain.GrowthLumpSumShedTrendPoint{}}
	for rows.Next() {
		var p domain.GrowthLumpSumShedTrendPoint
		var weekStart time.Time
		if err := rows.Scan(&p.LocationID, &p.DisplayName, &p.PartitionLabel, &weekStart, &p.AverageWeightKg, &p.HeadCount); err != nil {
			return domain.GrowthLumpSum{}, err
		}
		p.OperationalLocationDisplay = (oploc.OperationalLocation{
			ShedID:         p.LocationID,
			ShedName:       p.DisplayName,
			PartitionLabel: p.PartitionLabel,
		}).Display()
		p.WeekStart = weekStart.Format("2006-01-02")
		out.ShedWeekTrend = append(out.ShedWeekTrend, p)
	}
	return out, rows.Err()
}

// growthParkNames resolves park ids to display names from `locations` -- an allowlisted ORG
// table (where a park is), never herd or animal data. Without it a client has to invent labels
// like "Park 1", which is a lie dressed as a UI.
func (r *Repository) growthParkNames(ctx context.Context, tenantID string, parkIDs []string) ([]domain.GrowthPark, error) {
	if len(parkIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT location_id::text, name
		FROM locations
		WHERE tenant_id = $1::uuid AND location_id = ANY($2::uuid[])
		ORDER BY name ASC
	`, tenantID, parkIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GrowthPark{}
	for rows.Next() {
		var p domain.GrowthPark
		if err := rows.Scan(&p.ParkID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// growthLosingAnimals lists the animals whose MOST RECENT pair in the period shows a loss.
//
// One row per animal (its latest pair), not one per losing pair: a reader asking "which animals
// are losing weight" wants animals, and an animal that dipped once and recovered is not currently
// losing. Bounded so a bad period cannot return the whole herd.
func (r *Repository) growthLosingAnimals(
	ctx context.Context,
	tenantID string,
	parkIDs []string,
	lookbackStart, periodStart, periodEnd time.Time,
	sexFiltered bool,
	scope SexScope,
	idMap AnimalIdentityMap,
	weighingCategory string,
) ([]domain.GrowthLosingAnimal, error) {
	q := `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT * FROM qualifying
  WHERE accepted_at >= $5::timestamptz
    AND ($10::text = '' OR weighing_category = $10::text)
),
latest_pair AS (
  SELECT DISTINCT ON (animal_key) *
  FROM inperiod
  ORDER BY animal_key, accepted_at DESC
)
SELECT animal_key, shed_name, partition_label, location_id::text, prev_weight, weight_kg,
       adg_g_per_day, days_between,
       to_char(TIMEZONE('Asia/Kolkata', accepted_at)::date, 'YYYY-MM-DD')
FROM latest_pair
WHERE adg_g_per_day < 0
ORDER BY adg_g_per_day ASC
LIMIT 200`
	bound3, bindErr3 := sqlbind.Bind(q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, idMap.Tags, idMap.CanonicalTags, weighingCategory)
	if bindErr3 != nil {
		return nil, fmt.Errorf("weighing: bind growth query: %w", bindErr3)
	}
	rows, err := r.pool.Query(ctx, bound3.SQL(), bound3.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GrowthLosingAnimal{}
	for rows.Next() {
		var a domain.GrowthLosingAnimal
		var partitionLabel, locationID string
		if err := rows.Scan(
			&a.ScannedIdentifier, &a.ShedDisplayName, &partitionLabel, &locationID, &a.PreviousWeightKg, &a.LatestWeightKg,
			&a.ADGGPerDay, &a.DaysBetween, &a.LatestWeighDate,
		); err != nil {
			return nil, err
		}
		if partitionLabel != "" && strings.HasSuffix(a.ShedDisplayName, partitionLabel) {
			a.OperationalLocationDisplay = a.ShedDisplayName
		} else {
			a.OperationalLocationDisplay = (oploc.OperationalLocation{
				ShedID:         locationID,
				ShedName:       a.ShedDisplayName,
				PartitionLabel: partitionLabel,
			}).Display()
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// growthParkGains cuts the HEADLINE statistic per park, in ONE query.
//
// It is deliberately the same arithmetic as growthHeadlineStats -- the animal-weighted mean over
// every kid weighed twice (each kid once, at the median of its own pairs) PLUS every whole-shed
// pen weighed twice, each contributing its average-weight movement once per animal it holds
// (maintainer decision 2026-08-26) -- with a GROUP BY park instead of a single row. Two figures
// on one page that claim to be "daily gain" must be the same statistic, so this must never drift
// into a cheaper approximation of the number above it.
//
// The page used to build these cards by calling the whole growth endpoint once per park. That is
// one extra round trip per park on a screen with a sub-500ms budget, and it was removed for page
// speed -- which silently emptied the cards. Computing it here costs no extra request.
//
// projection-review: producer grain is one qualifying PAIR (weighing_observations) and one live
// whole-shed row per (location_id, partition_label); consumer grain is one row per park.
// animal_gain groups by (park_id, animal_key) so an animal counts ONCE in its park, and shed_span
// carries latest.rn = 1 joined to first.rn = 1 on the same pen, so sum(animals) ranges over
// disjoint pens. Numerator and denominator range over the IDENTICAL key set (both read `contrib`,
// same FROM, same WHERE), which is what makes the weighted mean honest. Bounded to the parks in
// scope -- two today -- and never paginated.
// growthParkGainsQuery is hoisted to package level so the query-plan proof and the scale guard
// can both reach it; it is the per-park cut of growthHeadlineStats' own arithmetic.
const growthParkGainsQuery = `WITH ` + growthPairsCTE + `),
-- projection-review: membership=weighing_observations pairs + live weighing_shed_observations; group_key=park_id; join_cardinality=one animal per (park, animal_key), one pen per (location_id, partition_label) via rn=1 both sides; pagination=one row per park in scope, never paged; scope=park_ids, plus the sex/origin scope applied to BOTH arms
--
-- The weighted mean's numerator and denominator range over the IDENTICAL key set: both read the
-- contrib CTE, same FROM, same WHERE. That is what makes sum(gain)/sum(animals) a mean of the
-- population it claims to describe rather than of two different ones.
inperiod AS (
  SELECT * FROM qualifying
  WHERE accepted_at >= $5::timestamptz
    AND ($12::text = '' OR weighing_category = $12::text)
),
-- Total grams over total days per animal, the same statistic the headline and the bucketed series
-- report; see the headline's animal_gain for why a median of leg RATES was wrong.
animal_gain AS (
  SELECT park_id, animal_key,
         sum((weight_kg - prev_weight) * 1000.0)::float8 / NULLIF(sum(days_between), 0) AS g
  FROM inperiod GROUP BY park_id, animal_key
),
shed_span AS (
  SELECT latest.park_id,
         latest.animal_count::float8 AS animals,
         (latest.average_weight_kg - first.average_weight_kg) * 1000.0
           / NULLIF(latest.d - first.d, 0) AS g_per_day
  FROM (
    SELECT c2.park_id, cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label,
           o.average_weight_kg, o.animal_count,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id, COALESCE(cs2.partition_label, '') ORDER BY o.accepted_at DESC) AS rn
    FROM weighing_shed_observations o
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id AND cs2.tenant_id = o.tenant_id
    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id AND c2.tenant_id = o.tenant_id
    WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
      AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
      AND o.accepted_at >= $5::timestamptz AND o.accepted_at < $4::timestamptz
      AND ($12::text = '' OR cs2.weighing_category = $12::text)
      -- A whole-shed weigh carries no tag, so under a Sex/Origin filter it is claimed only when
      -- its pen's cohort is entirely that cohort; a mixed pen is claimed by neither side, exactly
      -- as in the headline query above.
      AND (NOT $6::bool OR EXISTS (
        SELECT 1 FROM unnest($10::uuid[], $11::text[]) AS b(loc, part)
        WHERE b.loc = cs2.location_id AND b.part = COALESCE(cs2.partition_label, '')
      ))
  ) latest
  JOIN (
    SELECT cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label,
           o.average_weight_kg,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id, COALESCE(cs2.partition_label, '') ORDER BY o.accepted_at ASC) AS rn
    FROM weighing_shed_observations o
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id AND cs2.tenant_id = o.tenant_id
    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id AND c2.tenant_id = o.tenant_id
    WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
      AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
      AND o.accepted_at >= $5::timestamptz AND o.accepted_at < $4::timestamptz
      AND ($12::text = '' OR cs2.weighing_category = $12::text)
      AND (NOT $6::bool OR EXISTS (
        SELECT 1 FROM unnest($10::uuid[], $11::text[]) AS b(loc, part)
        WHERE b.loc = cs2.location_id AND b.part = COALESCE(cs2.partition_label, '')
      ))
  ) first ON first.location_id = latest.location_id
    AND first.partition_label = latest.partition_label
    AND first.rn = 1
  WHERE latest.rn = 1 AND latest.d > first.d
),
-- One row per contributing UNIT, so the weighted mean below is a plain sum/sum over one set: a
-- scanned kid weighs 1, a pen of 76 kids weighs 76.
contrib AS (
  SELECT park_id, g AS weighted_gain, 1::float8 AS animals FROM animal_gain
  UNION ALL
  SELECT park_id, animals * g_per_day, animals FROM shed_span
),
park_contrib AS (
  SELECT park_id, sum(weighted_gain) AS weighted_gain, sum(animals) AS animals
  FROM contrib
  GROUP BY park_id
)
-- The park's SHORT CODE (CBE, CPT) when it has one, falling back to its full name -- the SAME
-- rule ShedWeightsRow.ParkName and LoadPlacement.ParkName follow, so a card, a shed row and a
-- load placement cannot disagree about what a park is called on one screen.
SELECT pk.location_id::text, COALESCE(NULLIF(pk.location_code, ''), pk.name, ''),
       pc.weighted_gain / NULLIF(pc.animals, 0),
       COALESCE(pc.animals, 0)::bigint
FROM locations pk
LEFT JOIN park_contrib pc ON pc.park_id = pk.location_id
WHERE pk.tenant_id = $1::uuid AND pk.location_id = ANY($2::uuid[])
ORDER BY COALESCE(NULLIF(pk.location_code, ''), pk.name, '') ASC`

func (r *Repository) growthParkGains(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, idMap AnimalIdentityMap, weighingCategory string) ([]domain.GrowthParkGain, error) {
	rows, err := r.pool.Query(ctx, growthParkGainsQuery, tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, idMap.Tags, idMap.CanonicalTags,
		scope.LocationIDs, scope.PartitionLabels, weighingCategory)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GrowthParkGain{}
	for rows.Next() {
		var p domain.GrowthParkGain
		// The average scans into a POINTER: a park with nothing weighed twice contributes no
		// `contrib` row at all and is simply absent, and one whose movements cancel out is a real
		// 0 -- the two must not be told apart by a fabricated zero.
		if err := rows.Scan(&p.ParkID, &p.ParkName, &p.AverageADGGPerDay, &p.HeadlineAnimals); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
