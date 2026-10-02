package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestSalesExecutiveAnalyticsPageContract pins the Sales executive analytics page (maintainer
// request 2026-10-02): its own route under Sales, read-only by contract (no control at all),
// gated on the sales permission its data route requires, and every copy key the renderer reads.
func TestSalesExecutiveAnalyticsPageContract(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	page := pageByRouteID(t, resp.Pages, "sales-executive-analytics")
	if page.Href != "/sales/executive-analytics" || page.PathPattern != "/sales/executive-analytics" || page.SurfaceKind != "module-surface" {
		t.Fatalf("page = %q/%q/%q", page.Href, page.PathPattern, page.SurfaceKind)
	}
	if len(page.Controls) != 0 {
		t.Fatalf("a read-only analytics page must declare no control: %+v", page.Controls)
	}
	if got := permissionsForNav("sales-executive-analytics"); !reflect.DeepEqual(got, []string{permissions.SalesRead}) {
		t.Fatalf("nav gate = %v, want SalesRead (the data route's permission)", got)
	}
	for _, key := range []string{
		"crumb", "filter.period", "filter.period.days", "section.headline.aria",
		"kpi.vendors_added", "kpi.vendors_edited", "kpi.calls", "kpi.calls.detail", "kpi.sales",
		"kpi.sales.detail", "kpi.vs_previous", "kpi.vendors.register", "hint.edits",
		"section.trend.title", "section.trend.subtitle", "section.trend.title.week", "section.trend.subtitle.week", "value.week_to", "series.vendors_added", "series.vendors_edited",
		"series.calls", "series.sales", "empty.trend",
		"section.people.title", "section.people.subtitle", "column.person", "column.vendors_added",
		"column.vendors_edited", "column.calls", "column.sales", "column.payments", "column.active_days", "column.status_changes", "pager.page", "pager.of", "pager.activities", "pager.vendors", "action.prev_page", "action.next_page",
		"column.last_active", "empty.people",
		"section.vendors.title", "section.vendors.subtitle", "column.vendor", "column.added_by",
		"column.added_on", "value.imported", "empty.vendors",
		"section.recent.title", "section.recent.subtitle", "activity.vendor_added", "activity.vendor_edited",
		"activity.market_call", "activity.lead_call", "activity.sale_recorded", "activity.payment_recorded",
		"activity.deal_status", "activity.unnamed", "value.prices", "value.price", "value.animals",
		"value.animal", "empty.recent", "value.unknown_person", "error.load",
	} {
		if strings.TrimSpace(page.Copy[key]) == "" {
			t.Fatalf("copy key %q missing", key)
		}
	}
	for key, value := range page.Copy {
		if strings.Contains(strings.ToLower(value), "shed") {
			t.Fatalf("copy %q says shed: %q", key, value)
		}
	}
}
