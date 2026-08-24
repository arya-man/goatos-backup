package permissions

import (
	"sort"
	"strings"
	"testing"
)

// The cutover safety proof.
//
// Per-person access replaces role-derived access in one release (maintainer decision
// 2026-08-24: "do it in one go"). That is only safe if the one-time backfill reproduces
// every existing person's CURRENT effective permission set. This test compares, role by
// role, what rolePermissions grants today against what the same person's backfilled
// assignment rows will grant tomorrow.
//
// A LOSS (a permission held today and not tomorrow) locks someone out of their job.
// A GAIN (a permission not held today and held tomorrow) hands out authority nobody
// approved. Both fail unless recorded in acceptedDeltas with a reason.

// acceptedDelta records a reviewed difference between today's role set and tomorrow's
// backfilled set. Every entry is a maintainer-visible decision, not a way to silence the
// test -- adding one is how a difference gets APPROVED, so each carries its reasoning.
type acceptedDelta struct {
	gained []string
	lost   []string
	why    string
}

var acceptedDeltas = map[string]acceptedDelta{}

func TestBackfillReproducesEveryRolesEffectivePermissions(t *testing.T) {
	roles := make([]string, 0, len(rolePermissions))
	for role := range rolePermissions {
		roles = append(roles, role)
	}
	sort.Strings(roles)

	for _, role := range roles {
		t.Run(role, func(t *testing.T) {
			today := make([]string, 0, len(rolePermissions[role]))
			for p := range rolePermissions[role] {
				today = append(today, p)
			}
			sort.Strings(today)

			rows, mapped := AssignmentsForRole(role)
			if !mapped {
				t.Fatalf("role %q has no backfill mapping: every person carrying it would be "+
					"migrated with NO access at all", role)
			}
			tomorrow := PermissionsForAssignmentsWithBaseline(rows)

			gained := difference(tomorrow, today)
			lost := difference(today, tomorrow)

			accepted := acceptedDeltas[role]
			unexpectedGain := difference(gained, accepted.gained)
			unexpectedLoss := difference(lost, accepted.lost)

			if len(unexpectedLoss) > 0 {
				t.Errorf("LOSS -- %q holds these today and would NOT tomorrow; every person "+
					"carrying this role is locked out of that work:\n  %s",
					role, strings.Join(unexpectedLoss, "\n  "))
			}
			if len(unexpectedGain) > 0 {
				t.Errorf("GAIN -- %q does NOT hold these today and would tomorrow; this is "+
					"authority nobody approved:\n  %s",
					role, strings.Join(unexpectedGain, "\n  "))
			}
			// A stale accepted entry is its own defect: it reads as a reviewed difference that
			// no longer exists, and the next reader trusts it.
			if stale := difference(accepted.gained, gained); len(stale) > 0 {
				t.Errorf("stale acceptedDeltas[%q].gained -- no longer differs: %s",
					role, strings.Join(stale, ", "))
			}
			if stale := difference(accepted.lost, lost); len(stale) > 0 {
				t.Errorf("stale acceptedDeltas[%q].lost -- no longer differs: %s",
					role, strings.Join(stale, ", "))
			}
		})
	}
}

// TestBackfillReportsFullDiff is the human-readable companion: it never fails, and prints
// the whole picture so a maintainer can review the cutover in one read rather than
// assembling it from failures. Run with -v.
func TestBackfillReportsFullDiff(t *testing.T) {
	roles := make([]string, 0, len(rolePermissions))
	for role := range rolePermissions {
		roles = append(roles, role)
	}
	sort.Strings(roles)

	for _, role := range roles {
		today := make([]string, 0, len(rolePermissions[role]))
		for p := range rolePermissions[role] {
			today = append(today, p)
		}
		sort.Strings(today)

		rows, _ := AssignmentsForRole(role)
		tomorrow := PermissionsForAssignmentsWithBaseline(rows)
		gained := difference(tomorrow, today)
		lost := difference(today, tomorrow)

		t.Logf("%-24s today=%-3d tomorrow=%-3d  +%d  -%d", role, len(today), len(tomorrow), len(gained), len(lost))
		for _, p := range gained {
			t.Logf("    GAIN  %s", p)
		}
		for _, p := range lost {
			t.Logf("    LOSS  %s", p)
		}
	}
}

