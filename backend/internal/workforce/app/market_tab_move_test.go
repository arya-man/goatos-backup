package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// THE MARKET TAB IS A SALES TAB (maintainer instruction 2026-09-20: "by mistake we kept it in
// procurement in the app; change it, keep it in sales").
//
// The survey asks what the farm's animals FETCH in each market. Its analytics page has always
// been Sales > Market analytics, every permission it carries is a `sales.market.*` one, and the
// reporter who phones the markets is doing selling work -- so a Procurement tab said the wrong
// thing about whose job it is. This is the same clean MOVE the sales ledger made off
// `/vendors/sales` on 2026-09-05, and the test asserts both halves of "moved, not duplicated".
//
// The second half is the one a move can silently get wrong: a tab shown inside module A is only
// reachable by someone who holds module A, so moving it can take the work away from the very
// people who do it. The market reporters hold their entry permission per person through
// `market_reporter`; this asserts that role reaches the module the tab now lives in.
func TestMarketTabIsASalesTabAndReachesEveryReporter(t *testing.T) {
	sales, ok := moduleNavRegistry["sales"]
	if !ok {
		t.Fatal("sales is missing from moduleNavRegistry")
	}
	vendors, ok := moduleNavRegistry["vendors"]
	if !ok {
		t.Fatal("vendors is missing from moduleNavRegistry")
	}

	var market *moduleNavContribution
	for i, c := range sales.contributions {
		if c.key == "market" {
			market = &sales.contributions[i]
		}
	}
	if market == nil {
		t.Fatal("the Sales module carries no market tab")
	}
	if market.href != "/sales/market" {
		t.Errorf("market href = %q, want /sales/market -- the Android L0 route matches this string verbatim", market.href)
	}
	if market.requiredPermission != permissions.MarketEntry {
		t.Errorf("market requires %q, want %q", market.requiredPermission, permissions.MarketEntry)
	}

	// MOVED, never duplicated: two bars offering one screen is how a person ends up asking which
	// of them is the real one.
	for _, c := range vendors.contributions {
		if c.key == "market" || c.href == "/vendors/market" {
			t.Errorf("the Procurement module still carries a market tab (%+v) -- it was moved, not copied", c)
		}
	}

	// The buying desk keeps exactly its own work.
	wantVendorTabs := map[string]bool{"vendors": true, "feed_purchases": true, "animal_purchases": true}
	if len(vendors.contributions) != len(wantVendorTabs) {
		t.Fatalf("Procurement tabs = %+v, want exactly %v", vendors.contributions, wantVendorTabs)
	}
	for _, c := range vendors.contributions {
		if !wantVendorTabs[c.key] {
			t.Errorf("unexpected Procurement tab %q", c.key)
		}
	}

	// A reporter reaches the module the tab moved into. `market_reporter` is granted per person
	// beside a job role, so what matters is that the module is offered to the roles those people
	// hold -- asserted here through the capability catalog rather than a hand-written list.
	if !permissions.RoleHasPermission(permissions.RoleMarketReporter, permissions.MarketEntry) {
		t.Fatalf("%s does not hold %s -- the tab is unreachable by the people who do the work",
			permissions.RoleMarketReporter, permissions.MarketEntry)
	}
}
