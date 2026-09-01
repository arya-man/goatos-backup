package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Hoisted to package scope rather than built inside the function, so a query-plan test can reach
// them by name -- which is what the scale guard asks for, and what these two in particular deserve:
// they run over a 400-day lookback on every landing.
//
// projection-review: membership=live whole-shed observations inside tenant+park+range;
// group_key=NONE (a DISTINCT date list); join_cardinality=campaign_sheds 1 per observation (PK),
// campaigns 1 per shed (PK), and DISTINCT is insensitive to duplication in any case;
// pagination=NONE, bounded by the range; scope=tenant_id + park_id = ANY, plus the sex predicate,
// so a date can never advertise a day this reader has no data for.
const lumpWeighingDatesQuery = `
SELECT DISTINCT to_char((sh.accepted_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD') AS weigh_date
FROM weighing_shed_observations sh
JOIN weighing_campaign_sheds cs
  ON cs.campaign_shed_id = sh.campaign_shed_id
 AND cs.tenant_id = sh.tenant_id
JOIN weighing_campaigns c
  ON c.campaign_id = cs.campaign_id
 AND c.tenant_id = cs.tenant_id
WHERE sh.tenant_id = $1::uuid
  AND c.park_id = ANY($2::uuid[])
  AND cs.weighing_category = 'per_shed_partition'
  AND cs.status <> 'canceled'
  AND sh.withdrawn_at IS NULL
  AND sh.verification_status <> 'rejected'
  AND sh.accepted_at >= $3::timestamptz
  AND sh.accepted_at <  $4::timestamptz
  -- The calendar's lump markers follow the filter too: a day whose only whole-shed weigh
  -- belongs to the other sex is not a day this reader has data for.
  AND (NOT $5::bool OR EXISTS (
    SELECT 1 FROM unnest($6::uuid[], $7::text[]) AS b(loc, part)
    WHERE b.loc = cs.location_id AND b.part = COALESCE(cs.partition_label, '')
  ))
ORDER BY weigh_date`

// The last day the farm weighed ANYTHING in range, either grain. Scanned through a POINTER: a
// range with no weighs has no date and must come back empty rather than fabricating one for the
// page to land on.
//
// projection-review: membership=live weighs of either grain inside tenant+park+range;
// group_key=NONE, a single scalar max; join_cardinality=1:1 on both PKs, and max() is insensitive
// to duplication regardless; pagination=NONE; scope=tenant_id + park_id = ANY plus the same sex
// predicates the rows use.
const latestWeighingDateQuery = `
SELECT max(d)::text FROM (
  SELECT (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
  FROM weighing_observations o
  JOIN weighing_campaign_sheds cs ON cs.campaign_shed_id = o.campaign_shed_id AND cs.tenant_id = o.tenant_id
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE o.tenant_id = $1::uuid AND c.park_id = ANY($2::uuid[])
    AND cs.status <> 'canceled'
    AND o.verification_status <> 'rejected'
    AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz
    AND (NOT $5::bool OR lower(btrim(o.scanned_identifier)) = ANY($8::text[]))
  UNION ALL
  SELECT (sh.accepted_at AT TIME ZONE 'Asia/Kolkata')::date
  FROM weighing_shed_observations sh
  JOIN weighing_campaign_sheds cs ON cs.campaign_shed_id = sh.campaign_shed_id AND cs.tenant_id = sh.tenant_id
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE sh.tenant_id = $1::uuid AND c.park_id = ANY($2::uuid[])
    AND cs.status <> 'canceled'
    AND sh.withdrawn_at IS NULL AND sh.verification_status <> 'rejected'
    AND sh.accepted_at >= $3::timestamptz AND sh.accepted_at < $4::timestamptz
    AND (NOT $5::bool OR EXISTS (
      SELECT 1 FROM unnest($6::uuid[], $7::text[]) AS b(loc, part)
      WHERE b.loc = cs.location_id AND b.part = COALESCE(cs.partition_label, '')
    ))
) z`

// weighingDates answers ONLY "which days carry a whole-shed weigh, and when did we last weigh
// anything" -- the two facts the Weights screens resolve their landing window from.
//
// WHY THIS IS ITS OWN READ. That window used to be resolved by calling the whole shed-weights read
// over a 400-DAY lookback: four queries, the full shed table, per-load growth and the summary, all
// discarded except these two fields. Locally that was ~570ms against ~140ms for the real windowed
// read, and it is paid on every page load and every tab switch. Against a cloud database, where
// each of those queries carries its own round trip and the scan is over a year of observations, it
// is the screen's dominant cost. The scale rules ask for a narrow endpoint at the screen's grain
// rather than a frontend cache or a longer timeout, and this is it.
//
// GetShedWeights calls this same helper, so the window a page opens on and the dates that page
// then reports cannot drift into two answers.
// GetWeighingDates is the public narrow read behind GET /weighing/weighing-dates.
//
// It resolves the sex scope exactly as the shed-weights read does -- a day whose only whole-shed
// weigh belongs to the other sex is not a day this reader has data for, so the landing window must
// not open on it -- and then runs the two date queries and nothing else.
func (r *Repository) GetWeighingDates(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex string) (domain.WeighingDates, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	scope, err := r.resolveSexScope(ctx, tenantID, parkIDs, sex, periodStart, periodEnd)
	if err != nil {
		return domain.WeighingDates{}, err
	}
	return r.weighingDates(ctx, tenantID, parkIDs, periodStart, periodEnd, strings.TrimSpace(sex) != "", scope)
}

func (r *Repository) weighingDates(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope) (domain.WeighingDates, error) {
	out := domain.WeighingDates{LumpWeighingDates: []string{}}
	dateRows, err := r.pool.Query(ctx, lumpWeighingDatesQuery, tenantID, parkIDs, periodStart, periodEnd,
		sexFiltered, scope.LocationIDs, scope.PartitionLabels)
	if err != nil {
		return domain.WeighingDates{}, err
	}
	defer dateRows.Close()
	for dateRows.Next() {
		var day string
		if err := dateRows.Scan(&day); err != nil {
			return domain.WeighingDates{}, err
		}
		out.LumpWeighingDates = append(out.LumpWeighingDates, day)
	}
	if err := dateRows.Err(); err != nil {
		return domain.WeighingDates{}, err
	}

	// The last day the farm weighed ANYTHING in range, individual or whole-shed. The lump dates above
	// answer "which days can show a shed-average movement"; this answers "when did we last weigh",
	// and the Weights page needs both -- it opens on the last two lump dates, and closing that window
	// on the later of them dropped the 199 kids scanned on a day that had no whole-shed weigh of its
	// own. Scanned through a POINTER: a range with no weighs has no date, and must come back empty
	// rather than fabricating one for the page to land on.
	//
	// projection-review: membership=live weighs of either grain inside tenant+park+range; group_key=
	// NONE, this is a single scalar max over that set; join_cardinality=campaign_sheds 1 per
	// observation (PK) and campaigns 1 per shed (PK), and max() is insensitive to duplication in any
	// case; pagination=NONE; scope=tenant_id + park_id = ANY, plus the same sex predicates the rows
	// above use, so the date can never advertise a day this reader has no data for.
	var latestWeighed *string
	if err := r.pool.QueryRow(ctx, latestWeighingDateQuery, tenantID, parkIDs, periodStart, periodEnd,
		sexFiltered, scope.LocationIDs, scope.PartitionLabels, scope.Tags).Scan(&latestWeighed); err != nil {
		return domain.WeighingDates{}, err
	}
	if latestWeighed != nil {
		out.LatestWeighingDate = *latestWeighed
	}
	return out, nil
}
