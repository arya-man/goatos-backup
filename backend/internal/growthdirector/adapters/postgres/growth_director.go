package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"

	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
)

// weighingObsCTE is the shared scope CTE every weighing-backed widget starts
// from. Campaigns are WEEK-grain, so the window is an OVERLAP filter: every
// campaign week touching [start, end) is included in full, which
// period.resolution discloses. Identity is lower(btrim(scanned_identifier)) —
// weighing_observations.animal_id and mismatch_status were DROPPED (migrations
// 000078/000082) and are never referenced. Rework captures (bounced proof,
// weight untrusted) are excluded from all growth math; pending ones are
// included, with the pending count disclosed by the trust panel.
//
// Params (shared by every query in this file set):
//
//	$1 tenant_id uuid, $2 park_ids uuid[],
//	$3 period start date (inclusive), $4 period end date (EXCLUSIVE)
//	$9 weighing_category filter, or blank for both modes
const weighingObsCTE = `
obs AS (
  SELECT o.observation_id,
         lower(btrim(o.scanned_identifier)) AS tag_key,
         o.weight_kg, o.accepted_at, o.campaign_id,
         c.period_start_date,
         cs.location_id AS shed_id, cs.display_name AS shed_label,
         COALESCE(cs.partition_label, '') AS partition_label,
         COALESCE(pk.name, '') AS park_name
  FROM weighing_observations o
  JOIN weighing_campaigns c
    ON c.tenant_id = o.tenant_id AND c.campaign_id = o.campaign_id
  LEFT JOIN weighing_campaign_sheds cs
    ON cs.tenant_id = o.tenant_id AND cs.campaign_shed_id = o.campaign_shed_id
  LEFT JOIN locations pk
    ON pk.tenant_id = o.tenant_id AND pk.location_id = c.park_id
  WHERE o.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND c.status <> 'canceled'
    AND c.period_end_date >= $3::date
    AND c.period_start_date < $4::date
    AND btrim(o.scanned_identifier) <> ''
    AND o.verification_status <> 'rework'
    AND ($9::text = '' OR cs.weighing_category = $9::text)
    -- Sex filter, applied ONCE for every widget that starts from this CTE. $5 is FALSE for the
    -- unfiltered page, which therefore runs exactly the query it ran before. The tag list is
    -- resolved by the weighing package's sex_scope.go, so the Weights page and these widgets
    -- provably talk about the same kids rather than two implementations of "male".
    AND (NOT $5::bool OR lower(btrim(o.scanned_identifier)) = ANY($6::text[]))
)`

// roundLatestCTE collapses repeat scans to ONE weigh per (identity, campaign
// week): newest capture wins, the 000073 grain and 000080 reporting convention.
const roundLatestCTE = `
round_latest AS (
  SELECT DISTINCT ON (tag_key, campaign_id)
         tag_key, campaign_id, period_start_date, weight_kg, accepted_at, observation_id,
         shed_id, shed_label, partition_label, park_name
  FROM obs
  ORDER BY tag_key, campaign_id, accepted_at DESC, observation_id DESC
)`

// firstLastPairCTE reduces each identity with >=2 campaign rounds to its first
// and last round-latest weigh, attributing the shed from the LATEST round so a
// mid-period shifted goat counts in its current shed.
const firstLastPairCTE = `
pairs AS (
  SELECT tag_key,
         (array_agg(weight_kg   ORDER BY period_start_date, accepted_at, observation_id))[1]              AS w_first,
         (array_agg(accepted_at ORDER BY period_start_date, accepted_at, observation_id))[1]              AS t_first,
         (array_agg(weight_kg   ORDER BY period_start_date DESC, accepted_at DESC, observation_id DESC))[1]  AS w_last,
         (array_agg(accepted_at ORDER BY period_start_date DESC, accepted_at DESC, observation_id DESC))[1]  AS t_last,
         (array_agg(shed_id         ORDER BY period_start_date DESC, accepted_at DESC, observation_id DESC))[1] AS shed_id,
         (array_agg(shed_label      ORDER BY period_start_date DESC, accepted_at DESC, observation_id DESC))[1] AS shed_label,
         (array_agg(partition_label ORDER BY period_start_date DESC, accepted_at DESC, observation_id DESC))[1] AS partition_label,
         (array_agg(park_name       ORDER BY period_start_date DESC, accepted_at DESC, observation_id DESC))[1] AS park_name
  FROM round_latest
  GROUP BY tag_key
  HAVING count(*) >= 2
)`

