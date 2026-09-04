package main

import (
	"github.com/vgoats/goatos/backend/internal/parkscope"
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	authallow "github.com/vgoats/goatos/backend/internal/platform/authallow"
)

// Extra authority is granted PER PERSON, by name (maintainer decision 2026-08-05, reaffirmed
// 2026-08-07).
//
// The maintainer's words were "keep rbac per person, not per group". Permissions in this system
// resolve from the ROLES on a caller's active user_scope_grants rows, and a caller may hold SEVERAL
// rows whose permissions union, so the way to express "this person, not this job" is to grant a
// narrow role to named individuals ALONGSIDE the job role they already hold.
//
// This list is therefore the authority itself, not a convenience: adding an email here gives that
// person the listed roles on BOTH the phone and admin-web; removing it takes them away and leaves
// their job role untouched. It deliberately does NOT widen permissions.RolePCDirector or
// permissions.RoleGrowthDirector, so nobody inherits approval power, or an operator's capture
// surface, by being appointed to one of those jobs later.
//
// The 2026-08-07 decision was taken against an explicit alternative: the maintainer was asked
// whether to move counts authority onto the pc_director ROLE instead (which would have handed it to
// every future holder of that job, reversed the one-module-one-director segregation lock, and
// required rewriting director_module_segregation_test.go plus the two approvals tests) and chose to
// keep it per person. Do not "simplify" this list into role permissions later.
//
// Deliberately keyed by EMAIL, not by Firebase UID: these people are seeded by DIFFERENT paths
// (Chandrakant is a UID-backed account in stgLoginAccounts; Dinakar is seeded by the weighing scope
// seed and his Firebase UID is not committed anywhere in this repo). Email is the one identifier
// both share, and rosterDisplayName below closes the UID gap without inventing one.
var perPersonGrants = []personGrant{
	{
		// Chandrakant — Preventive Care Director. UID-backed in stgLoginAccounts, so every role
		// here goes ACTIVE at seed time.
		email:             "chandrakanth119527@gmail.com",
		rosterDisplayName: "Chandrakant",
		roles: []string{
			permissions.RoleCountsApprover,
			// Toxin tester (maintainer decision 2026-08-25): he runs the SHF 001-A
			// aflatoxin strip test on purchased feed loads. Per person, never on the
			// director job — see permissions.RoleToxinTester's doc comment.
			permissions.RoleToxinTester,
			// stg-operator-scope: tenant approved — maintainer decision 2026-08-07. This is an
			// authority grant layered on a DIRECTOR, not a park staff account: he already holds
			// pc_director at tenant scope for both-park visibility, and a park-scoped operator row
			// here would contradict it. It does NOT put him in the vaccination drive operator pool:
			// that pool reads workforce_positions with position_tier <> 'director'
			// (obligation/adapters/postgres/visit_shot_lock.go), never the RBAC grant, and he holds
			// no workforce_positions row at all. The CPT rehearsal invariant "Chandrakant is
			// director-only monitoring scope" (AGENTS.md) therefore still holds.
			permissions.RoleOperator,
		},
	},
	{
		// Dinakar — Growth Director, additionally made a PC Director (maintainer decision
		// 2026-08-07). He keeps growth_director: Weighing ownership is documented on that role and
		// dropping it would leave Weighing with no accountable director. Roles union, so listing
		// both is additive, not a choice between them.
		//
		// No committed Firebase UID, so his user_id is resolved from his EXISTING workforce_members
		// roster row (see resolvePersonUserID) rather than derived from a UID. That row is already
		// bound to the user_id carrying his active operator grant, so these roles land on the same
		// principal he signs in as.
		email:             "babureddy315@gmail.com",
		rosterDisplayName: "Dinakar",
		roles: []string{
			permissions.RoleCountsApprover,
			// Toxin tester (maintainer decision 2026-08-25): same per-person grant as
			// Chandrakant's — the two of them run the aflatoxin strip test.
			permissions.RoleToxinTester,
			permissions.RolePCDirector,
			permissions.RoleGrowthDirector,
			// Breeding Director (maintainer decision 2026-09-04): his designation, and the desk
			// that plans HOOF and HAIR TRIMMING (pc_care.plan_trimming). Deworming and ticks
			// removal stay CEO-planned; pc_director above still carries no plan capability.
			permissions.RoleBreedingDirector,
			// stg-operator-scope: tenant approved — maintainer decision 2026-08-07. Same reasoning
			// as Chandrakant's row above, and his position_tier is already 'director', which the
			// operator pool query excludes. Listed explicitly even though he holds an active
			// operator grant today, so this file states his whole authority rather than half of it.
			permissions.RoleOperator,
		},
	},
	{
		// Hemant Singh — Feed Director, additionally made the Procurement Director (maintainer
		// decision 2026-08-21). He KEEPS feed_director: his phone access, the Feed proof
		// notification routing, and the feed-chain write authority all ride on that role, and the
		// maintainer's instruction was to change nothing on the app side. procurement_director is
		// admin-web-only (no AppBootstrap) and layers the Procurement + Feed web workspace on top
		// (backend/internal/adminui/app/procurement_director_lens.go).
		//
		// His feed_director grant itself was a manual STG seed
		// (workforce_members.metadata source "stg_feed_director_manual_seed_2026_08_10"), so it is
		// listed here explicitly the way Dinakar's operator grant is — this file states his whole
		// authority rather than half of it. No committed Firebase UID: his user_id resolves from
		// his EXISTING bound roster row ("Hemant"), the same path Dinakar uses.
		email:             "hemant@vgoats.com",
		rosterDisplayName: "Hemant",
		roles: []string{
			permissions.RoleFeedDirector,
			permissions.RoleProcurementDirector,
		},
	},
}

