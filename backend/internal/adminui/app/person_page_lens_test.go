package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

type stubPageAccess struct {
	access   permissions.PageAccess
	assigned bool
	err      error
}

func (s stubPageAccess) ResolvePageAccess(context.Context, string, string) (permissions.PageAccess, bool, error) {
	return s.access, s.assigned, s.err
}

func leafHrefs(resp domain.BootstrapResponse) []string {
	out := make([]string, 0, 32)
	for _, item := range resp.Navigation.Primary {
		out = append(out, item.Href)
	}
	for _, group := range resp.Navigation.Groups {
		for _, leaf := range group.Leaves {
			out = append(out, leaf.Href)
		}
	}
	return out
}

func accessFor(roles ...string) permissions.PageAccess {
	assignments := permissions.NarrowForRetiredLenses(roles, permissions.AssignmentsForRoles(roles))
	return permissions.PageAccessForAssignments(permissions.FillDefaultPages(assignments))
}

// TestRetiredProcurementDirectorLensIsReproducedByTicks is the proof that retiring
// procurement_director_lens.go changed nothing for the person it was written for.
//
// The maintainer's 2026-08-21 words were "he should see only the Procurement and Feed
// modules in web" and "hide the Feed Config page for him under Feed". His live sidebar on
// 2026-08-27 was exactly the six leaves below. The lens is gone; his backfilled TICKS must
// still produce them, and must still produce nothing else.
func TestRetiredProcurementDirectorLensIsReproducedByTicks(t *testing.T) {
	// The real holder wears BOTH roles -- his phone access and feed-proof ownership ride on
	// feed_director -- and the retired lens applied anyway. Stacking them here is the case
	// that matters, not procurement_director alone.
	access := accessFor(permissions.RoleProcurementDirector, permissions.RoleFeedDirector)
	resp := applyPersonPageLens(compileForTest(), access)

	// All six leaves the lens left him, Feed SOP included -- and that one now WORKS.
	//
	// It had become a dead leaf: /admin/sops needs sop.read, and dropping his
	// non-Procurement web modules took it away. A cutover simulation against the real STG
	// roster caught the permission loss, and the fix restores the modules that own no
	// sidebar page at all (Protocols & SOPs, Herd Register, Parks & Sheds). They add nothing
	// visible, they carry reads he has always held, and Feed SOP opens instead of 403ing.
	want := []string{
		"/sales",
		// Purchase and Born (maintainer decision 2026-08-31). It rides SalesRead, which this
		// director already holds, and he is precisely the desk that records a load's landed
		// cost -- LoadCostWrite is granted exactly where FeedPurchaseWrite is.
		"/sales/loads",
		// Sales Config (maintainer decision 2026-09-01), which is where the load-cost write
		// he owns MOVED to when /sales/loads became read-only. Withholding it would leave him
		// the one desk that records a load's cost with nowhere to record it.
		"/sales/config",
		"/feed/analytics",
		"/feed/sops",
		// The role lens preserves the global top-level menu order (maintainer decision
		// 2026-09-02): Counts, Weight, Sales, Feed, Preventive Care, Procurement, Others.
		// This director has only Sales, Feed, and Procurement leaves from that sequence.
		"/procurement/source-entry",
		"/procurement/vendors",
		"/procurement/feed-purchases",
	}
	got := leafHrefs(resp)
	if len(got) != len(want) {
		t.Fatalf("sidebar is %v; want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sidebar is %v; want %v", got, want)
		}
	}

	// The deep-link half: a typed URL must fail closed, which requireAdminWebPageContract
	// does by throwing on a route with no contract.
	for _, banned := range []string{"/feed/config", "/people", "/verify", "/vaccination", "/"} {
		for _, page := range resp.Pages {
			if page.Href == banned {
				t.Errorf("page contract for %s survived; a typed URL would render", banned)
			}
		}
	}
	// And what he keeps must still carry its contract, or the sidebar links to a 404.
	for _, kept := range []string{"/feed/analytics", "/sales"} {
		found := false
		for _, page := range resp.Pages {
			if page.Href == kept {
				found = true
			}
		}
		if !found {
			t.Errorf("page contract for %s was dropped; its nav leaf would not open", kept)
		}
	}
}

// TestCeoIsNeverNarrowed pins the exemption both retired lenses carried: a lens must never
// narrow a leadership principal.
func TestCeoIsNeverNarrowed(t *testing.T) {
	access := accessFor(permissions.RoleCEOInternal, permissions.RoleProcurementDirector)
	resp := applyPersonPageLens(compileForTest(), access)
	for _, want := range []string{"/", "/people", "/feed/config", "/verify", "/vaccination"} {
		found := false
		for _, href := range leafHrefs(resp) {
			if href == want {
				found = true
			}
		}
		if !found {
			t.Errorf("CEO lost %s", want)
		}
	}
}

