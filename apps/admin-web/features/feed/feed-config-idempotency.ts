// The Feed Config idempotency-key lifecycle, extracted as a pure state machine so it is testable
// without mounting FeedConfigFormShell's React tree.
//
// THE BUG THIS CLOSES: `upsertFeedConfig*` (lib/api/server.ts) used to default the
// Idempotency-Key header to a fresh `randomUUID()` generated INSIDE the server action call. A
// server action is just an RPC: a lost/ambiguous network response does not tell the caller
// whether the write landed, and the caller's only safe move is to retry with the SAME key so the
// backend can recognize an exact replay and return the original result instead of applying the
// write again. Minting the default key per-call meant every resubmit of the SAME open form --
// including a genuine retry after a lost response -- silently sent a DIFFERENT key, so the
// idempotency contract on the backend never actually protected this screen.
//
// THE FIX: mint the key once per editing INTENT (when the form opens) and reuse it for every
// submit of that open form until a CONFIRMED successful write. Only a confirmed success rotates
// the key, because the next submit after a real success is a genuinely new intent (the form stays
// open to show the "Saved" message and can be edited again) and must not replay the completed
// write's key. A rejected/failed outcome had no side effect, so it keeps the same key -- an
// immediate retry (lost response, or the operator fixing a validation error) stays one intent.

export type FeedConfigIdempotencyState = {
  open: boolean;
  key: string | null;
};

export const CLOSED_STATE: FeedConfigIdempotencyState = { open: false, key: null };

/** The form opens: a fresh key for a fresh editing intent. */
export function openIntent(mintKey: () => string): FeedConfigIdempotencyState {
  return { open: true, key: mintKey() };
}

/**
 * One submit attempt resolved. `ok` is whether the write was CONFIRMED to have succeeded.
 *
 * - ok=false (rejected/failed/thrown): no side effect occurred, so the key is UNCHANGED. A retry
 *   of the exact same intent -- including a resubmit after a lost/ambiguous response, where the
 *   write may already have landed server-side -- replays the same key.
 * - ok=true: the write is confirmed to have landed. The form stays open (so the "Saved" message is
 *   visible), but the NEXT submit is a new intent, so the key is rotated now rather than waiting
 *   for the next open.
 */
export function afterSubmit(
  state: FeedConfigIdempotencyState,
  ok: boolean,
  mintKey: () => string,
): FeedConfigIdempotencyState {
  if (!state.open) return state;
  if (!ok) return state;
  return { open: true, key: mintKey() };
}
