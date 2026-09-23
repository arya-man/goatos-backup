// Package postgres persists browser web push registrations.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/browserpush"
)

const defaultQueryTimeout = 3 * time.Second

// Repository is the postgres implementation of browserpush.Repository.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewRepository wires the repository over a pool.
func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ browserpush.Repository = (*Repository)(nil)

// targetMemberCTE resolves the caller to a canonical, ACTIVE workforce member from either their
// workforce_member_id or their linked authenticated user_id. It is a deliberate copy of the
// resolution ResolveMemberRecipients uses (workforce/adapters/postgres/roster_repository.go): the
// admin-web session carries the Firebase user id, while some internal callers hold the member id,
// and a push address must land on the same canonical member whichever one arrived.
//
// ACTIVE IS PART OF THE PREDICATE, NOT AN AFTERTHOUGHT: a deactivated person must not be able to
// attach a new delivery address, and every query below therefore resolves to nothing rather than
// silently registering against an inactive row. $1 tenant, $2 member-or-user id.
const targetMemberCTE = `
WITH target_member AS (
  SELECT COALESCE(
    (SELECT wm.workforce_member_id
       FROM workforce_members wm
      WHERE wm.tenant_id = $1::uuid
        AND wm.workforce_member_id = $2::uuid
        AND wm.status = 'active'),
    (SELECT wm.workforce_member_id
       FROM workforce_members wm
      WHERE wm.tenant_id = $1::uuid
        AND wm.user_id = $2::uuid
        AND wm.status = 'active')
  ) AS workforce_member_id
)`

const registrationColumns = `
  browser_registration_id::text,
  workforce_member_id::text,
  provider,
  browser_install_id,
  browser_label,
  status,
  created_at,
  last_seen_at,
  stale_at,
  COALESCE(stale_reason, ''),
  row_version`

// registrationColumnsQualified is the same projection for queries that JOIN target_member, which
// also exposes a workforce_member_id and would otherwise make the reference ambiguous.
const registrationColumnsQualified = `
  reg.browser_registration_id::text,
  reg.workforce_member_id::text,
  reg.provider,
  reg.browser_install_id,
  reg.browser_label,
  reg.status,
  reg.created_at,
  reg.last_seen_at,
  reg.stale_at,
  COALESCE(reg.stale_reason, ''),
  reg.row_version`

