"use server";

import { getFeedPackingVerificationLog, type FeedPackingVerificationLogResponse } from "@/lib/api/server";

const ISO_DAY = /^\d{4}-\d{2}-\d{2}$/;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Loads ONE feed day for the Feed Verification drawer, called by the drawer itself when its button
 * opens it. This is what keeps the read OFF the /verify render path: the page no longer fetches the
 * log for a closed panel, so the queue and every Accept redirect skip it entirely.
 *
 * Same endpoint and the same capability gate as the server render (permissions.VerificationFeedPackingLog);
 * a caller without it gets the backend's refusal, never data.
 *
 * server-action-read-only: GET-backed panel read; no mutation replay key required.
 */
export async function loadFeedVerificationLogAction(
  feedDay?: string,
  parkId?: string,
): Promise<{ ok: true; log: FeedPackingVerificationLogResponse } | { ok: false; code: string }> {
  if (feedDay && !ISO_DAY.test(feedDay)) return { ok: false, code: "invalid_feed_day" };
  if (parkId && !UUID.test(parkId)) return { ok: false, code: "invalid_park" };
  const result = await getFeedPackingVerificationLog({ feedDay: feedDay || undefined, parkId: parkId || undefined });
  if (!result.ok) return { ok: false, code: result.error.code ?? result.error.kind };
  return { ok: true, log: result.data };
}
