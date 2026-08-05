package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	authallow "github.com/vgoats/goatos/backend/internal/platform/authallow"
)

// Counts approval authority is granted PER PERSON, by name (maintainer decision 2026-08-05).
//
// The maintainer's words were "keep rbac per person, not per group". Permissions in this system
// resolve from the ROLES on a caller's active user_scope_grants rows, and a caller may hold several
// rows, so the way to express "this person, not this job" is a narrow role
// (permissions.RoleCountsApprover) granted to named individuals ALONGSIDE their job role.
//
// This list is therefore the authority itself, not a convenience: adding an email here gives that
// person approve/reject over the birth/death/shifting queue on BOTH the phone and admin-web;
// removing it takes the authority away and leaves their job role untouched. It deliberately does
// NOT widen pc_director or growth_director, so nobody inherits approval power by being appointed to
// one of those jobs later.
//
// Deliberately keyed by EMAIL, not by Firebase UID: the two approvers below are seeded by DIFFERENT
// paths (Chandrakant is a UID-backed account in stgLoginAccounts; Dinakar's Growth Director account
// is seeded by the weighing scope seed and his Firebase UID is not committed anywhere in this
// repo). Email is the one identifier both share, and the pending-grant claim path already exists
// for exactly this case.
var countsApproverEmails = []string{
	// Chandrakant — Preventive Care Director. UID-backed below, so his grant goes ACTIVE at seed.
	"chandrakanth119527@gmail.com",
	// Dinakar — Growth Director. Not in stgLoginAccounts (no committed Firebase UID), so this can
	// only be staged as a pending grant that activates on his next sign-in — the same mechanism
	// the runbook already documents for Jyothi's verifier row.
	"babureddy315@gmail.com",
}

// approverResult is one named person's outcome, reported per person so a silent miss is impossible.
type approverResult struct {
	email string
	// active is true when an ACTIVE user_scope_grants row now exists (UID known).
	active bool
	// pending is true when the authority is staged in auth_pending_email_grants and will activate
	// on the person's next sign-in.
	pending bool
	err     error
}

// seedCountsApprovers grants permissions.RoleCountsApprover to each named approver.
//
// Two writes per person, and BOTH are attempted:
//
//  1. A pending email grant. This works with no Firebase UID and is what covers an approver whose
//     account is seeded elsewhere. It activates through the normal runtime claim path.
//  2. An ACTIVE user_scope_grants row, when — and only when — the person is one of the UID-backed
//     stgLoginAccounts. Per the STG login contract a pending row alone is NOT seed-complete,
//     because admin-web SSO does not reliably trigger the claim on a fresh seed.
//
// Tenant scope, not park scope: an approval decision is addressed by request id and a director's
// remit spans both parks. This does not violate the operator-scope invariant
// (make stg-operator-scope-guard) — that bans tenant scope on an OPERATOR grant, and this role is
// never held by an operator.
func seedCountsApprovers(ctx context.Context, pool *pgxpool.Pool, tenantID, authIssuer, source string) []approverResult {
	byEmail := make(map[string]Account, len(stgLoginAccounts))
	for _, acct := range stgLoginAccounts {
		byEmail[authallow.NormalizeEmail(acct.Email)] = acct
	}

	results := make([]approverResult, 0, len(countsApproverEmails))
	for _, rawEmail := range countsApproverEmails {
		email := authallow.NormalizeEmail(rawEmail)
		res := approverResult{email: email}

		if err := upsertPendingApproverGrant(ctx, pool, tenantID, email, source); err != nil {
			res.err = fmt.Errorf("pending approver grant: %w", err)
			results = append(results, res)
			continue
		}
		res.pending = true

		acct, known := byEmail[email]
		if !known || strings.TrimSpace(acct.FirebaseUID) == "" {
			// No committed UID: the pending row above is genuinely the most this command can do.
			// Reported as PENDING rather than OK so it is never mistaken for a finished grant.
			results = append(results, res)
			continue
		}

		userID := platformauth.StableSubjectID(authIssuer, acct.FirebaseUID)
		if err := materializeScopeGrant(ctx, pool, tenantID, userID, permissions.RoleCountsApprover, "tenant", tenantID); err != nil {
			res.err = fmt.Errorf("materialize counts_approver grant: %w", err)
			results = append(results, res)
			continue
		}
		res.active = true
		results = append(results, res)
	}
	return results
}

// upsertPendingApproverGrant stages the authority for an email whose UID may be unknown.
//
// Always tenant-scoped, so it does not reuse upsertPendingEmailGrant: that helper REVOKES the
// tenant row for a park-scoped account, which is right for a job role bound to one park and wrong
// for this one.
func upsertPendingApproverGrant(ctx context.Context, pool *pgxpool.Pool, tenantID, email, source string) error {
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
RETURNING pending_grant_id::text`, tenantID, email, permissions.RoleCountsApprover, source).Scan(&pendingGrantID)
}

// reportCountsApprovers prints one line per named approver and returns the failure count.
//
// Printed per person on purpose: this is an AUTHORITY list, and "who can approve births, deaths and
// shifts in STG" must be readable off the seed output rather than inferred from a summary count.
func reportCountsApprovers(results []approverResult) int {
	fmt.Println()
	fmt.Println("=== seed-stg-login-grants: counts approvers (per-person authority) ===")
	failed := 0
	for _, r := range results {
		switch {
		case r.err != nil:
			failed++
			fmt.Printf("FAIL     %-40s %v\n", r.email, r.err)
		case r.active:
			fmt.Printf("OK       %-40s counts_approver ACTIVE (may approve now, phone and web)\n", r.email)
		case r.pending:
			fmt.Printf("PENDING  %-40s counts_approver staged; activates on this person's next sign-in\n", r.email)
		}
	}
	return failed
}
