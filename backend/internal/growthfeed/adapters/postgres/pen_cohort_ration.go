package postgres

import (
	"context"

	// The kid ration group is imported from feed's own domain rather than
	// re-declared here. It is feed's business fact, and a local copy would drift
	// silently the day feed renames it.
	feeddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/growthfeed/ports"
)

// ListPenCohortRation reads the OTHER half of the comparison: what kind of animal
// each pen holds, and what feed config says that animal is meant to eat. It touches
// no weighing table at all — the growth figures arrive from ListPenGrowth and the
// two are joined in Go on location_id.
//
// AGREE OR GO BARE. A pen's breed / sex / stage is reported only when every live
// animal resolving to it shares one value. Castro's residents share one breed, one
// sex and one stage, so it can be attributed; Godel 2 holds nine breeds and six
// stages, so it is attributed to NOTHING rather than to whichever value happens to
// sort first. `HAVING`-style count(DISTINCT ...) = 1 checks are what enforce that,
// and an ORDER BY ... LIMIT 1 would be the defect they exist to prevent.
//
// THE PARTITION FALLBACK, copied deliberately from weighing's weight_demographics.go.
// A weighing bucket points at a PARTITION ("Castro 1") but the herd register puts
// the animals on the PHYSICAL SHED ("Castro") — 266 live goats on the parent, zero
// on each partition. Looking only at the partition therefore finds nothing and
// silently reports every partitioned pen as unresolved. So a pen resolves to its
// own residents when it has any, and otherwise to its parent shed's, matched BY
// NAME WITHIN THE SAME PARK — two parks both hold a "Castro", and a name-only
// match would merge them.
//
// RATION RESOLUTION mirrors feeddirection: the pen's management_stage matches a
// feed_shed_tags row, whose applies_to decides the course. A kid tag resolves to
// the fixed 'Kid' ration group of every breed; an adult tag resolves through
// feed_ration_groups by breed. The rate is then (park, ration group, shed tag,
// feed item) -> grams_per_head, multiplied by the pen's per-item shed factor.
//
// NO COALESCE ON grams_per_head, EVER. A missing rate row is the only encoding feed
// config has for "not configured" and must never read as 0 — see migration 000001's
// CONFIGURED ZERO block. The query therefore COUNTS resolved and missing cells
// separately and hands both to domain.ResolveFeedPlan, which decides what may be
// reported. LEFT JOIN is used precisely so the missing ones stay visible as NULLs
// instead of disappearing.
//
// projection-review: membership=one row per requested location_id that resolves to at least one live goat; group_key=p.location_id, exactly the GROUP BY in cohort and plan; join_cardinality=goats 0..N per pen COLLAPSED by the aggregate to one row per location and the resolved_id subquery is LIMIT 1 so it cannot fan a pen out, feed_shed_tags 0..1 per pen (unique on tenant_id+shed_tag_key), feed_ration_groups 0..1 per pen (unique on tenant_id+breed_key), rates 0..1 per (pen, feed item) via DISTINCT ON, factors 0..1 per (pen, feed item) via DISTINCT ON, experiment 0..1 per pen (grouped); pagination=NONE, the input location list is already bounded by MaxPenRows upstream; scope=tenant_id on every table plus park_id = ANY($4) on both feed config reads
//
// Ratio key sets: items_configured and items_blocked range over the IDENTICAL key
// set — active feed items x this pen — because both are FILTER counts over the one
// grouped row set the CROSS JOIN produces. planned_grams sums over exactly the
// items_configured subset and over no other, so "grams" and "how many items those
// grams came from" can never describe different populations.
func (r *Repository) ListPenCohortRation(ctx context.Context, tenantID string, parkIDs, locationIDs []string, asOfDate string) ([]ports.PenCohortRation, error) {
	if len(locationIDs) == 0 || len(parkIDs) == 0 {
		return []ports.PenCohortRation{}, nil
	}
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	const q = ` -- scale-guard:ignore: bounded read over an explicit pen list already capped at MaxPenRows, joined to one tenant's authored feed grid (~721 rows per park); every predicate leads on tenant_id/park_id and no obligation or kernel hot table is touched
WITH pen AS (
  SELECT l.location_id, l.parent_location_id AS park_id, l.name
  FROM locations l
  WHERE l.tenant_id = $1::uuid AND l.location_id = ANY($2::uuid[])
),
resolved_src AS (
  SELECT p.location_id, p.park_id, COALESCE(
    CASE WHEN EXISTS (
           SELECT 1 FROM goats gg
           WHERE gg.tenant_id = $1::uuid AND gg.lifecycle_status = 'alive'
             AND gg.shed_id = p.location_id)
         THEN p.location_id END,
    (SELECT phys.location_id FROM locations phys
      WHERE phys.tenant_id = $1::uuid
        AND phys.parent_location_id = p.park_id
        AND phys.location_type = 'shed'
        AND phys.name = regexp_replace(p.name, '\s*(-\s*)?(Part\s*)?[0-9]+$', '')
      LIMIT 1)) AS resolved_id
  FROM pen p
),
cohort AS (
  SELECT s.location_id, s.park_id,
         count(*)::int                      AS live_animals,
         min(g.breed)                       AS breed,
         count(DISTINCT g.breed)            AS breeds,
         min(g.sex)                         AS sex,
         count(DISTINCT g.sex)              AS sexes,
         min(g.management_stage)            AS stage,
         count(DISTINCT g.management_stage) AS stages
  FROM resolved_src s
  JOIN goats g
    ON g.shed_id = s.resolved_id AND g.tenant_id = $1::uuid AND g.lifecycle_status = 'alive'
  GROUP BY s.location_id, s.park_id
),
tagged AS (
  SELECT c.*,
         t.shed_tag_label,
         t.applies_to
  FROM cohort c
  LEFT JOIN feed_shed_tags t
    ON t.tenant_id = $1::uuid AND t.status = 'active'
   AND c.stages = 1
   AND t.shed_tag_key = feed_config_norm(c.stage)
),
grouped AS (
  SELECT tg.*,
         CASE
           -- Every kid resolves to the fixed 'Kid' group regardless of breed, so a
           -- kid pen needs no breed agreement to be priced.
           WHEN tg.applies_to = 'kid'   THEN $5::text
           WHEN tg.applies_to = 'adult' THEN rg.ration_group_label
         END AS ration_group_label
  FROM tagged tg
  LEFT JOIN feed_ration_groups rg
    ON rg.tenant_id = $1::uuid
   AND tg.breeds = 1
   AND rg.breed_key = feed_config_norm(tg.breed)
),
rates AS (
  -- DISTINCT ON, not a bare join: two authored rows whose validity windows overlap
  -- would otherwise fan the pen out and DOUBLE its grams. The newest valid_from
  -- wins, which is the same "latest authored rate in force" the feed sheet applies.
  SELECT DISTINCT ON (park_id, ration_group_key, shed_tag_key, feed_item_key)
         park_id, ration_group_key, shed_tag_key, feed_item_key, grams_per_head::float8 AS grams
  FROM feed_ration_rates
  WHERE tenant_id = $1::uuid AND park_id = ANY($4::uuid[])
    AND valid_from <= $3::date AND (valid_to IS NULL OR valid_to > $3::date)
  ORDER BY park_id, ration_group_key, shed_tag_key, feed_item_key, valid_from DESC
),
factors AS (
  SELECT DISTINCT ON (park_id, shed_id, feed_item_key)
         park_id, shed_id, feed_item_key, multiplier::float8 AS multiplier
  FROM feed_shed_factors
  WHERE tenant_id = $1::uuid AND park_id = ANY($4::uuid[])
    AND valid_from <= $3::date AND (valid_to IS NULL OR valid_to > $3::date)
  ORDER BY park_id, shed_id, feed_item_key, valid_from DESC
),
plan AS (
  -- Every ACTIVE feed item is a cell for every priced pen, exactly as the daily
  -- feed sheet builds its columns. A cell with no rate row is BLOCKED, counted,
  -- and contributes nothing to the sum.
  SELECT g.location_id,
         count(*) FILTER (WHERE rt.grams IS NOT NULL)::int AS items_configured,
         count(*) FILTER (WHERE rt.grams IS NULL)::int     AS items_blocked,
         COALESCE(sum(rt.grams * COALESCE(f.multiplier, 1.0)), 0)::float8 AS planned_grams,
         COALESCE(sum(rt.grams * COALESCE(f.multiplier, 1.0) / 1000.0
                        * ic.energy_kcal_per_kg), 0)::float8 AS energy_kcal,
         -- An item that IS fed but carries no authored energy value makes the whole
         -- energy rollup incomplete. Reporting the partial sum as if it were the
         -- total would understate the pen's intake.
         bool_or(rt.grams IS NOT NULL AND ic.energy_kcal_per_kg IS NULL) AS energy_incomplete
  FROM grouped g
  CROSS JOIN feed_item_catalog ic
  LEFT JOIN rates rt
    ON rt.park_id = g.park_id
   AND rt.ration_group_key = feed_config_norm(g.ration_group_label)
   AND rt.shed_tag_key     = feed_config_norm(g.shed_tag_label)
   AND rt.feed_item_key    = ic.feed_item_key
  LEFT JOIN factors f
    ON f.park_id = g.park_id AND f.shed_id = g.location_id AND f.feed_item_key = ic.feed_item_key
  WHERE ic.tenant_id = $1::uuid AND ic.status = 'active'
    AND g.ration_group_label IS NOT NULL AND g.shed_tag_label IS NOT NULL
  GROUP BY g.location_id
),
experiment AS (
  -- absolute_kg is a SHED TOTAL, never a per-head rate. head_count on that table is
  -- informational and is deliberately NOT multiplied in.
  SELECT shed_id AS location_id, sum(absolute_kg)::float8 AS total_kg
  FROM feed_experiment_config
  WHERE tenant_id = $1::uuid AND status = 'active' AND shed_id = ANY($2::uuid[])
  GROUP BY shed_id
)
SELECT g.location_id,
       CASE WHEN g.breeds  = 1 THEN g.breed END,
       CASE WHEN g.sexes   = 1 THEN g.sex   END,
       CASE WHEN g.stages  = 1 THEN g.stage END,
       -- A pen is priceable when it agrees on ONE stage and either agrees on one
       -- breed or is a kid pen, where every breed shares the same ration group.
       -- This is computed here, where applies_to is in scope, rather than guessed
       -- in Go from whether a ration happened to be found — those are different
       -- facts, and conflating them would report a mixed-breed pen and a pen the
       -- farm simply has not configured as the same problem.
       (g.stages = 1 AND (g.breeds = 1 OR g.applies_to = 'kid')) AS cohort_resolved,
       g.live_animals,
       g.ration_group_label,
       g.shed_tag_label,
       COALESCE(p.items_configured, 0),
       COALESCE(p.items_blocked, 0),
       COALESCE(p.planned_grams, 0),
       COALESCE(p.energy_kcal, 0),
       COALESCE(p.energy_incomplete, false),
       COALESCE(e.total_kg, 0),
       (e.location_id IS NOT NULL) AS is_experiment
FROM grouped g
LEFT JOIN plan p       ON p.location_id = g.location_id
LEFT JOIN experiment e ON e.location_id = g.location_id`

	rows, err := r.pool.Query(ctx, q, tenantID, locationIDs, asOfDate, parkIDs, feeddomain.KidRationGroupLabel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ports.PenCohortRation{}
	for rows.Next() {
		var (
			row                          ports.PenCohortRation
			breed, sex, stage            *string
			rationGroupLabel, shedTagLbl *string
			energyIncomplete             bool
		)
		if err := rows.Scan(
			&row.LocationID, &breed, &sex, &stage, &row.Plan.CohortResolved, &row.LiveAnimals,
			&rationGroupLabel, &shedTagLbl,
			&row.Plan.ItemsConfigured, &row.Plan.ItemsBlocked,
			&row.Plan.PlannedGramsPerHeadDay, &row.Plan.EnergyKcalPerHeadDay, &energyIncomplete,
			&row.Plan.ExperimentTotalKgPerDay, &row.Plan.IsExperiment,
		); err != nil {
			return nil, err
		}
		row.Breed, row.Sex, row.Stage = deref(breed), deref(sex), deref(stage)
		row.RationGroupLabel, row.ShedTagLabel = deref(rationGroupLabel), deref(shedTagLbl)
		row.Plan.LiveAnimals = row.LiveAnimals
		row.Plan.RationGroupLabel = row.RationGroupLabel
		row.Plan.ShedTagLabel = row.ShedTagLabel
		row.Plan.EnergyComplete = !energyIncomplete
		out = append(out, row)
	}
	return out, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
