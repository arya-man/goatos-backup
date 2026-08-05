"use server";

import { postVerificationReviewEvents } from "@/lib/api/server";
import type { PendingEvent } from "./review-events";

/**
 * Server Action to post review events to the backend.
 * Never expose the bearer token to the browser; this Server Action bridges to
 * the authenticated backend call.
 */
export async function submitVerificationReviewEvents(events: PendingEvent[]): Promise<void> {
  if (events.length === 0) return;

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

  const result = await postVerificationReviewEvents(apiEvents);
  if (!result.ok) {
    throw new Error(
      `Failed to post verification review events: ${result.error.kind} ${result.error.code}`,
    );
  }
}