// upsertRegistrationSQL stores or refreshes one browser profile's push address. Hoisted to a
// package-level const, like every other statement here, so a query-plan gate and the scale guard
// can NAME it -- an inline literal inside a function body is structurally unreachable to both.
// Params: $1 tenant, $2 member-or-user id, $3 provider, $4 browser install id, $5 token,
// $6 user agent, $7 browser label, $8 now.
//
// THE DO UPDATE PREDICATE IS THE WHOLE SECURITY PROPERTY OF THIS FILE. Read it before changing
// any clause above it. Without it, a conflicting row was refreshed no matter WHOSE it was, and
// because browser_install_id is per-browser-PROFILE rather than per-user (admin-web mints it once
// into localStorage, where it survives sign-out), the second person to sign in on a shared office
// desktop silently refreshed the FIRST person's registration: the row stayed attributed to the
// first person, went back to 'active', and every push for them -- leadership-task mention and
// comment bodies carry the task title and a note excerpt -- was delivered to the browser the
// second person was sitting at. The three OR branches are the only ways a conflicting row may be
// written, and each is a different kind of proof that writing it is legitimate:
//
//  1. SAME MEMBER -- the ordinary case, and the overwhelming majority of calls: this person's own
//     browser re-registering a rotated token. Nothing changes hands.
//  2. SAME TOKEN -- the genuine shared-profile HAND-OVER. An FCM web registration token is issued
//     to a browser PROFILE, not to a signed-in user, and it does not change across sign-out, so a
//     caller presenting the token the stored row already holds is demonstrably sitting at that
//     physical browser. That is the one proof of possession this endpoint has, and it is what
//     separates the person who really took the desk over from someone who merely learned an
//     install id: from a different browser an attacker holds a different token and falls through
//     to no branch at all.
//  3. NOT ACTIVE -- the previous owner's registration is 'unsubscribed' (they switched it off) or
//     'stale' (the provider confirmed the address is dead). Nobody is relying on that address, so
//     the browser is free to be claimed.
//
// ON A HAND-OVER THE ROW IS TRANSFERRED, NOT DUPLICATED, and the unique index on
// (tenant_id, browser_install_id) is deliberately kept rather than widened to include the member.
// Keying on the member would let two members hold live registrations for one browser profile
// carrying the SAME browser-scoped token, and the fan-out addresses the TOKEN -- so the first
// person's push would still land on the second person's screen. One live registration per browser
// profile is a statement about where the notification physically arrives, not bookkeeping.
// workforce_member_id, registered_by and created_at therefore all move to the new owner: the
// registration is hers from now on, and dating it from the previous person's first sign-in would
// misreport whose address it is and since when.
//
// A caller matching NO branch writes nothing, which the RETURNING turns into zero rows, and Upsert
// raises browserpush.ErrBrowserInstallConflict rather than reporting a success it did not perform.
const upsertRegistrationSQL = targetMemberCTE + `
INSERT INTO workforce_member_browser_push_registrations (
  tenant_id, workforce_member_id, provider, browser_install_id, fcm_token,
  user_agent, browser_label, status, created_at, last_seen_at, registered_by
)
SELECT $1::uuid, tm.workforce_member_id, $3::text, $4::text, $5::text,
       $6::text, $7::text, 'active', $8::timestamptz, $8::timestamptz, tm.workforce_member_id
  FROM target_member tm
 WHERE tm.workforce_member_id IS NOT NULL
ON CONFLICT (tenant_id, browser_install_id) DO UPDATE
   SET workforce_member_id = EXCLUDED.workforce_member_id,
       registered_by = EXCLUDED.registered_by,
       fcm_token    = EXCLUDED.fcm_token,
       user_agent   = EXCLUDED.user_agent,
       browser_label = EXCLUDED.browser_label,
       status       = 'active',
       stale_at     = NULL,
       stale_reason = NULL,
       created_at   = CASE
                        WHEN workforce_member_browser_push_registrations.workforce_member_id = EXCLUDED.workforce_member_id
                          THEN workforce_member_browser_push_registrations.created_at
                        ELSE EXCLUDED.created_at
                      END,
       last_seen_at = EXCLUDED.last_seen_at,
       row_version  = workforce_member_browser_push_registrations.row_version + 1
 WHERE workforce_member_browser_push_registrations.workforce_member_id = EXCLUDED.workforce_member_id
    OR workforce_member_browser_push_registrations.fcm_token = EXCLUDED.fcm_token
    OR workforce_member_browser_push_registrations.status <> 'active'
RETURNING` + registrationColumns + `, (row_version = 1) AS created`

// unsubscribeRegistrationSQL switches one browser off at the person's own request.
// Params: $1 tenant, $2 member-or-user id, $3 browser install id, $4 now.
const unsubscribeRegistrationSQL = targetMemberCTE + `
UPDATE workforce_member_browser_push_registrations reg
   SET status       = 'unsubscribed',
       stale_at     = $4::timestamptz,
       stale_reason = 'unsubscribed_by_user',
       last_seen_at = $4::timestamptz,
       row_version  = reg.row_version + 1
  FROM target_member tm
 WHERE reg.tenant_id = $1::uuid
   AND reg.workforce_member_id = tm.workforce_member_id
   AND reg.browser_install_id = $3::text
   AND reg.status = 'active'`

// listRegistrationsSQL returns the caller's registrations, whatever their status.
// Params: $1 tenant, $2 member-or-user id.
const listRegistrationsSQL = targetMemberCTE + `
SELECT` + registrationColumnsQualified + `
  FROM workforce_member_browser_push_registrations reg
  JOIN target_member tm ON tm.workforce_member_id = reg.workforce_member_id
 WHERE reg.tenant_id = $1::uuid
 ORDER BY reg.last_seen_at DESC, reg.browser_registration_id
 LIMIT 50`

