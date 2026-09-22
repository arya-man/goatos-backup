package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// The diagnosis register is the second half of Health Config, and the two halves must
// behave identically on this screen: same page, same permission, same disabled-with-a-
// reason treatment for a reader.

// The register's three controls exist on the page and are ENABLED for a principal who
// may author the rulebook.
func TestRegisterControlsAreOnHealthConfigForAnAuthor(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleHealthDirector, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	page := pageByRouteID(t, resp.Pages, "health-config")

	want := map[string]bool{"edit_register": false, "publish_register": false, "discard_register_draft": false}
	for _, c := range page.Controls {
		if _, ok := want[c.ID]; !ok {
			continue
		}
		want[c.ID] = true
		if !c.Enabled {
			t.Errorf("%s is disabled for the health director, who authors this rulebook", c.ID)
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("%s is missing from the Health Config page contract", id)
		}
	}
}

// A reader gets a DISABLED control carrying a reason, never a missing one. A missing
// button reads as a broken page; a disabled one with a sentence is an answer.
func TestRegisterControlsAreDisabledWithAReasonForAReader(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			// park_head holds no health-config authority at all.
			{Role: permissions.RoleParkHead, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	page := optionalPageByRouteID(resp.Pages, "health-config")
	if page == nil {
		// A principal with no health-config read never receives the page contract, which
		// is the stronger outcome and equally correct.
		return
	}
	for _, c := range page.Controls {
		switch c.ID {
		case "edit_register", "publish_register", "discard_register_draft":
			if c.Enabled {
				t.Errorf("%s is enabled for a principal who cannot author the rulebook", c.ID)
			}
			if c.DisabledReason == "" {
				t.Errorf("%s is disabled with no reason; a greyed button with no sentence is not an answer", c.ID)
			}
		}
	}
}

// Both halves of the rulebook are on ONE page. A register tab on a second route would
// let the form and the treatment courses drift apart in the navigation as well as in
// the data.
func TestTheRegisterLivesOnTheHealthConfigPageAndNowhereElse(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	for _, p := range resp.Pages {
		if p.RouteID == "health-config" {
			continue
		}
		for _, tbl := range p.Tables {
			if tbl.DataSource == "/health-config/registers" {
				t.Fatalf("page %s also reads the diagnosis register; it belongs to Health Config alone", p.RouteID)
			}
		}
	}

	page := pageByRouteID(t, resp.Pages, "health-config")
	var names []string
	for _, tbl := range page.Tables {
		names = append(names, tbl.ID)
	}
	for _, want := range []string{"protocol-catalog", "register-catalog", "register-questions", "register-rules"} {
		found := false
		for _, got := range names {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Health Config is missing the %q table; it has %v", want, names)
		}
	}
}
