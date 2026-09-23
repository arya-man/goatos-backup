// Package postgres reads and marks one person's own in-app notifications out of the shared
// notification_requests table.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/domain"
	"github.com/vgoats/goatos/backend/internal/notificationcentre/ports"
)

// Repository is the notification centre's storage adapter.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewRepository constructs the adapter.
func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = 10 * time.Second
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

// ---------------------------------------------------------------------------
// Cursor
// ---------------------------------------------------------------------------

// feedCursor is the keyset over this feed's one and only order: newest first, broken by the
// request id so two notifications stamped in the same instant still page stably and total.
//
// It is base64url JSON, following the house convention (weighing's alertCursor, leadership
// tasks' sort cursor). It carries NO offset and NO page number: there is exactly one sort
// here, so unlike the leadership-tasks cursor it needs no sort name to refuse a cursor
// minted under a different order.
type feedCursor struct {
	RequestedAt time.Time `json:"requested_at"`
	RequestID   string    `json:"notification_request_id"`
}

func encodeFeedCursor(c feedCursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeFeedCursor(value string) (feedCursor, error) {
	if strings.TrimSpace(value) == "" {
		return feedCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return feedCursor{}, domain.ErrInvalidCursor
	}
	var c feedCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return feedCursor{}, domain.ErrInvalidCursor
	}
	if c.RequestID == "" || c.RequestedAt.IsZero() {
		return feedCursor{}, domain.ErrInvalidCursor
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// SQL (package-level consts: inline SQL at the call site trips scale-guard's
// hot-path-inline-sql, and a hoisted const is what a query-plan test can reach)
// ---------------------------------------------------------------------------

// sqlTargetMemberCTE resolves the CALLER -- and only ever the caller -- to a canonical,
// ACTIVE workforce_member_id from either their workforce_member_id or their linked
// authenticated user_id. It is the same resolution ResolveMemberRecipients uses
// (workforce/adapters/postgres/roster_repository.go:777) and browserpush's targetMemberCTE
// copies, because the mobile session carries the member id while the admin-web session
// carries the Firebase user id, and one person's notifications must be the same set
// whichever id arrived.
//
// ACTIVE IS PART OF THE PREDICATE: a person who has left must not read the farm's
// notifications from a session that outlived their employment. When nothing resolves the
// CTE yields NULL and every query below yields NOTHING -- it fails closed to an empty feed,
// never open to an unfiltered one. $1 tenant, $2 member-or-user id.
const sqlTargetMemberCTE = `
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

// dedupeKeyExpr collapses the DELIVERY rows of one notification back into one notification.
//
// QueueRoleNotifications writes one notification_requests row per recipient DEVICE, so a
// person with two phones (a reinstall without sign-out, a second handset) holds TWO rows
// for one transition. A feed that showed both would show the same sentence twice and a
// badge that counts both would double-count it. The producer's own event_key identifies the
// transition, so grouping on it is the collapse; rows written by producers that stamp no
// event_key (the escalation sweeper, the nudge write) fall back to their own id and so
// group only with themselves.
const dedupeKeyExpr = `COALESCE(NULLIF(nr.context->>'event_key', ''), nr.notification_request_id::text)`

// unreadPredicate is "the caller has not read this yet".
//
// read_at IS NULL is the canonical test. status <> 'read' is belt and braces for any row
// written before this module existed that carried the status without the stamp; a row this
// module marks always gets BOTH.
// dedupeKeyExprNewer is dedupeKeyExpr against the `newer` alias, for the self anti join in
// sqlListNotifications ("does a newer row share my dedupe key?"). Both spellings must stay
// character-identical apart from the alias: they are compared to each other, and the
// nr-side spelling must also match notification_requests_member_dedupe_idx (migration
// 000393) exactly or the planner cannot use that index and the anti join degrades to a hash
// join over the member's whole history.
const dedupeKeyExprNewer = `COALESCE(NULLIF(newer.context->>'event_key', ''), newer.notification_request_id::text)`

const unreadPredicate = `nr.read_at IS NULL AND nr.status <> 'read'`

// sqlListNotifications reads ONE keyset page of the caller's own notifications, newest first.
//
// projection-review: membership=notification_requests at its notification_request_id key,
// filtered to tenant_id = $1 AND context->>'member_id' = the caller's own resolved ACTIVE
// workforce_member_id ($2 via target_member) -- that per-person equality is the ONLY
// audience predicate the feed has and there is no parameter, scope or role that can widen
// it, so a caller cannot address another person's rows at all; grain=one row per
// NOTIFICATION, not per delivery row, via the newest-per-dedupe-key anti join below,
// because QueueRoleNotifications writes one row per recipient DEVICE and a two-phone
// reader must not see one transition twice; group_key=none on the page read -- the only
// aggregate is unread_count, computed by sqlUnreadCount over the SAME tenant + member +
// dedupe grain as the rows so the bell badge can never advertise a notification this feed
// hides, and computed WHOLE-FEED never page-local so paging does not shrink the badge;
// join_cardinality=the single workforce_members LEFT JOIN for actor_name is 1:1 on
// workforce_members_active_user_unique_idx (tenant_id, user_id) WHERE user_id IS NOT NULL
// AND status = 'active', so it can neither multiply nor drop a page row, and it is joined on
// requested_by which is nullable -- a system-raised row simply has no actor name;
// pagination=KEYSET on (requested_at DESC, notification_request_id DESC) with the strict
// row-comparison predicate (requested_at, notification_request_id) < ($3, $4), matching the
// column order of notification_requests_member_feed_idx (tenant_id,
// (context->>'member_id'), requested_at DESC, notification_request_id DESC); NO OFFSET
// anywhere and limit+1 decides "is there a next page" without a second count;
// scope=tenant_id AND the caller's own member id on EVERY branch -- the page read, the
// unread aggregate and both halves of the mark-as-read write.
//
// scale-guard:ignore: workforce-scale read -- ONE person's own notifications. rows are
// produced per work-state transition per recipient device (bounded by headcount x
// transitions), never per animal, so this read does not grow with the herd.
//
// THE LIMIT AND THE KEYSET ARE INSIDE THE SCANNING CTE. That sentence is the whole point of
// this statement's shape, and it is the thing that was wrong here for a year.
//
// What it used to be: a `mine` CTE with DISTINCT ON and no LIMIT, with the keyset and the
// LIMIT outside it. Cost was linear in the member's LIFETIME notification count, not in the
// page size -- every request sorted the whole history and threw away all but 20 rows.
// Measured at 5,000 rows it looked fine (31ms) and the note here used to call it
// "keyset-paged at most 50 rows". By 2026-09-23 the production primary held 236,963 rows /
// 621 MB with 102,291 on the worst single member, and the endpoint was the API's slowest --
// 27 slow (>3s) requests in a 15-minute window and repeated 500s at a flat 15.0s timeout,
// still failing after the API was scaled from 2 instances to 4. The CTE added avoidable work to
// each request; shared-database contention and request fanout also affect endpoint latency.
//
// MIGRATION 000354 FIXED THE SCAN, NOT THE SHAPE. notification_requests_member_feed_idx is
// the NON-PARTIAL sibling of the four per-module alert indexes -- every one of those is
// PARTIAL on (context->>'message_key') LIKE '<prefix>.%', so the unfiltered feed matched
// none of them and the read was a Seq Scan of the WHOLE table. 000354 bounded the read to
// ONE person's rows. It could not bound it to one PAGE, because the LIMIT was outside the
// CTE; only restructuring the statement can do that, and that is what this is.
//
// HOW THE DEDUPE SURVIVES THE PUSHDOWN. DISTINCT ON cannot page: it has to sort a whole
// group set before the outer keyset can cut it. So the dedupe is re-expressed as the
// equivalent per-row test -- "no NEWER row shares my dedupe key" -- which a single row can
// answer on its own, so the planner may stop as soon as $5 rows qualify. Walking
// notification_requests_member_feed_idx in feed order, the first row of each dedupe group
// encountered IS that group's DISTINCT ON winner, because the winner is the group's maximum
// on exactly the (requested_at DESC, notification_request_id DESC) order the index walks.
//
// THE ANTI JOIN IS NOT DECORATION -- DO NOT "SIMPLIFY" IT AWAY. The obvious cheaper fix is
// to push the keyset into the DISTINCT ON and leave the LIMIT outside. It is wrong: a
// dedupe group whose winner sat on the PREVIOUS page still has older delivery rows below
// the cursor, so the group comes back a second time under a different row id. Walked 100
// pages against the 102,291-row fixture, that shape differed from the old query at 1,980 of
// 2,000 positions. The anti join is what makes an already-emitted group stay emitted.
//
// Repeatable large-fixture proof lives in repository_scale_test.go. Page-only timing
// excludes the whole-feed unread aggregate; report both when evaluating endpoint latency.
// First-page and cursor statements are separate so generic prepared plans can use the
// cursor as an index range instead of filtering all rows above it.
//
// THE INNER PROBE NEEDS ITS INDEX. notification_requests_member_dedupe_idx (migration
// 000393) carries dedupeKeyExpr VERBATIM; an expression index is only usable when it matches
// the query's expression character for character, so that migration and dedupeKeyExpr must
// change together or not at all. Without it the planner has no access path for the dedupe
// key and falls back to a Hash Anti Join that reads the member's whole history twice -- the
// measurement that got this rewrite shelved the first time it was proposed.
const sqlNotificationPagePrefix = sqlTargetMemberCTE + `,
mine AS (
  SELECT
    nr.notification_request_id,
    nr.notification_type,
    nr.title,
    nr.body,
    nr.status,
    nr.requested_at,
    nr.read_at,
    nr.requested_by,
    nr.context
  FROM notification_requests nr, target_member tm
  WHERE nr.tenant_id = $1::uuid
    AND tm.workforce_member_id IS NOT NULL
    AND nr.context->>'member_id' = tm.workforce_member_id::text
`

const sqlNotificationPageDedupe = `
    AND NOT EXISTS (
      SELECT 1
      FROM notification_requests newer
      WHERE newer.tenant_id = $1::uuid
        AND newer.context->>'member_id' = tm.workforce_member_id::text
        AND ` + dedupeKeyExprNewer + `
          = ` + dedupeKeyExpr + `
        AND (newer.requested_at, newer.notification_request_id)
          > (nr.requested_at, nr.notification_request_id)
    )
  ORDER BY nr.requested_at DESC, nr.notification_request_id DESC
`

const sqlNotificationPageProjection = `
)
SELECT
  mine.notification_request_id::text,
  mine.notification_type,
  mine.title,
  mine.body,
  mine.status,
  mine.requested_at,
  mine.read_at,
  COALESCE(NULLIF(mine.context->>'actor_name', ''), actor.display_name, '') AS actor_name,
  COALESCE(mine.context->>'task_id', ''),
  COALESCE(mine.context->>'task_no', ''),
  COALESCE(mine.context->>'screen', ''),
  COALESCE(mine.context->>'group_key', ''),
  COALESCE(mine.context->>'priority', ''),
  COALESCE(mine.context->>'message_key', ''),
  COALESCE(mine.context->>'target', ''),
  COALESCE(mine.context->>'status', '')
FROM mine
LEFT JOIN workforce_members actor
  ON actor.tenant_id = $1::uuid
 AND actor.user_id = mine.requested_by
 AND actor.status = 'active'
ORDER BY mine.requested_at DESC, mine.notification_request_id DESC`

// Both statements are compile-time SQL with fixed bind contracts. Keeping the cursor
// predicate out of a nullable OR is necessary even when pgx switches to a generic plan.
const sqlListNotificationsFirst = sqlNotificationPagePrefix + sqlNotificationPageDedupe +
	` LIMIT $3` + sqlNotificationPageProjection

const sqlListNotifications = sqlNotificationPagePrefix + `
    AND (nr.requested_at, nr.notification_request_id) < ($3::timestamptz, $4::uuid)
` + sqlNotificationPageDedupe + ` LIMIT $5` + sqlNotificationPageProjection

// sqlUnreadCount is the caller's WHOLE-FEED unread total.
//
// projection-review: membership=the SAME set as sqlListNotifications -- tenant_id = $1 AND
// context->>'member_id' = the caller's own resolved ACTIVE workforce_member_id -- so the
// badge and the list can never disagree about which notifications exist;
// group_key=none, the aggregate is a single COUNT(DISTINCT dedupeKeyExpr) at the same
// NOTIFICATION grain the list renders, so a two-phone reader's one transition counts once;
// join_cardinality=none, no join at all; pagination=none, this is deliberately NOT
// page-local -- the whole point of the number is the bell badge, which must not shrink when
// the reader pages; scope=tenant_id AND the caller's own member id, the same fail-closed
// target_member CTE.
//
// scale-guard:ignore: workforce-scale aggregate -- one person's own unread notifications,
// served by the same per-member feed indexes as the page read. Never herd-scale.
const sqlUnreadCount = sqlTargetMemberCTE + `
SELECT COUNT(DISTINCT ` + dedupeKeyExpr + `)
FROM notification_requests nr, target_member tm
WHERE nr.tenant_id = $1::uuid
  AND tm.workforce_member_id IS NOT NULL
  AND nr.context->>'member_id' = tm.workforce_member_id::text
  AND ` + unreadPredicate

// sqlMarkRead marks the caller's own notifications read and answers how many NOTIFICATIONS
// (not delivery rows) moved from unread to read.
//
// THE OWNERSHIP FENCE IS APPLIED TWICE, DELIBERATELY. `requested` turns the caller's id
// array into the set of dedupe keys the caller OWNS: an id belonging to somebody else, or
// to no row at all, matches nothing there and therefore contributes no key. The UPDATE then
// re-applies tenant + member on its own rows, so even a key that somehow leaked through
// could not reach a row that is not the caller's. An id the caller does not own is neither
// updated nor counted nor reported -- see app.HTTPError for why it is not a 403 either.
//
// DELIVERY STATUS IS NOT TRAMPLED. A row still in flight ('queued' -> leased 'sending') is
// owned by the dispatcher: rewriting its status to 'read' would strand a lease and make
// MarkSent's own WHERE miss. So read_at is ALWAYS stamped -- that is the canonical read
// state this module reads back -- while status only becomes 'read' for a row whose delivery
// has already finished. The in-flight row keeps its delivery status, gets its read stamp,
// and is correctly counted as read by unreadPredicate.
//
// projection-review: membership=notification_requests filtered to tenant_id = $1 AND
// context->>'member_id' = the caller's own resolved ACTIVE workforce_member_id AND the
// dedupe keys of the caller-owned ids in $3 -- the identical audience predicate the read
// path uses, so nothing can be marked that could not be listed; grain=every delivery row of
// each affected notification is stamped together, which is what keeps the notification's
// read state homogeneous across a two-phone reader's rows and therefore keeps
// COUNT(DISTINCT) in sqlUnreadCount exact; group_key=none, the returned aggregate is
// COUNT(DISTINCT dedupeKeyExpr) over the rows that actually transitioned, at the same
// NOTIFICATION grain the list renders, so read_count matches the number of cards the UI
// dims; join_cardinality=the `requested` CTE is at most 200 ids and joins by dedupe key,
// bounded by one page's worth of notifications x the reader's device count;
// pagination=none, this is a bounded-id write, and there is NO OFFSET; scope=tenant_id AND
// the caller's own member id on BOTH the id-resolution half and the UPDATE half.
//
// scale-guard:ignore: bounded write -- at most 200 caller-owned ids, expanded only to that
// same reader's own delivery rows. Never herd-scale.
const sqlMarkRead = sqlTargetMemberCTE + `,
requested AS (
  SELECT DISTINCT ` + dedupeKeyExpr + ` AS dedupe_key
  FROM notification_requests nr, target_member tm
  WHERE nr.tenant_id = $1::uuid
    AND tm.workforce_member_id IS NOT NULL
    AND nr.context->>'member_id' = tm.workforce_member_id::text
    AND nr.notification_request_id = ANY($3::uuid[])
),
marked AS (
  UPDATE notification_requests nr
  SET read_at = now(),
      status = CASE
        WHEN nr.status IN ('sent', 'failed', 'exhausted', 'suppressed') THEN 'read'
        ELSE nr.status
      END,
      updated_at = now()
  FROM target_member tm, requested rq
  WHERE nr.tenant_id = $1::uuid
    AND tm.workforce_member_id IS NOT NULL
    AND nr.context->>'member_id' = tm.workforce_member_id::text
    AND ` + dedupeKeyExpr + ` = rq.dedupe_key
    AND ` + unreadPredicate + `
  RETURNING ` + dedupeKeyExpr + ` AS dedupe_key
)
SELECT COUNT(DISTINCT dedupe_key) FROM marked`

// The shared idempotency_keys contract, reserved in the SAME transaction as the side
// effect: an exact replay returns the ORIGINAL read_count with no second write, and the
// same key re-presented with a DIFFERENT id set is refused instead of applied.
//
// read_count is carried in result_type ("notification_read:<n>") rather than result_id,
// because result_id is a uuid column and a count is not a uuid. That is the only place this
// module deviates from the leadership-tasks reservation helper it otherwise copies.
const (
	sqlIdempotencyReserve = `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status)
VALUES ($1, $2::uuid, $3, $4, 'started')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key`
	sqlIdempotencyLoad = `
SELECT request_hash, status, COALESCE(result_type, '')
FROM idempotency_keys
WHERE idempotency_key = $1`
	sqlIdempotencyComplete = `
UPDATE idempotency_keys
SET status = 'completed', result_type = $2, completed_at = now()
WHERE idempotency_key = $1`
)

const idempotencyScope = "notificationcentre.mark_read"

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// ListNotifications returns one keyset page of the caller's own notifications plus their
// whole-feed unread total.
func (r *Repository) ListNotifications(ctx context.Context, p ports.ListParams) (domain.Page, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cursor, err := decodeFeedCursor(p.Cursor)
	if err != nil {
		return domain.Page{}, err
	}
	var cursorAt, cursorID any
	if cursor.RequestID != "" {
		cursorAt = cursor.RequestedAt
		cursorID = cursor.RequestID
	}
	limit := p.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}

	// limit+1 decides whether a next page exists; no count query, no OFFSET.
	var rows pgx.Rows
	if cursor.RequestID == "" {
		rows, err = r.pool.Query(ctx, sqlListNotificationsFirst, p.TenantID, p.MemberOrUserID, limit+1)
	} else {
		rows, err = r.pool.Query(ctx, sqlListNotifications, p.TenantID, p.MemberOrUserID, cursorAt, cursorID, limit+1)
	}
	if err != nil {
		return domain.Page{}, fmt.Errorf("notificationcentre: list: %w", err)
	}
	defer rows.Close()

	page := domain.Page{Items: make([]domain.Notification, 0, limit)}
	stamps := make([]time.Time, 0, limit+1)
	for rows.Next() {
		var (
			item        domain.Notification
			requestedAt time.Time
			readAt      *time.Time
		)
		if err := rows.Scan(
			&item.NotificationRequestID,
			&item.NotificationType,
			&item.Title,
			&item.Body,
			&item.Status,
			&requestedAt,
			&readAt,
			&item.ActorName,
			&item.Context.TaskID,
			&item.Context.TaskNo,
			&item.Context.Screen,
			&item.Context.GroupKey,
			&item.Context.Priority,
			&item.Context.MessageKey,
			&item.Context.Target,
			&item.Context.Status,
		); err != nil {
			return domain.Page{}, fmt.Errorf("notificationcentre: scan notification: %w", err)
		}
		item.RequestedAt = requestedAt.UTC().Format(time.RFC3339Nano)
		if readAt != nil {
			item.ReadAt = readAt.UTC().Format(time.RFC3339Nano)
		}
		page.Items = append(page.Items, item)
		stamps = append(stamps, requestedAt)
	}
	if err := rows.Err(); err != nil {
		return domain.Page{}, fmt.Errorf("notificationcentre: list rows: %w", err)
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = encodeFeedCursor(feedCursor{RequestedAt: stamps[limit-1], RequestID: last.NotificationRequestID})
	}

	if err := r.pool.QueryRow(ctx, sqlUnreadCount, p.TenantID, p.MemberOrUserID).Scan(&page.UnreadCount); err != nil {
		return domain.Page{}, fmt.Errorf("notificationcentre: unread count: %w", err)
	}
	return page, nil
}

// ---------------------------------------------------------------------------
// Write
// ---------------------------------------------------------------------------

// MarkRead marks the caller's own notifications read inside one transaction alongside the
// idempotency reservation, and returns how many notifications moved from unread to read.
func (r *Repository) MarkRead(ctx context.Context, p ports.MarkReadParams) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("notificationcentre: begin mark read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	scoped := p.TenantID + ":" + idempotencyScope + ":" + strings.TrimSpace(p.IdempotencyKey)
	// The fingerprint covers WHO is marking and WHAT set: the same key from the same person
	// for the same notifications is a replay, while the same key for a different set is the
	// client reusing a key it should not have and is refused.
	fingerprint := markReadFingerprint(p)

	var claimed string
	err = tx.QueryRow(ctx, sqlIdempotencyReserve, scoped, p.TenantID, idempotencyScope, fingerprint).Scan(&claimed)
	switch {
	case err == nil:
		// Fresh key: do the work below.
	case errors.Is(err, pgx.ErrNoRows):
		var existingHash, status, resultType string
		if err := tx.QueryRow(ctx, sqlIdempotencyLoad, scoped).Scan(&existingHash, &status, &resultType); err != nil {
			return 0, fmt.Errorf("notificationcentre: load idempotency: %w", err)
		}
		if existingHash != fingerprint {
			return 0, ports.ErrIdempotencyConflict
		}
		return parseReadCount(resultType), nil
	default:
		return 0, fmt.Errorf("notificationcentre: reserve idempotency: %w", err)
	}

	var readCount int
	if err := tx.QueryRow(ctx, sqlMarkRead, p.TenantID, p.MemberOrUserID, p.IDs).Scan(&readCount); err != nil {
		return 0, fmt.Errorf("notificationcentre: mark read: %w", err)
	}
	if _, err := tx.Exec(ctx, sqlIdempotencyComplete, scoped, fmt.Sprintf("notification_read:%d", readCount)); err != nil {
		return 0, fmt.Errorf("notificationcentre: complete idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("notificationcentre: commit mark read: %w", err)
	}
	return readCount, nil
}

// markReadFingerprint hashes the effect of the operation: the tenant, the caller, and the
// SORTED id set, so the same batch sent twice in a different order is still recognised as
// the same replay.
func markReadFingerprint(p ports.MarkReadParams) string {
	ids := make([]string, len(p.IDs))
	copy(ids, p.IDs)
	sort.Strings(ids)
	parts := append([]string{p.TenantID, p.MemberOrUserID}, ids...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// parseReadCount reads the replayed count back out of result_type.
func parseReadCount(resultType string) int {
	const prefix = "notification_read:"
	if !strings.HasPrefix(resultType, prefix) {
		return 0
	}
	n := 0
	if _, err := fmt.Sscanf(resultType[len(prefix):], "%d", &n); err != nil {
		return 0
	}
	return n
}
