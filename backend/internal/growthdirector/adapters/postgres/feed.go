package postgres

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"

	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
)

// feedRowsCTE is the shared live-sheet scope: the exact feed_day range (feed is
// day-grain, unlike week-grain weighing campaigns) over the live sheet states,
// matching the feed_direction_issues_live_uidx predicate.
const feedRowsCTE = `
feed_rows AS (
  SELECT r.shed_id, r.shed_label, COALESCE(r.partition_label, '') AS partition_label,
         i.feed_day, r.workflow, r.head_count_informational,
         r.quantity_kg, r.head_count, r.shed_tag_key, r.breed_key,
         r.feed_item_key, r.feed_item_label, r.blocked_reason_code,
         COALESCE(pk.name, '') AS park_name
  FROM feed_direction_issue_rows r
  JOIN feed_direction_issues i
    ON i.tenant_id = r.tenant_id AND i.feed_direction_issue_id = r.feed_direction_issue_id
  LEFT JOIN locations pk
    ON pk.tenant_id = r.tenant_id AND pk.location_id = i.park_id
  WHERE r.tenant_id = $1::uuid
    AND i.park_id = ANY($2::uuid[])
    AND i.feed_day >= $3::date
    AND i.feed_day < $4::date
    AND i.state IN ('issued','amended','locked')
)`

