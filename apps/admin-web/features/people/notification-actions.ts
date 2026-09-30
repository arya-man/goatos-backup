"use server";

// Write path for the People / HRMS Notifications tab (maintainer decision 2026-09-08).
//
// A Server Action rather than a client fetch: the call is authenticated through the server
// config, and the matrix must not navigate or trigger a page-level load to save one row.
import { revalidatePath } from "next/cache";

import {
  saveNotificationAudience,
  type NotificationAudienceAlertRow,
  type SaveNotificationAudienceRequest,
} from "@/lib/api/server";

// Notifications is its own HRMS page since 2026-09-30.
const PEOPLE_PATH = "/people/notifications";

export type SaveAudienceResult =
  | { ok: true; row: NotificationAudienceAlertRow }
  | { ok: false; message: string };

/**
 * Replace one alert's audience (or reset it to the default). The failure message is rendered
 * VERBATIM by the matrix, so it stays the backend's farm-worded reason.
 */
export async function saveNotificationAudienceAction({
  alertKey,
  body,
}: {
  alertKey: string;
  body: SaveNotificationAudienceRequest;
}): Promise<SaveAudienceResult> {
  if (!Number.isInteger(body.row_version) || body.row_version < 0) {
    return { ok: false, message: "Reload the page to see the current settings, then try again." };
  }
  const result = await saveNotificationAudience(alertKey, body);
  if (!result.ok) {
    return {
      ok: false,
      message:
        result.error.message ||
        "That could not be saved. Reload the page to see the current settings, then try again.",
    };
  }
  revalidatePath(PEOPLE_PATH);
  return { ok: true, row: result.data };
}
