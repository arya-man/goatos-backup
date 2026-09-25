package postgres

import platformpostgres "github.com/vgoats/goatos/backend/internal/platform/postgres"

// ConnWarmups lists the weighing reads warmed on every new pooled connection (see
// platformpostgres.ConnWarmer): the per-park growth gains read, whose growthPairsCTE every
// growth-director read shares, so its relations' catalog caches are warm for all of them. The
// weight-demographics statement is rendered per section set and bucket and is not warmed.
func ConnWarmups() []platformpostgres.ConnWarmup {
	return []platformpostgres.ConnWarmup{
		{Name: "weighing.growth_park_gains", SQL: growthParkGainsQuery, Mode: platformpostgres.WarmCachedStatement, Args: platformpostgres.TenantFirst()},
	}
}
