package postgres

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// OversightAnalytics computes the CEO/PC-Director oversight aggregate in a small, FIXED number of
// bounded, tenant-scoped aggregate queries (six, run up to four at a time -- they are independent
// reads, so the cold request costs the slowest one rather than their sum) over verification_items / verification_review_events --
// never a per-verifier or per-module fan-out loop (see docs/decisions/scale-anti-patterns.md ->
// "N+1 fan-out"). Every query filters on tenant_id first and groups by a small-cardinality column
// (status, module, verifier), so each is a bounded aggregate regardless of table size.
func (r *Repository) OversightAnalytics(ctx context.Context, tenantID string) (domain.OversightAnalytics, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cacheKey := "oversight-analytics:" + tenantID
	if cached, ok := r.getCachedValue(cacheKey); ok {
		if result, ok := cached.(domain.OversightAnalytics); ok {
			return result, nil
		}
	}
	cacheEpoch := r.readCacheEpoch()

	var (
		out       domain.OversightAnalytics
		pending   []pendingModuleAgg
		verdicts  verdictWindowAgg
		volume    []domain.DailyVerificationVolume
		activity  []domain.VerifierActivity
		integrity []verifierIntegrityAgg
	)
	// The six reads are independent aggregates; the slowest (the watch-integrity join and the
	// latency medians) are queued first so the 4-slot limit never leaves them waiting behind the
	// cheap ones. Four is the same per-request ceiling the vaccination live tracker uses.
	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	group.Go(func() (err error) { integrity, err = r.oversightIntegrity(gctx, tenantID); return err })
	group.Go(func() (err error) {
		out.KPIs.PerModuleMedianReviewLatencyHours, err = r.oversightModuleLatency(gctx, tenantID)
		return err
	})
	group.Go(func() (err error) { pending, err = r.oversightPendingByModule(gctx, tenantID); return err })
	group.Go(func() (err error) { activity, err = r.oversightVerifierActivity(gctx, tenantID); return err })
	group.Go(func() (err error) { verdicts, err = r.oversightVerdictWindows(gctx, tenantID); return err })
	group.Go(func() (err error) { volume, err = r.oversightDailyVolume(gctx, tenantID); return err })
	if err := group.Wait(); err != nil {
		return domain.OversightAnalytics{}, err
	}

	// 1) Videos waiting + oldest pending age + the age SHAPE of that same backlog, and 5) the
	// pending backlog by module: ONE statement, grouped by module. The whole-backlog figures are
	// the sums (and the max) of the per-module rows of that same snapshot, so the four buckets
	// always sum back to videos_waiting and the per-module counts always sum to it too.
	var videosWaiting int
	var oldestPendingHours *float64
	var buckets domain.PendingAgeBuckets
	for _, m := range pending {
		videosWaiting += m.count
		if m.oldestHours != nil && (oldestPendingHours == nil || *m.oldestHours > *oldestPendingHours) {
			v := *m.oldestHours
			oldestPendingHours = &v
		}
		buckets.UpTo1Day += m.age1
		buckets.OneToThreeDays += m.age3
		buckets.ThreeToSevenDays += m.age7
		buckets.OverSevenDays += m.ageOver
		out.PendingByModule = append(out.PendingByModule, domain.ModulePendingBacklog{Module: m.module, Count: m.count})
	}
	out.KPIs.VideosWaiting = videosWaiting
	out.KPIs.OldestPendingAgeHours = oldestPendingHours
	out.PendingAgeBuckets = buckets

	// 2) Verdict throughput: verdicts + distinct active days in the last 7 days.
	if verdicts.activeDays7d > 0 {
		out.KPIs.VerdictsPerActiveDayLast7d = float64(verdicts.verdicts7d) / float64(verdicts.activeDays7d)
	}
	if out.KPIs.VerdictsPerActiveDayLast7d > 0 {
		days := float64(videosWaiting) / out.KPIs.VerdictsPerActiveDayLast7d
		out.KPIs.EstDaysToClearBacklog = &days
	}
	// 4) Reject rate over the last 30 days.
	if total := verdicts.approved30d + verdicts.rejected30d; total > 0 {
		rate := float64(verdicts.rejected30d) / float64(total)
		out.KPIs.RejectRateLast30d = &rate
	}

	out.DailyVolumeLast14d = volume

	// 6b) folds into 6): integrity figures attach only to verifiers the activity read returned.
	byVerifier := make(map[string]int, len(activity))
	for i := range activity {
		byVerifier[activity[i].VerifierID] = i
	}
	for _, in := range integrity {
		if i, ok := byVerifier[in.verifierID]; ok {
			activity[i].ItemsTracked = in.itemsTracked
			activity[i].WatchedToEndCount = in.watchedToEnd
			activity[i].VerdictWithoutPlay = in.verdictWithoutPlay
		}
	}
	out.VerifierActivity = activity

	r.setCachedValueIfEpoch(cacheKey, out, cacheEpoch)
	return out, nil
}

