package postgres

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"testing"
	"time"
)

func TestCalendarConfigUsesDBAndInvalidatesRevisions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	const tenant = "00000000-0000-4000-8000-000000000001"
	repo := NewRepository(pool, 5*time.Second)
	rules, rev, err := repo.loadWeighingWeightsPages(ctx, pool, tenant)
	if err != nil || rules == nil || rules.EarliestDate != "2026-07-05" || rules.DefaultFromDate != "2026-08-03" {
		t.Fatalf("defaults: %+v %v", rules, err)
	}
	before, err := repo.LoadContractFamilyRevisions(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE weighing_calendar_config SET default_from_mode='rolling_weeks',default_from_weeks=6 WHERE tenant_id=$1::uuid`, tenant); err != nil {
		t.Fatal(err)
	}
	after, err := repo.LoadContractFamilyRevisions(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if before["admin-ui:weighing-calendar"] == after["admin-ui:weighing-calendar"] {
		t.Fatal("SQL edit did not invalidate bootstrap revision")
	}
	// An unrelated SOP edit cannot override the dedicated database configuration.
	if _, err := pool.Exec(ctx, `UPDATE sop_versions v SET form_dsl=jsonb_set(form_dsl,'{weighing,weights_pages,default_from_date}','"2026-08-20"'::jsonb) FROM sop_definitions d WHERE v.tenant_id=d.tenant_id AND v.sop_id=d.sop_id AND d.code='weighing.session' AND v.tenant_id=$1::uuid AND v.status='published'`, tenant); err != nil {
		t.Fatal(err)
	}
	rules, newRev, err := repo.loadWeighingWeightsPages(ctx, pool, tenant)
	if err != nil || rules.DefaultFromMode != "rolling_weeks" || rules.DefaultFromWeeks != 6 || newRev == rev {
		t.Fatalf("DB authority: %+v %v", rules, err)
	}
	var version int
	if err := pool.QueryRow(ctx, `SELECT row_version FROM weighing_calendar_config WHERE tenant_id=$1::uuid`, tenant).Scan(&version); err != nil || version != 2 {
		t.Fatalf("row version %d %v", version, err)
	}
	for _, sql := range []string{
		`UPDATE weighing_calendar_config SET default_from_weeks=0`,
		`UPDATE weighing_calendar_config SET default_from_weeks=521`,
		`UPDATE weighing_calendar_config SET default_from_weeks=NULL`,
		`UPDATE weighing_calendar_config SET default_from_mode='unknown'`,
		`UPDATE weighing_calendar_config SET default_from_mode='fixed_date',default_from_date='2026-07-04'`,
		`UPDATE weighing_calendar_config SET earliest_date='infinity'`,
		`UPDATE weighing_calendar_config SET earliest_date='10000-01-01'`,
	} {
		if _, err := pool.Exec(ctx, sql+` WHERE tenant_id='00000000-0000-4000-8000-000000000001'`); err == nil {
			t.Fatalf("accepted invalid config: %s", sql)
		}
	}
}