// feedVsGrowth sets feed DIRECTED against growth measured, per shed.
//
// GRAIN (STG-verified 2026-08-11): feed sheets are authored at the PHYSICAL shed
// (18 sheds on STG), while weighing buckets are often synthetic per-partition
// locations ("Godel 1 - Part 3" as its own locations row, parent = park) — only
// 3 of 52 weighing locations join feed_direction_issue_rows.shed_id directly,
// and the locations parent chain does not connect them. The reliable bridge is
// the HERD REGISTER: goats.shed_id is the physical shed feed generation itself
// projects from (1670/1670 goats carry it, all inside the feed-shed set; 92 of
// 95 repeat-weighed tags reach a feed shed through it). So the growth side is
// grouped by each matched kid's goats.shed_id, never by the weighing bucket
// location. Kids whose tag matches no goat drop out of THIS widget only — the
// trust panel carries them.
//
// BLOCKED-VS-ZERO: quantity_kg is NEVER COALESCEd. NULL means blocked (nobody
// authored a ration) and must stay out of the sum — SUM skips NULLs natively —
// while an authored 0.000 is a real instruction and stays in. Any
// COALESCE(quantity_kg, 0) would silently convert a starved shed into a fed one.
//
// HEAD-DAYS: head_count repeats on every feed_item cell of a grain row AND
// across sessions of the same day, so it collapses to MAX per (shed, feed_day,
// shed_tag_key, breed_key) before summing. Experiment / head_count_informational
// rows are excluded from all per-head math — their authored kg is already a shed
// total. They still get a trial efficiency when the shed has positive measured
// gain: trial total feed kg / total kg gained across paired identities.
// projection-review: membership=one row per PEN appearing on a live feed sheet in the window;
// group_key=(shed_id, partition_label) on both sides, joined USING (shed_id, partition_label);
// join_cardinality=goat_identifiers 0..1 (lifetime-unique), goats 1 (PK), canon 0..1 (PK),
// goat_shed_partitions 0..1 (PK (tenant_id, goat_id)) so no side multiplies a pair row;
// pagination=NONE, bounded by the pens on the window's sheets (105 live);
// scope=tenant + park ANY + feed_day half-open window.
//
// The two sides' key sets are identical rather than merely similar: shed_feed and head_days group
// off feed_direction_issue_rows, shed_growth groups off the kid's goat_shed_partitions placement —
// the same placement feed generation projects from. Verified on the live DB before the re-grain:
// 105 feed pens, 105 goat pens, 105 exact matches, 0 feed rows carrying a blank pen.
//
// head_days still pre-collapses head_count with max() per (shed, pen, feed_day, shed_tag, breed)
// BEFORE summing, because head_count repeats on every feed_item cell and every session of a day.
// The kg_feed_per_kg_gain ratio now ranges over exactly one pen on BOTH numerator and denominator.
func (r *Repository) feedVsGrowth(ctx context.Context, tenantID string, parkIDs []string, startDate, endDate string, sexFiltered bool, scope weighingpg.SexScope, weighingCategory string) (domain.FeedVsGrowth, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	out := domain.FeedVsGrowth{Sheds: []domain.FeedVsGrowthShed{}, Estimate: true}
	const q = `
WITH ` + feedRowsCTE + `,
shed_feed AS (
  SELECT shed_id, partition_label, min(shed_label) AS shed_label, min(park_name) AS park_name,
         -- NEVER COALESCE quantity_kg: NULL = blocked cell and must stay out of
         -- the sum; an authored 0.000 is a real instruction and stays in.
         sum(quantity_kg) FILTER (WHERE quantity_kg IS NOT NULL
                                    AND workflow <> 'experiment' AND NOT head_count_informational) AS fed_kg_per_head_basis,
         sum(quantity_kg) FILTER (WHERE quantity_kg IS NOT NULL
                                    AND (workflow = 'experiment' OR head_count_informational)) AS trial_fed_kg,
         count(*) FILTER (WHERE workflow = 'experiment' OR head_count_informational) AS experiment_cells
  FROM feed_rows
  GROUP BY shed_id, partition_label
),
head_days AS (
  SELECT shed_id, partition_label, sum(grain_heads) AS head_days
  FROM (
    SELECT shed_id, partition_label, feed_day, shed_tag_key, breed_key, max(head_count) AS grain_heads
    FROM feed_rows
    WHERE workflow <> 'experiment' AND NOT head_count_informational
    GROUP BY shed_id, partition_label, feed_day, shed_tag_key, breed_key
  ) g
  GROUP BY shed_id, partition_label
),
` + weighingObsCTE + `,
` + roundLatestCTE + `,
` + firstLastPairCTE + `,
adg AS (
  SELECT p.*,
         (w_last - w_first) * 1000.0 / (t_last::date - t_first::date) AS adg_g_day
  FROM pairs p
  WHERE t_last::date > t_first::date
    AND (w_last - w_first) * 1000.0 / (t_last::date - t_first::date) > -300
),
-- Re-grain each pair from its weighing bucket to the kid's PEN via the herd
-- register (see the function comment). The pen, not the physical shed: feed is
-- directed, packed and served one bag per pen, so a shed-grain row would add up
-- ten pens of Mandela 1 and label the total with a name no operator uses --
-- there is no bare "Mandela 1" to walk to. Unmatched tags fall out by design.
--
-- The pen comes from goat_shed_partitions, which is the per-goat placement the
-- feed sheet itself is projected from; its key agrees exactly with the sheet's
-- (verified on the live DB: 105 feed pens, 105 goat pens, 105 exact matches).
-- 'whole' is that table's no-pen sentinel and normalizes to the '' the sheet
-- rows carry, so an undivided shed still joins.
goat_shed AS (
  SELECT COALESCE(canon.shed_id, g.shed_id) AS shed_id,
         COALESCE(NULLIF(gsp.partition_label, 'whole'), '') AS partition_label,
         a.adg_g_day, a.w_first, a.w_last, a.t_first, a.t_last
  FROM adg a
` + breedSexJoin + `
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = $1::uuid AND gsp.goat_id = COALESCE(canon.goat_id, g.goat_id)
),
shed_growth AS (
  SELECT shed_id, partition_label,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_day) AS median_adg_g_day,
         count(*) AS pair_identities,
         (avg(w_last) - avg(w_first))::float8 AS whole_shed_avg_delta_kg,
         GREATEST(max(t_last::date) - min(t_first::date), 1) AS growth_span_days
  FROM goat_shed
  WHERE shed_id IS NOT NULL
  GROUP BY shed_id, partition_label
)
SELECT f.shed_id::text, f.shed_label, f.partition_label, f.park_name,
       f.fed_kg_per_head_basis::float8,
       f.trial_fed_kg::float8,
       f.experiment_cells,
       h.head_days::bigint,
       g.pair_identities,
       g.median_adg_g_day,
       g.whole_shed_avg_delta_kg,
       g.growth_span_days
FROM shed_feed f
LEFT JOIN head_days   h USING (shed_id, partition_label)
LEFT JOIN shed_growth g USING (shed_id, partition_label)
ORDER BY f.shed_label, f.partition_label, f.shed_id`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, startDate, endDate, sexFiltered, scope.Tags,
		scope.LocationIDs, scope.PartitionLabels, weighingCategory)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var shedID, shedLabel, partitionLabel, parkName string
		var fedKg, trialFedKg *float64
		var experimentCells int
		var headDays, pairIdentities, growthSpanDays *int64
		var medianADG, wholeShedDeltaKg *float64
		if err := rows.Scan(&shedID, &shedLabel, &partitionLabel, &parkName, &fedKg, &trialFedKg, &experimentCells, &headDays, &pairIdentities, &medianADG, &wholeShedDeltaKg, &growthSpanDays); err != nil {
			return out, err
		}
		// Park-prefix the label AND carry the pen: both parks field identically-named
		// feed sheds, and within a shed the pen is the unit that is actually fed.
		out.Sheds = append(out.Sheds, buildFeedVsGrowthShed(
			shedID, partitionLabel, operationalLabel(parkName, shedLabel, partitionLabel), fedKg, trialFedKg, experimentCells, headDays, pairIdentities, medianADG, wholeShedDeltaKg, growthSpanDays,
		))
	}
	return out, rows.Err()
}

