package permissions

import "sort"

// One-time backfill mapping: what each retired ROLE becomes in the per-person model.
//
// This is the safety-critical half of the 2026-08-24 cutover. Every existing person is
// migrated by expanding their current role(s) through this map, so their effective
// permission set on the morning after release is the set they had the night before.
// capability_parity_test.go proves that claim role by role and fails on any unreviewed
// difference.
//
// After the migration runs, the ROLE MAPS below (flatRoleAssignments, tierAssignments,
// verticalModule) are DEAD DATA kept for audit: access comes from the person's own stored
// rows, and changing a mapping there changes nothing for anyone already migrated. They are
// not a second, parallel access model -- do not read them at request time.
//
// Two things in this file ARE request-path and must not be mistaken for backfill scaffolding:
// surfaceBaseline, and PermissionsForAssignmentsWithBaseline at the bottom, which is the
// function every authenticated request resolves a principal's permissions through. They live
// here so the baseline reads sit beside the parity proof that justifies each one.

// surfaceBaseline is granted to a principal holding at least one module on that surface.
// These are the "you work here" reads every role carrying the surface already had:
// admission to the surface, the work list, and the location directory every screen labels
// itself with. They are baseline precisely because making them assignable would let someone
// be given a module whose screen cannot render its own shed names.
var surfaceBaseline = map[string][]string{
	SurfaceWeb:    {AdminWebBootstrap, LocationsRead},
	SurfaceMobile: {AppBootstrap, LocationsRead, TaskRead},
}

// caps is a readability helper -- an assignment's capability set, in catalog order.
func caps(levels ...string) []string {
	out := make([]string, 0, len(levels))
	for _, want := range LevelOrder {
		for _, got := range levels {
			if got == want {
				out = append(out, want)
				break
			}
		}
	}
	return out
}

// assign builds one row.
func assign(module, surface string, levels ...string) ModuleAssignment {
	return ModuleAssignment{Module: module, Surface: surface, Capabilities: caps(levels...)}
}

// bothSurfaces is the common case: the same capabilities on web and phone.
func bothSurfaces(module string, levels ...string) []ModuleAssignment {
	return []ModuleAssignment{
		assign(module, SurfaceWeb, levels...),
		assign(module, SurfaceMobile, levels...),
	}
}