// breedSexJoin resolves breed/sex for a pair row EXCLUSIVELY through
// goat_identifiers -> goats with a one-hop merged_into_goat_id redirect. Shed
// names and feed-sheet breed strings are authored display text and are never
// used. goat_identifiers.normalized_value is UPPER(trim) (identity module's
// normalizer) while the weighing tag key is lower(btrim), so the join is
// normalized_value = upper(tag_key) — the form the lifetime-unique index
// (tenant_id, normalized_value) serves; that uniqueness makes the join 0..1,
// so it can never fan out. No status filter: a retired tag is still
// unambiguous for its lifetime.
const breedSexJoin = `
  JOIN goat_identifiers gi ON gi.tenant_id = $1::uuid AND gi.normalized_value = upper(a.tag_key)
  JOIN goats g             ON g.tenant_id = $1::uuid AND g.goat_id = gi.goat_id
  LEFT JOIN goats canon    ON canon.tenant_id = $1::uuid AND canon.goat_id = g.merged_into_goat_id`

// GetGrowthDirectorWeights builds all six Growth Director widgets for one
// half-open window. parkIDs must be non-empty and already authorization-checked
// by the caller: this method does no scoping of its own.
func (r *Repository) GetGrowthDirectorWeights(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string) (domain.GrowthDirectorWeights, error) {
	loc := biztime.DefaultLocation()
	out := domain.GrowthDirectorWeights{
		Period: domain.Period{
			Start: periodStart.In(loc).Format("2006-01-02"),
			// periodEnd arrives half-open; the label the screen shows is the
			// inclusive last day.
			End:        periodEnd.In(loc).AddDate(0, 0, -1).Format("2006-01-02"),
			Resolution: domain.PeriodResolutionCampaignWeek,
		},
		Parks:        []domain.Park{},
		RoadToSale:   domain.RoadToSale{Bands: emptyBands()},
		FairFight:    domain.FairFight{Cohorts: []domain.FairFightCohort{}},
		SlowGrowth:   domain.SlowGrowth{TargetGPerDay: domain.SlowGrowthTargetGPerDay, Groups: []domain.SlowGrowthGroup{}},
		FeedVsGrowth: domain.FeedVsGrowth{Sheds: []domain.FeedVsGrowthShed{}, Estimate: true},
		FeedProblems: domain.FeedProblems{Items: []domain.FeedProblemItem{}},
	}
	if len(parkIDs) == 0 {
		return out, nil
	}

	// The SQL window params are business DATES rendered in Asia/Kolkata, not
	// timestamps: casting a timestamptz param to ::date inside SQL would apply
	// the session time zone and shift the boundary a day.
	startDate := periodStart.In(loc).Format("2006-01-02")
	endExclusiveDate := periodEnd.In(loc).Format("2006-01-02")

	// The SAME resolver the Weights page uses, called ONCE for all six widgets. Two
	// implementations of "which kids are male" would drift, and one of the two would be the one
	// the reader is looking at; resolving per widget would let a herd write land between two of
	// them and show six widgets about six slightly different populations.
	scope, scopeErr := weighingpg.ResolveSexScope(ctx, r.pool, tenantID, parkIDs, sex, periodStart, periodEnd)
	if scopeErr != nil {
		return out, scopeErr
	}
	// Origin (farm born / purchased) is resolved by the SAME weighing-owned resolver the Weights
	// page uses, for the same reason: these widgets and that page must agree on which pens were
	// bought, or the Growth Director's screen describes a different herd from the one the CEO is
	// reading. Both filters narrow the identical opaque scope, so the six widgets below did not
	// change when the second one was added.
	originScope, originErr := weighingpg.ResolveOriginScope(ctx, r.pool, tenantID, parkIDs, origin, periodStart, periodEnd)
	if originErr != nil {
		return out, originErr
	}
	sexApplied := strings.TrimSpace(sex) != ""
	originApplied := strings.TrimSpace(origin) != ""
	scope = weighingpg.IntersectScopes(scope, sexApplied, originScope, originApplied)
	sexFiltered := sexApplied || originApplied

	parks, err := r.parks(ctx, tenantID, parkIDs)
	if err != nil {
		return out, err
	}
	out.Parks = parks

	if out.RoadToSale, err = r.roadToSale(ctx, tenantID, parkIDs, startDate, endExclusiveDate, sexFiltered, scope, weighingCategory); err != nil {
		return out, err
	}
	if out.FairFight, err = r.fairFight(ctx, tenantID, parkIDs, startDate, endExclusiveDate, sexFiltered, scope, weighingCategory); err != nil {
		return out, err
	}
	if out.SlowGrowth, err = r.slowGrowth(ctx, tenantID, parkIDs, startDate, endExclusiveDate, sexFiltered, scope, weighingCategory); err != nil {
		return out, err
	}
	if out.FeedVsGrowth, err = r.feedVsGrowth(ctx, tenantID, parkIDs, startDate, endExclusiveDate, sexFiltered, scope, weighingCategory); err != nil {
		return out, err
	}
	if out.FeedProblems, err = r.feedProblems(ctx, tenantID, parkIDs, startDate, endExclusiveDate, sexFiltered, scope); err != nil {
		return out, err
	}
	if out.Trust, err = r.trust(ctx, tenantID, parkIDs, startDate, endExclusiveDate, sexFiltered, scope, weighingCategory); err != nil {
		return out, err
	}
	return out, nil
}