// personGrant is one named individual's extra roles, granted alongside whatever job role the
// stgLoginAccounts roster (or another seed) already gave them.
type personGrant struct {
	// email is the sign-in email and the key for the pending-grant path.
	email string
	// rosterDisplayName is the workforce_members.display_name used to resolve this person's
	// user_id when no Firebase UID is committed for them. Required for anyone absent from
	// stgLoginAccounts, since a pending row alone is NOT seed-complete per the STG login contract.
	rosterDisplayName string
	// roles are permissions.RoleXxx constants, each granted at TENANT scope.
	roles []string
}

// grantResult is one (person, role) outcome, reported per row so a silent miss is impossible.
type grantResult struct {
	email string
	role  string
	// active is true when an ACTIVE user_scope_grants row now exists.
	active bool
	// pending is true when the authority is only staged in auth_pending_email_grants and will
	// activate on the person's next sign-in.
	pending bool
	err     error
}

// seedPerPersonGrants grants every role in perPersonGrants to its named person.
//
// Two writes per (person, role), and BOTH are attempted:
//
//  1. A pending email grant. This works with no user_id at all and keeps the runtime claim path
//     working as a redundant second route.
//  2. An ACTIVE user_scope_grants row, whenever the user_id can be resolved — from a committed
//     Firebase UID, or failing that from the person's existing roster row. Per the STG login
//     contract a pending row alone is NOT seed-complete, because admin-web SSO does not reliably
//     trigger the claim on a fresh seed.
//
// Tenant scope, not park scope: these are authority/visibility grants layered on directors whose
// remit spans both parks. This does not violate the operator-scope invariant
// (make stg-operator-scope-guard) the way a park staff account would — see the annotated
// RoleOperator entries above.
func seedPerPersonGrants(ctx context.Context, pool *pgxpool.Pool, tenantID, authIssuer, source string) []grantResult {
	byEmail := make(map[string]Account, len(stgLoginAccounts))
	for _, acct := range stgLoginAccounts {
		byEmail[authallow.NormalizeEmail(acct.Email)] = acct
	}

	results := make([]grantResult, 0, len(perPersonGrants)*2)
	for _, person := range perPersonGrants {
		email := authallow.NormalizeEmail(person.email)

		// Resolved once per person: a roster lookup is a DB round trip and every role below lands
		// on the same principal.
		userID, err := resolvePersonUserID(ctx, pool, tenantID, authIssuer, byEmail, person, email)
		if err != nil {
			results = append(results, grantResult{email: email, role: "(user id)", err: err})
			continue
		}

		for _, role := range person.roles {
			res := grantResult{email: email, role: role}

			if err := upsertPendingRoleGrant(ctx, pool, tenantID, email, role, source); err != nil {
				res.err = fmt.Errorf("pending grant: %w", err)
				results = append(results, res)
				continue
			}
			res.pending = true

			if userID == "" {
				// Neither a committed UID nor a bound roster row: the pending row above is
				// genuinely the most this command can do. Reported as PENDING rather than OK so it
				// is never mistaken for a finished grant.
				results = append(results, res)
				continue
			}

			if err := materializeScopeGrant(ctx, pool, tenantID, userID, role, "tenant", tenantID); err != nil {
				res.err = fmt.Errorf("materialize %s grant: %w", role, err)
				results = append(results, res)
				continue
			}
			res.active = true
			results = append(results, res)
		}
		if userID != "" {
			// The roles above were materialized at tenant scope, which is right for a director
			// and wrong for anyone the People screen has narrowed. Re-derive from the authored
			// scope so a per-person role lands where the person's ticks say (internal/parkscope).
			if err := reconcilePersonScope(ctx, pool, tenantID, userID); err != nil {
				results = append(results, grantResult{email: email, role: "(park scope)", err: err})
			}
		}
	}
	return results
}

