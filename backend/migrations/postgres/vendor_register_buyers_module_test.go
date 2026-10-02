package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func migrationDownSection(sqlText string) string {
	i := strings.Index(sqlText, "-- +goose Down")
	if i < 0 {
		return ""
	}
	return sqlText[i+len("-- +goose Down"):]
}

// TestBuyersModuleBackfillKeepsEveryoneOnTheScreensTheyHad pins 000465 (People / HRMS fixes,
// 2026-10-02): the buyers half of the vendor register moved onto its own module, and nobody may
// gain or lose a screen in the move.
func TestBuyersModuleBackfillKeepsEveryoneOnTheScreensTheyHad(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(n int) string { return fmt.Sprintf("04650000-0000-4000-8000-%012d", n) }
	tenant := id(1)
	exec(`INSERT INTO tenants (tenant_id,name,status) VALUES ($1,'Buyers test','active')`, tenant)
	person := func(n int) {
		exec(`INSERT INTO workforce_members (tenant_id,workforce_member_id,display_code,display_name,status,primary_role_hint) VALUES ($1,$2,$3,$3,'active','other')`, tenant, id(n), fmt.Sprint("p", n))
	}
	row := func(n int, surface, module string, caps, pages []string) {
		exec(`INSERT INTO person_module_access (tenant_id,workforce_member_id,surface,module_key,capabilities,pages) VALUES ($1,$2,$3,$4,$5,$6)`, tenant, id(n), surface, module, caps, pages)
	}
	// 10: CEO-like -- Vendors oversee + Sales (all pages) on both surfaces.
	person(10)
	row(10, "web", "vendors", []string{"view", "do", "oversee"}, []string{})
	row(10, "web", "sales", []string{"view", "do"}, []string{})
	row(10, "mobile", "vendors", []string{"view", "do"}, []string{})
	row(10, "mobile", "sales", []string{"view"}, []string{})
	// 20: Mohsin-like -- Vendors on the web, Sales narrowed to Sales > Vendors only.
	person(20)
	row(20, "web", "vendors", []string{"view"}, []string{"procurement-vendors"})
	row(20, "web", "sales", []string{"view"}, []string{"sales-vendors"})
	// 30: suppliers only -- Vendors, no Sales anywhere: must NOT gain the buyers.
	person(30)
	row(30, "web", "vendors", []string{"view", "do"}, []string{})
	row(30, "mobile", "vendors", []string{"view"}, []string{})
	// 40: Sales with Sales > Vendors unticked: keeps Sales, does not gain the buyers on the web.
	person(40)
	row(40, "web", "vendors", []string{"view"}, []string{})
	row(40, "web", "sales", []string{"view"}, []string{"sales-sold"})

	_, sql := onlyMigrationWithSuffix(t, "vendor_register_buyers_module")
	for _, tbl := range []string{"person_module_access_vendors_sales_backfill", "person_module_access_sales_vendors_page_removed",
		"designation_module_defaults_vendors_sales_backfill", "designation_module_defaults_sales_vendors_page_removed"} {
		exec(`DROP TABLE IF EXISTS public.` + tbl)
	}
	exec(migrationUp(sql))

	caps := func(n int, surface, module string) string {
		t.Helper()
		var c []string
		err := pool.QueryRow(ctx, `SELECT capabilities FROM person_module_access WHERE tenant_id=$1 AND workforce_member_id=$2 AND surface=$3 AND module_key=$4`, tenant, id(n), surface, module).Scan(&c)
		if err != nil {
			return "-"
		}
		return strings.Join(c, ",")
	}
	pages := func(n int, surface, module string) string {
		var p []string
		if err := pool.QueryRow(ctx, `SELECT pages FROM person_module_access WHERE tenant_id=$1 AND workforce_member_id=$2 AND surface=$3 AND module_key=$4`, tenant, id(n), surface, module).Scan(&p); err != nil {
			return "-"
		}
		return strings.Join(p, ",")
	}
	for _, c := range []struct {
		n                     int
		surface, module, want string
	}{
		{10, "web", "vendors_sales", "view,do,oversee"},
		{10, "mobile", "vendors_sales", "view,do"},
		{20, "web", "vendors_sales", "view"},
		{20, "web", "sales", "-"}, // existed only to reach the buyers: retired
		{30, "web", "vendors_sales", "-"},
		{30, "mobile", "vendors_sales", "-"},
		{40, "web", "vendors_sales", "-"},
		{40, "web", "sales", "view"},
	} {
		if got := caps(c.n, c.surface, c.module); got != c.want {
			t.Errorf("person %d %s %s = %q, want %q", c.n, c.surface, c.module, got, c.want)
		}
	}
	if got := pages(40, "web", "sales"); got != "sales-sold" {
		t.Errorf("person 40 sales pages = %q, want sales-sold", got)
	}

	// Down restores exactly what was there.
	exec(migrationDownSection(sql))
	if got := pages(20, "web", "sales"); got != "sales-vendors" {
		t.Errorf("after Down, person 20 sales pages = %q, want sales-vendors", got)
	}
	if got := caps(10, "web", "vendors_sales"); got != "-" {
		t.Errorf("after Down, person 10 still holds vendors_sales %q", got)
	}
}
