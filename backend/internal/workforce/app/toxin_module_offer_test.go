package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestToxinModuleIsOfferedPerPersonNotPerJob pins the 2026-08-25 maintainer decision:
// the Toxin module (aflatoxin strip tests) is visible to CEO/CXO and to the NAMED people
// holding the per-person toxin_tester grant — today the two park heads in
// perPersonGrants — never to a job role.
//
// The negative half is the point: a bare pc_director / growth_director / park_head — a
// future holder of any of those jobs with no personal toxin_tester grant — must resolve
// NO toxin module, or the per-person grant has silently become a per-job one.
//
// Mutation-tested when written: (a) deleting the ToxinRead offer branch in
// leadershipModuleKeys, (b) rekeying it on RolePCDirector, and (c) granting ToxinRead to
// RoleParkHead each turn a subtest red.
func TestToxinModuleIsOfferedPerPersonNotPerJob(t *testing.T) {
	const en = localization.DefaultTag

	hasKey := func(keys []string, want string) bool {
		for _, k := range keys {
			if k == want {
				return true
			}
		}
		return false
	}

	perPerson := map[string][]domain.GrantSummary{
		"Chandrakant (pc_director + toxin_tester)": {
			grantWithRole(permissions.RolePCDirector),
			grantWithRole(permissions.RoleToxinTester),
		},
		"Dinakar (growth_director + pc_director + toxin_tester)": {
			grantWithRole(permissions.RoleGrowthDirector),
			grantWithRole(permissions.RolePCDirector),
			grantWithRole(permissions.RoleToxinTester),
		},
		"CEO/CXO": {
			grantWithRole(permissions.RoleCEOInternal),
		},
	}
	for name, grants := range perPerson {
		t.Run(name+" is offered Toxin", func(t *testing.T) {
			if keys := leadershipModuleKeys(grants); !hasKey(keys, "toxin") {
				t.Fatalf("leadership module keys = %v, want toxin offered", keys)
			}
			// The offer must actually RENDER: these principals hold toxin.read, so the
			// drawer row and its nav item are real, not an empty module.
			if _, ok := moduleKeySet(modulesFor(grants, nil, en))["toxin"]; !ok {
				t.Fatal("Toxin module did not render for a principal holding toxin.read")
			}
		})
	}

	for name, grants := range map[string][]domain.GrantSummary{
		"bare pc_director":     {grantWithRole(permissions.RolePCDirector)},
		"bare growth_director": {grantWithRole(permissions.RoleGrowthDirector)},
		"bare park_head":       {grantWithRole(permissions.RoleParkHead)},
		"bare operator":        {grantWithRole(permissions.RoleOperator)},
		"bare verifier":        {grantWithRole(permissions.RoleVerifier)},
	} {
		t.Run(name+" is NOT offered Toxin", func(t *testing.T) {
			if keys := leadershipModuleKeys(grants); hasKey(keys, "toxin") {
				t.Fatalf("leadership module keys = %v; %s must not inherit the toxin module by job", keys, name)
			}
			if _, ok := moduleKeySet(modulesFor(grants, nil, en))["toxin"]; ok {
				t.Fatalf("Toxin module rendered for %s", name)
			}
		})
	}

	// The tester role itself must never quietly gain the CEO-only review authority.
	if permissions.RoleHasPermission(permissions.RoleToxinTester, permissions.ToxinVerdict) {
		t.Fatal("toxin_tester must not hold toxin.verdict — review is CEO/CXO only")
	}
}
