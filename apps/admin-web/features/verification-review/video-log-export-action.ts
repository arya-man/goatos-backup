"use server";

import { getVerificationVideoLog } from "@/lib/api/server";

export type VideoLogExport = {
  /** Header row followed by one row per VIDEO, ready to be written as CSV. */
  rows: string[][];
  /** True when the day exceeded the export bound, so the caller can say the file is partial. */
  truncated: boolean;
};

/**
 * Builds the WHOLE DAY's video log for export: every shed in scope, one row per video.
 *
 * Fetched on CLICK rather than rendered with the panel. The summary screen deliberately does not
 * carry every row -- a vaccination drive raises one item per animal, so a park-day runs to
 * thousands of proofs -- and paying that cost on every panel open, for a file most viewers never
 * ask for, is exactly the over-fetch the two-level design avoids. The export pays it once, on
 * demand, and only into a file.
 *
 * A server action rather than a route handler: this is a READ that needs the caller's session, and
 * the action gets it from the same authenticated server client every other read on this page uses.
 * The backend gates it on permissions.VerificationEvidenceTimeline exactly as it gates the panel,
 * so this opens no path a caller did not already have.
 */
export async function exportVideoLogAction(input: {
  businessDate: string;
  parkId?: string;
  /** Column headers, passed in so the file is labelled in the same backend-owned words as the screen. */
  headers: string[];
  /** Copy for a proof that was registered but never arrived. */
  awaitingLabel: string;
}): Promise<VideoLogExport> {
  const result = await getVerificationVideoLog({
    businessDate: input.businessDate,
    parkId: input.parkId,
    allSheds: true,
  });
  if (!result.ok) return { rows: [input.headers], truncated: false };

  const { business_date: day, rows, rows_truncated: truncated } = result.data;
  const body = rows.flatMap((row) => {
    // The producing module's own words plus its operator. Empty parts are dropped rather than left
    // as gaps -- feed transport legitimately writes no subject label.
    const work = [
      row.category_label?.trim() || row.module_label?.trim() || "",
      row.subject_label ?? "",
      row.operator_name ?? "",
    ]
      .filter((part) => part !== "")
      .join(" · ");
    return row.proofs.map((proof) => [
      day,
      // Park FIRST, then the location: shed names repeat across parks, so the location alone would
      // render two different sheds identically in a file that spans every park.
      row.park_label ?? "",
      // The row's OWN location: this file spans every shed, so a line that could not name its shed
      // would be unreadable.
      row.operational_location_display ?? "",
      work,
      proof.label ?? "",
      // Full date-time, not the bare clock the screen shows: a spreadsheet cell has no day picker
      // above it, and a proof that landed the next morning would otherwise read as same-day.
      proof.uploaded_at ? isoToIstDisplay(proof.uploaded_at) : input.awaitingLabel,
    ]);
  });
  return { rows: [input.headers, ...body], truncated };
}

/**
 * "YYYY-MM-DD HH:MM" in Asia/Kolkata.
 *
 * Formatted here, on the server, rather than shipped as an ISO instant for the browser to render:
 * the file must read in FARM time regardless of where the person downloading it is sitting, and a
 * client-side format would silently produce a different file in another timezone.
 */
function isoToIstDisplay(iso: string): string {
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) return iso;
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Kolkata",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).formatToParts(parsed);
  const get = (type: string) => parts.find((part) => part.type === type)?.value ?? "";
  return `${get("year")}-${get("month")}-${get("day")} ${get("hour")}:${get("minute")}`;
}
