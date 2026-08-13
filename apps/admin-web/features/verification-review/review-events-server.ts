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
    // The `permanent:` prefix tells the client buffer to DROP this batch instead of re-queueing it.
    // Telemetry ingest is verifier-only authority, so a leadership principal browsing the queue gets
    // a forbidden that retrying can never fix — without this the buffer would grow forever behind an
    // unsatisfiable retry. A validation rejection is equally hopeless on replay.
    const status = result.error.status ?? 0;
    const permanentKind =
      result.error.kind === "permission_denied" ||
      result.error.kind === "unauthorized" ||
      result.error.kind === "tenant_scope_mismatch" ||
      result.error.kind === "bad_request" ||
      result.error.kind === "not_found" ||
      result.error.kind === "missing_config";
    // A 4xx that is not a timeout or a rate limit will reject the same batch identically forever.
    const permanentStatus = status >= 400 && status < 500 && status !== 408 && status !== 429;
    const permanent = permanentKind || permanentStatus;
    throw new Error(
      `${permanent ? "permanent" : "transient"}: verification review events rejected: ${result.error.kind} ${result.error.code}`,
    );
  }
}
