package main

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// Adversarial cover for the roster aggregation this command performs. The stakes are that
// a mistake here grants a real person the wrong access on cutover morning, silently.

// TestStackedRolesAreOneToManyAndMergeToOneRowPerModule is the fan-out case. STG carries
// four people wearing several job titles at once (one wears five), so a person expands to
// MANY module rows that overlap. They must union into ONE row per (module, surface): two
// rows for the same pair would both be inserted and the second would silently win, and
// which one won would depend on map iteration order.
func TestStackedRolesAreOneToManyAndMergeToOneRowPerModule(t *testing.T) {
	stacked := person{
		roles:      []string{permissions.RoleFeedDirector, permissions.RoleProcurementDirector},
		tenantWide: true,
	}
	assignments, _ := shapePerson(stacked)
	if len(assignments) == 0 {
		t.Fatal("a stacked person mapped to no modules at all")
	}
	seen := map[string]int{}
	for _, a := range assignments {
		seen[a.Module+"|"+a.Surface]++
	}
	for key, count := range seen {
		if count != 1 {
			t.Errorf("%s appears %d times; stacked roles must merge to one row per module and surface", key, count)
		}
	}

	// And the union is a real union, not a last-one-wins: this person's Feed capabilities
	// must include everything either role carried.
	single, _ := shapePerson(person{roles: []string{permissions.RoleFeedDirector}, tenantWide: true})
	want := map[string]struct{}{}
	for _, a := range single {
		if a.Module == "feed_direction" && a.Surface == permissions.SurfaceMobile {
			for _, c := range a.Capabilities {
				want[c] = struct{}{}
			}
		}
	}
	got := map[string]struct{}{}
	for _, a := range assignments {
		if a.Module == "feed_direction" && a.Surface == permissions.SurfaceMobile {
			for _, c := range a.Capabilities {
				got[c] = struct{}{}
			}
		}
	}
	for c := range want {
		if _, ok := got[c]; !ok {
			t.Errorf("stacking dropped the Feed Director's %q on the phone; the merge must union, not replace", c)
		}
	}
}

// TestEveryPersonIsShapedExactlyOnceAcrossAPageBoundary pins the pagination property. The
// roster read is ONE set-based pass with no LIMIT/OFFSET, deliberately: a page boundary is
// where a person silently falls through and is left un-migrated, which looks identical to
// "never had access". This asserts the shaping is per-person and order-independent, so a
// batch of any size yields one result per input.
func TestEveryPersonIsShapedExactlyOnceAcrossAPageBoundary(t *testing.T) {
	roster := make([]person, 0, 45)
	for i := 0; i < 45; i++ {
		roster = append(roster, person{
			memberID:   string(rune('a'+i%26)) + "-member",
			roles:      []string{permissions.RoleOperator},
			parkIDs:    []string{"park-1"},
			tenantWide: false,
		})
	}
	for i, p := range roster {
		assignments, scope := shapePerson(p)
		if len(assignments) == 0 {
			t.Fatalf("person %d shaped to nothing; a page boundary must not drop anyone", i)
		}
		if scope != "parks" {
			t.Fatalf("person %d shaped to scope %q; a park operator must never be widened to tenant", i, scope)
		}
	}
}

// TestParkScopeIsNeverInferredFromAnEmptyParkList pins the scope rule. "Every park" and
// "no park chosen yet" are different answers, and inferring the first from the second
// silently widens someone to both parks.
func TestParkScopeIsNeverInferredFromAnEmptyParkList(t *testing.T) {
	_, scope := shapePerson(person{roles: []string{permissions.RoleOperator}, parkIDs: nil, tenantWide: false})
	if scope != "parks" {
		t.Fatalf("a person with NO park grants shaped to %q; an empty park list must not become tenant scope", scope)
	}
	_, scope = shapePerson(person{roles: []string{permissions.RoleOperator}, parkIDs: []string{"a", "b"}, tenantWide: true})
	if scope != "tenant" {
		t.Fatalf("a tenant-scoped grant shaped to %q; one tenant grant promotes the whole person", scope)
	}
}

// TestStatusMatrixOnlyActiveGrantsReachTheShaping records where the status filter lives: the
// roster query FILTERs to status='active', so an inactive or revoked grant never reaches
// this function. A person whose grants are all inactive arrives with NO roles, and the
// caller skips them rather than writing an empty access header -- "never migrated" must stay
// visible instead of looking like a deliberate grant of nothing.
func TestStatusMatrixOnlyActiveGrantsReachTheShaping(t *testing.T) {
	assignments, _ := shapePerson(person{roles: nil, tenantWide: false})
	if len(assignments) != 0 {
		t.Fatalf("a person with no active grant shaped to %d module rows; they must shape to nothing", len(assignments))
	}
	// An unknown/dormant role contributes nothing rather than something unintended.
	assignments, _ = shapePerson(person{roles: []string{"director_breeding_dormant"}, tenantWide: false})
	if len(assignments) != 0 {
		t.Fatalf("a dormant catalog role shaped to %d module rows; an unmapped role must grant nothing", len(assignments))
	}
}

func TestCEOInternalKeepsFutureWebPagesOpen(t *testing.T) {
	assignments, scope := shapePerson(person{roles: []string{permissions.RoleCEOInternal}, tenantWide: true})
	if scope != "tenant" {
		t.Fatalf("ceo_internal shaped to scope %q, want tenant", scope)
	}
	var sawWeb bool
	for _, a := range assignments {
		if a.Surface != permissions.SurfaceWeb {
			continue
		}
		sawWeb = true
		if len(a.Pages) != 0 {
			t.Fatalf("ceo_internal web module %q stored frozen pages %v; want empty list so future pages stay open", a.Module, a.Pages)
		}
	}
	if !sawWeb {
		t.Fatal("ceo_internal shaped to no web rows")
	}
}

func TestNonCEORetiredPageNarrowingStaysExplicit(t *testing.T) {
	assignments, _ := shapePerson(person{roles: []string{permissions.RoleProcurementDirector}, tenantWide: true})
	for _, a := range assignments {
		if a.Module != "feed_direction" || a.Surface != permissions.SurfaceWeb {
			continue
		}
		if len(a.Pages) == 0 {
			t.Fatal("procurement director Feed web row stored empty pages; retired lens narrowing must remain explicit")
		}
		for _, page := range a.Pages {
			if page == "feed-config" {
				t.Fatalf("procurement director Feed pages %v include Feed Config; retired lens narrowing was widened", a.Pages)
			}
		}
		return
	}
	t.Fatal("procurement director Feed web row missing")
}
