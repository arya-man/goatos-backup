package postgres

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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
const weighingObsCTE = `
obs AS (
  SELECT o.observation_id,
         lower(btrim(o.scanned_identifier)) AS tag_key,
         o.weight_kg, o.accepted_at, o.campaign_id,
         c.period_start_date,
         cs.location_id AS shed_id, cs.display_name AS shed_label
  FROM weighing_observations o
  JOIN weighing_campaigns c
    ON c.tenant_id = o.tenant_id AND c.campaign_id = o.campaign_id
  LEFT JOIN weighing_campaign_sheds cs
    ON cs.tenant_id = o.tenant_id AND cs.campaign_shed_id = o.campaign_shed_id
  WHERE o.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND c.status <> 'canceled'
    AND c.period_end_date >= $3::date
    AND c.period_start_date < $4::date
    AND btrim(o.scanned_identifier) <> ''
    AND o.verification_status <> 'rework'
)`

// roundLatestCTE collapses repeat scans to ONE weigh per (identity, campaign
// week): newest capture wins, the 000073 grain and 000080 reporting convention.
const roundLatestCTE = `
round_latest AS (
  SELECT DISTINCT ON (tag_key, campaign_id)
         tag_key, campaign_id, period_start_date, weight_kg, accepted_at, observation_id, shed_id, shed_label
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
         (array_agg(shed_id     ORDER BY period_start_date DESC, accepted_at DESC, observation_id DESC))[1]  AS shed_id,
         (array_agg(shed_label  ORDER BY period_start_date DESC, accepted_at DESC, observation_id DESC))[1]  AS shed_label
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
func (r *Repository) GetGrowthDirectorWeights(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.GrowthDirectorWeights, error) {
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

	parks, err := r.parks(ctx, tenantID, parkIDs)
	if err != nil {
		return out, err
	}
	out.Parks = parks

	if out.RoadToSale, err = r.roadToSale(ctx, tenantID, parkIDs, startDate, endExclusiveDate); err != nil {
		return out, err
	}
	if out.FairFight, err = r.fairFight(ctx, tenantID, parkIDs, startDate, endExclusiveDate); err != nil {
		return out, err
	}
	if out.SlowGrowth, err = r.slowGrowth(ctx, tenantID, parkIDs, startDate, endExclusiveDate); err != nil {
		return out, err
	}
	if out.FeedVsGrowth, err = r.feedVsGrowth(ctx, tenantID, parkIDs, startDate, endExclusiveDate); err != nil {
		return out, err
	}
	if out.FeedProblems, err = r.feedProblems(ctx, tenantID, parkIDs, startDate, endExclusiveDate); err != nil {
		return out, err
	}
	if out.Trust, err = r.trust(ctx, tenantID, parkIDs, startDate, endExclusiveDate); err != nil {
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

// roadToSale places each identity's latest weight in a band and scores band
// movement against the identity's previous campaign round.
//
// projection-review: membership=one row per weight band with identities in it;
// group_key=band_idx over latest-per-identity rows; join_cardinality=prev is
// 0..1 per tag_key (rn=2 of the same ranking), goat_identifiers is 0..1 by the
// lifetime-unique (tenant_id, normalized_value) index, so no side multiplies;
// pagination=NONE, at most six band rows; scope=tenant + park ANY + campaign
// week overlap.
func (r *Repository) roadToSale(ctx context.Context, tenantID string, parkIDs []string, startDate, endDate string) (domain.RoadToSale, error) {
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
scored AS (
  SELECT l.tag_key, l.weight_kg, l.weigh_rounds,
         width_bucket(l.weight_kg, ARRAY[15,20,25,30,35]::numeric[]) AS band_idx,
         CASE WHEN p.weight_kg IS NULL THEN NULL
              ELSE width_bucket(p.weight_kg, ARRAY[15,20,25,30,35]::numeric[]) END AS prev_band_idx,
         (gi.goat_id IS NOT NULL) AS is_matched
  FROM latest l
  LEFT JOIN prev p USING (tag_key)
  LEFT JOIN goat_identifiers gi
    ON gi.tenant_id = $1::uuid AND gi.normalized_value = upper(l.tag_key)
)
SELECT band_idx,
       count(*) AS identity_count,
       count(*) FILTER (WHERE is_matched) AS matched_count,
       count(*) FILTER (WHERE weigh_rounds >= 2) AS pair_count,
       count(*) FILTER (WHERE prev_band_idx IS NOT NULL AND band_idx > prev_band_idx) AS moved_up,
       count(*) FILTER (WHERE prev_band_idx IS NOT NULL AND band_idx = prev_band_idx) AS held,
       count(*) FILTER (WHERE prev_band_idx IS NOT NULL AND band_idx < prev_band_idx) AS moved_down
FROM scored
GROUP BY band_idx
ORDER BY band_idx`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, startDate, endDate)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var bandIdx, identityCount, matched, pairCount, movedUp, held, movedDown int
		if err := rows.Scan(&bandIdx, &identityCount, &matched, &pairCount, &movedUp, &held, &movedDown); err != nil {
			return out, err
		}
		if bandIdx >= 0 && bandIdx < len(out.Bands) {
			out.Bands[bandIdx].IdentityCount = identityCount
		}
		out.TotalIdentities += identityCount
		out.MatchedIdentities += matched
		out.Movement.PairIdentities += pairCount
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
func (r *Repository) trust(ctx context.Context, tenantID string, parkIDs []string, startDate, endDate string) (domain.Trust, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	var out domain.Trust
	const q = `
WITH obs AS (
  SELECT o.observation_id, lower(btrim(o.scanned_identifier)) AS tag_key,
         o.verification_status, o.campaign_id
  FROM weighing_observations o
  JOIN weighing_campaigns c ON c.tenant_id = o.tenant_id AND c.campaign_id = o.campaign_id
  WHERE o.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND c.status <> 'canceled'
    AND c.period_end_date >= $3::date
    AND c.period_start_date < $4::date
    AND btrim(o.scanned_identifier) <> ''
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
  WHERE s.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND c.status <> 'canceled'
    AND c.period_end_date >= $3::date
    AND c.period_start_date < $4::date
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
	err := r.pool.QueryRow(ctx, q, tenantID, parkIDs, startDate, endDate).Scan(
		&out.ScansTotal, &out.ScansMatched, &out.ScansUnmatched,
		&out.ScansPendingVerification, &out.ScansRework,
		&out.IdentitiesTotal, &out.IdentitiesWithPair, &out.IdentitiesOnceOnly,
		&out.WholeShedObservations,
	)
	return out, err
}