// TestStackedRoleMergeKeepsEveryPermission covers the real STG people who wear several job
// titles at once (one wears five). AssignmentsForRoles merges by level RANK, and levels are
// deliberately not a cumulative ladder -- so a rank merge CAN drop a permission the lower
// level carried. This proves it does not for the combinations that actually exist.
func TestStackedRoleMergeKeepsEveryPermission(t *testing.T) {
	// Exactly the stacks observed on STG on 2026-08-24.
	stacks := map[string][]string{
		"dinakar":     {RoleCountsApprover, RoleGrowthDirector, RoleOperator, RoleParkHead, RolePCDirector},
		"chandrakant": {RoleCountsApprover, RoleOperator, RoleParkHead, RolePCDirector},
		"hemant":      {RoleFeedDirector, RoleProcurementDirector},
		"avishek":     {RoleCountsApprover, RoleHealthDirector},
	}
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		roles := stacks[name]
		t.Run(name, func(t *testing.T) {
			// Today: the union of every stacked role's permissions -- that IS how the request
			// path resolves a multi-grant principal.
			union := map[string]struct{}{}
			for _, role := range roles {
				for p := range rolePermissions[role] {
					union[p] = struct{}{}
				}
			}
			today := make([]string, 0, len(union))
			for p := range union {
				today = append(today, p)
			}
			sort.Strings(today)

			tomorrow := PermissionsForAssignmentsWithBaseline(AssignmentsForRoles(roles))

			// Accept whatever each role was individually allowed to gain, since the stack
			// inherits those; anything else is a merge defect.
			allowedGain := map[string]struct{}{}
			for _, role := range roles {
				for _, p := range acceptedDeltas[role].gained {
					allowedGain[p] = struct{}{}
				}
			}
			allowedLoss := map[string]struct{}{}
			for _, role := range roles {
				for _, p := range acceptedDeltas[role].lost {
					allowedLoss[p] = struct{}{}
				}
			}

			for _, p := range difference(today, tomorrow) {
				if _, ok := allowedLoss[p]; !ok {
					t.Errorf("LOSS on stacked person %q (%s): %s", name, strings.Join(roles, "+"), p)
				}
			}
			for _, p := range difference(tomorrow, today) {
				if _, ok := allowedGain[p]; !ok {
					t.Errorf("GAIN on stacked person %q (%s): %s", name, strings.Join(roles, "+"), p)
				}
			}
		})
	}
}

// TestEveryPermissionIsReachable proves the catalog can express the whole vocabulary. A
// permission no level grants is unassignable: the route that requires it becomes dead to
// everyone, which is a silent lockout rather than a visible error.
func TestEveryPermissionIsReachable(t *testing.T) {
	reachable := map[string]struct{}{}
	for _, mod := range moduleCapabilities {
		for _, perms := range mod.Levels {
			for _, p := range perms {
				reachable[p] = struct{}{}
			}
		}
	}
	for _, perms := range surfaceBaseline {
		for _, p := range perms {
			reachable[p] = struct{}{}
		}
	}
	for _, p := range surfaceBootstrap {
		reachable[p] = struct{}{}
	}

	declared := map[string]struct{}{}
	for _, perms := range rolePermissions {
		for p := range perms {
			declared[p] = struct{}{}
		}
	}

	missing := make([]string, 0)
	for p := range declared {
		if _, ok := reachable[p]; !ok {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no module level grants these, so nobody can ever be given them:\n  %s",
			strings.Join(missing, "\n  "))
	}
}

