package domain

import (
	"sort"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// ParkAuthorities is the ONE definition of "this grant is relevant to vaccination
// execution", shared by the HTTP handler's park-scope resolution and the Postgres
// repository's in-query park filter.
//
// It exists because the module had grown THREE disagreeing definitions: the handler's
// tenant-wide check accepted VaccinationCampaign alone, the repository's tenant-wide check
// accepted VaccinationRead alone, and the two park sets differed from each other and from
// both. Disagreement between a gate and the filter behind it does not fail loudly — it
// fails as an EMPTY SCREEN. A tenant-wide Park Head (Oversee, no Campaign) passed the
// repository's tenant-wide check but not the handler's; a park-scoped Campaign-only grant
// would pass the handler and then be filtered to nothing by the repository. Neither
// produces a 403 a user can act on. One list, imported by both adapters, is the only way
// the gate and the filter cannot drift apart.
//
// The set is derived from what the ROUTE TABLE actually admits (permissions/routes.go),
// because a park set narrower than the route gate turns every admitted-but-unlisted role
// into a blanket 403 on a screen it is entitled to:
//
//   - VaccinationRead + ObligationRead gate every /vaccination/* read route. These are the
//     honest basis for READ access and are what Verifier, the vaccination manager and
//     assistant-manager tiers hold. Omitting them 403'd a park-scoped vaccination_manager
//     out of their OWN park's execution list.
//   - VaccinationOverseeExecution is the park head / director oversight authority.
//   - TaskExecute is the operator's own authority; the operator app routes gate on
//     AppBootstrap alone and the operator holds no admin-tier read permission.
//   - VaccinationCampaign is the planner/CEO scheduling-write authority. A park-scoped
//     planner must resolve their own park.
//
// This is NOT a return to capability-blind resolution: every entry is a vaccination- or
// task-relevant capability, and it is still each GRANT's own role that must carry one, so
// an unrelated grant (a growth-director weighing grant in park A) still cannot lend park A
// to a vaccination grant held in park B.
//
// AppBootstrap is deliberately NOT here even though the /app/vaccination/* routes gate on
// it: every authenticated principal holds AppBootstrap, so including it would make the set
// capability-blind by the back door and reopen exactly the cross-park hole this resolution
// exists to close.
var ParkAuthorities = []string{
	permissions.VaccinationRead,
	permissions.ObligationRead,
	permissions.VaccinationOverseeExecution,
	permissions.TaskExecute,
	permissions.VaccinationCampaign,
}

// HasTenantWideAuthority reports whether the actor holds a TENANT-scoped grant whose role
// carries vaccination-execution authority — the "no park restriction" escape hatch.
//
// Scope alone is not enough: a tenant-wide grant for an unrelated role (a growth_director
// scoped to the whole tenant for weighing) must not unlock vaccination execution. Role
// alone is not enough either: a vaccination role scoped to one park is park-scoped. Both
// halves are checked on the SAME grant.
//
// It tests the SAME ParkAuthorities the park path uses. When the two lists differed, an
// actor could be tenant-wide by one adapter's rule and park-scoped by the other's, which is
// how the tenant-wide Park Head ended up passing the handler and being filtered to zero rows.
func HasTenantWideAuthority(grants []permissions.ActiveGrant, tenantID string) bool {
	for _, grant := range grants {
		if grant.ScopeType != "tenant" || grant.ScopeID != tenantID {
			continue
		}
		for _, authority := range ParkAuthorities {
			if permissions.RoleHasPermission(grant.Role, authority) {
				return true
			}
		}
	}
	return false
}

// AuthorizedParks returns the parks in which the actor holds a vaccination-execution
// authority, capability-aware and deduplicated. A non-nil EMPTY slice means "no park" and
// must be treated as matching nothing, never as "all parks".
func AuthorizedParks(grants []permissions.ActiveGrant) []string {
	seen := map[string]struct{}{}
	parks := []string{}
	for _, authority := range ParkAuthorities {
		for _, parkID := range permissions.ScopeIDsForPermission(grants, authority, "park") {
			if _, ok := seen[parkID]; ok {
				continue
			}
			seen[parkID] = struct{}{}
			parks = append(parks, parkID)
		}
	}
	sort.Strings(parks)
	return parks
}
