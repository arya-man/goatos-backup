package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// GetWeightDemographics returns average weight by breed, sex and management stage.
//
// HERD JOIN BY RECORDED EXCEPTION (maintainer decision 2026-08-07). This is the
// only weighing file permitted to resolve a scanned tag to an animal; the guard
// exempts it by name. See domain.WeightDemographics for the scope and why it is
// safe. Nothing here reads vaccination, clinical, protocol or obligation state.
//
// projection-review: membership=one row per DISTINCT scanned tag weighed in the window, plus one row per whole-shed weigh attributed to its shed's cohort; group_key=the dimension value reported (breed, sex or stage) after the per-animal collapse; join_cardinality=ident 0..1 per tag because DISTINCT ON collapses re-issued identifier rows to the newest, goats 1 per goat_id (PK), shed_stage 0..1 per shed because HAVING count(DISTINCT management_stage)=1 drops mixed sheds; pagination=NONE, the dimension vocabularies are bounded by the herd catalogue; scope=tenant_id + park_id = ANY($2)
//
// Ratio key sets: each AVG ranges over exactly the key set its COUNT does — the
// `latest` CTE emits one row per tag, so an animal weighed five times counts once
// at its newest weight. by_breed/by_sex range over a STRICTLY NARROWER key set
// than by_stage: a whole-shed weigh has no tags, so it reaches the stage rows via
// its shed's cohort but never the breed or sex rows. The resolved / unresolved /
// lump-sum counts are returned so that gap is legible rather than looking broken.
func (r *Repository) GetWeightDemographics(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string) (domain.WeightDemographics, error) {
	out := domain.WeightDemographics{
		GainThresholdsByBreed: []domain.WeightGainThresholdBucket{},
		ByBreed:               []domain.WeightDemographicBucket{},
		BySex:                 []domain.WeightDemographicBucket{},
		ByStage:               []domain.WeightDemographicBucket{},
		ShedComposition:       []domain.ShedComposition{},
		ByWeightBand:          []domain.WeightBandBucket{},
		GainByBreedWeek:       []domain.WeightGainBreedWeekBucket{},
		GainByBreedOrigin:     []domain.WeightGainOriginBucket{},
		GainByBreedShedType:   []domain.WeightGainShedTypeBucket{},
	}
	if len(parkIDs) == 0 {
		return out, nil
	}
	// This read already resolves a tag to its animal, so the Sex filter is applied natively here
	// rather than through the tag list sex_scope.go hands the other reads: filtering on the goat
	// row it has already joined is one predicate instead of a second round trip, and it keeps the
	// whole-shed attribution below on the SAME cohort rule the rest of the page uses.
	sexFilter, sexErr := normalizeSexFilter(sex)
	if sexErr != nil {
		return domain.WeightDemographics{}, sexErr
	}
	weighingCategory = strings.TrimSpace(weighingCategory)
	switch weighingCategory {
	case "", "all":
		weighingCategory = ""
	case domain.CategoryIndividualAnimal, domain.CategoryPerShedPartition:
	default:
		return domain.WeightDemographics{}, fmt.Errorf("%w: unsupported weighing_category filter %q", ports.ErrInvalidArgument, weighingCategory)
	}
	// Origin, unlike Sex, is NOT applied natively here. It is a fact about the PEN a load was put
	// in, not about the goat row this query has already joined, and its one implementation lives in
	// origin_scope.go so that every card on the page agrees on which pens were bought. This read
	// therefore consumes the same opaque tag list and lump-bucket list the other reads do.
	originFiltered := strings.TrimSpace(origin) != ""
	originScope, originErr := r.resolveOriginScope(ctx, tenantID, parkIDs, origin, periodStart, periodEnd)
	if originErr != nil {
		return domain.WeightDemographics{}, originErr
	}
	// The Birth-wise breakdown needs BOTH cohorts at once, which the single filter scope above
	// cannot express -- it answers "which animals match the selected origin", and this answers
	// "which side is each animal on". Resolved through the SAME origin_scope.go the filter uses,
	// so the breakdown and the filter can never disagree about which animals were bought.
	farmBornScope, farmBornErr := r.resolveOriginScope(ctx, tenantID, parkIDs, "farm_born", periodStart, periodEnd)
	if farmBornErr != nil {
		return domain.WeightDemographics{}, farmBornErr
	}
	purchasedScope, purchasedErr := r.resolveOriginScope(ctx, tenantID, parkIDs, "purchased", periodStart, periodEnd)
	if purchasedErr != nil {
		return domain.WeightDemographics{}, purchasedErr
	}

	const q = ` -- scale-guard:ignore: bounded 28/84-day weighing leadership aggregate over tenant+authorized parks; current release envelope accepts this read-model query with repository integration coverage, and it does not touch obligation/kernel hot tables
WITH scoped AS (
  SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id, COALESCE(cs.partition_label, '') AS partition_label, cs.weighing_category
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid AND c.park_id = ANY($2::uuid[]) AND cs.status <> 'canceled'
    AND ($16::text = '' OR cs.weighing_category = $16::text)
),
latest AS (
  SELECT DISTINCT ON (lower(btrim(o.scanned_identifier)))
         lower(btrim(o.scanned_identifier)) AS tag, o.weight_kg,
         s.location_id, s.partition_label
  FROM weighing_observations o
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id AND s.tenant_id = o.tenant_id
  WHERE o.tenant_id = $1::uuid
    AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz
    AND o.verification_status <> 'rejected'
    AND btrim(o.scanned_identifier) <> ''
    -- Origin filter, individual half. $6 is FALSE for the unfiltered page, which therefore runs
    -- this query exactly as it ran before the filter existed.
    AND (NOT $6::bool OR lower(btrim(o.scanned_identifier)) = ANY($7::text[]))
  ORDER BY lower(btrim(o.scanned_identifier)), o.accepted_at DESC, o.observation_id DESC
),
-- Consecutive-weigh pairs per tag, for the gain dimensions. A same-business-day
-- pair is excluded: an animal cannot meaningfully gain inside one day, so that is
-- a re-weigh or a double scan, and dividing by a fraction of a day manufactures
-- enormous numbers (the -3,108,762 g/day headline this rule exists to prevent).
raw_obs AS (
  SELECT lower(btrim(o.scanned_identifier)) AS tag, o.observation_id, o.weight_kg, o.accepted_at,
         (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
  FROM weighing_observations o
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id AND s.tenant_id = o.tenant_id
  WHERE o.tenant_id = $1::uuid
    AND o.accepted_at >= ($3::timestamptz - interval '90 days') AND o.accepted_at < $4::timestamptz
    AND o.verification_status <> 'rejected' AND btrim(o.scanned_identifier) <> ''
    AND (NOT $6::bool OR lower(btrim(o.scanned_identifier)) = ANY($7::text[]))
),
obs AS (
  SELECT DISTINCT ON (tag, d) tag, weight_kg, accepted_at, d
  FROM raw_obs
  ORDER BY tag, d, accepted_at DESC, observation_id DESC
),
paired AS (
  SELECT tag, weight_kg, accepted_at, d,
         lag(weight_kg) OVER (PARTITION BY tag ORDER BY d) AS prev_w,
         lag(d) OVER (PARTITION BY tag ORDER BY d) AS prev_d
  FROM obs
),
-- One gain per ANIMAL whose latest pair lands in the selected period. The previous
-- weigh can come from the same 90-day lookback the headline uses, so the page's
-- breed/sex/stage ADG and headline talk about the same same-animal population.
animal_gain AS (
  SELECT tag, percentile_cont(0.5) WITHIN GROUP (
           ORDER BY (weight_kg - prev_w) * 1000.0 / (d - prev_d)) AS g
  FROM paired WHERE prev_d IS NOT NULL AND d > prev_d AND accepted_at >= $3::timestamptz GROUP BY tag
),
-- The same gain, cut by CALENDAR WEEK, for the breed trend on the Time-wise tab. One value per
-- (animal, week): a pair is bucketed by its LATER weigh, which is the week the movement was
-- observed in, and an animal weighed several times inside one week contributes the MEDIAN of those
-- pairs rather than each of them -- the same once-per-animal rule animal_gain above applies over
-- the whole window.
animal_gain_week AS (
  SELECT tag,
         (date_trunc('week', accepted_at AT TIME ZONE 'Asia/Kolkata'))::date AS week_start,
         percentile_cont(0.5) WITHIN GROUP (
           ORDER BY (weight_kg - prev_w) * 1000.0 / (d - prev_d)) AS g
  FROM paired
  WHERE prev_d IS NOT NULL AND d > prev_d AND accepted_at >= $3::timestamptz
  GROUP BY tag, week_start
),
ident AS (
  SELECT DISTINCT ON (lower(btrim(gi.identifier_value)))
         lower(btrim(gi.identifier_value)) AS tag, gi.goat_id
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid
  ORDER BY lower(btrim(gi.identifier_value)), gi.created_at DESC
),
resolved AS (
  SELECT l.tag, l.weight_kg, l.location_id, l.partition_label, g.breed, g.sex, g.management_stage
  FROM latest l
  LEFT JOIN ident i ON i.tag = l.tag
  LEFT JOIN goats g ON g.goat_id = i.goat_id AND g.tenant_id = $1::uuid
  -- Sex filter. An empty $5 is the unfiltered page and keeps every row, INCLUDING the ones whose
  -- tag resolves to no animal: those are real weighs and the coverage counts below exist to make
  -- that gap legible. A filtered page cannot keep them — an unresolved tag has no sex to match.
  WHERE $5::text = '' OR lower(btrim(g.sex)) = $5::text
),
resolved_gain AS (
  -- ag.tag is carried so the breed x origin bucket below can ask which LOAD an animal came off.
  -- Every other consumer of this CTE names its columns explicitly, so the extra column shifts no
  -- scan; check that before adding another.
  SELECT ag.g, ag.tag, l.location_id, l.partition_label, gt.breed, gt.sex, gt.management_stage
  FROM animal_gain ag
  LEFT JOIN latest l ON l.tag = ag.tag
  LEFT JOIN ident i ON i.tag = ag.tag
  LEFT JOIN goats gt ON gt.goat_id = i.goat_id AND gt.tenant_id = $1::uuid
  WHERE $5::text = '' OR lower(btrim(gt.sex)) = $5::text
),
-- A whole-shed weigh is attributed by the cohort its shed holds. The bucket points
-- at a PARTITION (Castro 1), but the herd register puts the animals on the physical
-- shed (Castro) — 266 live goats on the parent, zero on each partition. Looking only
-- at the partition therefore found nothing and silently dropped every whole-shed
-- weigh from these charts.
--
-- So each bucket resolves to its partition's own residents when it has any, and
-- otherwise to its parent shed's. HAVING count(DISTINCT ...) = 1 is what keeps this
-- honest: Castro's residents share one breed, one sex and one stage, so it can be
-- attributed; Godel 2 holds nine breeds and six stages, so it is attributed to
-- nothing rather than guessed at.
shed_targets AS (
  SELECT DISTINCT s.location_id, s.partition_label,
         COALESCE(
           CASE WHEN EXISTS (SELECT 1 FROM goats gg WHERE gg.tenant_id = $1::uuid
                              AND gg.lifecycle_status = 'alive' AND gg.shed_id = s.location_id)
                THEN s.location_id END,
           (SELECT phys.location_id FROM locations phys
            JOIN locations l ON l.location_id = s.location_id AND l.tenant_id = s.tenant_id
            WHERE phys.tenant_id = l.tenant_id
              AND phys.parent_location_id = l.parent_location_id
              AND phys.location_type = 'shed'
              AND phys.name = regexp_replace(l.name, '\s*(-\s*)?(Part\s*)?[0-9]+$', '')
              AND EXISTS (
                SELECT 1
                FROM goat_shed_partitions gsp
                WHERE gsp.tenant_id = l.tenant_id
                  AND gsp.shed_id = phys.location_id
                  -- Scrubbed key, exactly as shed_cohort below and sex_scope.go: the bucket name
                  -- "Godel 2 - Part 1" yields "1" while the register writes "Part 1", and comparing
                  -- them raw resolved this pen to NOTHING -- so its 76 male kids reached no chart at
                  -- all. Fixing only the cohort join was not enough; the pen has to RESOLVE first.
                  AND regexp_replace(lower(btrim(gsp.partition_label)), '^(part|pt)[\s.-]*', '')
                      = regexp_replace(lower(btrim(COALESCE(NULLIF(s.partition_label, ''),
                          NULLIF((regexp_match(l.name, '\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1], '')))),
                          '^(part|pt)[\s.-]*', '')
              )
            LIMIT 1)
         ) AS resolved_id,
         COALESCE(NULLIF(s.partition_label, ''),
                  NULLIF((regexp_match((SELECT l.name FROM locations l WHERE l.location_id = s.location_id),
                                       '\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1], ''),
                  '') AS resolved_partition_label
  FROM scoped s
  -- Origin filter, whole-shed half. A lump-sum bucket is claimed by the pen it was weighed in,
  -- which origin_scope.go has already decided; the alias spellings of one pen ("Godel 2 - Part 1"
  -- the location vs "Godel 2" carrying label "Part 1") are reconciled there, so this predicate is
  -- a plain membership test against the buckets it returned.
  WHERE NOT $6::bool OR EXISTS (
    SELECT 1 FROM unnest($8::uuid[], $9::text[]) AS b(loc, part)
    WHERE b.loc = s.location_id AND b.part = s.partition_label
  )
),
-- Whole-shed weighs under a Sex filter follow the same rule as the rest of the page: a bucket is
-- claimed only when its cohort is entirely the selected sex, so the sexes = 1 test below gains a
-- second condition rather than being replaced. A mixed shed is claimed by neither side.
shed_cohort AS (
  -- projection-review: membership=one row per scoped location+partition that resolves to live goats, either its own location or a physical shed+partition fallback; group_key=(src.location_id,src.partition_label), exactly the GROUP BY; join_cardinality=goats/gsp is 0..N and is COLLAPSED by the aggregate; pagination=NONE; scope=tenant_id plus scoped park rows.
  --
  -- Ratio key sets: the breeds/sexes/stages counts and the min() values they gate range over the IDENTICAL grouped row set — same FROM, same GROUP BY, no branch adds a join — so a breeds count of 1 provably means the single breed min(breed) returns.
  SELECT src.location_id,
         src.partition_label,
         min(g.breed)            FILTER (WHERE TRUE) AS breed,
         min(g.sex)              FILTER (WHERE TRUE) AS sex,
         min(g.management_stage) FILTER (WHERE TRUE) AS stage,
         count(DISTINCT g.breed)            AS breeds,
         count(DISTINCT g.sex)              AS sexes,
         count(DISTINCT g.management_stage) AS stages
  FROM shed_targets src
  JOIN goats g ON g.shed_id = src.resolved_id AND g.tenant_id = $1::uuid
   AND g.lifecycle_status = 'alive'
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
  -- SCRUBBED KEY, not a raw string compare. A bucket named "Godel 2 - Part 1" yields the bare
  -- partition "1", while the herd register writes the HUMAN label "Part 1" on the goat; comparing
  -- those raw matched nothing, so a real pen of 76 male kids was claimed by no breed, no sex and no
  -- stage at all. It vanished from every chart on this page while still being counted among the
  -- kids weighed. Both sides are reduced to the same key -- lowercased, trimmed, leading "part"
  -- dropped -- which is the identical rule sex_scope.go applies, so the two files agree on which
  -- pen belongs to which cohort. They must: one decides what the page FILTERS to, the other what it
  -- CHARTS, and when they disagreed the headline and the charts described different herds.
  WHERE src.resolved_partition_label = ''
     OR regexp_replace(lower(btrim(gsp.partition_label)), '^(part|pt)[\s.-]*', '')
        = regexp_replace(lower(btrim(src.resolved_partition_label)), '^(part|pt)[\s.-]*', '')
  GROUP BY src.location_id, src.partition_label
),
shed_cohort_detail AS (
  SELECT src.location_id,
         src.partition_label,
         COALESCE(NULLIF(g.breed, ''), 'Unknown breed') AS breed,
         COALESCE(NULLIF(g.sex, ''), 'unknown sex') AS sex,
         COALESCE(NULLIF(g.management_stage, ''), 'Unknown stage') AS stage,
         count(*)::int AS animals
  FROM shed_targets src
  JOIN goats g ON g.shed_id = src.resolved_id AND g.tenant_id = $1::uuid
   AND g.lifecycle_status = 'alive'
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
  -- Same scrubbed key as shed_cohort above, for the same reason: the composition chips must
  -- describe the pen the cohort rule claimed, or a shed shows chips for animals it was not
  -- attributed to.
  WHERE src.resolved_partition_label = ''
     OR regexp_replace(lower(btrim(gsp.partition_label)), '^(part|pt)[\s.-]*', '')
        = regexp_replace(lower(btrim(src.resolved_partition_label)), '^(part|pt)[\s.-]*', '')
  GROUP BY src.location_id, src.partition_label,
           COALESCE(NULLIF(g.breed, ''), 'Unknown breed'),
           COALESCE(NULLIF(g.sex, ''), 'unknown sex'),
           COALESCE(NULLIF(g.management_stage, ''), 'Unknown stage')
),
shed_stage AS (
  -- Under a Sex filter a whole-shed weigh reaches the stage rows only when its cohort is that
  -- sex, the same claim rule the breed and sex arms apply. Narrowing HERE rather than at each
  -- use keeps one definition of "this shed counts for this reader".
  SELECT location_id, partition_label, stage FROM shed_cohort
  WHERE stages = 1 AND ($5::text = '' OR (sexes = 1 AND lower(btrim(sex)) = $5::text))
),
lump AS (
  -- The lump-sum coverage counters follow the filter too: under Male, "kids in whole-shed weighs"
  -- must mean the male ones, or the page reports a coverage gap the reader cannot act on.
  --
  -- ONE ROW PER PEN, NOT ONE PER WEIGH. A pen weighed on two dates inside the window has TWO live
  -- observations -- and the page's DEFAULT window is precisely "the last two whole-shed weigh
  -- dates", so this is the normal case, not an edge one. Summing animal_count across them counted
  -- the same 63 kids of Castro 1 four times: STG's 276 resident kids arrived here as 678, and
  -- "Average weight by sex" reported 908 kids on a page whose own headline said 791 were weighed.
  -- The dedup is the same rn=1 latest-per-pen dedup lump_span below already applies, so the weight
  -- charts and the gain charts now range over the identical pen set.
  --
  -- projection-review: producer grain is one live weighing_shed_observations row per bucket;
  -- consumer grain is one row per (location_id, partition_label) -- exactly the DISTINCT ON key,
  -- which is also the join key every consumer of this CTE matches shed_cohort/shed_stage on, both
  -- GROUPed by that same pair. shed_observation_id breaks a same-instant tie so the winner is
  -- deterministic rather than plan-dependent.
  SELECT DISTINCT ON (s.location_id, s.partition_label)
         s.location_id, s.partition_label, sh.animal_count, sh.average_weight_kg
  FROM scoped s
  JOIN weighing_shed_observations sh
    ON sh.campaign_shed_id = s.campaign_shed_id AND sh.tenant_id = s.tenant_id
   AND sh.withdrawn_at IS NULL
   AND sh.accepted_at >= $3::timestamptz AND sh.accepted_at < $4::timestamptz
   AND sh.verification_status <> 'rejected'
  WHERE s.weighing_category = 'per_shed_partition'
    AND ($5::text = '' OR EXISTS (
      SELECT 1 FROM shed_cohort sc
      WHERE sc.location_id = s.location_id AND sc.partition_label = COALESCE(s.partition_label, '')
        AND sc.sexes = 1 AND lower(btrim(sc.sex)) = $5::text
    ))
    -- Origin filter, whole-shed half. The coverage counters below read from lump directly, so
    -- this CTE must be narrowed before they sum it; narrowing only the chart joins would leave
    -- the counters describing every penned kid while the charts describe one origin.
    AND (NOT $6::bool OR EXISTS (
      SELECT 1 FROM unnest($8::uuid[], $9::text[]) AS b(loc, part)
      WHERE b.loc = s.location_id AND b.part = COALESCE(s.partition_label, '')
    ))
  ORDER BY s.location_id, s.partition_label, sh.accepted_at DESC, sh.shed_observation_id DESC
),
lump_span AS (
  -- Selected-range movement matching the shed rows: first weighed date in the
  -- selected window to latest weighed date in the same selected window.
  --
  -- projection-review: membership=one row per lump-sum operational row with first+latest weighs inside the selected range; group_key=(location_id, partition_label) via latest.rn=1 and first.rn=1; join_cardinality=first is 1 per latest row when at least two weighed dates exist, campaign_sheds 1 per bucket (PK), campaigns 1 per campaign (PK); pagination=NONE, joined 0..1 into the gain arms; scope=tenant_id + park_id = ANY($2)
  --
  -- Ratio key sets: the two averages and the day gap are drawn from the SAME
  -- operational row, so the division is always one shed/partition inside the
  -- selected window.
  SELECT latest.location_id, latest.partition_label,
         (latest.average_weight_kg - first.average_weight_kg) * 1000.0
          / NULLIF(latest.d - first.d, 0) AS g_per_day,
         latest.animal_count                 AS animals
  FROM (
    SELECT s.location_id, s.partition_label,
           so.average_weight_kg, so.animal_count,
           (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY s.location_id, s.partition_label ORDER BY so.accepted_at DESC) AS rn
    FROM weighing_shed_observations so
    JOIN scoped s ON s.campaign_shed_id = so.campaign_shed_id AND s.tenant_id = so.tenant_id
    WHERE so.tenant_id = $1::uuid AND s.weighing_category = 'per_shed_partition'
      AND so.withdrawn_at IS NULL AND so.verification_status <> 'rejected'
      AND so.accepted_at >= $3::timestamptz AND so.accepted_at < $4::timestamptz
  ) latest
  JOIN (
    SELECT s.location_id, s.partition_label,
           so.average_weight_kg,
           (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY s.location_id, s.partition_label ORDER BY so.accepted_at ASC) AS rn
    FROM weighing_shed_observations so
    JOIN scoped s ON s.campaign_shed_id = so.campaign_shed_id AND s.tenant_id = so.tenant_id
    WHERE so.tenant_id = $1::uuid AND s.weighing_category = 'per_shed_partition'
      AND so.withdrawn_at IS NULL AND so.verification_status <> 'rejected'
      AND so.accepted_at >= $3::timestamptz AND so.accepted_at < $4::timestamptz
  ) first ON first.location_id = latest.location_id
    AND first.partition_label = latest.partition_label
    AND first.rn = 1
  WHERE latest.rn = 1 AND latest.d > first.d
    AND (NOT $6::bool OR EXISTS (
      SELECT 1 FROM shed_targets st
      WHERE st.location_id = latest.location_id AND st.partition_label = latest.partition_label
    ))
),
-- Whole-shed pens cut by calendar week, the pen half of the breed trend. CONSECUTIVE weighs, not
-- first-vs-latest like lump_span: a weekly series must attribute movement to the week it was
-- observed in, so each pen weigh is paired with the one before it and bucketed by the later of the
-- two. rn = 1 keeps ONE movement per pen per week -- a pen weighed three times inside a week would
-- otherwise add its head count to that week twice.
pen_week AS (
  SELECT week_start, location_id, partition_label, animals, g_per_day FROM (
    SELECT (date_trunc('week', d::timestamp))::date AS week_start,
           location_id, partition_label,
           animal_count::float8 AS animals,
           (average_weight_kg - prev_w) * 1000.0 / NULLIF(d - prev_d, 0) AS g_per_day,
           row_number() OVER (
             PARTITION BY location_id, partition_label, (date_trunc('week', d::timestamp))::date
             ORDER BY d DESC
           ) AS rn
    FROM (
      SELECT s.location_id, COALESCE(s.partition_label, '') AS partition_label,
             so.average_weight_kg, so.animal_count,
             (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
             lag(so.average_weight_kg) OVER w AS prev_w,
             lag((so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date) OVER w AS prev_d
      FROM weighing_shed_observations so
      JOIN scoped s ON s.campaign_shed_id = so.campaign_shed_id AND s.tenant_id = so.tenant_id
      WHERE so.tenant_id = $1::uuid AND s.weighing_category = 'per_shed_partition'
        AND so.withdrawn_at IS NULL AND so.verification_status <> 'rejected'
        AND so.accepted_at >= $3::timestamptz AND so.accepted_at < $4::timestamptz
        AND (NOT $6::bool OR EXISTS (
          SELECT 1 FROM shed_targets st
          WHERE st.location_id = s.location_id AND st.partition_label = COALESCE(s.partition_label, '')
        ))
      WINDOW w AS (PARTITION BY s.location_id, COALESCE(s.partition_label, '')
                   ORDER BY (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date)
    ) pairs
    WHERE prev_w IS NOT NULL AND d > prev_d
  ) ranked WHERE rn = 1
),
individual_composition AS (
  SELECT location_id, partition_label, 'scanned_tags'::text AS source,
         sum(animals)::int AS total_animals,
         jsonb_agg(jsonb_build_object('breed', breed, 'sex', sex, 'stage', stage, 'animals', animals)
                   ORDER BY animals DESC, stage, breed, sex) AS chips
  FROM (
    SELECT location_id, partition_label,
           COALESCE(NULLIF(breed, ''), 'Unknown breed') AS breed,
           COALESCE(NULLIF(sex, ''), 'unknown sex') AS sex,
           COALESCE(NULLIF(management_stage, ''), 'Unknown stage') AS stage,
           count(*)::int AS animals
    FROM resolved
    GROUP BY location_id, partition_label,
             COALESCE(NULLIF(breed, ''), 'Unknown breed'),
             COALESCE(NULLIF(sex, ''), 'unknown sex'),
             COALESCE(NULLIF(management_stage, ''), 'Unknown stage')
  ) chips
  GROUP BY location_id, partition_label
),
lump_composition AS (
  SELECT l.location_id, l.partition_label, 'live_shed_cohort'::text AS source,
         sum(scd.animals)::int AS total_animals,
         jsonb_agg(jsonb_build_object('breed', scd.breed, 'sex', scd.sex, 'stage', scd.stage, 'animals', scd.animals)
                   ORDER BY scd.animals DESC, scd.stage, scd.breed, scd.sex) AS chips
  FROM (SELECT DISTINCT location_id, partition_label FROM lump) l
  JOIN shed_cohort_detail scd ON scd.location_id = l.location_id AND scd.partition_label = l.partition_label
  WHERE NOT EXISTS (
    SELECT 1
    FROM individual_composition ic
    WHERE ic.location_id = l.location_id AND ic.partition_label = l.partition_label
  )
  GROUP BY l.location_id, l.partition_label
),
shed_type AS (
  SELECT DISTINCT src.location_id, src.partition_label,
         CASE
           WHEN lower(coalesce(loc.operational_notes, '') || ' ' || coalesce(parent_loc.operational_notes, '')) ~ '\m(elevated|elevate)\M'
             THEN 'elevated'
           WHEN lower(coalesce(loc.operational_notes, '') || ' ' || coalesce(parent_loc.operational_notes, '')) ~ '\m(crown|crowned|ground)\M'
             THEN 'ground'
           WHEN lower(loc.name) ~ '\m(gandhi|castro|ho chi minh|old yashoda|yashoda old)\M'
             OR lower(coalesce(parent_loc.name, '')) ~ '\m(gandhi|castro|ho chi minh|old yashoda|yashoda old)\M'
             THEN 'ground'
           WHEN lower(loc.name) ~ '\m(mandela|godel|sumathi|new yashoda|yashoda new|yashoda)\M'
             OR lower(coalesce(parent_loc.name, '')) ~ '\m(mandela|godel|sumathi|new yashoda|yashoda new|yashoda)\M'
             THEN 'elevated'
         END AS shed_type
  FROM shed_targets src
  JOIN locations loc ON loc.location_id = src.location_id AND loc.tenant_id = $1::uuid
  LEFT JOIN locations parent_loc ON parent_loc.location_id = src.resolved_id AND parent_loc.tenant_id = $1::uuid
)
SELECT
  (SELECT count(*) FROM resolved WHERE breed IS NOT NULL),
  (SELECT count(*) FROM resolved WHERE breed IS NULL),
  (SELECT COALESCE(sum(l.animal_count), 0) FROM lump l),
  (SELECT COALESCE(sum(l.animal_count), 0) FROM lump l
     LEFT JOIN shed_stage ss ON ss.location_id = l.location_id AND ss.partition_label = l.partition_label WHERE ss.stage IS NULL),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, n, avg) ORDER BY n DESC), '[]'::jsonb)
     FROM (SELECT breed, sum(n)::bigint n, (sum(total)/NULLIF(sum(n),0))::float8 avg FROM (
             SELECT breed, count(*)::bigint n, sum(weight_kg)::float8 total FROM resolved
              WHERE breed IS NOT NULL GROUP BY breed
             UNION ALL
             SELECT sc.breed, sum(l.animal_count)::bigint, sum(l.animal_count*l.average_weight_kg)::float8
              FROM lump l JOIN shed_cohort sc ON sc.location_id = l.location_id AND sc.partition_label = l.partition_label
              WHERE sc.breeds = 1 AND ($5::text = '' OR (sc.sexes = 1 AND lower(btrim(sc.sex)) = $5::text)) GROUP BY sc.breed) bp GROUP BY breed) b),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(sex, n, avg) ORDER BY n DESC), '[]'::jsonb)
     FROM (SELECT sex, sum(n)::bigint n, (sum(total)/NULLIF(sum(n),0))::float8 avg FROM (
             SELECT sex, count(*)::bigint n, sum(weight_kg)::float8 total FROM resolved
              WHERE sex IS NOT NULL GROUP BY sex
             UNION ALL
             SELECT sc.sex, sum(l.animal_count)::bigint, sum(l.animal_count*l.average_weight_kg)::float8
              FROM lump l JOIN shed_cohort sc ON sc.location_id = l.location_id AND sc.partition_label = l.partition_label
              WHERE sc.sexes = 1 AND ($5::text = '' OR lower(btrim(sc.sex)) = $5::text) GROUP BY sc.sex) sp GROUP BY sex) x),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(stage, n, avg) ORDER BY n DESC), '[]'::jsonb)
     FROM (
       SELECT stage, sum(n)::bigint n, (sum(total) / NULLIF(sum(n), 0))::float8 avg
       FROM (
         SELECT management_stage AS stage, count(*)::bigint n, sum(weight_kg)::float8 total
         FROM resolved WHERE management_stage IS NOT NULL GROUP BY management_stage
         UNION ALL
         SELECT ss.stage, sum(l.animal_count)::bigint, sum(l.animal_count * l.average_weight_kg)::float8
         FROM lump l JOIN shed_stage ss ON ss.location_id = l.location_id AND ss.partition_label = l.partition_label GROUP BY ss.stage
       ) parts GROUP BY stage
     ) st),
  -- Gain by breed/sex/stage: same-animal pairs PLUS lump-sum sheds (maintainer
  -- decision 2026-08-25). A lump-sum shed has no per-animal identity, so its
  -- animals ride at the SHED grain: every animal of the shed carries the shed's
  -- own average-weight change (lump_span.g_per_day), and the shed joins a
  -- breed/sex/stage bucket ONLY when its live cohort is homogeneous for that
  -- dimension (shed_cohort breeds/sexes/stages = 1) — a mixed shed still names
  -- nothing rather than guessing, per the standing whole-shed attribution rule.
  --
  -- projection-review: producer grain of the lump arm is one row per
  -- (location_id, partition_label) from lump_span (rn=1 latest x rn=1 first,
  -- provably one row per key), joined 1:1 to shed_cohort (GROUPed by the same
  -- key); animals is the latest submission's frozen head count, so sum(animals)
  -- ranges over disjoint sheds. The weighted mean's numerator and denominator
  -- range over the identical UNION row set (same FROM, same GROUP BY).
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, n, g) ORDER BY n DESC), '[]'::jsonb)
     FROM (
       SELECT breed, sum(n)::bigint n, (sum(gsum) / NULLIF(sum(n), 0))::float8 g FROM (
         SELECT breed, count(*)::bigint n, sum(g)::float8 gsum
         FROM resolved_gain WHERE breed IS NOT NULL GROUP BY breed
         UNION ALL
         SELECT sc.breed, sum(ls.animals)::bigint, sum(ls.animals * ls.g_per_day)::float8
         FROM lump_span ls JOIN shed_cohort sc
           ON sc.location_id = ls.location_id AND sc.partition_label = ls.partition_label
         -- The Sex filter reaches this arm too. It did not, and a whole-shed pen therefore joined
         -- the male AND the female breed chart alike: Anantapur Sheep read 403 male kids and 392
         -- female ones against 456 in total. Same claim rule as every other whole-shed arm -- the
         -- pen counts for a reader only when its cohort is entirely that sex.
         WHERE sc.breeds = 1 AND ($5::text = '' OR (sc.sexes = 1 AND lower(btrim(sc.sex)) = $5::text))
         GROUP BY sc.breed
       ) parts GROUP BY breed
     ) gb),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(sex, n, g) ORDER BY n DESC), '[]'::jsonb)
     FROM (
       SELECT sex, sum(n)::bigint n, (sum(gsum) / NULLIF(sum(n), 0))::float8 g FROM (
         SELECT sex, count(*)::bigint n, sum(g)::float8 gsum
         FROM resolved_gain WHERE sex IS NOT NULL GROUP BY sex
         UNION ALL
         SELECT sc.sex, sum(ls.animals)::bigint, sum(ls.animals * ls.g_per_day)::float8
         FROM lump_span ls JOIN shed_cohort sc
           ON sc.location_id = ls.location_id AND sc.partition_label = ls.partition_label
         WHERE sc.sexes = 1 AND ($5::text = '' OR lower(btrim(sc.sex)) = $5::text) GROUP BY sc.sex
       ) parts GROUP BY sex
     ) gx),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(stage, n, g) ORDER BY n DESC), '[]'::jsonb)
     FROM (
       SELECT stage, sum(n)::bigint n, (sum(gsum) / NULLIF(sum(n), 0))::float8 g FROM (
         SELECT management_stage AS stage, count(*)::bigint n, sum(g)::float8 gsum
         FROM resolved_gain WHERE management_stage IS NOT NULL GROUP BY management_stage
         UNION ALL
         SELECT ss.stage, sum(ls.animals)::bigint, sum(ls.animals * ls.g_per_day)::float8
         FROM lump_span ls JOIN shed_stage ss
           ON ss.location_id = ls.location_id AND ss.partition_label = ls.partition_label
         GROUP BY ss.stage
       ) parts GROUP BY stage
     ) gs),
  -- BREED x ORIGIN: the same weighted mean as gb above, split by where the animals came from.
  -- It is computed HERE, in the one query, rather than by asking this read twice under the two
  -- origins: this whole response is a single round trip of a heavy aggregate, and two of them
  -- would recompute by_sex, by_stage, the bands and the shed composition only to throw both
  -- copies away.
  --
  -- ORIGIN IS RESOLVED PER ANIMAL for a scanned weigh, and AGREE-OR-NEITHER for a whole-shed pen
  -- -- exactly the rule origin_scope.go already applies for the page's own Farm born / Purchased
  -- filter, which is why the two lists are consumed as opaque bind arrays here rather than
  -- re-derived. An animal or pen in NEITHER list is claimed by NEITHER side (origin IS NULL is
  -- dropped), so the two halves need not add up to the breed's own total. That gap is honest: a
  -- kid whose load is not recorded was still weighed, and still counts in gb above.
  --
  -- projection-review: producer grain is one resolved_gain row per animal and one lump_span row
  -- per (location_id, partition_label); consumer grain is (breed, origin), reached by GROUP BY on
  -- exactly those two columns after each arm is already at its own grain, so the UNION ALL cannot
  -- fan out. The weighted mean's numerator sum(gsum) and denominator sum(n) range over the
  -- identical row set (same FROM, same GROUP BY) -- one key set, stated identical.
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, origin, n, g) ORDER BY breed, origin), '[]'::jsonb)
     FROM (
       SELECT breed, origin, sum(n)::bigint n, (sum(gsum) / NULLIF(sum(n), 0))::float8 g FROM (
         SELECT breed, origin, count(*)::bigint n, sum(g)::float8 gsum
         FROM (
           SELECT rg.breed, rg.g,
                  CASE WHEN rg.tag = ANY($10::text[]) THEN 'farm_born'
                       WHEN rg.tag = ANY($11::text[]) THEN 'purchased' END AS origin
           FROM resolved_gain rg WHERE rg.breed IS NOT NULL
         ) scanned
         WHERE origin IS NOT NULL
         GROUP BY breed, origin
         UNION ALL
         SELECT sc.breed, pen.origin, sum(ls.animals)::bigint, sum(ls.animals * ls.g_per_day)::float8
         FROM lump_span ls
         JOIN shed_cohort sc
           ON sc.location_id = ls.location_id AND sc.partition_label = ls.partition_label
         JOIN LATERAL (
           SELECT CASE
             WHEN EXISTS (SELECT 1 FROM unnest($12::uuid[], $13::text[]) AS fb(loc, part)
                          WHERE fb.loc = ls.location_id AND fb.part = ls.partition_label) THEN 'farm_born'
             WHEN EXISTS (SELECT 1 FROM unnest($14::uuid[], $15::text[]) AS pu(loc, part)
                          WHERE pu.loc = ls.location_id AND pu.part = ls.partition_label) THEN 'purchased'
           END AS origin
         ) pen ON pen.origin IS NOT NULL
         -- Same claim rule as every other whole-shed arm: the pen counts for a reader only when
         -- its cohort is entirely one breed, and entirely the selected sex.
         WHERE sc.breeds = 1 AND ($5::text = '' OR (sc.sexes = 1 AND lower(btrim(sc.sex)) = $5::text))
         GROUP BY sc.breed, pen.origin
       ) parts GROUP BY breed, origin
     ) gbo),
  -- BREED x SHED TYPE: Manju's Shed-wise view. This is not a per-pen leaderboard; it compares the
  -- two physical shed classes the farm asked for, inside each breed. The class is read from explicit
  -- shed metadata when present; ground sheds also include the named sheds called out in the review
  -- note until that classification is modeled as first-class data.
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, shed_type, n, g) ORDER BY breed, shed_type), '[]'::jsonb)
     FROM (
       SELECT breed, shed_type, sum(n)::bigint n, (sum(gsum) / NULLIF(sum(n), 0))::float8 g FROM (
         SELECT rg.breed, st.shed_type, count(*)::bigint n, sum(rg.g)::float8 gsum
         FROM resolved_gain rg
         JOIN shed_type st ON st.location_id = rg.location_id AND st.partition_label = rg.partition_label
         WHERE rg.breed IS NOT NULL AND st.shed_type IS NOT NULL
           AND ($16::text = '' OR $16::text = 'individual_animal')
         GROUP BY rg.breed, st.shed_type
         UNION ALL
         SELECT sc.breed, st.shed_type, sum(ls.animals)::bigint, sum(ls.animals * ls.g_per_day)::float8
         FROM lump_span ls
         JOIN shed_cohort sc
           ON sc.location_id = ls.location_id AND sc.partition_label = ls.partition_label
         JOIN shed_type st
           ON st.location_id = ls.location_id AND st.partition_label = ls.partition_label
         WHERE sc.breeds = 1 AND st.shed_type IS NOT NULL
           AND ($5::text = '' OR (sc.sexes = 1 AND lower(btrim(sc.sex)) = $5::text))
           AND ($16::text = '' OR $16::text = 'per_shed_partition')
         GROUP BY sc.breed, st.shed_type
       ) parts GROUP BY breed, shed_type
     ) gbst),
  -- WEIGHT BANDS: how many animals sit in each weight bracket, and how fast that bracket is
  -- growing. Both arms, always -- a page that banded only the scanned kids would describe this
  -- farm from a minority of it, since most of its animals are weighed by the whole shed.
  --
  -- A SCANNED animal is banded by its OWN latest weight in the window and counts as one.
  -- A WHOLE-SHED pen is banded by the pen's own latest average weight and counts as ALL the
  -- animals it holds -- the shed average is the only measured fact, so every animal in the pen is
  -- kept in the one band that average falls into rather than spread across neighbouring bands,
  -- which would invent a distribution nobody measured. Same rule the gain bands already use.
  --
  -- The gain is the SAME animal-weighted mean every other gain figure on these screens reports:
  -- a scanned animal at the median of its own pairs, a pen at its average-weight movement once
  -- per animal. LEFT JOINed on purpose -- an animal or pen weighed ONCE has no gain but is still a
  -- real animal standing in that weight bracket, so it counts in the head count and not in the
  -- gain denominator. Reporting it
  -- as 0 g/day would drag the bracket's growth toward zero with animals nobody measured twice.
  --
  -- Bands are lower-inclusive and upper-exclusive, so every animal lands in exactly one and the
  -- counts sum to the population. The KEYS are stable identifiers; the farm words for them live in
  -- the page contract, never here.
  --
  -- projection-review: producer grain is one resolved row per scanned tag (DISTINCT ON tag) and
  -- one lump row per (location_id, partition_label) (DISTINCT ON that pair); consumer grain is
  -- the band, reached by GROUP BY after each arm is already at its own grain, so the UNION ALL
  -- cannot fan out. animal_gain is 1:0..1 per tag (GROUP BY tag) and lump_span is 1:0..1 per pen
  -- (rn=1 joined to rn=1), so neither LEFT JOIN multiplies a row. The mean's numerator sum(gsum)
  -- and denominator sum(gn) range over the identical row set.
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(band, n, gn, g) ORDER BY sort), '[]'::jsonb)
     FROM (
       SELECT band, sort, sum(n)::bigint AS n, sum(gn)::bigint AS gn,
              (sum(gsum) / NULLIF(sum(gn), 0))::float8 AS g
       FROM (
         SELECT b.band, b.sort,
                count(*)::bigint                              AS n,
                count(ag.g)::bigint                           AS gn,
                COALESCE(sum(ag.g), 0)::float8                AS gsum
         FROM resolved r
         LEFT JOIN animal_gain ag ON ag.tag = r.tag
         CROSS JOIN LATERAL (
           SELECT CASE WHEN r.weight_kg < 15 THEN 'under_15'
                       WHEN r.weight_kg < 20 THEN '15_20'
                       WHEN r.weight_kg < 25 THEN '20_25'
                       WHEN r.weight_kg < 30 THEN '25_30'
                       WHEN r.weight_kg < 35 THEN '30_35'
                       ELSE '35_plus' END AS band,
                  CASE WHEN r.weight_kg < 15 THEN 1
                       WHEN r.weight_kg < 20 THEN 2
                       WHEN r.weight_kg < 25 THEN 3
                       WHEN r.weight_kg < 30 THEN 4
                       WHEN r.weight_kg < 35 THEN 5
                       ELSE 6 END AS sort
         ) b
         GROUP BY b.band, b.sort
         UNION ALL
         SELECT b.band, b.sort,
                sum(lu.animal_count)::bigint,
                COALESCE(sum(ls.animals), 0)::bigint,
                COALESCE(sum(ls.animals * ls.g_per_day), 0)::float8
         FROM lump lu
         LEFT JOIN lump_span ls
           ON ls.location_id = lu.location_id AND ls.partition_label = lu.partition_label
         CROSS JOIN LATERAL (
           SELECT CASE WHEN lu.average_weight_kg < 15 THEN 'under_15'
                       WHEN lu.average_weight_kg < 20 THEN '15_20'
                       WHEN lu.average_weight_kg < 25 THEN '20_25'
                       WHEN lu.average_weight_kg < 30 THEN '25_30'
                       WHEN lu.average_weight_kg < 35 THEN '30_35'
                       ELSE '35_plus' END AS band,
                  CASE WHEN lu.average_weight_kg < 15 THEN 1
                       WHEN lu.average_weight_kg < 20 THEN 2
                       WHEN lu.average_weight_kg < 25 THEN 3
                       WHEN lu.average_weight_kg < 30 THEN 4
                       WHEN lu.average_weight_kg < 35 THEN 5
                       ELSE 6 END AS sort
         ) b
         GROUP BY b.band, b.sort
       ) parts GROUP BY band, sort
     ) wb),
  -- BREED x WEEK: the Time-wise tab's per-breed trend, over the same weeks the overall series
  -- covers. Both arms again -- a breed trend built from scanned kids only would describe this farm
  -- from a minority of it, and the overall weekly line beside it counts pens.
  --
  -- Same statistic, same claim rules as every other gain figure: a scanned animal at the median of
  -- its own pairs for that week, a pen at its average-weight movement once per animal, and a pen
  -- joins a breed only when its live cohort is entirely that breed. A mixed pen names nothing
  -- rather than guessing, so the per-breed weeks need not add up to the overall week.
  --
  -- projection-review: producer grain is one animal_gain_week row per (tag, week) and one pen_week
  -- row per (pen, week); consumer grain is (breed, week_start), reached by GROUP BY after each arm
  -- is already at its own grain, so the UNION ALL cannot fan out. ident is 1:0..1 per tag
  -- (DISTINCT ON) and shed_cohort is 1:1 per pen (GROUPed by that key), so neither join multiplies
  -- a row. The mean's numerator and denominator range over the identical row set.
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, week_start, n, g) ORDER BY breed, week_start), '[]'::jsonb)
     FROM (
       SELECT breed, week_start, sum(n)::bigint AS n, (sum(gsum) / NULLIF(sum(n), 0))::float8 AS g
       FROM (
         SELECT gt.breed, aw.week_start, count(*)::bigint AS n, sum(aw.g)::float8 AS gsum
         FROM animal_gain_week aw
         LEFT JOIN ident i ON i.tag = aw.tag
         LEFT JOIN goats gt ON gt.goat_id = i.goat_id AND gt.tenant_id = $1::uuid
         WHERE gt.breed IS NOT NULL AND ($5::text = '' OR lower(btrim(gt.sex)) = $5::text)
         GROUP BY gt.breed, aw.week_start
         UNION ALL
         SELECT sc.breed, pw.week_start, sum(pw.animals)::bigint, sum(pw.animals * pw.g_per_day)::float8
         FROM pen_week pw
         JOIN shed_cohort sc
           ON sc.location_id = pw.location_id AND sc.partition_label = pw.partition_label
         WHERE sc.breeds = 1
           AND ($5::text = '' OR (sc.sexes = 1 AND lower(btrim(sc.sex)) = $5::text))
         GROUP BY sc.breed, pw.week_start
       ) parts GROUP BY breed, week_start
     ) gbw),
  -- How many animals of each breed fell into each daily-gain band. DISJOINT bands
  -- (maintainer, 2026-08-24): an animal at 260 g/day is counted by the >250 filter ONLY,
  -- and the four counts partition n exactly — every animal with a gain lands in one band.
  --
  -- LUMP-SUM SHEDS INCLUDED (maintainer decision 2026-08-25): a homogeneous-breed
  -- lump-sum shed contributes ALL of its animals to the ONE band its own
  -- average-weight change (lump_span.g_per_day) falls into — the shed average is
  -- the only measured fact, so every animal is kept in that range and none is
  -- spread across bands. A mixed-breed shed still joins no breed row.
  --
  -- THE SEX FILTER NARROWS BOTH ARMS (maintainer, 2026-08-26). The per-animal arm is
  -- already narrowed upstream in resolved_gain; the lump arm is narrowed HERE, on the
  -- shed's own cohort, because a whole-shed weigh carries no tag and can only be claimed
  -- when its residents are all one sex. Dropping this arm is how a rewrite once turned
  -- Anantapur Sheep's 380 kids into 117: the individually scanned ones survived and every
  -- whole-shed kid silently vanished from the card.
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, n, b180, b1820, b2025, a250) ORDER BY n DESC, breed), '[]'::jsonb)
     FROM (
       SELECT breed, sum(n)::bigint AS n, sum(b180)::bigint AS b180, sum(b1820)::bigint AS b1820,
              sum(b2025)::bigint AS b2025, sum(a250)::bigint AS a250
       FROM (
         SELECT breed,
                count(*)::bigint                                       AS n,
                count(*) FILTER (WHERE g <= 180)::bigint               AS b180,
                count(*) FILTER (WHERE g > 180 AND g <= 200)::bigint   AS b1820,
                count(*) FILTER (WHERE g > 200 AND g <= 250)::bigint   AS b2025,
                count(*) FILTER (WHERE g > 250)::bigint                AS a250
         FROM resolved_gain
         WHERE breed IS NOT NULL
         GROUP BY breed
         UNION ALL
         SELECT sc.breed,
                sum(ls.animals)::bigint,
                COALESCE(sum(ls.animals) FILTER (WHERE ls.g_per_day <= 180), 0)::bigint,
                COALESCE(sum(ls.animals) FILTER (WHERE ls.g_per_day > 180 AND ls.g_per_day <= 200), 0)::bigint,
                COALESCE(sum(ls.animals) FILTER (WHERE ls.g_per_day > 200 AND ls.g_per_day <= 250), 0)::bigint,
                COALESCE(sum(ls.animals) FILTER (WHERE ls.g_per_day > 250), 0)::bigint
         FROM lump_span ls JOIN shed_cohort sc
           ON sc.location_id = ls.location_id AND sc.partition_label = ls.partition_label
         WHERE sc.breeds = 1
           AND ($5::text = '' OR (sc.sexes = 1 AND lower(btrim(sc.sex)) = $5::text))
         GROUP BY sc.breed
       ) parts GROUP BY breed
     ) gt),
  (SELECT COALESCE(jsonb_agg(jsonb_build_object(
       'location_id', location_id::text,
       'partition_label', partition_label,
       'source', source,
       'total_animals', total_animals,
       'chips', chips
     ) ORDER BY location_id::text, partition_label, source), '[]'::jsonb)
   FROM (
     SELECT * FROM individual_composition
     UNION ALL
     SELECT * FROM lump_composition
   ) c)`

	var (
		resolvedCount, unresolvedCount, lumpTotal, lumpUnattributed int
		breedJSON, sexJSON, stageJSON                               []byte
		gainBreedJSON, gainSexJSON, gainStageJSON                   []byte
		gainByBreedOriginJSON                                       []byte
		gainByBreedShedTypeJSON                                     []byte
		weightBandJSON                                              []byte
		gainBreedWeekJSON                                           []byte
		gainThresholdBreedJSON                                      []byte
		compositionJSON                                             []byte
	)
	if err := r.pool.QueryRow(ctx, q, tenantID, parkIDs, periodStart, periodEnd, sexFilter,
		originFiltered, originScope.Tags, originScope.LocationIDs, originScope.PartitionLabels,
		farmBornScope.Tags, purchasedScope.Tags,
		farmBornScope.LocationIDs, farmBornScope.PartitionLabels,
		purchasedScope.LocationIDs, purchasedScope.PartitionLabels,
		weighingCategory).Scan(
		&resolvedCount, &unresolvedCount, &lumpTotal, &lumpUnattributed,
		&breedJSON, &sexJSON, &stageJSON,
		&gainBreedJSON, &gainSexJSON, &gainStageJSON,
		&gainByBreedOriginJSON,
		&gainByBreedShedTypeJSON,
		&weightBandJSON,
		&gainBreedWeekJSON,
		&gainThresholdBreedJSON,
		&compositionJSON,
	); err != nil {
		return domain.WeightDemographics{}, err
	}

	out.ResolvedAnimals = resolvedCount
	out.UnresolvedAnimals = unresolvedCount
	out.LumpSumAnimals = lumpTotal
	out.LumpSumUnattributedAnimals = lumpUnattributed
	var err error
	if out.ByBreed, err = decodeWeightDemographicBuckets(breedJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	if out.BySex, err = decodeWeightDemographicBuckets(sexJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	if out.ByStage, err = decodeWeightDemographicBuckets(stageJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	if out.GainByBreed, err = decodeWeightGainBuckets(gainBreedJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	if out.GainBySex, err = decodeWeightGainBuckets(gainSexJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	if out.GainByStage, err = decodeWeightGainBuckets(gainStageJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	if out.GainByBreedOrigin, err = decodeWeightGainOriginBuckets(gainByBreedOriginJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	if out.GainByBreedShedType, err = decodeWeightGainShedTypeBuckets(gainByBreedShedTypeJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	if out.ByWeightBand, err = decodeWeightBandBuckets(weightBandJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	if out.GainByBreedWeek, err = decodeWeightGainBreedWeekBuckets(gainBreedWeekJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	if out.GainThresholdsByBreed, err = decodeWeightGainThresholdBuckets(gainThresholdBreedJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	if out.ShedComposition, err = decodeShedComposition(compositionJSON); err != nil {
		return domain.WeightDemographics{}, err
	}
	return out, nil
}

// exportGoatIdentity carries the same-animal reporting facts the whole-window CSV
// export prints beside a scanned tag: the goat's register id, its other active tag,
// and the breed/sex chips the Weights screen already shows. Reporting only — no
// write path, no gate, and a tag that resolves to nothing simply exports blank.
type exportGoatIdentity struct {
	DisplayID string
	SecondTag string
	Breed     string
	Sex       string
}

// exportGoatIdentities resolves every DISTINCT tag weighed in the window to its
// same-animal reporting facts, in ONE set-based query, so the CSV export can be
// enriched without a per-row lookup. It lives in THIS file because this is the one
// weighing file the free-flow guard permits to read goats/goat_identifiers
// (maintainer decisions 2026-08-07/2026-08-19; the whole-window CSV export is the
// same Weights reporting surface, same tables, read-only).
//
// projection-review: membership=one row per DISTINCT lower(btrim(scanned_identifier)) weighed in the window; group_key=the normalized tag; join_cardinality=ident 0..1 per tag (DISTINCT ON newest identifier row), goats 1 per goat_id (PK), second-tag LATERAL 0..1 per goat (LIMIT 1, newest first); pagination=NONE, bounded by distinct tags weighed in the window; scope=tenant_id + park_id = ANY($2), optional shed filter $5
func (r *Repository) exportGoatIdentities(ctx context.Context, tenantID string, parkIDs []string, shedLocationIDs []string, periodStart, periodEnd time.Time) (map[string]exportGoatIdentity, error) {
	out := map[string]exportGoatIdentity{}
	if len(parkIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
WITH scoped AS (
  SELECT cs.campaign_shed_id, cs.tenant_id
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid AND c.park_id = ANY($2::uuid[])
    AND (cardinality($5::uuid[]) = 0 OR cs.location_id = ANY($5::uuid[]))
),
tags AS (
  SELECT DISTINCT lower(btrim(o.scanned_identifier)) AS tag
  FROM weighing_observations o
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id AND s.tenant_id = o.tenant_id
  WHERE o.tenant_id = $1::uuid
    AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz
    AND btrim(o.scanned_identifier) <> ''
),
ident AS (
  SELECT DISTINCT ON (lower(btrim(gi.identifier_value)))
         lower(btrim(gi.identifier_value)) AS tag, gi.goat_id
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid
  ORDER BY lower(btrim(gi.identifier_value)), gi.created_at DESC
)
SELECT t.tag,
       COALESCE(g.display_id, ''),
       COALESCE(second_tag.identifier_value, ''),
       COALESCE(g.breed, ''),
       COALESCE(g.sex, '')
FROM tags t
JOIN ident i ON i.tag = t.tag
JOIN goats g ON g.goat_id = i.goat_id AND g.tenant_id = $1::uuid
LEFT JOIN LATERAL (
  SELECT gi2.identifier_value
  FROM goat_identifiers gi2
  WHERE gi2.tenant_id = $1::uuid AND gi2.goat_id = i.goat_id
    AND lower(btrim(gi2.identifier_value)) <> t.tag
  ORDER BY gi2.created_at DESC
  LIMIT 1
) second_tag ON true`, tenantID, parkIDs, periodStart, periodEnd, shedLocationIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tag string
		var identity exportGoatIdentity
		if err := rows.Scan(&tag, &identity.DisplayID, &identity.SecondTag, &identity.Breed, &identity.Sex); err != nil {
			return nil, err
		}
		out[tag] = identity
	}
	return out, rows.Err()
}

// decodeWeightDemographicBuckets reads the [label, animals, average] triples the
// query aggregates. A null label cannot occur — every branch filters IS NOT NULL —
// but a malformed row is skipped rather than rendered as an empty category.
func decodeWeightDemographicBuckets(raw []byte) ([]domain.WeightDemographicBucket, error) {
	out := []domain.WeightDemographicBucket{}
	if len(raw) == 0 {
		return out, nil
	}
	var rows [][]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if len(row) != 3 {
			continue
		}
		var label string
		var animals int
		var avg float64
		if json.Unmarshal(row[0], &label) != nil || label == "" {
			continue
		}
		if json.Unmarshal(row[1], &animals) != nil || json.Unmarshal(row[2], &avg) != nil {
			continue
		}
		out = append(out, domain.WeightDemographicBucket{Label: label, Animals: animals, AverageWeightKg: avg})
	}
	return out, nil
}

// decodeWeightGainBuckets mirrors decodeWeightDemographicBuckets for the gain
// dimensions, whose value is g/day rather than kg.
func decodeWeightGainBuckets(raw []byte) ([]domain.WeightGainBucket, error) {
	rows, err := decodeWeightDemographicBuckets(raw)
	if err != nil {
		return nil, err
	}
	out := make([]domain.WeightGainBucket, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.WeightGainBucket{
			Label: row.Label, Animals: row.Animals, MedianGainGPerDay: row.AverageWeightKg,
		})
	}
	return out, nil
}

// decodeWeightGainThresholdBuckets reads the
// [breed, animals, <=180, 180-200, 200-250, >250] tuples. A row whose counts do not parse
// is skipped rather than rendered as a breed with zero animals in every band, which would
// read as a real growth failure.
//
// There is ONE grain, not one per sex: the Sex filter narrows the whole read upstream, so
// these rows already describe the kids the reader asked for. Emitting a second, per-sex grain
// beside the combined one duplicated a rule the query above already applies, and two
// implementations of one rule drift.
// decodeWeightGainOriginBuckets reads the four-element rows gbo emits: breed, origin, animals,
// gain. A row missing either name is SKIPPED rather than defaulted -- a bucket with a blank origin
// could not be drawn on either side of the Birth-wise comparison, and inventing a side for it would
// claim an animal for a cohort the query deliberately declined to claim it for.
func decodeWeightGainOriginBuckets(raw []byte) ([]domain.WeightGainOriginBucket, error) {
	out := []domain.WeightGainOriginBucket{}
	if len(raw) == 0 {
		return out, nil
	}
	var rows [][]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if len(row) != 4 {
			continue
		}
		var label, origin string
		var animals int
		var gain float64
		if json.Unmarshal(row[0], &label) != nil || label == "" {
			continue
		}
		if json.Unmarshal(row[1], &origin) != nil || (origin != "farm_born" && origin != "purchased") {
			continue
		}
		if json.Unmarshal(row[2], &animals) != nil || json.Unmarshal(row[3], &gain) != nil {
			continue
		}
		out = append(out, domain.WeightGainOriginBucket{
			Label: label, Origin: origin, Animals: animals, MedianGainGPerDay: gain,
		})
	}
	return out, nil
}

func decodeWeightGainShedTypeBuckets(raw []byte) ([]domain.WeightGainShedTypeBucket, error) {
	out := []domain.WeightGainShedTypeBucket{}
	if len(raw) == 0 {
		return out, nil
	}
	var rows [][]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if len(row) != 4 {
			continue
		}
		var label, shedType string
		var animals int
		var gain float64
		if json.Unmarshal(row[0], &label) != nil || label == "" {
			continue
		}
		if json.Unmarshal(row[1], &shedType) != nil || (shedType != "elevated" && shedType != "ground") {
			continue
		}
		if json.Unmarshal(row[2], &animals) != nil || json.Unmarshal(row[3], &gain) != nil {
			continue
		}
		out = append(out, domain.WeightGainShedTypeBucket{
			Label: label, ShedType: shedType, Animals: animals, AverageGainGPerDay: gain,
		})
	}
	return out, nil
}

// decodeWeightBandBuckets reads the four-element rows wb emits: band key, animals, gain animals,
// gain. A row whose band key is not one of the six is SKIPPED rather than defaulted -- a bracket
// the client cannot name is a bracket it cannot draw, and filing it under a neighbour would move
// animals into a weight range they are not in.
//
// The gain arrives NULL for a bracket nothing was weighed twice in, and stays nil: 0 g/day would
// read as a bracket that stopped growing.
func decodeWeightBandBuckets(raw []byte) ([]domain.WeightBandBucket, error) {
	out := []domain.WeightBandBucket{}
	if len(raw) == 0 {
		return out, nil
	}
	var rows [][]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	known := map[string]bool{"under_15": true, "15_20": true, "20_25": true, "25_30": true, "30_35": true, "35_plus": true}
	for _, row := range rows {
		if len(row) != 4 {
			continue
		}
		var band string
		var animals, gainAnimals int
		if json.Unmarshal(row[0], &band) != nil || !known[band] {
			continue
		}
		if json.Unmarshal(row[1], &animals) != nil || json.Unmarshal(row[2], &gainAnimals) != nil {
			continue
		}
		bucket := domain.WeightBandBucket{Band: band, Animals: animals, GainAnimals: gainAnimals}
		var gain *float64
		if json.Unmarshal(row[3], &gain) == nil && gain != nil {
			bucket.AverageGainGPerDay = gain
		}
		out = append(out, bucket)
	}
	return out, nil
}

// decodeWeightGainBreedWeekBuckets reads the four-element rows gbw emits: breed, week start,
// animals, gain. A row missing either name is SKIPPED rather than defaulted -- a point with no
// breed or no week cannot be plotted on a per-breed trend, and inventing either would put a
// measurement on a line it does not belong to.
func decodeWeightGainBreedWeekBuckets(raw []byte) ([]domain.WeightGainBreedWeekBucket, error) {
	out := []domain.WeightGainBreedWeekBucket{}
	if len(raw) == 0 {
		return out, nil
	}
	var rows [][]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if len(row) != 4 {
			continue
		}
		var label, week string
		var animals int
		var gain float64
		if json.Unmarshal(row[0], &label) != nil || label == "" {
			continue
		}
		if json.Unmarshal(row[1], &week) != nil || week == "" {
			continue
		}
		if json.Unmarshal(row[2], &animals) != nil || json.Unmarshal(row[3], &gain) != nil {
			continue
		}
		out = append(out, domain.WeightGainBreedWeekBucket{
			Label: label, WeekStart: week, Animals: animals, AverageGainGPerDay: gain,
		})
	}
	return out, nil
}

func decodeWeightGainThresholdBuckets(raw []byte) ([]domain.WeightGainThresholdBucket, error) {
	out := []domain.WeightGainThresholdBucket{}
	if len(raw) == 0 {
		return out, nil
	}
	var rows [][]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if len(row) != 6 {
			continue
		}
		var label string
		if json.Unmarshal(row[0], &label) != nil || label == "" {
			continue
		}
		var animals, atOrBelow180, band180To200, band200To250, above250 int
		if json.Unmarshal(row[1], &animals) != nil ||
			json.Unmarshal(row[2], &atOrBelow180) != nil ||
			json.Unmarshal(row[3], &band180To200) != nil ||
			json.Unmarshal(row[4], &band200To250) != nil ||
			json.Unmarshal(row[5], &above250) != nil {
			continue
		}
		out = append(out, domain.WeightGainThresholdBucket{
			Label: label, Animals: animals,
			AtOrBelow180: atOrBelow180, Band180To200: band180To200,
			Band200To250: band200To250, Above250: above250,
		})
	}
	return out, nil
}

func decodeShedComposition(raw []byte) ([]domain.ShedComposition, error) {
	out := []domain.ShedComposition{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Chips == nil {
			out[i].Chips = []domain.ShedCompositionChip{}
		}
	}
	return out, nil
}