const recordEventSQL = targetMemberCTE + `
INSERT INTO browser_push_events (
  tenant_id, notification_request_id, workforce_member_id, browser_registration_id,
  browser_install_id, event_type, occurred_at, trace_id
)
SELECT $1::uuid, nr.notification_request_id, tm.workforce_member_id, reg.browser_registration_id,
       NULLIF($4, ''), $5, $6::timestamptz, NULLIF($7, '')
  FROM target_member tm
  JOIN notification_requests nr
    ON nr.tenant_id = $1::uuid
   AND nr.notification_request_id = $3::uuid
  JOIN workforce_member_browser_push_registrations reg
    ON reg.tenant_id = $1::uuid
   AND reg.browser_install_id = NULLIF($4, '')
   AND reg.workforce_member_id = tm.workforce_member_id
 WHERE tm.workforce_member_id IS NOT NULL
   AND (nr.context->>'member_id' = tm.workforce_member_id::text OR nr.recipient_ref = reg.fcm_token)
ON CONFLICT (tenant_id, notification_request_id, browser_registration_id, event_type) DO NOTHING`

// resolveRecipientsSQL returns one person's reachable browsers, for the notification fan-out.
// Params: $1 tenant, $2 member-or-user id.
const resolveRecipientsSQL = targetMemberCTE + `
SELECT DISTINCT ON (reg.fcm_token)
       reg.workforce_member_id::text,
       reg.browser_registration_id::text,
       reg.fcm_token
  FROM workforce_member_browser_push_registrations reg
  JOIN target_member tm ON tm.workforce_member_id = reg.workforce_member_id
 WHERE reg.tenant_id = $1::uuid
   AND reg.status = 'active'
 ORDER BY reg.fcm_token, reg.browser_registration_id
 LIMIT 50`

// resolveModuleDutyRecipientsSQL returns reachable browsers for the same module-duty audience
// workforce's phone resolver uses. Params: $1 tenant, $2 scope type, $3 scope id, $4 at,
// $5 module code, $6 duty type, $7 at.
const resolveModuleDutyRecipientsSQL = `
WITH duty_members AS (
SELECT DISTINCT p.workforce_member_id
FROM workforce_positions p
JOIN position_module_duties pmd
  ON pmd.tenant_id = p.tenant_id
 AND pmd.position_code = p.position_code
 AND pmd.module_code = $5
 AND pmd.duty_type = $6
 AND pmd.status = 'active'
 AND pmd.effective_from <= $7::timestamptz
 AND (pmd.effective_to IS NULL OR pmd.effective_to > $7::timestamptz)
JOIN workforce_members m
  ON m.tenant_id = p.tenant_id
 AND m.workforce_member_id = p.workforce_member_id
 AND m.status = 'active'
WHERE p.tenant_id = $1::uuid
  AND p.scope_type = $2
  AND p.scope_id = $3::uuid
  AND p.status = 'active'
  AND p.valid_from <= $4::timestamptz
  AND (p.valid_to IS NULL OR p.valid_to > $4::timestamptz)
), grant_members AS (
SELECT DISTINCT m.workforce_member_id
FROM user_scope_grants g
JOIN workforce_members m
  ON m.tenant_id = g.tenant_id
 AND m.user_id = g.user_id
 AND m.status = 'active'
WHERE $6 = 'verify'
  AND g.tenant_id = $1::uuid
  AND g.scope_type = 'tenant'
  AND g.scope_id = $1::uuid
  AND g.role = 'verifier'
  AND g.status = 'active'
  AND g.valid_from <= $7::timestamptz
  AND (g.valid_to IS NULL OR g.valid_to > $7::timestamptz)
), target_members AS (
  SELECT workforce_member_id FROM duty_members
  UNION
  SELECT workforce_member_id FROM grant_members
)
SELECT DISTINCT ON (reg.fcm_token)
       reg.workforce_member_id::text,
       reg.browser_registration_id::text,
       reg.fcm_token
  FROM workforce_member_browser_push_registrations reg
  JOIN target_members tm ON tm.workforce_member_id = reg.workforce_member_id
 WHERE reg.tenant_id = $1::uuid
   AND reg.status = 'active'
 ORDER BY reg.fcm_token, reg.browser_registration_id
 LIMIT 1000`

