package postgres

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// OversightAnalytics computes the CEO/PC-Director oversight aggregate in a small, FIXED number of
// bounded, tenant-scoped aggregate queries over verification_items / verification_review_events --
// never a per-verifier or per-module fan-out loop (see docs/decisions/scale-anti-patterns.md ->
// "N+1 fan-out"). Every query filters on tenant_id first and groups by a small-cardinality column
// (status, module, verifier), so each is a bounded aggregate regardless of table size.
func (r *Repository) OversightAnalytics(ctx context.Context, tenantID string) (domain.OversightAnalytics, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var out domain.OversightAnalytics

	// 1) Videos waiting + oldest pending age.
	var videosWaiting int
	var oldestPendingHours *float64
	if err := r.pool.QueryRow(ctx, `
SELECT count(*),
       max(EXTRACT(EPOCH FROM (now() - captured_at)) / 3600.0)
FROM verification_items
WHERE tenant_id = $1::uuid AND status = 'pending'`, tenantID).Scan(&videosWaiting, &oldestPendingHours); err != nil {
		return out, err
	}
	out.KPIs.VideosWaiting = videosWaiting
	out.KPIs.OldestPendingAgeHours = oldestPendingHours

	// 2) Verdict throughput: verdicts + distinct active days in the last 7 days.
	var verdicts7d, activeDays7d int
	if err := r.pool.QueryRow(ctx, `
SELECT count(*), count(DISTINCT date_trunc('day', verified_at AT TIME ZONE 'Asia/Kolkata'))
FROM verification_items
WHERE tenant_id = $1::uuid AND verified_at IS NOT NULL
  AND verified_at >= now() - interval '7 days'`, tenantID).Scan(&verdicts7d, &activeDays7d); err != nil {
		return out, err
	}
	if activeDays7d > 0 {
		out.KPIs.VerdictsPerActiveDayLast7d = float64(verdicts7d) / float64(activeDays7d)
	}
	if out.KPIs.VerdictsPerActiveDayLast7d > 0 {
		days := float64(videosWaiting) / out.KPIs.VerdictsPerActiveDayLast7d
		out.KPIs.EstDaysToClearBacklog = &days
	}

	// 3) Per-module median review latency (verdicts in the last 30 days).
	moduleRows, err := r.pool.Query(ctx, `
SELECT module,
       percentile_cont(0.5) WITHIN GROUP (
         ORDER BY EXTRACT(EPOCH FROM (verified_at - captured_at)) / 3600.0
       ) AS median_hours
FROM verification_items
WHERE tenant_id = $1::uuid AND verified_at IS NOT NULL
  AND verified_at >= now() - interval '30 days'
GROUP BY module
ORDER BY module`, tenantID)
	if err != nil {
		return out, err
	}
	for moduleRows.Next() {
		var ml domain.ModuleLatency
		if err := moduleRows.Scan(&ml.Module, &ml.MedianHours); err != nil {
			moduleRows.Close()
			return out, err
		}
		out.KPIs.PerModuleMedianReviewLatencyHours = append(out.KPIs.PerModuleMedianReviewLatencyHours, ml)
	}
	if err := moduleRows.Err(); err != nil {
		return out, err
	}
	moduleRows.Close()

	// 4) Reject rate over the last 30 days.
	var approved30d, rejected30d int
	if err := r.pool.QueryRow(ctx, `
SELECT
  count(*) FILTER (WHERE status = 'approved'),
  count(*) FILTER (WHERE status = 'rejected')
FROM verification_items
WHERE tenant_id = $1::uuid AND verified_at IS NOT NULL
  AND verified_at >= now() - interval '30 days'`, tenantID).Scan(&approved30d, &rejected30d); err != nil {
		return out, err
	}
	if total := approved30d + rejected30d; total > 0 {
		rate := float64(rejected30d) / float64(total)
		out.KPIs.RejectRateLast30d = &rate
	}

	// 5) Pending backlog by module.
	backlogRows, err := r.pool.Query(ctx, `
SELECT module, count(*)
FROM verification_items
WHERE tenant_id = $1::uuid AND status = 'pending'
GROUP BY module
ORDER BY module`, tenantID)
	if err != nil {
		return out, err
	}
	for backlogRows.Next() {
		var b domain.ModulePendingBacklog
		if err := backlogRows.Scan(&b.Module, &b.Count); err != nil {
			backlogRows.Close()
			return out, err
		}
		out.PendingByModule = append(out.PendingByModule, b)
	}
	if err := backlogRows.Err(); err != nil {
		return out, err
	}
	backlogRows.Close()

	// 6) Per-verifier last-14-day activity: verdicts/approved/rejected/busiest day, ONE grouped
	// query keyed by (verified_by, verified_by_name) -- never a per-verifier loop.
	activityRows, err := r.pool.Query(ctx, `
-- projection-review: membership=all verdict-completed verification_items for the tenant in the last 14 days, joined to workforce_members for display names (1:0 LEFT JOIN); group_key=verified_by; join_cardinality=1:1 on verified_by (LEFT JOIN + pre-aggregated busiest per verifier); pagination=one row per verifier; scope=tenant_id + 14-day window.
WITH decided AS (
  SELECT vi.verified_by,
         wm.display_name AS verified_by_name,
         date_trunc('day', vi.verified_at AT TIME ZONE 'Asia/Kolkata') AS decided_day,
         vi.status
  FROM verification_items vi
  LEFT JOIN workforce_members wm
    ON wm.tenant_id = vi.tenant_id AND wm.user_id = vi.verified_by
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
		return out, err
	}
	activityByVerifier := map[string]*domain.VerifierActivity{}
	var verifierOrder []string
	for activityRows.Next() {
		var a domain.VerifierActivity
		var name *string
		var busiestDay *time.Time
		if err := activityRows.Scan(&a.VerifierID, &name, &a.Verdicts, &a.Approved, &a.Rejected, &busiestDay); err != nil {
			activityRows.Close()
			return out, err
		}
		if name != nil {
			a.VerifierName = *name
		}
		if busiestDay != nil {
			a.BusiestDay = busiestDay.Format("2006-01-02")
		}
		verifierOrder = append(verifierOrder, a.VerifierID)
		activityByVerifier[a.VerifierID] = &a
	}
	if err := activityRows.Err(); err != nil {
		return out, err
	}
	activityRows.Close()

	// 6b) Watch-integrity aggregate per verifier, over items THOSE verifiers decided in the same
	// 14-day window: items tracked (has any review-event telemetry), watched-to-end count
	// (WatchedFullThreshold-equivalent 90%+ position/duration), verdict-without-play count (a
	// verdict_recorded event with no preceding video_play for that item/actor). ONE grouped query,
	// bounded to items decided in the window via a join on verification_items, never a per-item or
	// per-verifier loop.
	if len(verifierOrder) > 0 {
		integrityRows, err := r.pool.Query(ctx, `
-- projection-review: membership=verification_items with verification_review_events bounded to (item, actor=verifier) pairs in last 14 days; group_key=verified_by; join_cardinality=1:N on (item, verifier) but LATERAL aggregates to 1 row per (verified_by, item_id); pagination=one row per verifier; scope=tenant_id + 14-day window.
WITH decided_items AS (
  SELECT item_id, verified_by
  FROM verification_items
  WHERE tenant_id = $1::uuid AND verified_by IS NOT NULL AND verified_at IS NOT NULL
    AND verified_at >= now() - interval '14 days'
),
per_item AS (
  SELECT di.verified_by,
         di.item_id,
         bool_or(e.event_type = 'item_opened') AS opened,
         bool_or(e.event_type = 'video_play') AS played,
         max((e.payload->>'video_position_ms')::bigint) AS max_position_ms,
         max((e.payload->>'video_duration_ms')::bigint) AS max_duration_ms
  FROM decided_items di
  LEFT JOIN verification_review_events e
    ON e.tenant_id = $1::uuid AND e.item_id = di.item_id AND e.actor_id = di.verified_by
  GROUP BY di.verified_by, di.item_id
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
			return out, err
		}
		for integrityRows.Next() {
			var verifierID string
			var itemsTracked, watchedToEnd, verdictWithoutPlay int
			if err := integrityRows.Scan(&verifierID, &itemsTracked, &watchedToEnd, &verdictWithoutPlay); err != nil {
				integrityRows.Close()
				return out, err
			}
			if a, ok := activityByVerifier[verifierID]; ok {
				a.ItemsTracked = itemsTracked
				a.WatchedToEndCount = watchedToEnd
				a.VerdictWithoutPlay = verdictWithoutPlay
			}
		}
		if err := integrityRows.Err(); err != nil {
			return out, err
		}
		integrityRows.Close()
	}

	for _, id := range verifierOrder {
		out.VerifierActivity = append(out.VerifierActivity, *activityByVerifier[id])
	}

	return out, nil
}