type pendingModuleAgg struct {
	module                    string
	count                     int
	oldestHours               *float64
	age1, age3, age7, ageOver int
}

// oversightPendingByModule is the in-sample pending backlog per module with its oldest age and
// age buckets. The bucket FILTERs are disjoint half-open ranges on the bare captured_at column, so
// each row lands in exactly one bucket and verification_items_queue_idx / the pending indexes still
// drive the read.
func (r *Repository) oversightPendingByModule(ctx context.Context, tenantID string) ([]pendingModuleAgg, error) {
	query, err := sqlbind.Bind(`
SELECT vi.module,
       count(*),
       max(EXTRACT(EPOCH FROM (now() - vi.captured_at)) / 3600.0),
       count(*) FILTER (WHERE vi.captured_at >= now() - interval '1 day'),
       count(*) FILTER (WHERE vi.captured_at <  now() - interval '1 day'  AND vi.captured_at >= now() - interval '3 days'),
       count(*) FILTER (WHERE vi.captured_at <  now() - interval '3 days' AND vi.captured_at >= now() - interval '7 days'),
       count(*) FILTER (WHERE vi.captured_at <  now() - interval '7 days')
FROM verification_items vi
WHERE vi.tenant_id = $1::uuid AND vi.status = 'pending'
  AND `+samplingInSampleSQL()+`
GROUP BY vi.module
ORDER BY vi.module`, tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, query.SQL(), query.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pendingModuleAgg
	for rows.Next() {
		var m pendingModuleAgg
		if err := rows.Scan(&m.module, &m.count, &m.oldestHours, &m.age1, &m.age3, &m.age7, &m.ageOver); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type verdictWindowAgg struct {
	verdicts7d, activeDays7d int
	approved30d, rejected30d int
}

// oversightVerdictWindows reads the 7-day throughput and the 30-day approve/reject split in one
// pass over the 30-day human-verdict window (the 7-day figures are FILTERs of the same rows).
func (r *Repository) oversightVerdictWindows(ctx context.Context, tenantID string) (verdictWindowAgg, error) {
	var v verdictWindowAgg
	err := r.pool.QueryRow(ctx, `
SELECT
  count(*) FILTER (WHERE verified_at >= now() - interval '7 days'),
  count(DISTINCT date_trunc('day', verified_at AT TIME ZONE 'Asia/Kolkata')) FILTER (WHERE verified_at >= now() - interval '7 days'),
  count(*) FILTER (WHERE status = 'approved'),
  count(*) FILTER (WHERE status = 'rejected')
FROM verification_items
WHERE tenant_id = $1::uuid AND verified_at IS NOT NULL
  AND auto_resolution IS NULL
  AND verified_at >= now() - interval '30 days'`, tenantID).Scan(&v.verdicts7d, &v.activeDays7d, &v.approved30d, &v.rejected30d)
	return v, err
}

// oversightModuleLatency is 3) the per-module median review latency (verdicts in the last 30 days).
func (r *Repository) oversightModuleLatency(ctx context.Context, tenantID string) ([]domain.ModuleLatency, error) {
	rows, err := r.pool.Query(ctx, `
SELECT module,
       percentile_cont(0.5) WITHIN GROUP (
         ORDER BY EXTRACT(EPOCH FROM (verified_at - captured_at)) / 3600.0
       ) AS median_hours
FROM verification_items
WHERE tenant_id = $1::uuid AND verified_at IS NOT NULL
  AND auto_resolution IS NULL
  AND verified_at >= now() - interval '30 days'
GROUP BY module
ORDER BY module`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ModuleLatency
	for rows.Next() {
		var ml domain.ModuleLatency
		if err := rows.Scan(&ml.Module, &ml.MedianHours); err != nil {
			return nil, err
		}
		out = append(out, ml)
	}
	return out, rows.Err()
}

// oversightDailyVolume is 5b) the daily flow for the last 14 business days: videos ARRIVED vs verdicts RECORDED.
//
// -- projection-review: membership=verification_items rows for ONE tenant whose captured_at (arrived) or verified_at (verdicts) falls in the trailing 14 Asia/Kolkata days; group_key=the Asia/Kolkata calendar date of that column; join_cardinality=days LEFT JOIN verdicts/arrived on business_date, and each side is already GROUPed to one row per date, so both joins are 1:0..1 and no day can be double-counted; pagination=none, the window is a fixed 14-row series; scope=explicit tenant_id on every branch.
// -- GRAIN PROOF (producer vs consumer, mandatory per AGENTS.md):
// --   producer unique key = verification_items (tenant_id, item_id) UNIQUE.
// --   consumer match key  = business_date, produced by GROUP BY on each branch, so `days` (one
// --                        row per date by generate_series) matches at most one row per side.
// --   row multiplicity    = exactly 14 output rows, one per business day, zero-filled.
// --   key sets compared   = arrived and verdicts range over the SAME 14 dates, which is what
// --                        makes their difference a real trajectory rather than two windows
// --                        subtracted from each other.
//
// An item can appear on BOTH sides (arrived Monday, decided Wednesday) and must: they are two
// different events about the same video, and the whole point is comparing inflow to outflow.
//
// The date predicates are on the BARE timestamp columns (>= a computed instant), never on a
// converted column: `(verified_at AT TIME ZONE ...)::date >= x` would be non-SARGable and give up
// the index (docs/decisions/scale-anti-patterns.md -> "non-SARGable predicate"). The conversion
// happens only in the SELECT/GROUP BY, where it costs nothing at this row count.
func (r *Repository) oversightDailyVolume(ctx context.Context, tenantID string) ([]domain.DailyVerificationVolume, error) {
	rows, err := r.pool.Query(ctx, `
WITH bounds AS (
  SELECT ((now() AT TIME ZONE 'Asia/Kolkata')::date - interval '13 days')::date AS first_day,
         (now() AT TIME ZONE 'Asia/Kolkata')::date AS last_day
),
window_start AS (
  SELECT (first_day::timestamp AT TIME ZONE 'Asia/Kolkata') AS from_instant FROM bounds
),
days AS (
  SELECT generate_series(b.first_day, b.last_day, interval '1 day')::date AS business_date FROM bounds b
),
verdicts AS (
  SELECT (vi.verified_at AT TIME ZONE 'Asia/Kolkata')::date AS business_date, count(*) AS n
  FROM verification_items vi, window_start w
  WHERE vi.tenant_id = $1::uuid AND vi.verified_at IS NOT NULL AND vi.auto_resolution IS NULL
    AND vi.verified_at >= w.from_instant
  GROUP BY 1
),
arrived AS (
  SELECT (vi.captured_at AT TIME ZONE 'Asia/Kolkata')::date AS business_date, count(*) AS n
  FROM verification_items vi, window_start w
  WHERE vi.tenant_id = $1::uuid AND vi.captured_at >= w.from_instant
  GROUP BY 1
)
SELECT d.business_date, coalesce(v.n, 0), coalesce(a.n, 0)
FROM days d
LEFT JOIN verdicts v ON v.business_date = d.business_date
LEFT JOIN arrived a ON a.business_date = d.business_date
ORDER BY d.business_date`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DailyVerificationVolume
	for rows.Next() {
		var day time.Time
		var verdicts, arrived int
		if err := rows.Scan(&day, &verdicts, &arrived); err != nil {
			return nil, err
		}
		out = append(out, domain.DailyVerificationVolume{
			BusinessDate: day.Format("2006-01-02"),
			Verdicts:     verdicts,
			Arrived:      arrived,
		})
	}
	return out, rows.Err()
}

// oversightVerifierActivity is 6) per-verifier last-14-day activity: verdicts/approved/rejected/busiest day, ONE grouped
// query keyed by (verified_by, verified_by_name) -- never a per-verifier loop.
func (r *Repository) oversightVerifierActivity(ctx context.Context, tenantID string) ([]domain.VerifierActivity, error) {
	rows, err := r.pool.Query(ctx, `
-- projection-review: membership=verification_items rows for ONE tenant that carry a verdict (verified_by AND verified_at NOT NULL) in the trailing 14 days -- the verdict row itself is the membership source, never reconstructed from workforce/duty tables; group_key=verified_by; join_cardinality=workforce_members is joined ONLY on its active-unique key (tenant_id, user_id) WHERE status='active', which workforce_members_active_user_unique_idx makes at most one row, so the decoration is 1:0..1 and cannot fan a verdict row out; busiest is one row per verified_by (DISTINCT ON), also 1:0..1; pagination=whole 14-day window aggregated in one statement, one output row per verifier, no LIMIT can truncate a verifier's verdicts; scope=explicit tenant_id.
-- GRAIN PROOF (producer vs consumer, mandatory per AGENTS.md):
--   producer unique key   = verification_items (tenant_id, item_id) UNIQUE -- one verdict per item.
--   consumer match key    = verified_by, a column OF that same row; workforce_members is matched on
--                           (tenant_id, user_id) WHERE status='active', its own partial-unique key.
--   row multiplicity      = decided: exactly one row per decided verification_item (the LEFT JOINs
--                           add at most one match each); output: exactly one row per verified_by.
--   cap/ratio key sets    = verdicts, approved and rejected are count(*) / count(*) FILTER over the
--                           SAME decided set of the SAME verifier, so approved+rejected can never
--                           exceed the verdict total that is displayed beside them.
--   unnamed actor         = a verified_by with no active workforce_members row (automation and
--                           backfill writes) keeps its verdict counts and yields a NULL name; it is
--                           labelled in the UI rather than dropped, so the totals stay complete.
WITH decided AS (
  SELECT vi.verified_by,
         wm.display_name AS verified_by_name,
         date_trunc('day', vi.verified_at AT TIME ZONE 'Asia/Kolkata') AS decided_day,
         vi.status
  FROM verification_items vi
  LEFT JOIN workforce_members wm
    ON wm.tenant_id = vi.tenant_id AND wm.user_id = vi.verified_by AND wm.status = 'active'
  WHERE vi.tenant_id = $1::uuid AND vi.verified_by IS NOT NULL AND vi.verified_at IS NOT NULL
    AND vi.verified_at >= now() - interval '14 days'
),
by_day AS (
  SELECT verified_by, decided_day, count(*) AS day_count
  FROM decided
  GROUP BY verified_by, decided_day
),
busiest AS (
  SELECT DISTINCT ON (verified_by) verified_by, decided_day
  FROM by_day
  ORDER BY verified_by, day_count DESC, decided_day DESC
)
SELECT d.verified_by, max(d.verified_by_name),
       count(*),
       count(*) FILTER (WHERE d.status = 'approved'),
       count(*) FILTER (WHERE d.status = 'rejected'),
       max(b.decided_day)
FROM decided d
LEFT JOIN busiest b ON b.verified_by = d.verified_by
GROUP BY d.verified_by
ORDER BY d.verified_by`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.VerifierActivity
	for rows.Next() {
		var a domain.VerifierActivity
		var name *string
		var busiestDay *time.Time
		if err := rows.Scan(&a.VerifierID, &name, &a.Verdicts, &a.Approved, &a.Rejected, &busiestDay); err != nil {
			return nil, err
		}
		if name != nil {
			a.VerifierName = *name
		}
		if busiestDay != nil {
			a.BusiestDay = busiestDay.Format("2006-01-02")
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type verifierIntegrityAgg struct {
	verifierID                                     string
	itemsTracked, watchedToEnd, verdictWithoutPlay int
}

// oversightIntegrity is 6b) the watch-integrity aggregate per verifier, over items THOSE verifiers decided in the same
// 14-day window: items tracked (has any review-event telemetry), watched-to-end count
// (WatchedFullThreshold-equivalent 90%+ position/duration), verdict-without-play count (a
// verdict_recorded event with no preceding video_play for that item/actor). ONE grouped query,
// bounded to items decided in the window via a join on verification_items, never a per-item or
// per-verifier loop.
//
// It runs concurrently with oversightVerifierActivity rather than after it: its decided set is the
// same (tenant, verified_by and verified_at NOT NULL, last 14 days) set, so when that read finds no
// verifier this one returns no rows either, and its rows are only ever attached to verifiers the
// activity read returned.
func (r *Repository) oversightIntegrity(ctx context.Context, tenantID string) ([]verifierIntegrityAgg, error) {
	rows, err := r.pool.Query(ctx, `
-- projection-review: membership=verification_items with the verification_review_item_watch summary bounded to (item, actor=verifier) pairs in last 14 days; group_key=verified_by; join_cardinality=1:0..1 on the summary's (tenant_id, item_id, actor_id) primary key, so one row per (verified_by, item_id); pagination=one row per verifier; scope=tenant_id + 14-day window.
WITH decided_items AS (
  SELECT item_id, verified_by
  FROM verification_items
  WHERE tenant_id = $1::uuid AND verified_by IS NOT NULL AND verified_at IS NOT NULL
    AND verified_at >= now() - interval '14 days'
),
per_item AS (
  -- verification_review_item_watch (migration 000431) is the stored per-(item, actor) fold of
  -- verification_review_events, maintained in the event write's own transaction: one row per pair
  -- with >= 1 event, none otherwise, so this PK-keyed LEFT JOIN yields exactly what the former
  -- LEFT JOIN + GROUP BY over the raw events did, without re-reading every event per request.
  SELECT di.verified_by,
         di.item_id,
         w.opened,
         w.played,
         w.max_position_ms,
         w.max_duration_ms
  FROM decided_items di
  LEFT JOIN verification_review_item_watch w
    ON w.tenant_id = $1::uuid AND w.item_id = di.item_id AND w.actor_id = di.verified_by
)
SELECT verified_by,
       count(*) FILTER (WHERE opened OR played OR max_duration_ms IS NOT NULL) AS items_tracked,
       count(*) FILTER (
         WHERE max_duration_ms IS NOT NULL AND max_duration_ms > 0
           AND max_position_ms IS NOT NULL
           AND max_position_ms::float8 / max_duration_ms::float8 >= 0.9
       ) AS watched_to_end,
       count(*) FILTER (WHERE NOT played) AS verdict_without_play
FROM per_item
GROUP BY verified_by`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []verifierIntegrityAgg
	for rows.Next() {
		var in verifierIntegrityAgg
		if err := rows.Scan(&in.verifierID, &in.itemsTracked, &in.watchedToEnd, &in.verdictWithoutPlay); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}