// buildFeedVsGrowthShed derives the row's nullable figures. Null never means
// zero here: it means "not computable", with the reason readable from the other
// fields on the same row.
func buildFeedVsGrowthShed(
	shedID, partitionLabel, shedLabel string,
	fedKg, trialFedKg *float64, experimentCells int,
	headDays, pairIdentities *int64,
	medianADG, wholeShedDeltaKg *float64, growthSpanDays *int64,
) domain.FeedVsGrowthShed {
	row := domain.FeedVsGrowthShed{
		LocationID:      shedID,
		PartitionLabel:  partitionLabel,
		ShedDisplayName: shedLabel,
		Basis:           domain.FeedGrowthBasisWholeShed,
		IsExperiment:    experimentCells > 0,
	}
	if pairIdentities != nil {
		row.PairIdentities = int(*pairIdentities)
	}

	// Feed per head-day, in grams. Needs a per-head feed basis AND head-days:
	// a shed whose only rows are experiment/informational has neither.
	var feedKgPerHeadDay *float64
	if fedKg != nil && headDays != nil && *headDays > 0 {
		kg := *fedKg / float64(*headDays)
		g := kg * 1000
		feedKgPerHeadDay = &kg
		row.FeedGPerHeadPerDay = &g
	}

	// Growth basis: a per-animal median once >=3 pairs exist; the whole-shed
	// average movement when the pair set is thinner. With no pairs at all the
	// figures stay null — "needs a second weigh" is the honest state.
	var adg *float64
	if row.PairIdentities >= 3 && medianADG != nil {
		row.Basis = domain.FeedGrowthBasisPerAnimal
		adg = medianADG
	} else if row.PairIdentities > 0 && wholeShedDeltaKg != nil && growthSpanDays != nil && *growthSpanDays > 0 {
		perDay := *wholeShedDeltaKg / float64(*growthSpanDays) * 1000
		adg = &perDay
	}
	if adg != nil {
		v := *adg
		row.ADGGPerDay = &v
	}

	// kg feed per kg gained: only when both sides exist and gain is positive —
	// a zero or negative gain would render as infinite or negative feed cost,
	// which is a lie about a shed that simply is not growing.
	if feedKgPerHeadDay != nil && adg != nil && *adg > 0 {
		ratio := *feedKgPerHeadDay / (*adg / 1000)
		row.KgFeedPerKgGain = &ratio
	}
	if row.KgFeedPerKgGain == nil && row.IsExperiment && trialFedKg != nil &&
		wholeShedDeltaKg != nil && pairIdentities != nil && *pairIdentities > 0 {
		totalGainKg := *wholeShedDeltaKg * float64(*pairIdentities)
		if totalGainKg > 0 {
			ratio := *trialFedKg / totalGainKg
			row.KgFeedPerKgGain = &ratio
		}
	}
	return row
}

// feedProblems reports blocked feed-sheet cells. Blocked is STRUCTURAL:
// quantity_kg IS NULL iff blocked_reason_code exists (schema CHECK), so no
// heuristic is involved and an authored zero can never appear as a problem.
func (r *Repository) feedProblems(ctx context.Context, tenantID string, parkIDs []string, startDate, endDate string, sexFiltered bool, scope weighingpg.SexScope) (domain.FeedProblems, error) {
	out := domain.FeedProblems{Items: []domain.FeedProblemItem{}}
	if err := r.feedProblemTotals(ctx, &out, tenantID, parkIDs, startDate, endDate); err != nil {
		return out, err
	}
	if err := r.feedProblemItems(ctx, &out, tenantID, parkIDs, startDate, endDate); err != nil {
		return out, err
	}
	return out, nil
}

func (r *Repository) feedProblemTotals(ctx context.Context, out *domain.FeedProblems, tenantID string, parkIDs []string, startDate, endDate string) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	const q = `
WITH ` + feedRowsCTE + `,
latest_day AS (SELECT max(feed_day) AS d FROM feed_rows)
SELECT
  count(*) FILTER (WHERE quantity_kg IS NULL)                                          AS blocked_history,
  count(*) FILTER (WHERE quantity_kg IS NULL AND feed_day = (SELECT d FROM latest_day)) AS blocked_latest_day,
  count(*) FILTER (WHERE quantity_kg = 0)                                              AS zero_history,
  count(*) FILTER (WHERE quantity_kg = 0 AND feed_day = (SELECT d FROM latest_day))    AS zero_latest_day
FROM feed_rows`
	return r.pool.QueryRow(ctx, q, tenantID, parkIDs, startDate, endDate).Scan(
		&out.BlockedRowsHistory, &out.BlockedRowsLatestDay,
		&out.AuthoredZeroHistory, &out.AuthoredZeroLatestDay,
	)
}

func (r *Repository) feedProblemItems(ctx context.Context, out *domain.FeedProblems, tenantID string, parkIDs []string, startDate, endDate string) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	const q = `
WITH ` + feedRowsCTE + `
SELECT min(shed_label) AS shed_label,
       min(feed_item_label) AS feed_item_label,
       count(DISTINCT feed_day) AS blocked_days,
       (array_agg(blocked_reason_code ORDER BY feed_day DESC))[1] AS latest_reason_code
FROM feed_rows
WHERE quantity_kg IS NULL
GROUP BY shed_id, feed_item_key
ORDER BY max(feed_day) DESC, count(DISTINCT feed_day) DESC, min(shed_label), feed_item_key
LIMIT 200`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, startDate, endDate)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.FeedProblemItem
		if err := rows.Scan(&item.ShedLabel, &item.FeedItemLabel, &item.BlockedDays, &item.LatestReasonCode); err != nil {
			return err
		}
		out.Items = append(out.Items, item)
	}
	return rows.Err()
}
