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

// Every difference below is a REVIEWED maintainer decision, not a way to silence the test.
// A gain not listed here fails the build.
//
// There are no LOSS entries and there must never be one: a permission held today and not
// tomorrow locks a real person out of their job on cutover morning. If a loss appears, the
// catalog is wrong, not the expectation.

// benignReadGains are accepted on ANY role. Each is a READ that the retired role model
// happened to withhold from a principal who already holds the surrounding data -- an
// artefact of hand-maintained per-role permission lists, not a deliberate boundary. Granting
// them changes what a screen can DISPLAY, never what anyone can DO.
var benignReadGains = map[string]string{
	LocationsRead:           "the shed/park directory every screen labels itself with; a module whose screen cannot name its own shed is broken",
	TaskRead:                "the work list the phone opens onto",
	ProtocolRead:            "reading the standing rule the work is carried out against",
	SOPRead:                 "reading the written procedure for work the principal already performs",
	ObligationRead:          "the due-work rows behind a vaccination screen the principal already reads",
	CountsAlertsRead:        "a notification about herd movement, not the Counts screens (those are counts.read, deliberately held at LevelConfigure)",
	VaccinationRead:         "the vaccination rows behind a screen the principal already works in",
	VaccinationOverviewRead: "the summary card above data the principal already reads",
	VaccinationAlertsRead:   "a notification about vaccination work the principal already owns",
	WeighingMonitor:         "seeing the weighing board for work the principal already carries out",
	PCCareMonitor:           "seeing the preventive-care board for work the principal already carries out",
	HealthReport:            "raising a sick-goat report -- field work every tier does, including one that never carries out a course",
	FeedPackingRead:         "another page of the feed chain the principal already reads",
	FeedWastageRead:         "another page of the feed chain the principal already reads",
	FeedTransportRead:       "another page of the feed chain the principal already reads",
	FeedAnalyticsStockRead:  "the stock-only read behind Feed Analytics for anyone who already reads the feed chain",
}

// orgGridGains are accepted ONLY on the 36 composite tier x vertical roles. Those are
// dormant catalog scaffolding (AGENTS.md: "treat roles outside that list as dormant catalog
// scaffolding, not live STG/mobile personas") -- on STG only am_health and manager_health
// are granted, and neither gains anything here. Activating one of these roles for a real
// person is a separate decision that must re-examine this list.
var orgGridGains = map[string]string{
	VerificationReview: "the queue this tier can already act on (it holds verification.act without verification.review today, which is acting blind)",
	OperatorsViewAudit: "reading the audit trail of the team this tier already manages",
	TaskVerify:         "signing off task work this tier already supervises",
	VaccinationVerify:  "the older per-vaccination verify, alongside the drive authoring this tier already holds",
	SOPWrite:           "authoring the procedure for the vertical this tier owns, alongside the protocol authoring it already holds",
	SOPPublish:         "publishing that procedure",
}

// namedRoleGains are accepted for one specific live role, with the reason it is safe.
var namedRoleGains = map[string]map[string]string{
	RoleCountsApprover: {
		AdminWebBootstrap: "holding a module means the app opens. This role grants nothing openable today, and on STG all three holders (Dinakar, Chandrakant, Avishek) also carry a job role -- so no real person is affected. Verified read-only against STG on 2026-08-24.",
		AppBootstrap:      "same as admin_web.bootstrap above",
	},
	RoleToxinTester: {
		AdminWebBootstrap: "holding a module means the app opens. Like counts_approver, this is a per-person authority granted BY NAME alongside a job (maintainer decision 2026-08-25: the two named PARK HEADS), so every holder already carries a job role that admits them to the surface.",
		AppBootstrap:      "same as admin_web.bootstrap above -- and the strip test is run ON the phone, so the mobile surface is the one that matters here",
	},
	RoleParkHead: {
		VerificationReview: "he holds verification.act today WITHOUT verification.review -- able to close or send back work he cannot see. This closes that gap rather than widening authority",
		OperatorsViewAudit: "reading the audit trail of the park team he already manages",
	},
	RoleFeedDirector:   {VerificationReview: "holds verification.act without verification.review today -- acting blind on his own module's queue"},
	RoleGrowthDirector: {VerificationReview: "holds verification.act without verification.review today -- acting blind on his own module's queue"},
	RoleHealthDirector: {VerificationReview: "holds verification.act without verification.review today -- acting blind on his own module's queue"},
}

// gainAccepted reports whether a gained permission is a reviewed decision for this role.
func gainAccepted(role, permission string) bool {
	if _, ok := benignReadGains[permission]; ok {
		return true
	}
	if IsOrgRoleKey(role) {
		if _, ok := orgGridGains[permission]; ok {
			return true
		}
	}
	if perms, ok := namedRoleGains[role]; ok {
		if _, ok := perms[permission]; ok {
			return true
		}
	}
	return false
}

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

			if len(lost) > 0 {
				t.Errorf("LOSS -- %q holds these today and would NOT tomorrow; every person "+
					"carrying this role is locked out of that work:\n  %s",
					role, strings.Join(lost, "\n  "))
			}
			unreviewed := make([]string, 0, len(gained))
			for _, p := range gained {
				if !gainAccepted(role, p) {
					unreviewed = append(unreviewed, p)
				}
			}
			if len(unreviewed) > 0 {
				t.Errorf("UNREVIEWED GAIN -- %q does NOT hold these today and would tomorrow. "+
					"Either tighten the catalog or record the decision in benignReadGains / "+
					"orgGridGains / namedRoleGains with a reason:\n  %s",
					role, strings.Join(unreviewed, "\n  "))
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
		// breeding_director layered on 2026-09-04 (maintainer decision: he plans hoof / hair trimming).
		"dinakar":     {RoleBreedingDirector, RoleCountsApprover, RoleGrowthDirector, RoleOperator, RoleParkHead, RolePCDirector},
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

			// A stack inherits whatever each of its roles was individually allowed to gain;
			// anything else means the union merge itself introduced a difference.
			for _, p := range difference(today, tomorrow) {
				t.Errorf("LOSS on stacked person %q (%s): %s", name, strings.Join(roles, "+"), p)
			}
			for _, p := range difference(tomorrow, today) {
				accepted := false
				for _, role := range roles {
					if gainAccepted(role, p) {
						accepted = true
						break
					}
				}
				if !accepted {
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