// resolvePositionRecipientsSQL returns reachable browsers for the same position/role-grant
// audience workforce's phone resolver uses. Params: $1 tenant, $2 scope type, $3 scope id,
// $4 position code, $5 at.
const resolvePositionRecipientsSQL = `
WITH position_members AS (
SELECT DISTINCT p.workforce_member_id
FROM workforce_positions p
JOIN workforce_members m
  ON m.tenant_id = p.tenant_id
 AND m.workforce_member_id = p.workforce_member_id
 AND m.status = 'active'
WHERE p.tenant_id = $1::uuid
  AND p.scope_type = $2
  AND p.scope_id = $3::uuid
  AND p.position_code = $4
  AND p.status = 'active'
  AND p.valid_from <= $5::timestamptz
  AND (p.valid_to IS NULL OR p.valid_to > $5::timestamptz)
), grant_members AS (
SELECT DISTINCT m.workforce_member_id
FROM user_scope_grants g
JOIN workforce_members m
  ON m.tenant_id = g.tenant_id
 AND m.user_id = g.user_id
 AND m.status = 'active'
WHERE $2 = 'tenant'
  AND g.tenant_id = $1::uuid
  AND g.scope_type = 'tenant'
  AND g.scope_id = $3::uuid
  AND g.role = $4
  AND g.role = ANY(ARRAY['ceo_internal','pc_director','growth_director','feed_director','health_director','procurement_director','breeding_director','verifier'])
  AND g.status = 'active'
  AND g.valid_from <= $5::timestamptz
  AND (g.valid_to IS NULL OR g.valid_to > $5::timestamptz)
), park_grant_members AS (
SELECT DISTINCT m.workforce_member_id
FROM user_scope_grants g
JOIN workforce_members m
  ON m.tenant_id = g.tenant_id
 AND m.user_id = g.user_id
 AND m.status = 'active'
WHERE $2 = 'center'
  AND g.tenant_id = $1::uuid
  AND g.scope_type = 'park'
  AND g.scope_id = $3::uuid
  AND g.role = $4
  AND g.role = ANY(ARRAY['park_head','procurement_manager'])
  AND g.status = 'active'
  AND g.valid_from <= $5::timestamptz
  AND (g.valid_to IS NULL OR g.valid_to > $5::timestamptz)
), target_members AS (
  SELECT workforce_member_id FROM position_members
  UNION
  SELECT workforce_member_id FROM grant_members
  UNION
  SELECT workforce_member_id FROM park_grant_members
)
SELECT DISTINCT ON (reg.fcm_token)
       reg.workforce_member_id::text,
       reg.browser_registration_id::text,
       reg.fcm_token
  FROM workforce_member_browser_push_registrations reg
  JOIN target_members tm ON tm.workforce_member_id = reg.workforce_member_id
 WHERE reg.tenant_id = $1::uuid
   AND reg.status = 'active'
 ORDER BY reg.fcm_token, reg.browser_registration_id
 LIMIT 1000`

// resolveModuleDutyRecipientsBatchSQL is the batched form of resolveModuleDutyRecipientsSQL.
// Params: $1 tenant, $2 scope type, $3 scope ids, $4 module code, $5 duty types, $6 at.
const resolveModuleDutyRecipientsBatchSQL = `
WITH duty_members AS (
SELECT DISTINCT p.scope_id::text, p.position_code, p.workforce_member_id
FROM workforce_positions p
JOIN position_module_duties pmd
  ON pmd.tenant_id = p.tenant_id
 AND pmd.position_code = p.position_code
 AND pmd.module_code = $4
 AND pmd.duty_type = ANY($5::text[])
 AND pmd.status = 'active'
 AND pmd.effective_from <= $6::timestamptz
 AND (pmd.effective_to IS NULL OR pmd.effective_to > $6::timestamptz)
JOIN workforce_members m
  ON m.tenant_id = p.tenant_id
 AND m.workforce_member_id = p.workforce_member_id
 AND m.status = 'active'
WHERE p.tenant_id = $1::uuid
  AND p.scope_type = $2
  AND p.scope_id = ANY($3::uuid[])
  AND p.status = 'active'
  AND p.valid_from <= $6::timestamptz
  AND (p.valid_to IS NULL OR p.valid_to > $6::timestamptz)
)
SELECT DISTINCT ON (dm.scope_id, dm.position_code, reg.fcm_token)
       dm.scope_id,
       dm.position_code,
       reg.workforce_member_id::text,
       reg.browser_registration_id::text,
       reg.fcm_token
  FROM duty_members dm
  JOIN workforce_member_browser_push_registrations reg
    ON reg.tenant_id = $1::uuid
   AND reg.workforce_member_id = dm.workforce_member_id
   AND reg.status = 'active'
 ORDER BY dm.scope_id, dm.position_code, reg.fcm_token, reg.browser_registration_id
 LIMIT 5000`

