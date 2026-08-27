package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// The phone bar follows the person's ticks (maintainer decision 2026-08-27).
//
// This is the regression for a defect a live persona sweep found and that a log out and log
// in could not clear: the two halves of a phone module came from DIFFERENT places. Permissions
// came from the person's own rows and dropped in 0.02s; the BAR came from
// department_module_grants and never moved. The operator kept the icon for a module he had
// lost, tapped it, and the work failed -- and re-logging in changed nothing, because nothing
// was stale. The server was answering from a source the tick never touched.

func phoneRepo(t *testing.T, grants []domain.GrantSummary, dept, person []string) *fakeRepo {
	t.Helper()
	return &fakeRepo{
		profile:             profile("active"),
		grants:              grants,
		grantedModules:      dept,
		personMobileModules: person,
	}
}

func moduleKeys(resp *domain.BootstrapResponse) []string {
	out := make([]string, 0, len(resp.Modules))
	for _, m := range resp.Modules {
		out = append(out, m.Key)
	}
	return out
}

func operatorGrants() []domain.GrantSummary { return []domain.GrantSummary{grant()} }

func TestPhoneBarFollowsThePersonsTicks(t *testing.T) {
	repo := phoneRepo(t, operatorGrants(),
		[]string{"vaccination", "counts", "weighing"}, // what the department still grants
		[]string{"vaccination", "weighing"},           // what the person is TICKED for
	)
	resp, err := NewService(repo).Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	for _, key := range moduleKeys(resp) {
		if key == "counts" {
			t.Fatal("an unticked phone module is still on the bar; the operator would tap it and the work would fail")
		}
	}
}

// TestAPersonWithNoTicksKeepsTheirDepartmentBar is the fail-open half. Someone the backfill
// has not reached must not lose their phone: no stored rows means the department grants still
// decide, exactly as before.
func TestAPersonWithNoTicksKeepsTheirDepartmentBar(t *testing.T) {
	repo := phoneRepo(t, operatorGrants(), []string{"vaccination", "counts"}, nil)
	resp, err := NewService(repo).Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	got := moduleKeys(resp)
	if len(got) == 0 {
		t.Fatal("a person with no stored ticks lost their whole bar")
	}
}

// TestAStandaloneVerifiersBarIsNotRecomposedFromTicks pins the exemption. Her modules are the
// features her verify DUTIES name, and the whole verifier workspace is composed from them;
// feeding her ticks in would rebuild a surface the maintainer said does not change.
func TestAStandaloneVerifiersBarIsNotRecomposedFromTicks(t *testing.T) {
	g := grant()
	g.Role = permissions.RoleVerifier
	verifier := []domain.GrantSummary{g}
	duties := []string{"vaccination", "weighing"}
	withTicks := phoneRepo(t, verifier, duties, []string{"people"})
	withoutTicks := phoneRepo(t, verifier, duties, nil)

	a, err := NewService(withTicks).Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	b, err := NewService(withoutTicks).Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	got, want := moduleKeys(a), moduleKeys(b)
	if len(got) != len(want) {
		t.Fatalf("the verifier's bar changed with her ticks: %v vs %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the verifier's bar changed with her ticks: %v vs %v", got, want)
		}
	}
}

// TestEveryPhoneModuleExistsInTheCapabilityCatalog is the guard for the silent-loss class
// this change created and that only a live run caught.
//
// Once the bar reads the person's TICKS, a module key the phone registry can serve but the
// catalog has no entry for becomes untickable -- so it silently vanishes from every
// operator's phone at cutover. `milk` was exactly that: a real vertical (split out of Counts
// by the 2026-07-31 decision, with its own admin-web group) that the catalog had folded into
// Herd Operations, so the first live run dropped Milk off an operator's bar.
//
// `breeding` is deliberately exempt: it is a `soon` module, surfaced to EVERY principal as a
// disabled roadmap row regardless of grants, so it needs no tick and confers no access.
func TestEveryPhoneModuleExistsInTheCapabilityCatalog(t *testing.T) {
	exempt := map[string]struct{}{}
	for _, key := range soonModuleKeys {
		exempt[key] = struct{}{}
	}
	for key := range moduleNavRegistry {
		if _, skip := exempt[key]; skip {
			continue
		}
		// The registry also holds the synthesised verifier module, which is composed from
		// verify duties rather than ticked.
		if key == "verification" {
			continue
		}
		if !permissions.ModuleSupportsSurface(key, permissions.SurfaceMobile) {
			t.Errorf("phone module %q has no mobile entry in permissions.ModuleCapabilities; ticking cannot reach it, so it would disappear from every operator's bar", key)
		}
	}
}