func emptyBands() []domain.WeightBand {
	bands := make([]domain.WeightBand, 0, len(domain.BandLabels))
	for _, label := range domain.BandLabels {
		bands = append(bands, domain.WeightBand{Band: label})
	}
	return bands
}

// roadToSale places every kid's latest weight in a band and scores band movement against the
// previous campaign round.
//
// TWO ARMS, ONE BOARD (maintainer decision 2026-09-01). A scanned kid contributes ITSELF at its
// own weight. A whole-shed pen contributes ALL ITS ANIMALS at the pen's average, because that is
// the only weight the pen has. Before this the board read `weighing_observations` alone and so
// answered "where is every kid" from a minority of them -- 226 scanned kids while 555 more sat in
// nine pens it could not see. The pen arm is the same shape the daily-gain headline already uses.
//
// The two arms are DISJOINT by construction: `weighing_category` is fixed when a bucket is created
// and the write path fills exactly one of the two tables, so no animal can be counted twice.
//
// projection-review: membership=one row per weight band with animals in it; group_key=band_idx
// over the UNION of latest-per-identity and latest-per-pen rows; join_cardinality=individual prev
// is 0..1 per tag_key (rn=2 of the same ranking) and lump prev is 0..1 per (location_id,
// partition_label) (rn=2 of its own ranking), goat_identifiers is 0..1 by the lifetime-unique
// (tenant_id, normalized_value) index, and the lump arm joins weighing_campaign_sheds on its
// primary key -- so no side multiplies; pagination=NONE, at most six band rows; scope=tenant +
// park ANY + campaign week overlap.
//
//	PRODUCER UNIQUENESS vs CONSUMER MATCH KEYS, side by side:
//	  latest      unique on tag_key                        [ranked rn = 1]
//	  lump_latest unique on (location_id, partition_label)  [lump_ranked rn = 1]
//	  scored      one row per either of those, UNION ALL of two disjoint key spaces
//	  final       groups on band_idx, summing ANIMALS
//
//	ROW MULTIPLICITY OF EVERY JOINED SIDE:
//	  prev / lump_prev   0..1 per row, absent when weighed only once
//	  goat_identifiers   0..1 per tag, by the lifetime-unique index
//
//	Ratio key sets: none -- every output is a count of animals, not a ratio.
func (r *Repository) roadToSale(ctx context.Context, tenantID string, parkIDs []string, startDate, endDate string, sexFiltered bool, scope weighingpg.SexScope, weighingCategory string) (domain.RoadToSale, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	out := domain.RoadToSale{Bands: emptyBands()}
	const q = `
WITH ` + weighingObsCTE + `,
` + roundLatestCTE + `,
ranked AS (
  SELECT rl.*,
         row_number() OVER (PARTITION BY tag_key ORDER BY period_start_date DESC, accepted_at DESC, observation_id DESC) AS rn,
         count(*)    OVER (PARTITION BY tag_key) AS weigh_rounds
  FROM round_latest rl
),
latest AS (SELECT * FROM ranked WHERE rn = 1),
prev   AS (SELECT * FROM ranked WHERE rn = 2),
-- THE PEN ARM. withdrawn_at IS NOT OPTIONAL: live-row uniqueness on this table is a PARTIAL index
-- (000067), so a reopened-and-resubmitted bucket legitimately keeps its superseded rows and
-- dropping the predicate fans one pen-week out into several.
lump_obs AS (
  SELECT cs.location_id,
         COALESCE(cs.partition_label, '') AS partition_label,
         c.campaign_id, c.period_start_date,
         so.average_weight_kg, so.animal_count, so.accepted_at, so.shed_observation_id
  FROM weighing_shed_observations so
  JOIN weighing_campaign_sheds cs
    ON cs.tenant_id = so.tenant_id AND cs.campaign_shed_id = so.campaign_shed_id
  JOIN weighing_campaigns c
    ON c.tenant_id = cs.tenant_id AND c.campaign_id = cs.campaign_id
  WHERE so.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND c.status <> 'canceled'
    AND cs.status <> 'canceled'
    AND c.period_end_date >= $3::date
    AND c.period_start_date < $4::date
    AND so.withdrawn_at IS NULL
    -- Rework is excluded from all growth math, exactly as the scanned arm excludes it; pending is
    -- included, because an unverified weight is still a measurement.
    AND so.verification_status <> 'rework'
    AND so.animal_count > 0
    AND so.average_weight_kg IS NOT NULL
    AND ($9::text = '' OR cs.weighing_category = $9::text)
    -- The page's cohort filters (Sex, and Origin since 2026-09-01) reach a pen through its BUCKET,
    -- not through a tag it does not have. $5 is FALSE for the unfiltered page, which therefore runs
    -- this arm unnarrowed.
    AND (NOT $5::bool OR EXISTS (
      SELECT 1 FROM unnest($7::uuid[], $8::text[]) AS b(loc, part)
      WHERE b.loc = cs.location_id AND b.part = COALESCE(cs.partition_label, '')
    ))
),
-- ONE WEIGH PER PEN PER CAMPAIGN WEEK, newest wins -- the same round grain round_latest applies to
-- scanned tags, so a pen weighed twice in one week is one round on both arms.
lump_round AS (
  SELECT DISTINCT ON (location_id, partition_label, campaign_id)
         location_id, partition_label, campaign_id, period_start_date,
         average_weight_kg, animal_count, accepted_at, shed_observation_id
  FROM lump_obs
  ORDER BY location_id, partition_label, campaign_id, accepted_at DESC, shed_observation_id DESC
),
lump_ranked AS (
  SELECT lr.*,
         row_number() OVER (PARTITION BY location_id, partition_label
                            ORDER BY period_start_date DESC, accepted_at DESC, shed_observation_id DESC) AS rn
  FROM lump_round lr
),
lump_latest AS (SELECT * FROM lump_ranked WHERE rn = 1),
lump_prev   AS (SELECT * FROM lump_ranked WHERE rn = 2),
-- The two arms meet here, each row carrying the number of ANIMALS it speaks for: one for a scanned
-- kid, the pen's whole head count for a pen. The is_scanned flag keeps the tag-matching counts below
-- answerable -- they are about tags, and a pen has none.
scored AS (
  SELECT width_bucket(l.weight_kg, ARRAY[15,20,25,30,35]::numeric[]) AS band_idx,
         CASE WHEN p.weight_kg IS NULL THEN NULL
              ELSE width_bucket(p.weight_kg, ARRAY[15,20,25,30,35]::numeric[]) END AS prev_band_idx,
         1::int AS animals,
         (gi.goat_id IS NOT NULL) AS is_matched,
         TRUE AS is_scanned
  FROM latest l
  LEFT JOIN prev p USING (tag_key)
  LEFT JOIN goat_identifiers gi
    ON gi.tenant_id = $1::uuid AND gi.normalized_value = upper(l.tag_key)
  UNION ALL
  SELECT width_bucket(ll.average_weight_kg, ARRAY[15,20,25,30,35]::numeric[]),
         CASE WHEN lp.average_weight_kg IS NULL THEN NULL
              ELSE width_bucket(lp.average_weight_kg, ARRAY[15,20,25,30,35]::numeric[]) END,
         ll.animal_count,
         FALSE,
         FALSE
  FROM lump_latest ll
  LEFT JOIN lump_prev lp
    ON lp.location_id = ll.location_id AND lp.partition_label = ll.partition_label
)
-- COALESCE ON EVERY FILTERED sum: a filtered sum over no matching rows is NULL, not 0, and a band
-- holding only scanned kids matches none of the pen filters. Without this the scan fails outright
-- ("cannot scan NULL into *int") on the ordinary case of a farm that weighed nothing by the pen.
SELECT band_idx,
       COALESCE(sum(animals), 0)::int                         AS animal_count,
       count(*) FILTER (WHERE is_scanned)::int                AS identity_count,
       count(*) FILTER (WHERE is_scanned AND is_matched)::int AS matched_count,
       COALESCE(sum(animals) FILTER (WHERE NOT is_scanned), 0)::int AS lump_animals,
       count(*) FILTER (WHERE NOT is_scanned)::int            AS lump_pens,
       COALESCE(sum(animals) FILTER (WHERE prev_band_idx IS NOT NULL), 0)::int AS pair_animals,
       COALESCE(sum(animals) FILTER (WHERE prev_band_idx IS NOT NULL AND band_idx > prev_band_idx), 0)::int AS moved_up,
       COALESCE(sum(animals) FILTER (WHERE prev_band_idx IS NOT NULL AND band_idx = prev_band_idx), 0)::int AS held,
       COALESCE(sum(animals) FILTER (WHERE prev_band_idx IS NOT NULL AND band_idx < prev_band_idx), 0)::int AS moved_down
FROM scored
GROUP BY band_idx
ORDER BY band_idx`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, startDate, endDate, sexFiltered, scope.Tags,
		scope.LocationIDs, scope.PartitionLabels, weighingCategory)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var bandIdx, animalCount, identityCount, matched, lumpAnimals, lumpPens, pairAnimals, movedUp, held, movedDown int
		if err := rows.Scan(&bandIdx, &animalCount, &identityCount, &matched,
			&lumpAnimals, &lumpPens, &pairAnimals, &movedUp, &held, &movedDown); err != nil {
			return out, err
		}
		if bandIdx >= 0 && bandIdx < len(out.Bands) {
			out.Bands[bandIdx].AnimalCount = animalCount
		}
		out.TotalAnimals += animalCount
		out.TotalIdentities += identityCount
		out.MatchedIdentities += matched
		out.LumpSumAnimals += lumpAnimals
		out.LumpSumPens += lumpPens
		out.Movement.PairAnimals += pairAnimals
		out.Movement.MovedUp += movedUp
		out.Movement.Held += held
		out.Movement.MovedDown += movedDown
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.UnmatchedIdentities = out.TotalIdentities - out.MatchedIdentities
	return out, nil
}