func rows(groups ...[]ModuleAssignment) []ModuleAssignment {
	out := make([]ModuleAssignment, 0, 24)
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

func one(a ModuleAssignment) []ModuleAssignment { return []ModuleAssignment{a} }

// flatRoleAssignments is the retired FLAT role catalog expressed in the new vocabulary.
// Read each entry as "this job title meant these modules with this much authority".
var flatRoleAssignments = map[string][]ModuleAssignment{
	// The operator is phone-only field work: execute, capture, record. No web at all.
	RoleOperator: rows(
		one(assign("vaccination", SurfaceMobile, LevelView, LevelDo)),
		one(assign("weighing", SurfaceMobile, LevelView, LevelDo)),
		one(assign("counts", SurfaceMobile, LevelView, LevelDo)),
		// The kid-milk round. Its own module since the 2026-07-31 split, and the operator's
		// department grants it today -- omitting it here would take Milk off their bar the
		// moment the phone started reading these ticks.
		one(assign("milk", SurfaceMobile, LevelDo)),
		one(assign("feed_direction", SurfaceMobile, LevelView, LevelDo)),
		one(assign("aas_health", SurfaceMobile, LevelView, LevelDo)),
		one(assign("pc_care", SurfaceMobile, LevelView, LevelDo)),
		one(assign("procurement", SurfaceMobile, LevelView, LevelDo)),
		one(assign("herd_register", SurfaceMobile, LevelView)),
		one(assign("calendar", SurfaceMobile, LevelView)),
	),
	// The park head runs a park's execution from the phone: supervises task work, completes
	// feed on his own ground, captures counts. Deliberately NOT admin-web.
	RoleParkHead: rows(
		one(assign("vaccination", SurfaceMobile, LevelView, LevelOversee)),
		one(assign("counts", SurfaceMobile, LevelView, LevelDo)),
		one(assign("milk", SurfaceMobile, LevelDo)),
		one(assign("feed_direction", SurfaceMobile, LevelView, LevelDo)),
		one(assign("aas_health", SurfaceMobile, LevelView)),
		one(assign("procurement", SurfaceMobile, LevelView, LevelDo, LevelOversee)),
		one(assign("people", SurfaceMobile, LevelView, LevelDo, LevelOversee)),
		one(assign("verification", SurfaceMobile, LevelOversee)),
		one(assign("herd_register", SurfaceMobile, LevelView)),
		one(assign("calendar", SurfaceMobile, LevelView, LevelDo)),
		one(assign("config", SurfaceMobile, LevelView)),
	),
	// PC Director owns vaccination end to end and executes it too -- unusual for a director,
	// preserved rather than tidied away: removing it would stop him covering a shed.
	RolePCDirector: rows(
		one(assign("leadership_tasks", SurfaceMobile, LevelView, LevelDo)),
		bothSurfaces("vaccination", LevelView, LevelDo, LevelOversee, LevelConfigure),
		bothSurfaces("aas_health", LevelOversee),
		bothSurfaces("pc_care", LevelView, LevelDo, LevelOversee),
		bothSurfaces("people", LevelView, LevelDo, LevelOversee),
		bothSurfaces("herd_register", LevelView, LevelDo),
		one(assign("verification", SurfaceWeb, LevelConfigure)),
		one(assign("procurement", SurfaceWeb, LevelView)),
		one(assign("calendar", SurfaceWeb, LevelView, LevelDo)),
		one(assign("config", SurfaceWeb, LevelView)),
	),
	// Growth Director runs Weighing and ONLY Weighing (maintainer decision 2026-08-01). He
	// monitors, oversees the operators and executes -- and does NOT plan, which is CEO-only.
	// He holds no health-module access at all; his GoatWriteHealth rides on herd_register.
	RoleGrowthDirector: rows(
		one(assign("leadership_tasks", SurfaceMobile, LevelView, LevelDo)),
		bothSurfaces("weighing", LevelView, LevelDo, LevelOversee),
		bothSurfaces("people", LevelView, LevelDo, LevelOversee),
		bothSurfaces("herd_register", LevelView, LevelDo),
		one(assign("verification", SurfaceWeb, LevelOversee)),
		one(assign("procurement", SurfaceWeb, LevelView)),
		one(assign("calendar", SurfaceWeb, LevelView, LevelDo)),
		one(assign("config", SurfaceWeb, LevelView)),
	),
	// Feed Director: authors the ration AND oversees the chain, and still cannot record a
	// task as done. LevelDo is deliberately ABSENT from feed_direction -- that is the whole
	// point of capabilities being a set rather than a ladder.
	RoleFeedDirector: rows(
		one(assign("leadership_tasks", SurfaceMobile, LevelView, LevelDo)),
		bothSurfaces("feed_direction", LevelView, LevelOversee, LevelConfigure),
		one(assign("feed_purchases", SurfaceWeb, LevelView)),
		bothSurfaces("people", LevelView, LevelDo, LevelOversee),
		one(assign("herd_register", SurfaceWeb, LevelView)),
		one(assign("verification", SurfaceWeb, LevelOversee)),
		one(assign("procurement", SurfaceWeb, LevelView)),
		one(assign("calendar", SurfaceWeb, LevelView, LevelDo)),
		one(assign("config", SurfaceWeb, LevelView)),
	),
	// Health Director authors the treatment rulebook and is the declared OWNER of Counts
	// without Counts access -- counts at LevelView is the alerts read alone, never the
	// screens (AGENTS.md: the module is off, and ownership is not access).
	RoleHealthDirector: rows(
		one(assign("leadership_tasks", SurfaceMobile, LevelView, LevelDo)),
		bothSurfaces("aas_health", LevelConfigure),
		bothSurfaces("people", LevelView, LevelDo, LevelOversee),
		bothSurfaces("herd_register", LevelView, LevelDo, LevelOversee),
		one(assign("counts", SurfaceWeb, LevelView)),
		one(assign("verification", SurfaceWeb, LevelOversee)),
		one(assign("procurement", SurfaceWeb, LevelView)),
		one(assign("calendar", SurfaceWeb, LevelView, LevelDo)),
		one(assign("config", SurfaceWeb, LevelView)),
	),
	// The vendor desk, plus reading source entry. Vendors is on BOTH surfaces (maintainer
	// decision 2026-09-03): the register and feed purchases, view and add, on the phone.
	RoleProcurementManager: rows(
		one(assign("procurement", SurfaceWeb, LevelView)),
		one(assign("feed_purchases", SurfaceWeb, LevelView, LevelDo)),
		one(assign("load_costs", SurfaceWeb, LevelDo)),
		bothSurfaces("vendors", LevelView, LevelDo, LevelOversee),
		// Sales on this desk too (maintainer instruction 2026-09-04): the Procurement phone
		// module's Sales tab records a sale and tags its animals; the web pages follow the same
		// permissions.
		// Sales is on BOTH surfaces from 2026-09-05: it became its own phone module (the ledger
		// moved out of the Procurement module and took the selling half of the vendor register
		// with it). Migration 000257 copies the same mobile row onto everyone already backfilled.
		bothSurfaces("sales", LevelView, LevelDo),
		one(assign("sale_allocation", SurfaceWeb, LevelDo)),
	),
	// Web-only except Vendors. Procurement Director keeps Sales Config, Vendors, Feed Purchases,
	// and sees Feed Analytics stock only (the Hemant case). Source Entry is intentionally absent.
	// Vendors is on BOTH surfaces (maintainer decision 2026-09-03): the Procurement phone module.
	RoleProcurementDirector: rows(
		one(assign("leadership_tasks", SurfaceMobile, LevelView, LevelDo)),
		// Sales is on BOTH surfaces from 2026-09-05: it became its own phone module (the ledger
		// moved out of the Procurement module and took the selling half of the vendor register
		// with it). Migration 000257 copies the same mobile row onto everyone already backfilled.
		bothSurfaces("sales", LevelView, LevelDo),
		one(assign("sale_allocation", SurfaceWeb, LevelDo)),
		bothSurfaces("vendors", LevelView, LevelDo, LevelOversee),
		one(assign("feed_purchases", SurfaceWeb, LevelView, LevelDo)),
		one(assign("load_costs", SurfaceWeb, LevelDo)),
		one(assign("feed_direction", SurfaceWeb, LevelStock)),
	),
	// Breeding Director (maintainer decision 2026-09-04): reads the Preventive Care board and
	// plans hoof / hair trimming through the pc_trimming row. No Do on either -- a planner
	// does not film the work they planned. People at View is the operator directory the
	// create wizard's assignee picker reads (operators.read).
	RoleBreedingDirector: rows(
		one(assign("leadership_tasks", SurfaceMobile, LevelView, LevelDo)),
		bothSurfaces("pc_care", LevelView),
		bothSurfaces("pc_trimming", LevelView, LevelConfigure),
		bothSurfaces("people", LevelView),
	),
	// Granted BY NAME alongside a job (maintainer decision 2026-08-05). Carries approval
	// authority and nothing else -- no read, no write. LevelView is deliberately absent.
	// Approving is its OWN module on both surfaces (the 2026-08-05 decision that put the
	// queue back on the phone as a separate module rather than a tab inside Counts). The
	// `counts` row is kept alongside it because the retired role's three approve permissions
	// resolve from either -- dropping it would change what this person holds.
	RoleCountsApprover: rows(
		bothSurfaces("counts", LevelOversee),
		bothSurfaces("approvals", LevelOversee),
	),
	// Granted BY NAME to the park heads who run the strip test (maintainer decision
	// 2026-08-25), the same per-person shape as counts_approver. Carries testing
	// authority and nothing else.
	RoleToxinTester: bothSurfaces("toxin", LevelView, LevelDo),
	// The verifier casts verdicts and does not carry out the work being judged. This is the
	// one principal for whom verification at LevelDo is correct rather than a risk.
	RoleVerifier: rows(
		bothSurfaces("verification", LevelView, LevelDo),
		bothSurfaces("vaccination", LevelView),
		one(assign("feed_direction", SurfaceWeb, LevelView)),
		one(assign("procurement", SurfaceWeb, LevelView, LevelOversee)),
		one(assign("people", SurfaceWeb, LevelView)),
		one(assign("herd_register", SurfaceWeb, LevelView, LevelOversee)),
		one(assign("locations", SurfaceWeb, LevelView, LevelOversee)),
		one(assign("calendar", SurfaceWeb, LevelView)),
		one(assign("config", SurfaceWeb, LevelView)),
	),
	// Whole-org. Note weighing and pc_care at View+Configure and NOT Do: the CEO plans that
	// work and never carries it out.
	RoleCEOInternal: rows(
		// Leadership Tasks (2026-09-04): the desk the directors write to.
		one(assign("leadership_tasks", SurfaceMobile, LevelView, LevelOversee)),
		bothSurfaces("vaccination", LevelView, LevelOversee, LevelConfigure),
		bothSurfaces("weighing", LevelView, LevelConfigure),
		bothSurfaces("pc_care", LevelView, LevelConfigure),
		bothSurfaces("counts", LevelView, LevelDo, LevelOversee, LevelConfigure),
		bothSurfaces("feed_direction", LevelView, LevelOversee, LevelConfigure),
		bothSurfaces("aas_health", LevelView, LevelDo, LevelOversee, LevelConfigure),
		bothSurfaces("people", LevelView, LevelDo, LevelOversee, LevelConfigure),
		bothSurfaces("herd_register", LevelView, LevelDo, LevelOversee, LevelConfigure),
		one(assign("procurement", SurfaceWeb, LevelView, LevelDo, LevelOversee)),
		bothSurfaces("vendors", LevelView, LevelDo, LevelOversee),
		// Sales is on BOTH surfaces from 2026-09-05: it became its own phone module (the ledger
		// moved out of the Procurement module and took the selling half of the vendor register
		// with it). Migration 000257 copies the same mobile row onto everyone already backfilled.
		bothSurfaces("sales", LevelView, LevelDo),
		one(assign("sale_allocation", SurfaceWeb, LevelDo)),
		one(assign("verification", SurfaceWeb, LevelConfigure)),
		one(assign("config", SurfaceWeb, LevelView, LevelDo, LevelConfigure)),
		one(assign("locations", SurfaceWeb, LevelView, LevelDo, LevelOversee, LevelConfigure)),
		one(assign("calendar", SurfaceWeb, LevelView, LevelDo)),
		one(assign("operations", SurfaceWeb, LevelOversee)),
		one(assign("herd_signals", SurfaceWeb, LevelView, LevelDo, LevelConfigure)),
		// Watches and judges the strip test; never runs one (2026-08-26).
		bothSurfaces("toxin", LevelView, LevelOversee),
		one(assign("feed_purchases", SurfaceWeb, LevelView, LevelDo)),
		one(assign("load_costs", SurfaceWeb, LevelDo)),
		one(assign("verification_policy", SurfaceWeb, LevelConfigure)),
		// Clock In / Out presence oversight (2026-08-28): the CEO/CXO sees who
		// is at work on both surfaces; punching itself is baseline, not a tick.
		bothSurfaces("clock", LevelOversee),
		// The kid-milk round and the approval queue. Both are real modules the leadership
		// drawer offered before the phone read these ticks, and the founder/builder
		// visibility invariant says leadership holds every BUILT module -- so omitting them
		// here took Milk and Approvals off the CEO's phone.
		bothSurfaces("milk", LevelDo, LevelConfigure),
		bothSurfaces("approvals", LevelOversee),
		// The roadmap row. Offered to the CEO today by the curated leadership drawer, so it
		// is ticked here to keep it. It grants nothing -- `breeding` opens no screen.
		one(assign("breeding", SurfaceMobile, LevelView)),
	),
}

// tierAssignments expresses the org-role grid (tier x vertical, 36 composite roles) in the
// new vocabulary. The grid is ALREADY the same idea this change generalises -- a tier is an
// authority level and a vertical is a module -- so it maps almost one to one. It is mapped
// by TIER here and the vertical's own module is added by verticalModule below, exactly the
// way permissions_orgrole.go composes tierPermissions with its per-vertical additions.
//
// These 36 roles are dormant catalog scaffolding (AGENTS.md); only am_health and
// manager_health are granted on STG today.
var tierAssignments = map[Tier][]ModuleAssignment{
	// Assistant Manager supervises ground execution; the Operator role owns capture. Counts
	// carries Do AND Oversee together -- this tier records the count and approves it, which
	// is the case a single-select ladder could not express.
	TierAssistantManager: rows(
		bothSurfaces("vaccination", LevelView),
		bothSurfaces("counts", LevelView, LevelDo, LevelOversee),
		bothSurfaces("procurement", LevelView, LevelDo),
		bothSurfaces("herd_register", LevelView),
		bothSurfaces("calendar", LevelView),
	),
	// Manager runs the vertical's daily ops at a park and manages the local roster.
	TierManager: rows(
		bothSurfaces("vaccination", LevelView),
		bothSurfaces("counts", LevelView, LevelDo, LevelOversee),
		bothSurfaces("procurement", LevelView, LevelDo),
		bothSurfaces("people", LevelView, LevelDo),
		bothSurfaces("herd_register", LevelView),
		bothSurfaces("calendar", LevelView, LevelDo),
	),
	// Head -- park/vertical oversight and standards; acts on verified items. No capture, so
	// counts carries Oversee (approve) WITHOUT Do (record).
	TierHead: rows(
		bothSurfaces("vaccination", LevelView, LevelOversee),
		bothSurfaces("counts", LevelView, LevelOversee),
		bothSurfaces("procurement", LevelView, LevelOversee),
		bothSurfaces("people", LevelView, LevelDo, LevelOversee),
		bothSurfaces("herd_register", LevelView),
		bothSurfaces("calendar", LevelView, LevelDo),
		one(assign("verification", SurfaceWeb, LevelOversee)),
		one(assign("config", SurfaceWeb, LevelView)),
	),
	// Director -- owns the vertical: plan, set SOPs/protocols, act. No capture, no verify.
	TierDirector: rows(
		bothSurfaces("vaccination", LevelView, LevelOversee, LevelConfigure),
		bothSurfaces("counts", LevelView, LevelOversee),
		bothSurfaces("procurement", LevelView),
		bothSurfaces("people", LevelView, LevelDo, LevelOversee),
		bothSurfaces("herd_register", LevelView, LevelDo),
		bothSurfaces("calendar", LevelView, LevelDo),
		one(assign("verification", SurfaceWeb, LevelOversee)),
		one(assign("config", SurfaceWeb, LevelView, LevelConfigure)),
	),
}

// verticalModule adds the vertical's OWN module on top of the tier set, mirroring the
// per-vertical additions in permissions_orgrole.go's init().
func verticalModule(tier Tier, vertical Vertical) []ModuleAssignment {
	senior := tier == TierManager || tier == TierHead || tier == TierDirector
	switch vertical {
	case VerticalHealth:
		// Raising a sick-goat report is field work every health tier does; clinical diagnosis
		// starts at Manager (maintainer decision 2026-07-30).
		if senior {
			return bothSurfaces("aas_health", LevelView, LevelOversee)
		}
		return bothSurfaces("aas_health", LevelView)
	case VerticalFeed:
		// Oversight tiers read the feed dispatch sheet through Feed's own permission.
		if tier == TierHead || tier == TierDirector {
			return bothSurfaces("feed_direction", LevelView)
		}
	case VerticalSales:
		if senior {
			return one(assign("sales", SurfaceWeb, LevelView, LevelDo))
		}
		return one(assign("sales", SurfaceWeb, LevelView))
	}
	return nil
}

// AssignmentsForRole reports the per-person rows a retired role expands into, for the flat
// roles and for every composite tier x vertical org role key. Used by the one-time migration
// and by the parity test; NOT a request-time path.
func AssignmentsForRole(role string) ([]ModuleAssignment, bool) {
	if flat, ok := flatRoleAssignments[role]; ok {
		out := make([]ModuleAssignment, len(flat))
		copy(out, flat)
		return mergeAssignments(out), true
	}
	tier, vertical, ok := ParseRoleKey(role)
	if !ok {
		return nil, false
	}
	base, ok := tierAssignments[tier]
	if !ok {
		return nil, false
	}
	combined := make([]ModuleAssignment, 0, len(base)+4)
	combined = append(combined, base...)
	combined = append(combined, verticalModule(tier, vertical)...)
	return mergeAssignments(combined), true
}

// AssignmentsForRoles merges the rows for a person carrying several stacked roles -- which
// is how the retired model expressed a real job (STG has one person wearing five). Where two
// roles name the same module and surface, the capability sets UNION, because the person
// demonstrably held the union of both roles' permissions. A union cannot drop a permission
// the way a rank-based merge could, which is why capabilities being a set matters here too.
func AssignmentsForRoles(roles []string) []ModuleAssignment {
	all := make([]ModuleAssignment, 0, 32)
	for _, role := range roles {
		rowsForRole, ok := AssignmentsForRole(role)
		if !ok {
			continue
		}
		all = append(all, rowsForRole...)
	}
	return mergeAssignments(all)
}

// mergeAssignments collapses duplicate (module, surface) rows by unioning their capability
// sets, and returns a stable order. Stability matters: these rows are diffed by the parity
// test, written by the migration, and rendered to a human.
func mergeAssignments(in []ModuleAssignment) []ModuleAssignment {
	byKey := make(map[string]map[string]struct{}, len(in))
	order := make([]string, 0, len(in))
	for _, row := range in {
		key := row.Module + "|" + row.Surface
		set, seen := byKey[key]
		if !seen {
			set = make(map[string]struct{}, 4)
			byKey[key] = set
			order = append(order, key)
		}
		for _, c := range row.Capabilities {
			set[c] = struct{}{}
		}
	}
	sort.Strings(order)

	out := make([]ModuleAssignment, 0, len(order))
	for _, key := range order {
		sep := -1
		for i := 0; i < len(key); i++ {
			if key[i] == '|' {
				sep = i
				break
			}
		}
		if sep < 0 {
			continue
		}
		levels := make([]string, 0, len(byKey[key]))
		for c := range byKey[key] {
			levels = append(levels, c)
		}
		out = append(out, ModuleAssignment{
			Module:       key[:sep],
			Surface:      key[sep+1:],
			Capabilities: caps(levels...),
		})
	}
	return out
}

// PermissionsForAssignmentsWithBaseline is PermissionsForAssignments plus the per-surface
// baseline reads. This is the function the request path uses; the bare form exists so the
// catalog can be tested without baseline noise.
func PermissionsForAssignmentsWithBaseline(assignments []ModuleAssignment) []string {
	granted := PermissionsForAssignments(assignments)
	set := make(map[string]struct{}, len(granted)+4)
	for _, p := range granted {
		set[p] = struct{}{}
	}
	// A surface is "in use" exactly when the catalog admitted at least one row for it --
	// re-derived from the bootstrap permission the bare form already added, so the two
	// functions cannot disagree about which surfaces a person holds.
	for surface, bootstrap := range surfaceBootstrap {
		if _, holds := set[bootstrap]; !holds {
			continue
		}
		for _, p := range surfaceBaseline[surface] {
			set[p] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
