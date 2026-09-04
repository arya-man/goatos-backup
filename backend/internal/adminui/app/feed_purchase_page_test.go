package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestFeedPurchasePageContractAndNavigation pins the feed purchase ledger's published contract: the
// nav leaf under Procurement, the breadcrumb, the ledger table shape, and the backend-owned copy
// the client renders verbatim.
func TestFeedPurchasePageContractAndNavigation(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})

	page := pageByRouteID(t, resp.Pages, "feed-purchases")
	if page.Href != "/procurement/feed-purchases" || page.PathPattern != "/procurement/feed-purchases" {
		t.Fatalf("feed purchases href/pattern = %q/%q", page.Href, page.PathPattern)
	}
	if page.SurfaceKind != "module-surface" {
		t.Fatalf("feed purchases must be a module surface, got %q", page.SurfaceKind)
	}

	if len(page.Tables) != 1 || page.Tables[0].ID != "feed-purchases" {
		t.Fatalf("feed purchase tables = %+v", page.Tables)
	}
	ledger := page.Tables[0]
	if ledger.DataSource != "/procurement/feed-purchases" {
		t.Fatalf("ledger table source = %q", ledger.DataSource)
	}
	if ledger.RowClick.Param != "purchase_id" {
		t.Fatalf("ledger row param = %q", ledger.RowClick.Param)
	}
	// The column ORDER is part of the contract: the renderer maps the contract's labels into its
	// header row and writes its body cells in the same order, so a reorder here without a matching
	// reorder there pairs a value with the wrong heading.
	wantColumns := []string{
		"purchase_date", "farm", "feed_item", "batch_no", "quantity_kg",
		"total_cost", "per_kg_cost", "vendor", "payment_status", "payment_balance",
	}
	if len(ledger.Columns) != len(wantColumns) {
		t.Fatalf("ledger columns = %+v", ledger.Columns)
	}
	for i, want := range wantColumns {
		if ledger.Columns[i].Key != want {
			t.Fatalf("ledger column %d = %q want %q", i, ledger.Columns[i].Key, want)
		}
		if ledger.Columns[i].Label == "" {
			t.Fatalf("ledger column %q has no backend-owned label", want)
		}
	}

	// Backend-owned copy: every key the renderer needs must be published, farm language only.
	for _, key := range []string{
		"crumb", "summary.count", "summary.count.one", "summary.quantity", "summary.spend",
		"action.record_feed_purchase.label", "drawer.record_purchase.title", "drawer.detail.title",
		"field.purchase_date", "field.farm", "field.feed_item", "field.batch_no",
		"field.quantity_kg", "field.feed_cost", "field.transport_cost", "field.loading_cost",
		"field.unloading_cost", "field.total_cost", "field.per_kg_cost", "field.vendor",
		"field.payment_released", "field.payment_status",
		"hint.batch_no", "hint.total_cost", "required.hint",
		"value.none", "value.entry_app", "value.entry_sheet",
		"column.entry_source", "column.payment_status", "column.payment_balance",
		"section.payments.title", "payments.paid_so_far", "payments.balance", "payments.empty",
		"payments.column.paid_on", "payments.column.amount", "payments.column.note",
		"field.paid_on", "field.amount_rupees", "field.note", "hint.record_payment",
		"action.record_feed_payment.label", "action.update_payment_status.label",
		"action.payment_recorded", "action.payment_record_failed",
		"action.payment_status_updated", "action.payment_status_update_failed",
		"action.edit_feed_purchase.label", "drawer.edit.title", "hint.edit_identity",
		"action.purchase_updated", "action.purchase_update_failed",
		"action.save", "action.cancel", "action.close",
		"action.purchase_recorded", "action.purchase_record_failed", "action.error_form",
		"filter.farm", "pager.page", "pager.of", "action.next_page", "action.prev_page",
		"empty.purchases", "empty.purchases.unset", "error.load", "error.options", "disabled.write",
	} {
		if page.Copy[key] == "" {
			t.Fatalf("feed purchase copy missing %q", key)
		}
	}

	// Option groups: the farm scope (with the read-only "all" entry) and the payment vocabulary.
	// The FEED list is deliberately absent -- it is live tenant rows served by
	// /procurement/feed-purchase-options, and a constant list of feed labels in contract code is
	// the banned pattern.
	groups := map[string]int{}
	for _, g := range page.OptionGroups {
		groups[g.ID] = len(g.Options)
	}
	if groups["feed_purchase_farms"] != 3 {
		t.Fatalf("feed_purchase_farms options = %d", groups["feed_purchase_farms"])
	}
	if groups["feed_purchase_payment_statuses"] != 2 {
		t.Fatalf("feed_purchase_payment_statuses options = %d", groups["feed_purchase_payment_statuses"])
	}
	for id := range groups {
		if id == "feed_purchase_feed_items" {
			t.Fatal("the feed catalog must not be a contract option group: it is live tenant rows")
		}
	}

	// Nav: the leaf lives in the Procurement group and points at the page.
	foundLeaf := false
	for _, group := range resp.Navigation.Groups {
		if group.ID != "procurement" {
			continue
		}
		for _, leaf := range group.Leaves {
			if leaf.ID == "procurement-feed-purchases" {
				foundLeaf = true
				if leaf.Href != "/procurement/feed-purchases" {
					t.Fatalf("feed purchases leaf href = %q", leaf.Href)
				}
			}
		}
	}
	if !foundLeaf {
		t.Fatal("procurement-feed-purchases leaf missing from the Procurement nav group")
	}

	// Breadcrumb rule.
	foundLabel := false
	for _, rule := range resp.RouteLabels {
		if rule.Pattern == "/procurement/feed-purchases" && rule.Label == "Feed Purchases" && rule.Match == "exact" {
			foundLabel = true
		}
	}
	if !foundLabel {
		t.Fatal("route label for /procurement/feed-purchases missing")
	}
}