// trust is the honest-denominator panel. Unlike the growth widgets it INCLUDES
// rework captures — trust reports the raw stream — and breaks out the rework
// count so the two views reconcile. The lump-sum side MUST filter
// withdrawn_at IS NULL: live-row uniqueness is a PARTIAL index (000067), and
// dropping the predicate fans out reopened buckets.
func (r *Repository) trust(ctx context.Context, tenantID string, parkIDs []string, startDate, endDate string, sexFiltered bool, scope weighingpg.SexScope, weighingCategory string) (domain.Trust, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	var out domain.Trust
	const q = `
WITH obs AS (
  SELECT o.observation_id, lower(btrim(o.scanned_identifier)) AS tag_key,
         o.verification_status, o.campaign_id
  FROM weighing_observations o
  JOIN weighing_campaigns c ON c.tenant_id = o.tenant_id AND c.campaign_id = o.campaign_id
  LEFT JOIN weighing_campaign_sheds cs ON cs.tenant_id = o.tenant_id AND cs.campaign_shed_id = o.campaign_shed_id
  WHERE o.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND c.status <> 'canceled'
    AND c.period_end_date >= $3::date
    AND c.period_start_date < $4::date
    AND btrim(o.scanned_identifier) <> ''
    AND ($9::text = '' OR cs.weighing_category = $9::text)
    AND (NOT $5::bool OR lower(btrim(o.scanned_identifier)) = ANY($6::text[]))
),
tagged AS (
  SELECT o.*, (gi.goat_id IS NOT NULL) AS is_matched
  FROM obs o
  LEFT JOIN goat_identifiers gi
    ON gi.tenant_id = $1::uuid AND gi.normalized_value = upper(o.tag_key)
),
identity_rounds AS (
  SELECT tag_key, count(DISTINCT campaign_id) AS weigh_rounds
  FROM tagged GROUP BY tag_key
),
shed_obs AS (
  SELECT count(*) FILTER (WHERE s.withdrawn_at IS NULL) AS live_shed_observations
  FROM weighing_shed_observations s
  JOIN weighing_campaigns c ON c.tenant_id = s.tenant_id AND c.campaign_id = s.campaign_id
  JOIN weighing_campaign_sheds cs ON cs.tenant_id = s.tenant_id AND cs.campaign_shed_id = s.campaign_shed_id
  WHERE s.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND c.status <> 'canceled'
    AND c.period_end_date >= $3::date
    AND c.period_start_date < $4::date
    AND ($9::text = '' OR cs.weighing_category = $9::text)
    -- A whole-shed weigh has no tag, so under a filter it counts only when its shed's cohort is
    -- that sex — the same claim rule the Weights page applies, from the same resolver.
    AND (NOT $5::bool OR EXISTS (
      SELECT 1 FROM unnest($7::uuid[], $8::text[]) AS b(loc, part)
      WHERE b.loc = cs.location_id AND b.part = COALESCE(cs.partition_label, '')
    ))
)
SELECT
  (SELECT count(*) FROM tagged)                                                  AS scans_total,
  (SELECT count(*) FILTER (WHERE is_matched) FROM tagged)                        AS scans_matched,
  (SELECT count(*) FILTER (WHERE NOT is_matched) FROM tagged)                    AS scans_unmatched,
  (SELECT count(*) FILTER (WHERE verification_status = 'pending') FROM tagged)   AS scans_pending,
  (SELECT count(*) FILTER (WHERE verification_status = 'rework') FROM tagged)    AS scans_rework,
  (SELECT count(*) FROM identity_rounds)                                         AS identities_total,
  (SELECT count(*) FILTER (WHERE weigh_rounds >= 2) FROM identity_rounds)        AS identities_with_pair,
  (SELECT count(*) FILTER (WHERE weigh_rounds = 1) FROM identity_rounds)         AS identities_once_only,
  s.live_shed_observations
FROM shed_obs s`
	err := r.pool.QueryRow(ctx, q, tenantID, parkIDs, startDate, endDate,
		sexFiltered, scope.Tags, scope.LocationIDs, scope.PartitionLabels, weighingCategory).Scan(
		&out.ScansTotal, &out.ScansMatched, &out.ScansUnmatched,
		&out.ScansPendingVerification, &out.ScansRework,
		&out.IdentitiesTotal, &out.IdentitiesWithPair, &out.IdentitiesOnceOnly,
		&out.WholeShedObservations,
	)
	return out, err
}