// resolvePositionRecipientsBatchSQL is the batched form of resolvePositionRecipientsSQL.
// Params: $1 tenant, $2 scope type, $3 scope ids, $4 position codes, $5 at.
const resolvePositionRecipientsBatchSQL = `
WITH position_members AS (
SELECT DISTINCT p.scope_id::text, p.position_code, p.workforce_member_id
FROM workforce_positions p
JOIN workforce_members m
  ON m.tenant_id = p.tenant_id
 AND m.workforce_member_id = p.workforce_member_id
 AND m.status = 'active'
WHERE p.tenant_id = $1::uuid
  AND p.scope_type = $2
  AND p.scope_id = ANY($3::uuid[])
  AND p.position_code = ANY($4::text[])
  AND p.status = 'active'
  AND p.valid_from <= $5::timestamptz
  AND (p.valid_to IS NULL OR p.valid_to > $5::timestamptz)
), grant_members AS (
SELECT DISTINCT g.scope_id::text, g.role AS position_code, m.workforce_member_id
FROM user_scope_grants g
JOIN workforce_members m
  ON m.tenant_id = g.tenant_id
 AND m.user_id = g.user_id
 AND m.status = 'active'
WHERE $2 = 'tenant'
  AND g.tenant_id = $1::uuid
  AND g.scope_type = 'tenant'
  AND g.scope_id = ANY($3::uuid[])
  AND g.role = ANY($4::text[])
  AND g.role = ANY(ARRAY['ceo_internal','pc_director','growth_director','feed_director','health_director','procurement_director','breeding_director','verifier'])
  AND g.status = 'active'
  AND g.valid_from <= $5::timestamptz
  AND (g.valid_to IS NULL OR g.valid_to > $5::timestamptz)
), park_grant_members AS (
SELECT DISTINCT g.scope_id::text, g.role AS position_code, m.workforce_member_id
FROM user_scope_grants g
JOIN workforce_members m
  ON m.tenant_id = g.tenant_id
 AND m.user_id = g.user_id
 AND m.status = 'active'
WHERE $2 = 'center'
  AND g.tenant_id = $1::uuid
  AND g.scope_type = 'park'
  AND g.scope_id = ANY($3::uuid[])
  AND g.role = ANY($4::text[])
  AND g.role = ANY(ARRAY['park_head','procurement_manager'])
  AND g.status = 'active'
  AND g.valid_from <= $5::timestamptz
  AND (g.valid_to IS NULL OR g.valid_to > $5::timestamptz)
), target_members AS (
  SELECT scope_id, position_code, workforce_member_id FROM position_members
  UNION
  SELECT scope_id, position_code, workforce_member_id FROM grant_members
  UNION
  SELECT scope_id, position_code, workforce_member_id FROM park_grant_members
)
SELECT DISTINCT ON (tm.scope_id, tm.position_code, reg.fcm_token)
       tm.scope_id,
       tm.position_code,
       reg.workforce_member_id::text,
       reg.browser_registration_id::text,
       reg.fcm_token
  FROM target_members tm
  JOIN workforce_member_browser_push_registrations reg
    ON reg.tenant_id = $1::uuid
   AND reg.workforce_member_id = tm.workforce_member_id
   AND reg.status = 'active'
 ORDER BY tm.scope_id, tm.position_code, reg.fcm_token, reg.browser_registration_id
 LIMIT 5000`

// markTokenStaleSQL is the PRUNE: retire every active registration holding a provider-confirmed
// dead token. Addressed BY TOKEN because that is all a delivery failure knows.
// Params: $1 tenant, $2 token, $3 reason, $4 now.
const markTokenStaleSQL = `
UPDATE workforce_member_browser_push_registrations
   SET status       = 'stale',
       stale_at     = $4::timestamptz,
       stale_reason = $3::text,
       row_version  = row_version + 1
 WHERE tenant_id = $1::uuid
   AND fcm_token = $2::text
   AND status = 'active'`

// resolveTargetMemberSQL answers ONE question, and only on the failure path: does this caller
// resolve to an active workforce member of this tenant at all? It is what lets Upsert tell an
// authorization gap (the caller is not a member here) apart from a browser-ownership conflict
// (they are, but that browser profile is somebody else's live registration) after the upsert has
// already written nothing. Two very different answers to the client -- 403 versus 409 with an
// install id to re-mint -- so guessing between them is not an option.
// Params: $1 tenant, $2 member-or-user id.
const resolveTargetMemberSQL = targetMemberCTE + `
SELECT workforce_member_id::text
  FROM target_member
 WHERE workforce_member_id IS NOT NULL`

