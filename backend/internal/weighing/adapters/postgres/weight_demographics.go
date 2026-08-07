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
		ByBreed: []domain.WeightDemographicBucket{},
		BySex:   []domain.WeightDemographicBucket{},
		ByStage: []domain.WeightDemographicBucket{},
	}
	if len(parkIDs) == 0 {
		return out, nil
	}

	const q = `
WITH scoped AS (
  SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id, cs.weighing_category
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid AND c.park_id = ANY($2::uuid[]) AND cs.status <> 'canceled'
),
latest AS (
  SELECT DISTINCT ON (lower(btrim(o.scanned_identifier)))
         lower(btrim(o.scanned_identifier)) AS tag, o.weight_kg
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
  SELECT lower(btrim(o.scanned_identifier)) AS tag, o.weight_kg,
         (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
  FROM weighing_observations o
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id AND s.tenant_id = o.tenant_id
  WHERE o.tenant_id = $1::uuid
    AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz
    AND o.verification_status <> 'rejected' AND btrim(o.scanned_identifier) <> ''
),
paired AS (
  SELECT tag, weight_kg, d,
         lag(weight_kg) OVER (PARTITION BY tag ORDER BY d) AS prev_w,
         lag(d) OVER (PARTITION BY tag ORDER BY d) AS prev_d
  FROM obs
),
-- One gain per ANIMAL (the median of its own pairs), so an animal weighed six
-- times does not outvote one weighed twice inside a breed.
animal_gain AS (
  SELECT tag, percentile_cont(0.5) WITHIN GROUP (
           ORDER BY (weight_kg - prev_w) * 1000.0 / (d - prev_d)) AS g
  FROM paired WHERE prev_d IS NOT NULL AND d > prev_d GROUP BY tag
),
ident AS (
  SELECT DISTINCT ON (lower(btrim(gi.identifier_value)))
         lower(btrim(gi.identifier_value)) AS tag, gi.goat_id
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid
  ORDER BY lower(btrim(gi.identifier_value)), gi.created_at DESC
),
resolved AS (
  SELECT l.tag, l.weight_kg, g.breed, g.sex, g.management_stage
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
shed_cohort AS (
  -- projection-review: membership=one row per location that resolves to a set of live goats, either its own or its physical shed's; group_key=src.location_id, exactly the GROUP BY; join_cardinality=goats is 0..N per resolved shed and is COLLAPSED by the aggregate to one row per location, and the resolved_id subquery is LIMIT 1 so it can never fan a location out; pagination=NONE, this is a lookup joined 0..1 into the lump-sum arms; scope=tenant_id on both the locations scan and the goats join
  --
  -- Ratio key sets: the breeds/sexes/stages counts and the min() values they gate range over the IDENTICAL grouped row set — same FROM, same GROUP BY, no branch adds a join — so a breeds count of 1 provably means the single breed min(breed) returns.
  SELECT src.location_id,
         min(g.breed)            FILTER (WHERE TRUE) AS breed,
         min(g.sex)              FILTER (WHERE TRUE) AS sex,
         min(g.management_stage) FILTER (WHERE TRUE) AS stage,
         count(DISTINCT g.breed)            AS breeds,
         count(DISTINCT g.sex)              AS sexes,
         count(DISTINCT g.management_stage) AS stages
  FROM (
    -- The partition's parent is the PARK, not the physical shed: "Castro 1" hangs off
    -- Coimbatore, not off "Castro". So the fallback resolves by NAME within the same
    -- park — "Castro 1" -> "Castro" — which is the same storage-vs-display split
    -- AGENTS.md describes, where a partition is stored as shed + label.
    --
    -- Matched within the park, never globally: two parks both hold a "Castro", and a
    -- name-only match would merge them.
    SELECT l.location_id, COALESCE(
      CASE WHEN EXISTS (SELECT 1 FROM goats gg WHERE gg.tenant_id = $1::uuid
                          AND gg.lifecycle_status = 'alive' AND gg.shed_id = l.location_id)
           THEN l.location_id END,
      (SELECT phys.location_id FROM locations phys
        WHERE phys.tenant_id = l.tenant_id
          AND phys.parent_location_id = l.parent_location_id
          AND phys.location_type = 'shed'
          AND phys.name = regexp_replace(l.name, '\s*(-\s*)?(Part\s*)?[0-9]+$', '')
        LIMIT 1)) AS resolved_id
    FROM locations l WHERE l.tenant_id = $1::uuid
  ) src
  JOIN goats g ON g.shed_id = src.resolved_id AND g.tenant_id = $1::uuid
   AND g.lifecycle_status = 'alive'
  GROUP BY src.location_id
),
shed_stage AS (
  SELECT location_id, stage FROM shed_cohort WHERE stages = 1
),
lump AS (
  SELECT s.location_id, sh.animal_count, sh.average_weight_kg
  FROM scoped s
  JOIN weighing_shed_observations sh
    ON sh.campaign_shed_id = s.campaign_shed_id AND sh.tenant_id = s.tenant_id
   AND sh.withdrawn_at IS NULL
   AND sh.accepted_at >= $3::timestamptz AND sh.accepted_at < $4::timestamptz
   AND sh.verification_status <> 'rejected'
  WHERE s.weighing_category = 'per_shed_partition'
),
lump_span AS (
  -- LAST TWO weighs and the days between them, matching the shed rows and the legacy
  -- adg_goat_last2 shape. A full-span figure hides a shed that has just stalled.
  --
  -- projection-review: membership=one row per lump-sum location with at least two weighs in the window; group_key=location_id, exactly the GROUP BY; join_cardinality=weighing_shed_observations 0..N per location and COLLAPSED by the aggregate to one row, campaign_sheds 1 per bucket (PK), campaigns 1 per campaign (PK); pagination=NONE, joined 0..1 into the gain arms; scope=tenant_id + park_id = ANY($2)
  --
  -- Ratio key sets: the two averages and the day gap are drawn from the SAME two rows of the SAME location, so the division is always one shed against its own previous weigh.
  SELECT sh.location_id,
         (max(sh.average_weight_kg) FILTER (WHERE sh.rn = 1)
        - max(sh.average_weight_kg) FILTER (WHERE sh.rn = 2)) * 1000.0
          / NULLIF(max(sh.d) FILTER (WHERE sh.rn = 1) - max(sh.d) FILTER (WHERE sh.rn = 2), 0) AS g_per_day,
         max(sh.animal_count) FILTER (WHERE sh.rn = 1)                                          AS animals
  FROM (
    SELECT cs2.location_id, so.average_weight_kg, so.animal_count,
           (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id ORDER BY so.accepted_at DESC) AS rn
    FROM weighing_shed_observations so
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = so.campaign_shed_id
    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id
    WHERE so.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
      AND so.withdrawn_at IS NULL AND so.verification_status <> 'rejected'
      AND so.accepted_at >= $3::timestamptz AND so.accepted_at < $4::timestamptz
  ) sh
  WHERE sh.rn <= 2
  GROUP BY sh.location_id
  HAVING count(*) = 2
)
SELECT
  (SELECT count(*) FROM resolved WHERE breed IS NOT NULL),
  (SELECT count(*) FROM resolved WHERE breed IS NULL),
  (SELECT COALESCE(sum(l.animal_count), 0) FROM lump l),
  (SELECT COALESCE(sum(l.animal_count), 0) FROM lump l
     LEFT JOIN shed_stage ss ON ss.location_id = l.location_id WHERE ss.stage IS NULL),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, n, avg) ORDER BY n DESC), '[]'::jsonb)
     FROM (SELECT breed, sum(n)::bigint n, (sum(total)/NULLIF(sum(n),0))::float8 avg FROM (
             SELECT breed, count(*)::bigint n, sum(weight_kg)::float8 total FROM resolved
              WHERE breed IS NOT NULL GROUP BY breed
             UNION ALL
             SELECT sc.breed, sum(l.animal_count)::bigint, sum(l.animal_count*l.average_weight_kg)::float8
              FROM lump l JOIN shed_cohort sc ON sc.location_id = l.location_id
              WHERE sc.breeds = 1 GROUP BY sc.breed) bp GROUP BY breed) b),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(sex, n, avg) ORDER BY n DESC), '[]'::jsonb)
     FROM (SELECT sex, sum(n)::bigint n, (sum(total)/NULLIF(sum(n),0))::float8 avg FROM (
             SELECT sex, count(*)::bigint n, sum(weight_kg)::float8 total FROM resolved
              WHERE sex IS NOT NULL GROUP BY sex
             UNION ALL
             SELECT sc.sex, sum(l.animal_count)::bigint, sum(l.animal_count*l.average_weight_kg)::float8
              FROM lump l JOIN shed_cohort sc ON sc.location_id = l.location_id
              WHERE sc.sexes = 1 GROUP BY sc.sex) sp GROUP BY sex) x),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(stage, n, avg) ORDER BY n DESC), '[]'::jsonb)
     FROM (
       SELECT stage, sum(n)::bigint n, (sum(total) / NULLIF(sum(n), 0))::float8 avg
       FROM (
         SELECT management_stage AS stage, count(*)::bigint n, sum(weight_kg)::float8 total
         FROM resolved WHERE management_stage IS NOT NULL GROUP BY management_stage
         UNION ALL
         SELECT ss.stage, sum(l.animal_count)::bigint, sum(l.animal_count * l.average_weight_kg)::float8
         FROM lump l JOIN shed_stage ss ON ss.location_id = l.location_id GROUP BY ss.stage
       ) parts GROUP BY stage
     ) st),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, n, g) ORDER BY n DESC), '[]'::jsonb)
     FROM (SELECT breed, sum(n)::bigint n, (sum(tot)/NULLIF(sum(n),0))::float8 g FROM (
             SELECT breed, count(*)::bigint n, sum(g)::float8 tot
               FROM resolved_gain WHERE breed IS NOT NULL GROUP BY breed
             UNION ALL
             -- Whole-shed movement counts here too, weighted by the animals behind it.
             -- Without this arm a stage weighed only as sheds showed nothing at all.
             SELECT sc.breed, sum(ls.animals)::bigint, sum(ls.g_per_day * ls.animals)::float8
               FROM lump_span ls JOIN shed_cohort sc ON sc.location_id = ls.location_id
               WHERE sc.breeds = 1 AND sc.breed IS NOT NULL GROUP BY sc.breed) gp
           GROUP BY breed) gb),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(sex, n, g) ORDER BY n DESC), '[]'::jsonb)
     FROM (SELECT sex, sum(n)::bigint n, (sum(tot)/NULLIF(sum(n),0))::float8 g FROM (
             SELECT sex, count(*)::bigint n, sum(g)::float8 tot
               FROM resolved_gain WHERE sex IS NOT NULL GROUP BY sex
             UNION ALL
             -- Whole-shed movement counts here too, weighted by the animals behind it.
             -- Without this arm a stage weighed only as sheds showed nothing at all.
             SELECT sc.sex, sum(ls.animals)::bigint, sum(ls.g_per_day * ls.animals)::float8
               FROM lump_span ls JOIN shed_cohort sc ON sc.location_id = ls.location_id
               WHERE sc.sexes = 1 AND sc.sex IS NOT NULL GROUP BY sc.sex) gp
           GROUP BY sex) gx),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(management_stage, n, g) ORDER BY n DESC), '[]'::jsonb)
     FROM (SELECT management_stage, sum(n)::bigint n, (sum(tot)/NULLIF(sum(n),0))::float8 g FROM (
             SELECT management_stage, count(*)::bigint n, sum(g)::float8 tot
               FROM resolved_gain WHERE management_stage IS NOT NULL GROUP BY management_stage
             UNION ALL
             -- Whole-shed movement counts here too, weighted by the animals behind it.
             -- Without this arm a stage weighed only as sheds showed nothing at all.
             SELECT sc.stage, sum(ls.animals)::bigint, sum(ls.g_per_day * ls.animals)::float8
               FROM lump_span ls JOIN shed_cohort sc ON sc.location_id = ls.location_id
               WHERE sc.stages = 1 AND sc.stage IS NOT NULL GROUP BY sc.stage) gp
           GROUP BY management_stage) gs)`

	var (
		resolvedCount, unresolvedCount, lumpTotal, lumpUnattributed int
		breedJSON, sexJSON, stageJSON                               []byte
		gainBreedJSON, gainSexJSON, gainStageJSON                   []byte
	)
	if err := r.pool.QueryRow(ctx, q, tenantID, parkIDs, periodStart, periodEnd).Scan(
		&resolvedCount, &unresolvedCount, &lumpTotal, &lumpUnattributed,
		&breedJSON, &sexJSON, &stageJSON,
		&gainBreedJSON, &gainSexJSON, &gainStageJSON,
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
	return out, nil
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
