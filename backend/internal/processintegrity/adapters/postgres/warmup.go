package postgres

import platformpostgres "github.com/vgoats/goatos/backend/internal/platform/postgres"

// ConnWarmups lists the process-integrity (Action Center board) reads warmed on every new pooled
// connection (see platformpostgres.ConnWarmer). They pin a custom plan per call
// (QueryExecModeExec), so nothing is cached: the warm-up only loads the catalog caches the ~27 ms
// custom planning needs, which on a fresh backend otherwise land on the first request.
func ConnWarmups() []platformpostgres.ConnWarmup {
	return []platformpostgres.ConnWarmup{
		{Name: "processintegrity.rows_and_counts", SQL: processIntegrityCanonicalRowsAndCountsSQL, Mode: platformpostgres.WarmCustomPlan, Args: platformpostgres.TenantFirst()},
		{Name: "processintegrity.rows_and_summary", SQL: processIntegrityCanonicalRowsAndSummarySQL, Mode: platformpostgres.WarmCustomPlan, Args: platformpostgres.TenantFirst()},
	}
}
