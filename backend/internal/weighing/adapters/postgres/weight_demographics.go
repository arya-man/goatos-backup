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
shed_stage AS (
  SELECT g.shed_id, min(g.management_stage) AS stage
  FROM goats g
  WHERE g.tenant_id = $1::uuid AND g.lifecycle_status = 'alive' AND g.management_stage IS NOT NULL
  GROUP BY g.shed_id
  HAVING count(DISTINCT g.management_stage) = 1
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
)
SELECT
  (SELECT count(*) FROM resolved WHERE breed IS NOT NULL),
  (SELECT count(*) FROM resolved WHERE breed IS NULL),
  (SELECT COALESCE(sum(l.animal_count), 0) FROM lump l),
  (SELECT COALESCE(sum(l.animal_count), 0) FROM lump l
     LEFT JOIN shed_stage ss ON ss.shed_id = l.location_id WHERE ss.stage IS NULL),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, n, avg) ORDER BY n DESC), '[]'::jsonb)
     FROM (SELECT breed, count(*) n, avg(weight_kg)::float8 avg FROM resolved
            WHERE breed IS NOT NULL GROUP BY breed) b),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(sex, n, avg) ORDER BY n DESC), '[]'::jsonb)
     FROM (SELECT sex, count(*) n, avg(weight_kg)::float8 avg FROM resolved
            WHERE sex IS NOT NULL GROUP BY sex) x),
  (SELECT COALESCE(jsonb_agg(jsonb_build_array(stage, n, avg) ORDER BY n DESC), '[]'::jsonb)
     FROM (
       SELECT stage, sum(n)::bigint n, (sum(total) / NULLIF(sum(n), 0))::float8 avg
       FROM (
         SELECT management_stage AS stage, count(*)::bigint n, sum(weight_kg)::float8 total
         FROM resolved WHERE management_stage IS NOT NULL GROUP BY management_stage
         UNION ALL
         SELECT ss.stage, sum(l.animal_count)::bigint, sum(l.animal_count * l.average_weight_kg)::float8
         FROM lump l JOIN shed_stage ss ON ss.shed_id = l.location_id GROUP BY ss.stage
       ) parts GROUP BY stage
     ) st)`

	var (
		resolvedCount, unresolvedCount, lumpTotal, lumpUnattributed int
		breedJSON, sexJSON, stageJSON                               []byte
	)
	if err := r.pool.QueryRow(ctx, q, tenantID, parkIDs, periodStart, periodEnd).Scan(
		&resolvedCount, &unresolvedCount, &lumpTotal, &lumpUnattributed,
		&breedJSON, &sexJSON, &stageJSON,
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
