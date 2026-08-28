"use server";

// Detail-fetch path for the Clock In / Out drawer. A Server Action rather than
// a Route Handler or a client fetch: the call is authenticated through the
// server config, and the overlay must not navigate or trigger a page-level
// load to read its own detail (the local-overlay contract in AGENTS.md).

import { getAdminClockEntry, type ClockEntryDetail } from "@/lib/api/server";

export type LoadClockEntryResult = { ok: true; detail: ClockEntryDetail } | { ok: false; message: string };

/** One clocking's full capture: both punches with location, device, integrity. */
export async function loadClockEntryDetailAction(clockEntryId: string): Promise<LoadClockEntryResult> {
  const result = await getAdminClockEntry(clockEntryId);
  if (!result.ok) {
    return { ok: false, message: result.error.message || (result.error.code ?? result.error.kind) };
  }
  return { ok: true, detail: result.data };
}
