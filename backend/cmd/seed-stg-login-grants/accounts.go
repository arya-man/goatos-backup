// Package main: committed roster of the 9 canonical STG login accounts.
//
// WHY A COMMITTED GO TABLE, NOT A RUNTIME FIREBASE LOOKUP: StableSubjectID
// (backend/internal/platform/auth/jwt.go) is a one-way hash of
// (issuer, firebase_uid). To materialize an active user_scope_grants row for
// someone BEFORE their first sign-in, this command needs their real Firebase
// UID up front — there is no way around fetching it once, from Firebase, per
// account. Two designs were considered:
//
//  1. Runtime Identity Toolkit lookup (accounts:lookup) by email, using an
//     access token with x-goog-user-project. Pro: no manual UID copy step.
//     Con: adds a live Google Cloud dependency (token, IAM, network) to what
//     is otherwise a pure-SQL idempotent seeder, and silently breaks (or
//     silently returns nothing useful) if the Firebase user does not exist
//     yet, if IAM changes, or if run offline.
//  2. A small committed table of (email, firebase_uid, role, department),
//     filled in ONCE by the maintainer after creating/confirming each
//     Firebase user in the goatos-stg project, then never touched again
//     (Firebase UIDs are permanent for the life of the account).
//
// (2) is implemented here: it is simpler, has zero runtime Google Cloud
// dependency, fails loudly and specifically ("firebase_uid is empty for
// X — fill it in before seeding") instead of failing on a live API call, and
// matches how docs/runbooks/stg-operator-login-credentials.md already treats
// these 9 identities as a fixed, hand-maintained roster (fixed emails, fixed
// STG passwords). If the maintainer prefers runtime lookup instead, the
// FirebaseUID field can be left blank and a lookup helper added later behind
// the same Account.FirebaseUID slot — nothing else in main.go needs to change.
//
// FLAG FOR REVIEW: the FirebaseUID values below are PLACEHOLDERS ("") and
// MUST be filled in by the maintainer from the goatos-stg Firebase Auth
// console (Authentication -> Users -> copy the "User UID" column) before this
// command can seed anything. The command refuses to run (loud, per-account
// error) while any configured account has an empty FirebaseUID.
package main

import (
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// Account is one of the 9 canonical STG login identities.
type Account struct {
	// DisplayName is a human label for log output only.
	DisplayName string
	// Email is the sign-in email: Google SSO email for leadership, Firebase
	// email/password email for operators/director.
	Email string
	// FirebaseUID is the permanent Firebase Auth "User UID" (Google SSO uses
	// the Google-linked Firebase UID; email/password uses the Firebase
	// email/password provider UID). MUST be filled in by the maintainer —
	// see the package doc above. Left blank here as a placeholder.
	FirebaseUID string
	// Role is a permissions.RoleXxx constant.
	Role string
	// DepartmentCode is the HR department code (departments.code) this
	// person's existing workforce_members roster row should be bound to.
	// Empty for the 5 ceo_internal leadership accounts, which have no
	// department (bootstrap_copy.go:214 grants them every module without one).
	DepartmentCode string
	// RosterDisplayNameMatch is the (case-insensitive) display_name to look
	// up on the EXISTING workforce_members row seeded by seed-roster-real /
	// seed-vaccination-cpt-operator-drive. Empty for leadership (no roster
	// row expected/required).
	RosterDisplayNameMatch string
}

// stgLoginAccounts is the canonical 9-person STG roster. Source of truth for
// names/emails/roles: docs/runbooks/stg-login-seed-contract.md and
// docs/runbooks/stg-operator-login-credentials.md (do not let this list and
// those docs drift — update both in the same change).
var stgLoginAccounts = []Account{
	// --- 5 leadership: Google SSO, ceo_internal, tenant scope, no department ---
	{DisplayName: "Ravi", Email: "ravi@mesha.sg", FirebaseUID: "VNAvpunR93ck7Ckf3mz6JZyDgjV2", Role: permissions.RoleCEOInternal},
	{DisplayName: "Manohar K", Email: "manohark@mesha.sg", FirebaseUID: "h8oAB7Asc5YCuvR5y5btaUtatX53", Role: permissions.RoleCEOInternal},
	{DisplayName: "Manju", Email: "manju@mesha.sg", FirebaseUID: "Iz3I6SC3ZTeAjFJQ7sqjb6ZLSHB3", Role: permissions.RoleCEOInternal},
	{DisplayName: "Abhishek", Email: "abhishek@mesha.sg", FirebaseUID: "mnzDI9IauadgjXxOkXo1WWFsIqq1", Role: permissions.RoleCEOInternal},
	{DisplayName: "Aryaman", Email: "aryaman@mesha.sg", FirebaseUID: "BKTRgCyvZwMC9c6MiVHtGRDJTuG2", Role: permissions.RoleCEOInternal},

	// --- 3 operators: Firebase email/password, operator role, preventive_care dept ---
	{
		DisplayName:            "Amit Kumar",
		Email:                  "amit797069@gmail.com",
		FirebaseUID:            "kjVehMX54kddXdGkG4lyF7cslG13",
		Role:                   permissions.RoleOperator,
		DepartmentCode:         "preventive_care",
		RosterDisplayNameMatch: "Amit Kumar",
	},
	{
		DisplayName:            "Darshan Talwar",
		Email:                  "darshantalawar033@gmail.com",
		FirebaseUID:            "0HTcWFuGMJSB5KqxsrPzDf9LOeH3",
		Role:                   permissions.RoleOperator,
		DepartmentCode:         "preventive_care",
		RosterDisplayNameMatch: "Darshan Talwar",
	},
	{
		DisplayName:            "Sagar Mahoor",
		Email:                  "sagarmahoor143@gmail.com",
		FirebaseUID:            "P4rMyrwkLuV1vTUwq2ORqHAaX843",
		Role:                   permissions.RoleOperator,
		DepartmentCode:         "preventive_care",
		RosterDisplayNameMatch: "Sagar Mahoor",
	},

	// --- 1 director: Firebase email/password, pc_director role, preventive_care dept ---
	{
		DisplayName:            "Chandrakant",
		Email:                  "chandrakanth119527@gmail.com",
		FirebaseUID:            "i1yluofkzfU0RMsdKFy2SMt6hD73",
		Role:                   permissions.RolePCDirector,
		DepartmentCode:         "preventive_care",
		RosterDisplayNameMatch: "Chandrakant",
	},
}

// requireFirebaseUIDs fails loudly, listing every account still missing its
// FirebaseUID placeholder, instead of silently seeding garbage grants.
func requireFirebaseUIDs(accounts []Account) error {
	var missing []string
	for _, a := range accounts {
		if strings.TrimSpace(a.FirebaseUID) == "" {
			missing = append(missing, a.Email)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"firebase_uid is empty for %d account(s): %s — fill in backend/cmd/seed-stg-login-grants/accounts.go from the goatos-stg Firebase Auth console (Authentication -> Users -> User UID) before seeding",
			len(missing), strings.Join(missing, ", "))
	}
	return nil
}
