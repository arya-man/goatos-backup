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
   SET fcm_token    = EXCLUDED.fcm_token,
       user_agent   = EXCLUDED.user_agent,
       browser_label = EXCLUDED.browser_label,
       status       = 'active',
       stale_at     = NULL,
       stale_reason = NULL,
       last_seen_at = EXCLUDED.last_seen_at,
       row_version  = workforce_member_browser_push_registrations.row_version + 1
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

// Upsert stores or refreshes one browser profile's push address.
//
// ON CONFLICT REVIVES, IT DOES NOT SKIP. The conflict target is the browser profile, so a person
// who turned notifications off and later turned them back on, or whose address was pruned as
// stale and has now produced a fresh token, reuses the SAME row: status returns to 'active' and
// stale_at/stale_reason are cleared (the table's stale CHECK requires exactly that pairing). The
// alternative -- insert-only, leaving the dead row behind -- would accumulate one dead row per
// permission cycle per browser and make "is this person reachable" ambiguous.
//
// workforce_member_id is NOT in the SET list: a browser profile belongs to whoever first claimed
// it, and a silent re-assignment on conflict would let a second person's session inherit an
// address the first person's browser still holds. A shared-machine hand-over therefore has to go
// through the first person's unregister, which is the honest behaviour.
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
			// The INSERT ... SELECT produced no row, which means target_member resolved to
			// nothing: the caller is authenticated but is not an active workforce member of this
			// tenant. That is an authorization gap, not an empty result, and must not be reported
			// as a successful registration.
			return browserpush.Registration{}, false, fmt.Errorf("browser push: %w", browserpush.ErrRegistrationNotFound)
		}
		return browserpush.Registration{}, false, fmt.Errorf("browser push: upsert registration: %w", err)
	}
	return registration, created, nil
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
	defer rows.Close()
	recipients := make([]browserpush.Recipient, 0, 4)
	for rows.Next() {
		var recipient browserpush.Recipient
		if err := rows.Scan(&recipient.WorkforceMemberID, &recipient.BrowserRegistrationID, &recipient.Token); err != nil {
			return nil, fmt.Errorf("browser push: scan recipient: %w", err)
		}
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browser push: resolve recipients: %w", err)
	}
	return recipients, nil
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
