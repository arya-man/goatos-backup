package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
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
func (r *Repository) GetWeightDemographics(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.WeightDemographics, error) {
	out := domain.WeightDemographics{
		GainThresholdsByBreed: []domain.WeightGainThresholdBucket{},
		ByBreed:               []domain.WeightDemographicBucket{},
		BySex:                 []domain.WeightDemographicBucket{},
		ByStage:               []domain.WeightDemographicBucket{},
		ShedComposition:       []domain.ShedComposition{},
	}
	if len(parkIDs) == 0 {
		return out, nil
	}

	const q = ` -- scale-guard:ignore: bounded 28/84-day weighing leadership aggregate over tenant+authorized parks; current release envelope accepts this read-model query with repository integration coverage, and it does not touch obligation/kernel hot tables
WITH scoped AS (
  SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id, COALESCE(cs.partition_label, '') AS partition_label, cs.weighing_category
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid AND c.park_id = ANY($2::uuid[]) AND cs.status <> 'canceled'
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
  ORDER BY lower(btrim(o.scanned_identifier)), o.accepted_at DESC, o.observation_id DESC
),
-- Consecutive-weigh pairs per tag, for the gain dimensions. A same-business-day
-- pair is excluded: an animal cannot meaningfully gain inside one day, so that is
-- a re-weigh or a double scan, and dividing by a fraction of a day manufactures
-- enormous numbers (the -3,108,762 g/day headline this rule exists to prevent).
obs AS (
  SELECT lower(btrim(o.scanned_identifier)) AS tag, o.weight_kg, o.accepted_at,
         (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
  FROM weighing_observations o
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id AND s.tenant_id = o.tenant_id
  WHERE o.tenant_id = $1::uuid
    AND o.accepted_at >= ($3::timestamptz - interval '90 days') AND o.accepted_at < $4::timestamptz
    AND o.verification_status <> 'rejected' AND btrim(o.scanned_identifier) <> ''
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
),
resolved_gain AS (
  SELECT ag.g, gt.breed, gt.sex, gt.management_stage
  FROM animal_gain ag
  LEFT JOIN ident i ON i.tag = ag.tag
  LEFT JOIN goats gt ON gt.goat_id = i.goat_id AND gt.tenant_id = $1::uuid
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
                  AND gsp.partition_label = COALESCE(NULLIF(s.partition_label, ''),
                    NULLIF((regexp_match(l.name, '\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1], ''))
              )
            LIMIT 1)
         ) AS resolved_id,
         COALESCE(NULLIF(s.partition_label, ''),
                  NULLIF((regexp_match((SELECT l.name FROM locations l WHERE l.location_id = s.location_id),
                                       '\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1], ''),
                  '') AS resolved_partition_label
  FROM scoped s
),
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
  WHERE src.resolved_partition_label = '' OR gsp.partition_label = src.resolved_partition_label
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
  WHERE src.resolved_partition_label = '' OR gsp.partition_label = src.resolved_partition_label
  GROUP BY src.location_id, src.partition_label,
           COALESCE(NULLIF(g.breed, ''), 'Unknown breed'),
           COALESCE(NULLIF(g.sex, ''), 'unknown sex'),
           COALESCE(NULLIF(g.management_stage, ''), 'Unknown stage')
),
shed_stage AS (
  SELECT location_id, partition_label, stage FROM shed_cohort WHERE stages = 1
),
lump AS (
  SELECT s.location_id, s.partition_label, sh.animal_count, sh.average_weight_kg
  FROM scoped s
  JOIN weighing_shed_observations sh
    ON sh.campaign_shed_id = s.campaign_shed_id AND sh.tenant_id = s.tenant_id
   AND sh.withdrawn_at IS NULL
   AND sh.accepted_at >= $3::timestamptz AND sh.accepted_at < $4::timestamptz
   AND sh.verification_status <> 'rejected'
  WHERE s.weighing_category = 'per_shed_partition'
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
    SELECT cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label,
           so.average_weight_kg, so.animal_count,
           (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id, COALESCE(cs2.partition_label, '') ORDER BY so.accepted_at DESC) AS rn
    FROM weighing_shed_observations so
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = so.campaign_shed_id
    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id
    WHERE so.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
      AND so.withdrawn_at IS NULL AND so.verification_status <> 'rejected'
      AND so.accepted_at >= $3::timestamptz AND so.accepted_at < $4::timestamptz
  ) latest
  JOIN (
    SELECT cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label,
           so.average_weight_kg,
           (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id, COALESCE(cs2.partition_label, '') ORDER BY so.accepted_at ASC) AS rn
    FROM weighing_shed_observations so
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = so.campaign_shed_id
    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id
    WHERE so.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
      AND so.withdrawn_at IS NULL AND so.verification_status <> 'rejected'
      AND so.accepted_at >= $3::timestamptz AND so.accepted_at < $4::timestamptz
  ) first ON first.location_id = latest.location_id
    AND first.partition_label = latest.partition_label
    AND first.rn = 1
  WHERE latest.rn = 1 AND latest.d > first.d
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
              WHERE sc.breeds = 1 GROUP BY sc.breed) bp GROUP BY breed) b),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(sex, n, avg) ORDER BY n DESC), '[]'::jsonb)
     FROM (SELECT sex, sum(n)::bigint n, (sum(total)/NULLIF(sum(n),0))::float8 avg FROM (
             SELECT sex, count(*)::bigint n, sum(weight_kg)::float8 total FROM resolved
              WHERE sex IS NOT NULL GROUP BY sex
             UNION ALL
             SELECT sc.sex, sum(l.animal_count)::bigint, sum(l.animal_count*l.average_weight_kg)::float8
              FROM lump l JOIN shed_cohort sc ON sc.location_id = l.location_id AND sc.partition_label = l.partition_label
              WHERE sc.sexes = 1 GROUP BY sc.sex) sp GROUP BY sex) x),
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
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, n, g) ORDER BY n DESC), '[]'::jsonb)
     FROM (
       SELECT breed, count(*)::bigint n, avg(g)::float8 g
       FROM resolved_gain
       WHERE breed IS NOT NULL
       GROUP BY breed
     ) gb),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(sex, n, g) ORDER BY n DESC), '[]'::jsonb)
     FROM (
       SELECT sex, count(*)::bigint n, avg(g)::float8 g
       FROM resolved_gain
       WHERE sex IS NOT NULL
       GROUP BY sex
     ) gx),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(management_stage, n, g) ORDER BY n DESC), '[]'::jsonb)
     FROM (
       SELECT management_stage, count(*)::bigint n, avg(g)::float8 g
       FROM resolved_gain
       WHERE management_stage IS NOT NULL
       GROUP BY management_stage
     ) gs),
  -- How many animals of each breed fell into each daily-gain band. DISJOINT bands
  -- (maintainer, 2026-08-24): an animal at 260 g/day is counted by the >250 filter ONLY,
  -- and the four counts partition n exactly — every animal with a gain lands in one band.
  --
  -- projection-review: membership=one row per animal in resolved_gain with a breed, i.e. exactly the population gain_by_breed reports; group_key=breed, the GROUP BY; join_cardinality=none added here, resolved_gain is already one row per tag; pagination=NONE, bounded by the breed vocabulary; scope=inherited from resolved_gain (tenant + scoped parks + window).
  --
  -- Ratio key sets: n and the four FILTER counts range over the IDENTICAL grouped row
  -- set — same FROM, same GROUP BY, no branch adds a join — so a client may take
  -- band/n as this breed's share without reaching for a second query's denominator, and
  -- b180 + b1820 + b2025 + a250 = n for every row.
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, n, b180, b1820, b2025, a250) ORDER BY n DESC, breed), '[]'::jsonb)
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
		gainThresholdBreedJSON                                      []byte
		compositionJSON                                             []byte
	)
	if err := r.pool.QueryRow(ctx, q, tenantID, parkIDs, periodStart, periodEnd).Scan(
		&resolvedCount, &unresolvedCount, &lumpTotal, &lumpUnattributed,
		&breedJSON, &sexJSON, &stageJSON,
		&gainBreedJSON, &gainSexJSON, &gainStageJSON,
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