// TestRecordFeedPurchaseControlIsCapabilityGated pins the write split: record_feed_purchase is
// ENABLED only for a holder of permissions.FeedPurchaseWrite and is otherwise present-but-disabled
// with a backend reason.
//
// The feed_director row is the mutation test that matters. That role holds FeedPurchaseRead and
// every feed oversight permission there is, so any change that enables the control from a broader
// key -- FeedPurchaseRead, ProcurementRead, or a role string in the component -- turns it red. The
// operator row covers the same leak one permission over.
func TestRecordFeedPurchaseControlIsCapabilityGated(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    string
		enabled bool
	}{
		{"ceo_internal", permissions.RoleCEOInternal, true},
		{"procurement_manager", permissions.RoleProcurementManager, true},
		{"feed_director reads the ledger but does not buy", permissions.RoleFeedDirector, false},
		{"growth_director", permissions.RoleGrowthDirector, false},
		{"operator holds procurement read but no purchase write", permissions.RoleOperator, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: "00000000-0000-4000-8000-000000000001",
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants: []permissions.ActiveGrant{
					{Role: tc.role, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
				},
			})
			pageControls := pageByRouteID(t, resp.Pages, "feed-purchases").Controls
			control := controlByID(t, pageControls, "record_feed_purchase")
			if control.Enabled != tc.enabled {
				t.Fatalf("%s record_feed_purchase.enabled = %v want %v (%#v)", tc.name, control.Enabled, tc.enabled, control)
			}
			if !tc.enabled && control.DisabledReason == "" {
				t.Fatalf("%s: disabled control must carry a backend disabled reason", tc.name)
			}
			if control.Action != "POST /procurement/feed-purchases" {
				t.Fatalf("record_feed_purchase.action = %q want the record-purchase write", control.Action)
			}
			// The payment writes ride the SAME permission split: instalments and the status edit
			// are money writes on the same ledger. The feed_director row is again the mutation
			// test -- enabling either from any broader feed permission turns this red.
			for controlID, action := range map[string]string{
				"record_feed_purchase_payment":        "POST /procurement/feed-purchases/{purchase_id}/payments",
				"edit_feed_purchase":                  "PUT /procurement/feed-purchases/{purchase_id}",
				"update_feed_purchase_payment_status": "PUT /procurement/feed-purchases/{purchase_id}/payment-status",
			} {
				payment := controlByID(t, pageControls, controlID)
				if payment.Enabled != tc.enabled {
					t.Fatalf("%s %s.enabled = %v want %v (%#v)", tc.name, controlID, payment.Enabled, tc.enabled, payment)
				}
				if !tc.enabled && payment.DisabledReason == "" {
					t.Fatalf("%s: disabled %s must carry a backend disabled reason", tc.name, controlID)
				}
				if payment.Action != action {
					t.Fatalf("%s.action = %q want %q", controlID, payment.Action, action)
				}
			}
		})
	}
}

// TestFeedPurchaseNavLeafIsGatedOnItsOwnPermission pins that the ledger leaf's permission is its
// OWN, not ProcurementRead.
//
// The leak this guards: park_head and operator both hold ProcurementRead for the source-entry
// screens they work, so gating the leaf on that permission would put supplier prices and payment
// state in their sidebars -- the exact leak VendorRead exists to avoid. Asserted against
// permissionsForNav rather than a rendered navigation tree because that function is where the leaf
// declares what it requires.
func TestFeedPurchaseNavLeafIsGatedOnItsOwnPermission(t *testing.T) {
	required := permissionsForNav("procurement-feed-purchases")
	if len(required) != 1 || required[0] != permissions.FeedPurchaseRead {
		t.Fatalf("procurement-feed-purchases nav permissions = %v want [%s]", required, permissions.FeedPurchaseRead)
	}
	for _, role := range []string{permissions.RoleParkHead, permissions.RoleOperator} {
		if permissions.RolesAuthorize([]string{role}, required, false) {
			t.Fatalf("%s must not reach the feed purchase ledger leaf", role)
		}
		if !permissions.RolesAuthorize([]string{role}, []string{permissions.ProcurementRead}, false) {
			t.Fatalf("%s is expected to hold ProcurementRead -- otherwise this test proves nothing", role)
		}
	}
	for _, role := range []string{
		permissions.RoleCEOInternal, permissions.RoleProcurementManager, permissions.RoleFeedDirector,
	} {
		if !permissions.RolesAuthorize([]string{role}, required, false) {
			t.Fatalf("%s must reach the feed purchase ledger leaf", role)
		}
	}
}
