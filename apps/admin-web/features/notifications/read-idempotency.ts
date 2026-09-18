import { createHash } from "node:crypto";

/**
 * The idempotency key for a "mark these rows read" write.
 *
 * WHY THIS IS ITS OWN FILE. It is derived rather than typed, the backend REFUSES a key outside
 * 8..200 characters (`invalid_idempotency_key`, 400 -- see the notification centre handler's
 * idempotencyKey gate), and the previous derivation could not honour that. It therefore has to be
 * unit-testable against the real function, and it needs `node:crypto`, which must not be pulled
 * into `notification-model.ts` because a Client Component imports that.
 *
 * WHAT WENT WRONG, because the fix only makes sense next to it. The key was the sorted ids
 * CONCATENATED: `admin-web-notifications-read:${[...ids].sort().join(",")}`. That is 28 + 37n
 * characters, so it fit up to FOUR notifications (176) and 400'd from FIVE (213) -- and
 * `idsToMarkAllRead` returns up to NOTIFICATION_PAGE_LIMIT = 20, which is 768. A leader with six
 * unread clicked "Mark all read", the optimistic state cleared every row and dropped the badge to
 * zero, the write 400'd, the bell deliberately keeps rows read locally on failure, and the next
 * route change brought all six back bold. NOTHING was ever marked read server-side. Single-row
 * clicks (n=1, 65 characters) worked, which is exactly why manual testing never saw it.
 *
 * WHY A HASH AND NOT A TRUNCATION OR A COUNT. The key must stay a function of the id SET and
 * nothing else, because that is what makes the same click twice -- or a retried Server Action --
 * one write, and a DIFFERENT set its own write. Truncating the concatenation would make two
 * different sets sharing a prefix collide onto one key, so the second, genuinely different write
 * would be swallowed as a replay. A sha256 of the sorted ids is stable for the same set, bounded
 * at a constant 93 characters for any n, and order-independent in the same way the old key was.
 */
const KEY_PREFIX = "admin-web-notifications-read:";

/** The backend's own ceiling, mirrored so an oversized key is impossible rather than refused. */
export const MAX_IDEMPOTENCY_KEY_LENGTH = 200;
/** The backend's own floor. A hashed key clears it by construction; asserted, not assumed. */
export const MIN_IDEMPOTENCY_KEY_LENGTH = 8;

export function notificationsReadIdempotencyKey(ids: readonly string[]): string {
  // Sorted, so the key is a property of the SET rather than of the click order the panel
  // happened to collect the rows in. The separator is kept inside the hashed material so two
  // id lists cannot be re-partitioned into the same string.
  const material = [...ids].sort().join(",");
  return `${KEY_PREFIX}${createHash("sha256").update(material).digest("hex")}`;
}

/**
 * The same bound the leadership-tasks actions check on every write in this PR
 * (`features/leadership-tasks/actions.ts`). Here it can only fail if someone changes the
 * derivation above, which is precisely when it should.
 */
export function isUsableIdempotencyKey(key: string): boolean {
  return key.length >= MIN_IDEMPOTENCY_KEY_LENGTH && key.length <= MAX_IDEMPOTENCY_KEY_LENGTH;
}
