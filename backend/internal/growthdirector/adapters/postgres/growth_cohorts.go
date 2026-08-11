package postgres

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
)

// fairFight compares the same (breed, sex) across sheds. A cohort renders only
// when >=2 sheds each field >=3 plausible pair-identities.
//
// projection-review: membership=(breed, sex, shed) groups clearing the per-shed
// floor, inside cohorts clearing the two-shed floor; group_key=(breed, sex,
// shed_id); join_cardinality=goat_identifiers 0..1 (lifetime-unique), goats 1
// (PK), canon 0..1 (PK), so a pair row cannot multiply; pagination=NONE,
// bounded by the shed/cohort floors; scope=tenant + park ANY + week overlap.
func (r *Repository) fairFight(ctx context.Context, tenantID string, parkIDs []string, startDate, endDate string) (domain.FairFight, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	out := domain.FairFight{Cohorts: []domain.FairFightCohort{}}
	const q = `
WITH ` + weighingObsCTE + `,
` + roundLatestCTE + `,
` + firstLastPairCTE + `,
adg AS (
  SELECT p.*,
         (w_last - w_first) * 1000.0 / (t_last::date - t_first::date) AS adg_g_day
  FROM pairs p
  WHERE t_last::date > t_first::date   -- same-day pairs carry no growth signal
),
plausible AS (
  -- >0.30 kg/day loss is a bad scan or scale, not growth truth.
  SELECT * FROM adg WHERE adg_g_day > -300
),
cohorted AS (
  SELECT a.*,
         COALESCE(NULLIF(btrim(COALESCE(canon.breed, g.breed)), ''), '(unknown)') AS breed,
         COALESCE(canon.sex, g.sex) AS sex
  FROM plausible a
` + breedSexJoin + `
),
shed_cohort AS (
  SELECT breed, sex, shed_id, shed_label,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_day) AS median_adg_g_day,
         count(*) AS pair_identity_count
  FROM cohorted
  WHERE shed_id IS NOT NULL
  GROUP BY breed, sex, shed_id, shed_label
  HAVING count(*) >= 3                 -- a median of 2 is a coin flip
)
SELECT breed, sex, shed_id::text, COALESCE(shed_label, ''), pair_identity_count, median_adg_g_day
FROM (
  SELECT sc.*, count(*) OVER (PARTITION BY breed, sex) AS sheds_in_cohort
  FROM shed_cohort sc
) x
WHERE sheds_in_cohort >= 2             -- a fair fight needs two sheds fielding the same kind of kid
ORDER BY breed, sex, median_adg_g_day DESC`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, startDate, endDate)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var breed, sex, shedID, shedLabel string
		var pairCount int
		var median float64
		if err := rows.Scan(&breed, &sex, &shedID, &shedLabel, &pairCount, &median); err != nil {
			return out, err
		}
		n := len(out.Cohorts)
		if n == 0 || out.Cohorts[n-1].Breed != breed || out.Cohorts[n-1].Sex != sex {
			out.Cohorts = append(out.Cohorts, domain.FairFightCohort{Breed: breed, Sex: sex, Sheds: []domain.FairFightShed{}})
			n++
		}
		out.Cohorts[n-1].Sheds = append(out.Cohorts[n-1].Sheds, domain.FairFightShed{
			LocationID:       shedID,
			ShedDisplayName:  shedLabel,
			PairIdentities:   pairCount,
			MedianADGGPerDay: median,
		})
	}
	return out, rows.Err()
}

// slowGrowth lists every (shed, breed, sex) group with >=3 plausible pairs and
// labels its status against the disclosed target. The raw median is reported;
// the status is judged on the NOISE-ADJUSTED median (weight changes within 3%
// of starting body weight scored as flat), so one noisy scale never brands a
// shed as shrinking. Week-over-week deltas come from a second query over
// consecutive-round pairs and merge by group key.
func (r *Repository) slowGrowth(ctx context.Context, tenantID string, parkIDs []string, startDate, endDate string) (domain.SlowGrowth, error) {
	out := domain.SlowGrowth{TargetGPerDay: domain.SlowGrowthTargetGPerDay, Groups: []domain.SlowGrowthGroup{}}
	groups, err := r.slowGrowthGroups(ctx, tenantID, parkIDs, startDate, endDate)
	if err != nil {
		return out, err
	}
	deltas, err := r.weekOverWeekDeltas(ctx, tenantID, parkIDs, startDate, endDate)
	if err != nil {
		return out, err
	}
	for i := range groups {
		key := groups[i].LocationID + "|" + groups[i].Breed + "|" + groups[i].Sex
		if delta, ok := deltas[key]; ok {
			d := delta
			groups[i].WeekOverWeekDeltaG = &d
		}
	}
	out.Groups = groups
	return out, nil
}

