package postgres

import platformpostgres "github.com/vgoats/goatos/backend/internal/platform/postgres"

// ConnWarmups lists the growth-director FCR reads warmed on every new pooled connection (see
// platformpostgres.ConnWarmer). The call sites bind these constants through sqlbind and run the
// identical text through the pgx statement cache, so warming the constant prepares the same entry.
func ConnWarmups() []platformpostgres.ConnWarmup {
	cached := func(name, sql string) platformpostgres.ConnWarmup {
		return platformpostgres.ConnWarmup{Name: name, SQL: sql, Mode: platformpostgres.WarmCachedStatement, Args: platformpostgres.TenantFirst()}
	}
	return []platformpostgres.ConnWarmup{
		cached("growth.fcr.pens", fcrPensSQL),
		cached("growth.fcr.segments", fcrSegmentsSQL),
		cached("growth.fcr.sale_prices", salePricesSQL),
		cached("growth.fcr.rollup_dirty_parks", fcrRollupDirtyParksSQL),
	}
}