func reconcilePersonScope(ctx context.Context, pool *pgxpool.Pool, tenantID, userID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, _, err := parkscope.ReconcileUser(ctx, tx, tenantID, userID, ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// resolvePersonUserID returns the user_id these grants must land on, or "" when it cannot be
// determined (in which case only the pending path is available).
//
// Two sources, in order of authority:
//
//  1. A committed Firebase UID in stgLoginAccounts, run through the SAME
//     platformauth.StableSubjectID derivation the backend applies at request time.
//  2. The person's existing workforce_members roster row. This is what makes a person with no
//     committed UID seed-complete: the roster row's user_id is the principal a prior seed already
//     bound them to, so a grant written against it is the grant they sign in holding.
//
// A roster row that exists but carries no user_id yet is NOT an error: that person simply has not
// been bound by an earlier seed, and the pending path still covers them.
func resolvePersonUserID(
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID, authIssuer string,
	byEmail map[string]Account,
	person personGrant,
	email string,
) (string, error) {
	if acct, known := byEmail[email]; known && strings.TrimSpace(acct.FirebaseUID) != "" {
		return platformauth.StableSubjectID(authIssuer, acct.FirebaseUID), nil
	}
	if strings.TrimSpace(person.rosterDisplayName) == "" {
		return "", nil
	}

	var userID *string
	err := pool.QueryRow(ctx, `
SELECT user_id::text
FROM workforce_members
WHERE tenant_id = $1
  AND lower(display_name) = lower($2)
  AND status = 'active'
ORDER BY updated_at DESC
LIMIT 1`, tenantID, person.rosterDisplayName).Scan(&userID)
	if err != nil {
		if isNoRows(err) {
			return "", nil
		}
		return "", fmt.Errorf("resolve user_id from roster row %q: %w", person.rosterDisplayName, err)
	}
	if userID == nil {
		return "", nil
	}
	return *userID, nil
}

// upsertPendingRoleGrant stages one role for an email whose user_id may be unknown.
//
// Always tenant-scoped, so it does not reuse upsertPendingEmailGrant: that helper REVOKES the
// tenant row for a park-scoped account, which is right for a job role bound to one park and wrong
// for these.
func upsertPendingRoleGrant(ctx context.Context, pool *pgxpool.Pool, tenantID, email, role, source string) error {
	var pendingGrantID string
	return pool.QueryRow(ctx, `
INSERT INTO auth_pending_email_grants (
  tenant_id, email, normalized_email, role, scope_type, scope_id, status, valid_from, source
) VALUES (
  $1, $2, $2, $3, 'tenant', $1, 'active', now(), $4
)
ON CONFLICT (tenant_id, normalized_email, role, scope_type, scope_id)
  WHERE status = 'active' AND valid_to IS NULL
DO UPDATE SET
  email = EXCLUDED.email,
  source = EXCLUDED.source,
  updated_at = now()
RETURNING pending_grant_id::text`, tenantID, email, role, source).Scan(&pendingGrantID)
}

// reportPerPersonGrants prints one line per (person, role) and returns the failure count.
//
// Printed per row on purpose: this is an AUTHORITY list, and "who can approve births, deaths and
// shifts in STG, and who carries an operator's surface" must be readable off the seed output rather
// than inferred from a summary count.
func reportPerPersonGrants(results []grantResult) int {
	fmt.Println()
	fmt.Println("=== seed-stg-login-grants: per-person authority grants ===")
	failed := 0
	for _, r := range results {
		switch {
		case r.err != nil:
			failed++
			fmt.Printf("FAIL     %-40s %-16s %v\n", r.email, r.role, r.err)
		case r.active:
			fmt.Printf("OK       %-40s %-16s ACTIVE (effective now, phone and web)\n", r.email, r.role)
		case r.pending:
			fmt.Printf("PENDING  %-40s %-16s staged; activates on this person's next sign-in\n", r.email, r.role)
		}
	}
	return failed
}
