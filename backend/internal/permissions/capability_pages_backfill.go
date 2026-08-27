package permissions

import "sort"

// The one-time translation of the RETIRED admin-web lenses into page ticks
// (maintainer decision 2026-08-27).
//
// The procurement-director lens (2026-08-21) narrowed the whole compiled contract for
// anyone holding that role: keep only the Procurement and Feed groups, and hide Feed
// Config. That was CODE. This file turns it into the person's own stored rows, once, at
// backfill time, so the same person sees the same sidebar the morning after -- and so the
// next change to it is a tick rather than a commit.
//
// It runs at BACKFILL time only. Nothing here is read on the request path: after the
// migration, access comes from the person's stored rows, and editing this file changes
// nothing for anyone already migrated.

// retiredProcurementDirectorWebModules are the web modules the retired lens left reachable.
// Everything else was dropped from the sidebar AND from the page contracts, so keeping the
// module here would grant a screen the person could not open under the rule being preserved.
var retiredProcurementDirectorWebModules = map[string]struct{}{
	"procurement":    {},
	"vendors":        {},
	"sales":          {},
	"feed_purchases": {},
	"feed_direction": {},
}

// retiredProcurementDirectorFeedPages is the second half of the 2026-08-21 decision --
// "hide the Feed Config page for him under Feed". Feed Config is the AUTHORED ration grid,
// the Feed Director's own instrument, and this workspace is read-only feed oversight.
var retiredProcurementDirectorFeedPages = []string{"feed-analytics", "feed-sops"}

// applyRetiredProcurementDirectorNarrowing rewrites a person's backfilled rows to what the
// retired lens actually served them.
//
// It touches WEB ROWS ONLY. The holder is also the Feed Director, and his phone access and
// feed-proof ownership ride on that role; the lens never reached the phone and neither does
// this. Because resolved permissions UNION across surfaces, anything he also holds on
// mobile (the verification verdict work, most of all) is untouched by dropping the web row.
func applyRetiredProcurementDirectorNarrowing(in []ModuleAssignment) []ModuleAssignment {
	out := make([]ModuleAssignment, 0, len(in))
	for _, row := range in {
		if row.Surface != SurfaceWeb {
			out = append(out, row)
			continue
		}
		if _, kept := retiredProcurementDirectorWebModules[row.Module]; !kept {
			continue
		}
		if row.Module == "feed_direction" {
			row.Pages = append([]string(nil), retiredProcurementDirectorFeedPages...)
		}
		out = append(out, row)
	}
	return out
}

// leadershipBarRoles are the roles whose PHONE module set is composed from their permissions
// rather than from their department (workforce/app.leadershipGrantRoles, which this mirrors
// for the one-time backfill). Their bar never came from department_module_grants, so it must
// not be intersected with it.
var leadershipBarRoles = map[string]struct{}{
	RoleCEOInternal:    {},
	RolePCDirector:     {},
	RoleGrowthDirector: {},
	RoleFeedDirector:   {},
	RoleHealthDirector: {},
	RoleParkHead:       {},
	RoleVerifier:       {},
}

// NarrowMobileToDepartmentBar trims a person's PHONE rows to the modules their department
// actually grants today.
//
// The role mapping is a superset by construction: `operator` maps to nine modules because
// SOME operator's department grants each of them, while any ONE operator's department grants
// six. That was harmless while the bar came from department_module_grants and the ticks only
// carried permissions -- but the moment the phone reads the ticks, the extra rows become
// modules that operator never had. A live run showed exactly that: Health appeared on an
// operator's bar for the first time.
//
// So the backfill intersects, once, per person. `dept` empty means this person's bar was
// never department-composed (leadership, a verifier, or someone with no department at all),
// and nothing is trimmed.
func NarrowMobileToDepartmentBar(roles []string, dept []string, assignments []ModuleAssignment) []ModuleAssignment {
	if len(dept) == 0 {
		return assignments
	}
	for _, r := range roles {
		if _, leadership := leadershipBarRoles[r]; leadership {
			return assignments
		}
	}
	allowed := make(map[string]struct{}, len(dept))
	for _, d := range dept {
		allowed[d] = struct{}{}
	}
	out := make([]ModuleAssignment, 0, len(assignments))
	for _, a := range assignments {
		if a.Surface == SurfaceMobile {
			if _, ok := allowed[a.Module]; !ok {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// NarrowForRetiredLenses applies every recorded lens narrowing that a person's retired
// roles used to receive as code. Called by the backfill, never at request time.
//
// ceo_internal is exempt for the same reason the lens exempted it: a lens must never narrow
// a leadership principal.
func NarrowForRetiredLenses(roles []string, assignments []ModuleAssignment) []ModuleAssignment {
	hasRole := func(want string) bool {
		for _, r := range roles {
			if r == want {
				return true
			}
		}
		return false
	}
	if hasRole(RoleCEOInternal) {
		return assignments
	}
	if hasRole(RoleProcurementDirector) {
		assignments = applyRetiredProcurementDirectorNarrowing(assignments)
	}
	return assignments
}

// FillDefaultPages stamps the full page list onto every web row that has none, so the
// backfilled rows say explicitly which screens a person keeps.
//
// The stored default could equally be an empty list -- PageAccessForAssignments reads empty
// as "every page" -- but an explicit list is what the access editor shows as ticked boxes,
// and a screen that opens with everything blank on a person who can reach everything reads
// as broken.
func FillDefaultPages(in []ModuleAssignment) []ModuleAssignment {
	out := make([]ModuleAssignment, len(in))
	copy(out, in)
	// Only screens this person can actually OPEN are stamped. A stored tick for a screen
	// their capabilities do not reach resolves to nothing at read time and would be refused
	// on the next save, so writing one leaves a row the editor cannot round-trip.
	held := PermissionsForAssignments(in)
	for i := range out {
		if out[i].Surface != SurfaceWeb {
			continue
		}
		openable := OpenablePagesForModuleWithHeld(out[i].Module, out[i].Capabilities, held)
		if len(openable) == 0 {
			// Either a module with no sidebar leaf of its own (herd_register, toxin,
			// pc_care, config, locations, verification_policy), or one held at a level that
			// opens none of its screens. Nothing to tick, and that is a real answer.
			out[i].Pages = nil
			continue
		}
		allowed := make(map[string]struct{}, len(openable))
		order := make([]string, 0, len(openable))
		for _, p := range openable {
			allowed[p.Key] = struct{}{}
			order = append(order, p.Key)
		}
		if len(out[i].Pages) == 0 {
			out[i].Pages = order
			continue
		}
		// A list set by the retired-lens narrowing is PRUNED to what is openable, not taken
		// on trust. The lens named Feed SOP for the Procurement Director, and that screen
		// 403s on its own data for him -- carrying the name forward would have stored a tick
		// that renders a dead leaf and that the next save refuses.
		kept := make([]string, 0, len(out[i].Pages))
		for _, key := range order {
			for _, want := range out[i].Pages {
				if want == key {
					kept = append(kept, key)
					break
				}
			}
		}
		out[i].Pages = kept
	}
	for i := range out {
		sort.Strings(out[i].Pages)
	}
	return out
}