// TestUnassignedAndErroringPeopleAreNotNarrowed pins the two fail-open cases. A person the
// backfill has not reached must keep the role-composed contract; so must one whose access
// could not be read. Narrowing on absence or on error would lock people out of a product
// they are authorized for, and the routes behind every page are independently gated anyway.
func TestUnassignedAndErroringPeopleAreNotNarrowed(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  stubPageAccess
	}{
		{"no stored rows", stubPageAccess{assigned: false}},
		{"source error", stubPageAccess{assigned: true, err: errors.New("db down")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService().WithPersonPageAccess(tc.src)
			_, assigned, err := svc.personPageAccessFor(context.Background(), BootstrapInput{
				TenantID: "t", ActorID: "u",
			})
			if assigned {
				t.Fatal("narrowing applied; the person would lose pages they are authorized for")
			}
			// The error is not swallowed: it travels back so compile() can declare the
			// degradation in the contract it serves.
			if (err != nil) != (tc.src.err != nil) {
				t.Fatalf("error reporting is %v; the failure must reach the caller", err)
			}
			if err != nil && personPageAccessUnavailableRule(err).Summary == "" {
				t.Fatal("the degradation rule says nothing")
			}
		})
	}
}

// TestAModuleTickWithoutPageTicksKeepsEveryPage pins the property that makes a NEW page
// reach the people who already hold its module: an empty page list means all of them.
func TestAModuleTickWithoutPageTicksKeepsEveryPageItCanOpen(t *testing.T) {
	// Feed at `view` opens Feed Analytics alone: Feed Config needs feed_config.read, and
	// Feed SOP needs sop.read from another module. "Every page" means every page this
	// person can actually open -- a screen that would 403 is never offered.
	access := permissions.PageAccessForAssignments([]permissions.ModuleAssignment{
		{Module: "feed_direction", Surface: permissions.SurfaceWeb, Capabilities: []string{permissions.LevelView}},
	})
	if got := leafHrefs(applyPersonPageLens(compileForTest(), access)); len(got) != 1 || got[0] != "/feed/analytics" {
		t.Fatalf("sidebar is %v; want [/feed/analytics]", got)
	}

	// With the authority for all three -- Feed at `configure` plus Protocols & SOPs -- all
	// three appear, and none of them is greyed.
	access = permissions.PageAccessForAssignments([]permissions.ModuleAssignment{
		{Module: "feed_direction", Surface: permissions.SurfaceWeb, Capabilities: []string{permissions.LevelConfigure}},
		{Module: "config", Surface: permissions.SurfaceWeb, Capabilities: []string{permissions.LevelView}},
	})
	got := leafHrefs(applyPersonPageLens(compileForTest(), access))
	want := []string{"/feed/config", "/feed/analytics", "/feed/sops"}
	if len(got) != len(want) {
		t.Fatalf("sidebar is %v; want %v", got, want)
	}
}

func compileForTest() domain.BootstrapResponse {
	return baseBootstrap()
}

// TestASavedTickIsNotHiddenBehindTheContractCache is the regression for a defect the
// end-to-end run found: an admin ticked a page, told the person to reload, and nothing
// happened for up to 60 seconds.
//
// The cache key is built from tenant, actor, roles and grants -- none of which move when
// access is EDITED -- so the stale entry kept serving the old sidebar until its TTL lapsed.
// That was invisible while a role change meant a deploy. It is the normal case now.
func TestASavedTickIsNotHiddenBehindTheContractCache(t *testing.T) {
	// Held at `configure`, so Feed Config is genuinely openable and the tick is the only
	// thing deciding whether it shows.
	before := permissions.PageAccessForAssignments([]permissions.ModuleAssignment{{
		Module: "feed_direction", Surface: permissions.SurfaceWeb,
		Capabilities: []string{permissions.LevelConfigure},
		Pages:        []string{"feed-analytics"},
	}})
	after := permissions.PageAccessForAssignments([]permissions.ModuleAssignment{{
		Module: "feed_direction", Surface: permissions.SurfaceWeb,
		Capabilities: []string{permissions.LevelConfigure},
		Pages:        []string{"feed-analytics", "feed-config"},
	}})

	if pageAccessFingerprint(before, true) == pageAccessFingerprint(after, true) {
		t.Fatal("ticking a page did not change the cache fingerprint; the save would sit behind the TTL")
	}
	// Stable across calls: a fingerprint that varied with map iteration order would be a
	// cache that never hits, which is the opposite failure and just as real.
	if pageAccessFingerprint(after, true) != pageAccessFingerprint(after, true) {
		t.Fatal("the fingerprint is not stable for the same access set")
	}
	// "Not narrowed at all" and "narrowed to nothing" compile to DIFFERENT contracts and
	// must never share a cache entry.
	if pageAccessFingerprint(permissions.PageAccess{}, false) == pageAccessFingerprint(permissions.PageAccess{}, true) {
		t.Fatal("an unassigned principal shares a cache key with one narrowed to nothing")
	}
	// A module tick alone moves it too: capabilities change what compiles, not only pages.
	moduleOnly := permissions.PageAccessForAssignments([]permissions.ModuleAssignment{{
		Module: "sales", Surface: permissions.SurfaceWeb, Capabilities: []string{permissions.LevelView},
	}})
	if pageAccessFingerprint(moduleOnly, true) == pageAccessFingerprint(before, true) {
		t.Fatal("two different module sets share a fingerprint")
	}
}
