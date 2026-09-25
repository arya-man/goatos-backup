package postgres

import platformpostgres "github.com/vgoats/goatos/backend/internal/platform/postgres"

// ConnWarmups lists the feed-analytics stock reads warmed on every new pooled connection (see
// platformpostgres.ConnWarmer). /feed-analytics/stock runs these seven statements through the pgx
// statement cache (plan_cache_mode auto); on a fresh connection their parse/plan cost put the route
// over 200 ms while a warm call is ~5 ms.
func ConnWarmups() []platformpostgres.ConnWarmup {
	cached := func(name, sql string) platformpostgres.ConnWarmup {
		return platformpostgres.ConnWarmup{Name: name, SQL: sql, Mode: platformpostgres.WarmCachedStatement, Args: platformpostgres.TenantFirst()}
	}
	return []platformpostgres.ConnWarmup{
		cached("feed.stock.revision", stockRevisionSQL),
		cached("feed.stock.items", stockItemsSQL),
		cached("feed.stock.farm_items", stockFarmItemsSQL),
		cached("feed.stock.forecast", stockForecastSQL),
		cached("feed.stock.expenditure", stockExpenditureSQL),
		cached("feed.stock.item_expenditure", stockItemExpenditureSQL),
		cached("feed.stock.spend", stockSpendSQL),
	}
}
