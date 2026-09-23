-- +goose Up
-- +goose NO TRANSACTION
--
-- THE SIBLING INDEX THAT LETS THE FEED PAGE READ A PAGE INSTEAD OF A HISTORY.
--
-- 000354 added notification_requests_member_feed_idx and bounded the /app/notifications
-- read to ONE person's rows instead of the whole table. It did not bound it to one PAGE:
-- the `mine` CTE still had no LIMIT, so every request materialised that member's entire
-- deduped history before the keyset and the LIMIT -- which sat outside the CTE -- threw all
-- but 20 rows away. Measured on a throwaway database seeded to the production primary's
-- worst member (102,291 delivery rows, 68,194 notifications, 2026-09-23): first page
-- 355.6ms, deep page 348.8ms, each one scanning all 102,291 rows and spilling a 41MB
-- external merge sort to disk. That endpoint is the API's slowest and returns 500 at a flat
-- 15.0s timeout under load.
--
-- The fix pushes the keyset and the LIMIT INSIDE the CTE, which means the query can no
-- longer dedupe by sorting the whole history: it must decide "is this row the newest of its
-- event_key group?" one row at a time, as a NOT EXISTS anti join, while walking
-- notification_requests_member_feed_idx in feed order. THIS index is what makes that
-- decision an index probe instead of a scan.
--
-- WITHOUT IT the planner has no access path for the dedupe key and falls back to a Hash
-- Anti Join that reads the member's whole history twice -- which is exactly the measurement
-- recorded in repository.go when the anti-join rewrite was first proposed and shelved
-- ("reads the member's 7,500 rows twice as a Hash Anti Join, 24.8ms -- so it needs rework
-- before it is worth doing"). This index IS that rework. With it the plan is a Nested Loop
-- Anti Join: first page 0.31ms, deep page 0.22ms, 31 and 32 rows scanned.
--
-- COLUMN ORDER mirrors the dedupe decision exactly: tenant and member are equality keys,
-- the dedupe key is an equality key, and (requested_at, notification_request_id) DESC is
-- the range half of the probe ("does a NEWER row with this key exist?"). The dedupe
-- expression is dedupeKeyExpr from
-- backend/internal/notificationcentre/adapters/postgres/repository.go VERBATIM -- an
-- expression index is only usable when it matches the query's expression character for
-- character, so these two must be changed together or not at all.
--
-- ISOLATION: this index is on notification_requests alone. It references no goats,
-- goat_identifiers, herd_animals, obligation, protocol or vaccination object, and creates
-- no path by which the feed read could acquire one. Rows are produced per work-state
-- transition per recipient device (bounded by headcount x transitions), never per animal.
--
-- LOCK SAFETY: CONCURRENTLY + NO TRANSACTION, so the build takes no ACCESS EXCLUSIVE lock
-- on notification_requests. That table is on the hot notification dispatch path (the
-- queue/lease indexes are read and updated continuously by the dispatcher), so a blocking
-- CREATE INDEX here would stall delivery for every module, not just the notification centre.
CREATE INDEX CONCURRENTLY IF NOT EXISTS notification_requests_member_dedupe_idx
ON public.notification_requests (
  tenant_id,
  (context->>'member_id'),
  (COALESCE(NULLIF(context->>'event_key', ''), notification_request_id::text)),
  requested_at DESC,
  notification_request_id DESC
);

-- notification_requests_member_feed_idx is deliberately NOT dropped. It is still the
-- DRIVING scan of the feed read (walk one member's rows newest-first); this one is only
-- the inner probe. Neither replaces the other.

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.notification_requests_member_dedupe_idx;