func (r *Repository) slowGrowthGroups(ctx context.Context, tenantID string, parkIDs []string, startDate, endDate string) ([]domain.SlowGrowthGroup, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	const q = `
WITH ` + weighingObsCTE + `,
` + roundLatestCTE + `,
` + firstLastPairCTE + `,
adg AS (
  SELECT p.*,
         (w_last - w_first) * 1000.0 / (t_last::date - t_first::date) AS adg_g_day,
         -- NOISE BAND: |delta| <= 3% of starting body weight is gut fill /
         -- scale noise -> scored as flat (0 g/day) for status judgement.
         CASE WHEN abs(w_last - w_first) <= 0.03 * w_first THEN 0
              ELSE (w_last - w_first) * 1000.0 / (t_last::date - t_first::date) END AS adg_noise_adj_g_day,
         (((w_last - w_first) * 1000.0 / (t_last::date - t_first::date)) <= -300) AS implausible
  FROM pairs p
  WHERE t_last::date > t_first::date
),
cohorted AS (
  SELECT a.*,
         COALESCE(NULLIF(btrim(COALESCE(canon.breed, g.breed)), ''), '(unknown)') AS breed,
         COALESCE(canon.sex, g.sex) AS sex
  FROM adg a
` + breedSexJoin + `
)
SELECT shed_id::text, COALESCE(shed_label, ''), breed, sex,
       count(*) FILTER (WHERE NOT implausible) AS pair_count,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_day)           FILTER (WHERE NOT implausible) AS median_adg_g_day,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_noise_adj_g_day) FILTER (WHERE NOT implausible) AS median_noise_adj_g_day
FROM cohorted
WHERE shed_id IS NOT NULL
GROUP BY shed_id, shed_label, breed, sex
HAVING count(*) FILTER (WHERE NOT implausible) >= 3
ORDER BY median_noise_adj_g_day ASC NULLS LAST, shed_id, breed, sex`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []domain.SlowGrowthGroup{}
	for rows.Next() {
		var g domain.SlowGrowthGroup
		var median, noiseAdj *float64
		if err := rows.Scan(&g.LocationID, &g.ShedDisplayName, &g.Breed, &g.Sex, &g.PairIdentities, &median, &noiseAdj); err != nil {
			return nil, err
		}
		if median != nil {
			g.MedianADGGPerDay = *median
		}
		g.Status = slowGrowthStatus(noiseAdj)
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func slowGrowthStatus(noiseAdjustedMedian *float64) string {
	if noiseAdjustedMedian == nil {
		return domain.SlowGrowthStatusBelowTarget
	}
	switch {
	case *noiseAdjustedMedian < 0:
		return domain.SlowGrowthStatusLosing
	case *noiseAdjustedMedian < domain.SlowGrowthTargetGPerDay:
		return domain.SlowGrowthStatusBelowTarget
	default:
		return domain.SlowGrowthStatusOnTrack
	}
}

// weekOverWeekDeltas compares each group's latest campaign week against the
// week before it, over CONSECUTIVE-round pairs (each pair attributed to the
// shed and week of its later round). A group needs pairs in two distinct weeks
// before a trend exists; everything else is simply absent from the map.
func (r *Repository) weekOverWeekDeltas(ctx context.Context, tenantID string, parkIDs []string, startDate, endDate string) (map[string]float64, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	const q = `
WITH ` + weighingObsCTE + `,
` + roundLatestCTE + `,
consec AS (
  SELECT tag_key, shed_id, period_start_date AS week_start,
         weight_kg, accepted_at::date AS obs_date, observation_id,
         lag(weight_kg)          OVER w AS prev_w,
         lag(accepted_at::date)  OVER w AS prev_date
  FROM round_latest
  WINDOW w AS (PARTITION BY tag_key ORDER BY period_start_date, accepted_at, observation_id)
),
consec_adg AS (
  SELECT tag_key, shed_id, week_start,
         (weight_kg - prev_w) * 1000.0 / (obs_date - prev_date) AS adg_g_day
  FROM consec
  WHERE prev_w IS NOT NULL
    AND obs_date > prev_date
    AND (weight_kg - prev_w) * 1000.0 / (obs_date - prev_date) > -300
),
cohorted AS (
  SELECT a.*,
         COALESCE(NULLIF(btrim(COALESCE(canon.breed, g.breed)), ''), '(unknown)') AS breed,
         COALESCE(canon.sex, g.sex) AS sex
  FROM consec_adg a
` + breedSexJoin + `
),
week_medians AS (
  -- The SAME >=3-pairs floor the group itself clears, applied PER WEEK: without
  -- it a group can look solid overall while its "vs last week" number is one
  -- kid's single pair. Weeks below the floor simply don't exist for the trend,
  -- so the delta stays absent rather than thin.
  SELECT shed_id, breed, sex, week_start,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_day) AS wk_median,
         count(*) AS wk_pairs,
         row_number() OVER (PARTITION BY shed_id, breed, sex ORDER BY week_start DESC) AS wk_rn
  FROM cohorted
  WHERE shed_id IS NOT NULL
  GROUP BY shed_id, breed, sex, week_start
),
solid_weeks AS (
  SELECT *, row_number() OVER (PARTITION BY shed_id, breed, sex ORDER BY week_start DESC) AS solid_rn
  FROM week_medians
  WHERE wk_pairs >= 3
)
SELECT cur.shed_id::text, cur.breed, cur.sex, (cur.wk_median - prev.wk_median)::float8 AS delta_g
FROM solid_weeks cur
JOIN solid_weeks prev
  ON prev.shed_id = cur.shed_id AND prev.breed = cur.breed AND prev.sex = cur.sex AND prev.solid_rn = 2
WHERE cur.solid_rn = 1`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deltas := map[string]float64{}
	for rows.Next() {
		var shedID, breed, sex string
		var delta float64
		if err := rows.Scan(&shedID, &breed, &sex, &delta); err != nil {
			return nil, err
		}
		deltas[shedID+"|"+breed+"|"+sex] = delta
	}
	return deltas, rows.Err()
}
