package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// The TOXIN review tab is CEO/CXO-ONLY (maintainer decision 2026-08-25). Both halves of the
// role-scoped-UI lock are pinned elsewhere for the endpoint (TestToxinVerdictIsCEOOnly in
// permissions); this test pins the CONTRACT half: the toxin_tab / toxin_verdict controls on the
// verification-review page follow permissions.ToxinVerdict, so ceo_internal gets them enabled and
// the verifier — who must never see toxin work at all — gets them disabled with a reason, on the
// full-IA contract and on the verifier lens alike.
func TestToxinReviewControlsAreCEOOnly(t *testing.T) {
	controlByID := func(t *testing.T, page domain.PageContract, id string) domain.Control {
		t.Helper()
		for _, control := range page.Controls {
			if control.ID == id {
				return control
			}
		}
		t.Fatalf("missing control %q on page %q", id, page.RouteID)
		return domain.Control{}
	}

	ceoPage := pageByRouteID(t, bootstrapAs(lensService(), permissions.RoleCEOInternal).Pages, verifierLensPageID)
	for _, id := range []string{"toxin_tab", "toxin_verdict"} {
		got := controlByID(t, ceoPage, id)
		if !got.Enabled {
			t.Fatalf("ceo %s = %#v, want enabled — toxin review belongs to the CEO's office", id, got)
		}
		if got.DisabledReason != "" {
			t.Fatalf("ceo %s carries a disabled reason %q on an enabled control", id, got.DisabledReason)
		}
	}

	// The verifier lens serves exactly one page contract; toxin must be disabled there too, so the
	// lens output can never light the tab up for the tenant verifier.
	verifierResp := bootstrapAs(lensService(), permissions.RoleVerifier)
	if len(verifierResp.Pages) != 1 || verifierResp.Pages[0].RouteID != verifierLensPageID {
		t.Fatalf("verifier lens pages = %v, want only %q", verifierResp.Pages, verifierLensPageID)
	}
	verifierPage := verifierResp.Pages[0]
	for _, id := range []string{"toxin_tab", "toxin_verdict"} {
		got := controlByID(t, verifierPage, id)
		if got.Enabled {
			t.Fatalf("verifier %s = %#v, want DISABLED — the verifier must never see toxin work", id, got)
		}
		if got.DisabledReason == "" {
			t.Fatalf("verifier %s must carry a disabled reason", id)
		}
	}

	// A leadership role BELOW the CEO (park head reaches /verify through verification.act) gets the
	// controls disabled too: the gate is the toxin.verdict permission, not page reachability.
	parkHeadPage := pageByRouteID(t, bootstrapAs(lensService(), permissions.RoleParkHead).Pages, verifierLensPageID)
	for _, id := range []string{"toxin_tab", "toxin_verdict"} {
		got := controlByID(t, parkHeadPage, id)
		if got.Enabled {
			t.Fatalf("park head %s = %#v, want disabled", id, got)
		}
	}
}
