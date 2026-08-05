"use server";

import { createHash } from "node:crypto";

import { postVerificationReviewEvents } from "@/lib/api/server";
import type { PendingEvent } from "./review-events";

/**
 * Server Action to post review events to the backend.
 * Never expose the bearer token to the browser; this Server Action bridges to
 * the authenticated backend call.
 */
export async function submitVerificationReviewEvents(events: PendingEvent[]): Promise<void> {
  if (events.length === 0) return;

  // Replay protection: each event already carries a client-minted `client_event_id`, which the
  // backend dedupes on (tenant_id, client_event_id) — so a retried or beaconed batch inserts zero
  // duplicates. The batch-level Idempotency-Key below is derived from those ids rather than random,
  // so the SAME batch retried is the SAME key end to end.
  const idempotencyKey = createHash("sha256")
    .update(events.map((event) => event.client_event_id).join(":"))
    .digest("hex")
    .slice(0, 32);

  // Transform PendingEvent[] to the backend contract shape
  const apiEvents = events.map((e) => ({
    item_id: e.item_id,
    proof_id: e.proof_id ?? undefined,
    session_id: e.session_id,
    event_type: e.event_type,
    occurred_at: e.occurred_at,
    client_event_id: e.client_event_id,
    payload: e.payload,
  }));

  const result = await postVerificationReviewEvents(apiEvents, idempotencyKey);
  if (!result.ok) {
    throw new Error(
      `Failed to post verification review events: ${result.error.kind} ${result.error.code}`,
    );
  }
}