// Upsert stores or refreshes one browser profile's push address.
//
// ON CONFLICT REVIVES, IT DOES NOT SKIP. The conflict target is the browser profile, so a person
// who turned notifications off and later turned them back on, or whose address was pruned as
// stale and has now produced a fresh token, reuses the SAME row: status returns to 'active' and
// stale_at/stale_reason are cleared (the table's stale CHECK requires exactly that pairing). The
// alternative -- insert-only, leaving the dead row behind -- would accumulate one dead row per
// permission cycle per browser and make "is this person reachable" ambiguous.
//
// A BROWSER PROFILE CAN CHANGE HANDS, BUT ONLY ON PROOF. browser_install_id lives in that Chrome
// profile's localStorage and survives sign-out, so two colleagues sharing one office desktop
// present the SAME install id. The conflicting row is therefore rewritten only when the caller
// owns it already, or presents the token the row holds (proof they are at that physical browser,
// since an FCM web token belongs to the profile and not to the signed-in user), or the previous
// owner's registration is no longer active. Anything else is somebody else's live address and is
// refused with ErrBrowserInstallConflict -- see the predicate on upsertRegistrationSQL, which is
// where the reasoning for each branch lives. The client answers a conflict by minting a fresh
// install id for itself, so the person still gets their own registration for that browser; they
// are never left silently unreachable, which is what the pre-fix behaviour did to the second
// person to sign in.
//
// Because the predicate can only ever write the CALLER's own row, the returned Registration --
// which the POST echoes back verbatim -- can no longer carry another member's workforce_member_id.
func (r *Repository) Upsert(ctx context.Context, tenantID, memberOrUserID string, in browserpush.RegisterRequest, now time.Time) (browserpush.Registration, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	row := r.pool.QueryRow(ctx, upsertRegistrationSQL,
		tenantID, memberOrUserID, browserpush.ProviderWebFCM, in.BrowserInstallID, in.Token,
		in.UserAgent, in.BrowserLabel, now)
	var (
		registration browserpush.Registration
		created      bool
	)
	if err := scanRegistration(row, &registration, &created); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Zero rows has exactly two causes and they are not the same answer. Either the
			// INSERT ... SELECT produced nothing because target_member resolved to nothing (the
			// caller is authenticated but is not an active workforce member of this tenant -- an
			// authorization gap), or it conflicted and the DO UPDATE predicate refused because
			// that browser profile holds somebody else's live registration. Ask the database
			// which, on this cold path only, rather than reporting a registration that never
			// happened as a success.
			return browserpush.Registration{}, false, r.classifyUpsertNoRows(ctx, tenantID, memberOrUserID)
		}
		return browserpush.Registration{}, false, fmt.Errorf("browser push: upsert registration: %w", err)
	}
	return registration, created, nil
}

// classifyUpsertNoRows turns "the upsert wrote nothing" into the one honest error for it.
func (r *Repository) classifyUpsertNoRows(ctx context.Context, tenantID, memberOrUserID string) error {
	var memberID string
	if err := r.pool.QueryRow(ctx, resolveTargetMemberSQL, tenantID, memberOrUserID).Scan(&memberID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("browser push: %w", browserpush.ErrRegistrationNotFound)
		}
		// The diagnosis itself failed. Report the conflict -- the write demonstrably did not
		// happen -- but keep the cause, because swallowing it would hide a real database fault
		// behind a routine 409.
		return fmt.Errorf("browser push: %w: resolve target member: %v", browserpush.ErrBrowserInstallConflict, err)
	}
	return fmt.Errorf("browser push: %w", browserpush.ErrBrowserInstallConflict)
}

