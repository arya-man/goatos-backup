package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// warmLandingLookbackDays mirrors admin-web's LATEST_LUMP_LOOKBACK_DAYS: every Weights/ADG page
// load first resolves its landing window through GET /weighing/weighing-dates over exactly this
// lookback ending today, and blocks on it. That is the one key every visit is guaranteed to hit.
const warmLandingLookbackDays = 400

// warmTenantLimit bounds the start-up warm pass: at most this many recently active tenants, each
// warmed serially (tenant-wide + one per park), so warm-up can never flood the pool.
const warmTenantLimit = 20

// warmActiveTenantsSQL lists the tenants whose weighing campaigns changed recently, newest first.
// weighing_campaigns is one row per weekly campaign per park, so the scan is tiny and bounded.
const warmActiveTenantsSQL = `
SELECT tenant_id::text
FROM weighing_campaigns
WHERE updated_at >= now() - make_interval(days => $1::int)
GROUP BY tenant_id
ORDER BY max(updated_at) DESC
LIMIT $2::int`

// WarmLandingReads pre-loads the landing weighing-dates read for recently active tenants into the
// read cache, for the tenant-wide monitor scope (every park, the key a tenant-wide monitor's
// request resolves to) and for each single park. It is started in the background on instance
// start (readcache.Cache.StartWarmup) and never blocks readiness.
func (r *Repository) WarmLandingReads(ctx context.Context) error {
	q := sqlbind.MustBind(warmActiveTenantsSQL, 60, warmTenantLimit)
	rows, err := r.pool.Query(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return err
	}
	var tenants []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		tenants = append(tenants, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	loc := biztime.DefaultLocation()
	todayStart := biztime.BusinessDayStart(time.Now().In(loc))
	periodStart := todayStart.AddDate(0, 0, -(warmLandingLookbackDays - 1))
	periodEnd := todayStart.AddDate(0, 0, 1)
	var errs []error
	for _, tenantID := range tenants { // scale-guard:ignore: bounded start-up warm pass, <= warmTenantLimit tenants, serial by design
		parks, err := r.ListParks(ctx, tenantID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		parkIDs := make([]string, 0, len(parks))
		for _, p := range parks {
			parkIDs = append(parkIDs, p.ParkID)
		}
		if len(parkIDs) == 0 {
			continue
		}
		scopes := [][]string{parkIDs}
		if len(parkIDs) > 1 {
			for _, p := range parkIDs {
				scopes = append(scopes, []string{p})
			}
		}
		for _, scope := range scopes { // scale-guard:ignore: bounded start-up warm pass, one landing read per park, serial by design
			if _, err := r.GetWeighingDates(ctx, tenantID, scope, periodStart, periodEnd, "", "", ""); err != nil {
				errs = append(errs, err)
			}
			if ctx.Err() != nil {
				return errors.Join(append(errs, ctx.Err())...)
			}
		}
	}
	return errors.Join(errs...)
}
