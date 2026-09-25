package postgres

import platformpostgres "github.com/vgoats/goatos/backend/internal/platform/postgres"

// ConnWarmups lists the calendar reads worth warming on every new pooled connection (see
// platformpostgres.ConnWarmer). Modes mirror the call sites: the canonical list and the single-event
// detail / exists statements run under WithGenericPlanReadTx, history uses the statement cache, and
// the drive-targets list pins a custom plan (QueryExecModeDescribeExec).
//
// Single-event statements are warmed for the drive variant (parkdrive: / vaccinationdrive:assignment:
// ids -- every /calendar/vaccination/events/{id}[/targets|/history] drive route) and the obligation
// variant; the completion / calendar variants are rare and plan on first use.
func ConnWarmups() []platformpostgres.ConnWarmup {
	return []platformpostgres.ConnWarmup{
		{Name: "calendar.list", SQL: calendarCanonicalListSQL, Mode: platformpostgres.WarmGenericPlan, Begin: calendarListTx.BeginQuery, Args: platformpostgres.TenantFirst()},
		{Name: "calendar.detail.drive", SQL: calendarCanonicalDetailSQL["drive"], Mode: platformpostgres.WarmGenericPlan, Args: platformpostgres.TenantFirst()},
		{Name: "calendar.exists.drive", SQL: calendarCanonicalExistsSQL["drive"], Mode: platformpostgres.WarmGenericPlan, Args: platformpostgres.TenantFirst()},
		{Name: "calendar.detail.obligation", SQL: calendarCanonicalDetailSQL["obligation"], Mode: platformpostgres.WarmGenericPlan, Args: platformpostgres.TenantFirst()},
		{Name: "calendar.exists.obligation", SQL: calendarCanonicalExistsSQL["obligation"], Mode: platformpostgres.WarmGenericPlan, Args: platformpostgres.TenantFirst()},
		{Name: "calendar.history", SQL: calendarHistorySQL, Mode: platformpostgres.WarmCachedStatement, Args: platformpostgres.TenantFirst()},
		{Name: "calendar.drive_targets", SQL: calendarDriveTargetsSQL, Mode: platformpostgres.WarmCustomPlan,
			Args: map[int]any{0: platformpostgres.WarmupTenantID, 5: platformpostgres.WarmupTenantID}},
	}
}
