package postgres

import platformpostgres "github.com/vgoats/goatos/backend/internal/platform/postgres"

// ConnWarmups lists the vaccination execution / command-board reads warmed on every new pooled
// connection (see platformpostgres.ConnWarmer). Modes mirror the call sites: the execution list and
// the command-board batch statements use the statement cache (plan_cache_mode auto); the shed-dose
// matrix and card summaries pin a custom plan per call, so warming them only loads catalog caches.
func ConnWarmups() []platformpostgres.ConnWarmup {
	cached := func(name, sql string) platformpostgres.ConnWarmup {
		return platformpostgres.ConnWarmup{Name: name, SQL: sql, Mode: platformpostgres.WarmCachedStatement, Args: platformpostgres.TenantFirst()}
	}
	custom := func(name, sql string) platformpostgres.ConnWarmup {
		return platformpostgres.ConnWarmup{Name: name, SQL: sql, Mode: platformpostgres.WarmCustomPlan, Args: platformpostgres.TenantFirst()}
	}
	return []platformpostgres.ConnWarmup{
		cached("vaccination.execution_list", vaccinationExecutionSQL),
		custom("vaccination.card_summaries", cardSummariesSQL),
		cached("vaccination.command.kpi", commandBoardKPISQL),
		cached("vaccination.command.drive_options", driveOptionsSQL),
		cached("vaccination.command.shed_vaccine", commandBoardShedVaccineSQL),
		cached("vaccination.command.weekly", commandBoardWeeklySQL),
		cached("vaccination.command.verify_queue", commandBoardVerifyQueueSQL),
		cached("vaccination.command.cohort", commandBoardCohortSQL),
		cached("vaccination.command.cohort_head", commandBoardCohortHeadSQL),
		custom("vaccination.command.shed_dose_matrix", commandBoardShedDoseSQL),
	}
}