// TestFeedOverseeNeverGrantsComplete pins the asymmetry that forced levels to be
// non-cumulative. The Feed Director reads every page of the feed chain and cannot record a
// task as done; a cumulative ladder would hand him that silently.
func TestFeedOverseeNeverGrantsComplete(t *testing.T) {
	for _, level := range []string{LevelView, LevelOversee, LevelConfigure} {
		perms := PermissionsForAssignments([]ModuleAssignment{
			{Module: "feed_direction", Surface: SurfaceWeb, Capabilities: []string{level}},
		})
		for _, p := range perms {
			if p == FeedDirectionComplete {
				t.Fatalf("feed_direction at %q grants %s -- the director could record a "+
					"transport task as done, reversing a recorded decision", level, FeedDirectionComplete)
			}
		}
	}
	// And the level that SHOULD carry it still does, so this is a boundary, not a removal.
	do := PermissionsForAssignments([]ModuleAssignment{
		{Module: "feed_direction", Surface: SurfaceWeb, Capabilities: []string{LevelDo}},
	})
	if !contains(do, FeedDirectionComplete) {
		t.Fatalf("feed_direction at %q must grant %s -- the operator records the work", LevelDo, FeedDirectionComplete)
	}
}

// TestUnknownRowsGrantNothing pins the fail-closed rule: a typo, a retired module, or a
// level a module does not offer must contribute NOTHING, never something unintended.
func TestUnknownRowsGrantNothing(t *testing.T) {
	cases := []struct {
		name string
		row  ModuleAssignment
	}{
		{"unknown module", ModuleAssignment{Module: "not_a_module", Surface: SurfaceWeb, Capabilities: []string{LevelConfigure}}},
		{"unknown surface", ModuleAssignment{Module: "weighing", Surface: "watch", Capabilities: []string{LevelConfigure}}},
		{"unoffered level", ModuleAssignment{Module: "sales", Surface: SurfaceWeb, Capabilities: []string{LevelConfigure}}},
		{"module absent from surface", ModuleAssignment{Module: "sales", Surface: SurfaceMobile, Capabilities: []string{LevelDo}}},
		{"explicit none", ModuleAssignment{Module: "weighing", Surface: SurfaceWeb, Capabilities: []string{LevelNone}}},
		{"empty capabilities", ModuleAssignment{Module: "weighing", Surface: SurfaceWeb, Capabilities: nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PermissionsForAssignmentsWithBaseline([]ModuleAssignment{tc.row}); len(got) > 0 {
				t.Fatalf("granted %v, want nothing", got)
			}
		})
	}
}

// TestSeparationRiskWarnsOnSelfVerification covers the one separation-of-duty control the
// retired role model enforced structurally and per-person assignment cannot: a person who
// both carries out work and casts the verdict on it.
func TestSeparationRiskWarnsOnSelfVerification(t *testing.T) {
	risky := []ModuleAssignment{
		{Module: "verification", Surface: SurfaceWeb, Capabilities: []string{LevelView, LevelDo}},
		{Module: "weighing", Surface: SurfaceMobile, Capabilities: []string{LevelView, LevelDo}},
	}
	risks := SeparationRisks(risky)
	if len(risks) != 1 || risks[0].ConflictsWithModule != "weighing" {
		t.Fatalf("SeparationRisks = %+v, want one weighing conflict", risks)
	}
	if risks[0].Reason == "" {
		t.Fatal("risk carries no farm-readable reason to show the person granting it")
	}

	// A verifier who verifies and does NOT execute is the intended shape -- no warning.
	clean := []ModuleAssignment{
		{Module: "verification", Surface: SurfaceWeb, Capabilities: []string{LevelView, LevelDo}},
		{Module: "weighing", Surface: SurfaceWeb, Capabilities: []string{LevelView}},
	}
	if risks := SeparationRisks(clean); len(risks) != 0 {
		t.Fatalf("SeparationRisks = %+v, want none for a verifier who only views the work", risks)
	}
}

func difference(a, b []string) []string {
	inB := make(map[string]struct{}, len(b))
	for _, s := range b {
		inB[s] = struct{}{}
	}
	out := make([]string, 0)
	for _, s := range a {
		if _, ok := inB[s]; !ok {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