// MarkUnsubscribed switches one browser off at the person's own request.
//
// Scoped to the CALLER's own member id as well as the browser install id: the install id is
// client-supplied, so without the member predicate one person could switch off another person's
// browser by guessing or replaying an id.
func (r *Repository) MarkUnsubscribed(ctx context.Context, tenantID, memberOrUserID, browserInstallID string, now time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, unsubscribeRegistrationSQL, tenantID, memberOrUserID, browserInstallID, now)
	if err != nil {
		return false, fmt.Errorf("browser push: unsubscribe registration: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListForMember returns the caller's registrations, whatever their status.
// scale-guard: bounded per-person fan-out LIMIT 50; a person's browser count is small and a
// larger result would be a bug, not a legitimate read.
func (r *Repository) ListForMember(ctx context.Context, tenantID, memberOrUserID string) ([]browserpush.Registration, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, listRegistrationsSQL, tenantID, memberOrUserID)
	if err != nil {
		return nil, fmt.Errorf("browser push: list registrations: %w", err)
	}
	defer rows.Close()
	registrations := make([]browserpush.Registration, 0, 4)
	for rows.Next() {
		var registration browserpush.Registration
		if err := scanRegistration(rows, &registration, nil); err != nil {
			return nil, fmt.Errorf("browser push: scan registration: %w", err)
		}
		registrations = append(registrations, registration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browser push: list registrations: %w", err)
	}
	return registrations, nil
}

func (r *Repository) RecordEvent(ctx context.Context, tenantID, memberOrUserID string, event browserpush.EventRequest, now time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, recordEventSQL,
		tenantID,
		memberOrUserID,
		event.NotificationRequestID,
		event.BrowserInstallID,
		event.EventType,
		now,
		event.TraceID,
	)
	if err != nil {
		return false, fmt.Errorf("browser push: record event: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ResolveMemberRecipients returns one person's reachable browsers.
//
// Mirrors ResolveMemberRecipients on the phone path (workforce roster repository) down to the
// bounded fan-out and the DISTINCT: the same token can legitimately appear under two browser
// install ids after a profile copy, and sending twice to one browser is a duplicate notification,
// not redundancy.
// scale-guard: bounded recipient fan-out LIMIT 50 prevents unbounded multi-browser notifications.
func (r *Repository) ResolveMemberRecipients(ctx context.Context, tenantID, memberOrUserID string) ([]browserpush.Recipient, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, resolveRecipientsSQL, tenantID, memberOrUserID)
	if err != nil {
		return nil, fmt.Errorf("browser push: resolve recipients: %w", err)
	}
	return scanRecipients(rows, "browser push: resolve recipients")
}

// ResolveModuleDutyRecipients returns reachable browsers for a module-duty audience.
//
// This is the browser twin of workforce.ResolveModuleDutyRecipients. The member selection is the
// same, but the address table is workforce_member_browser_push_registrations instead of Android
// devices, so a CXO with Chrome enabled and no phone token still receives the push.
// scale-guard: bounded recipient fan-out LIMIT 1000 mirrors the phone resolver.
func (r *Repository) ResolveModuleDutyRecipients(ctx context.Context, tenantID, scopeType, scopeID, moduleCode, dutyType string, at time.Time) ([]browserpush.Recipient, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, resolveModuleDutyRecipientsSQL, tenantID, scopeType, scopeID, at, moduleCode, dutyType, at)
	if err != nil {
		return nil, fmt.Errorf("browser push: resolve module-duty recipients: %w", err)
	}
	return scanRecipients(rows, "browser push: resolve module-duty recipients")
}

// ResolvePositionRecipients returns reachable browsers for a fixed-position audience.
//
// This is the browser twin of workforce.ResolvePositionRecipients, including the tenant leadership
// and park-role grant fallbacks. Without it, in-app notification rows can appear for leadership
// and park desks while Chrome stays silent.
// scale-guard: bounded recipient fan-out LIMIT 1000 mirrors the phone resolver.
func (r *Repository) ResolvePositionRecipients(ctx context.Context, tenantID, scopeType, scopeID, positionCode string, at time.Time) ([]browserpush.Recipient, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, resolvePositionRecipientsSQL, tenantID, scopeType, scopeID, positionCode, at)
	if err != nil {
		return nil, fmt.Errorf("browser push: resolve position recipients: %w", err)
	}
	return scanRecipients(rows, "browser push: resolve position recipients")
}

// ResolveModuleDutyRecipientsBatch returns reachable browsers for module-duty audiences in one
// set-based read. The result key is "<scopeID>|<positionCode>", matching workforce's batch resolver.
// scale-guard: bounded recipient fan-out LIMIT 5000 mirrors the phone resolver.
func (r *Repository) ResolveModuleDutyRecipientsBatch(ctx context.Context, tenantID, scopeType string, scopeIDs []string, moduleCode string, dutyTypes []string, at time.Time) (map[string][]browserpush.Recipient, error) {
	out := map[string][]browserpush.Recipient{}
	if len(scopeIDs) == 0 || len(dutyTypes) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, resolveModuleDutyRecipientsBatchSQL, tenantID, scopeType, scopeIDs, moduleCode, dutyTypes, at)
	if err != nil {
		return nil, fmt.Errorf("browser push: resolve module-duty recipients batch: %w", err)
	}
	return scanRecipientsByKey(rows, "browser push: resolve module-duty recipients batch")
}

