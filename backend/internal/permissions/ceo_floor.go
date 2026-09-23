package permissions

// THE CEO/CXO FLOOR (maintainer decision 2026-09-08).
//
// The founder/builder cohort (`ceo_internal`, see AGENTS.md "Founder/builder visibility
// invariant") must see and open EVERY built admin-web page, always. Since the 2026-08-24
// cutover a person's stored `person_module_access` rows DECIDE their permissions and their
// sidebar, and those rows are a SNAPSHOT: every module shipped after a CEO's rows were
// backfilled arrived with no row for it (sale_allocation needed migration 000245 as a
// repair; pc_trimming had none at all), and any tick cleared on /people took a page away
// from the CEO exactly as it would from an operator. The visible symptom each time was the
// same -- a missing sidebar leaf and a 403 on the data route for the one principal who is
// supposed to hold everything.
//
// The rule is therefore a FLOOR, applied on both halves of the capability-gated lock:
//
//  1. REQUEST PATH (httpmiddleware.decideAuthorization): a `ceo_internal` principal is
//     authorized whenever the ROLE path authorizes, whatever their person rows say, and
//     their scope stays tenant-wide. Person rows may only ever ADD for the CEO.
//  2. BOOTSTRAP (adminui/app.personPageAccessFor): a `ceo_internal` principal is never
//     page-narrowed; the full role-composed contract is served.
//  3. CATALOG (TestCEOFloorReachesEveryWebModuleAndPage): the `ceo_internal` role must hold
//     every permission every ModulePage needs, and the CEO row map must carry a web row for
//     every web-surface module and tick every page -- so a new module or page that forgets
//     the CEO fails the build instead of the CEO's sidebar.
//
// It is a floor and not a bypass: the role's own permission set still encodes the recorded
// exclusions (verification.verdict is verifier-only, toxin.execute is tester-only), and the
// floor never grants past the role. Nothing here touches the phone module bar.

// CEOFloorApplies reports whether the CEO/CXO floor governs this principal.
func CEOFloorApplies(roles []string) bool {
	for _, role := range roles {
		if role == RoleCEOInternal {
			return true
		}
	}
	return false
}