// ResolvePositionRecipientsBatch returns reachable browsers for position audiences in one set-based
// read. The result key is "<scopeID>|<positionCode>", matching workforce's batch resolver.
// scale-guard: bounded recipient fan-out LIMIT 5000 mirrors the phone resolver.
func (r *Repository) ResolvePositionRecipientsBatch(ctx context.Context, tenantID, scopeType string, scopeIDs, positionCodes []string, at time.Time) (map[string][]browserpush.Recipient, error) {
	out := map[string][]browserpush.Recipient{}
	if len(scopeIDs) == 0 || len(positionCodes) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, resolvePositionRecipientsBatchSQL, tenantID, scopeType, scopeIDs, positionCodes, at)
	if err != nil {
		return nil, fmt.Errorf("browser push: resolve position recipients batch: %w", err)
	}
	return scanRecipientsByKey(rows, "browser push: resolve position recipients batch")
}

// MarkTokenStale retires every active registration holding a provider-confirmed dead token.
//
// Addressed by token, not by browser or member, because a delivery failure knows only the
// recipient_ref FCM rejected -- see browserpush.Service.PruneToken for why this is the only
// signal a browser subscription's death ever produces. Idempotent: an already-stale row does not
// match, so a redelivered failure is a no-op rather than a second row_version bump.
func (r *Repository) MarkTokenStale(ctx context.Context, tenantID, token, reason string, now time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, markTokenStaleSQL, tenantID, token, reason, now)
	if err != nil {
		return 0, fmt.Errorf("browser push: mark token stale: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// scanner is the shared shape of pgx.Row and pgx.Rows for the registration projection.
type scanner interface {
	Scan(dest ...any) error
}

type recipientRows interface {
	Close()
	Err() error
	Next() bool
	Scan(dest ...any) error
}

func scanRecipients(rows recipientRows, errPrefix string) ([]browserpush.Recipient, error) {
	defer rows.Close()
	recipients := make([]browserpush.Recipient, 0, 4)
	for rows.Next() {
		var recipient browserpush.Recipient
		if err := rows.Scan(&recipient.WorkforceMemberID, &recipient.BrowserRegistrationID, &recipient.Token); err != nil {
			return nil, fmt.Errorf("%s: scan recipient: %w", errPrefix, err)
		}
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return recipients, nil
}

func scanRecipientsByKey(rows recipientRows, errPrefix string) (map[string][]browserpush.Recipient, error) {
	defer rows.Close()
	out := map[string][]browserpush.Recipient{}
	for rows.Next() {
		var scopeID, positionCode string
		var recipient browserpush.Recipient
		if err := rows.Scan(&scopeID, &positionCode, &recipient.WorkforceMemberID, &recipient.BrowserRegistrationID, &recipient.Token); err != nil {
			return nil, fmt.Errorf("%s: scan recipient: %w", errPrefix, err)
		}
		out[scopeID+"|"+positionCode] = append(out[scopeID+"|"+positionCode], recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return out, nil
}

func scanRegistration(src scanner, out *browserpush.Registration, created *bool) error {
	var staleAt *time.Time
	dest := []any{
		&out.BrowserRegistrationID,
		&out.WorkforceMemberID,
		&out.Provider,
		&out.BrowserInstallID,
		&out.BrowserLabel,
		&out.Status,
		&out.CreatedAt,
		&out.LastSeenAt,
		&staleAt,
		&out.StaleReason,
		&out.RowVersion,
	}
	if created != nil {
		dest = append(dest, created)
	}
	if err := src.Scan(dest...); err != nil {
		return err
	}
	out.StaleAt = staleAt
	return nil
}
