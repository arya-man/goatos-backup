import type { CalendarDateMarker, CalendarEvent } from "./calendar-contract";

export type CalendarMarkerTone = "drive" | "due" | "deferred" | "other" | "history";

const OPEN_DUE_STATUSES = new Set(["due", "overdue", "in_progress", "proof_pending", "verification_pending", "rework_due", "blocked"]);

export function markerTonesForDate(marker: CalendarDateMarker | undefined, events: CalendarEvent[]): CalendarMarkerTone[] {
  const tones = new Set<CalendarMarkerTone>();

  for (const event of events) {
    const summary = event.drive_summary ?? null;
    if (summary?.deferred_count && summary.deferred_count > 0) tones.add("deferred");
    if ((summary?.due_count ?? 0) > 0 || (summary?.overdue_count ?? 0) > 0) tones.add("due");
    if (event.event_type === "vaccination_drive" || (summary?.total_count ?? 0) > 0) tones.add("drive");
    if (event.status === "deferred" || event.deferred_count > 0) tones.add("deferred");
    if (OPEN_DUE_STATUSES.has(event.status)) tones.add("due");
    if (event.status === "completed") tones.add("history");
  }

  if ((marker?.deferred_count ?? 0) > 0) tones.add("deferred");
  if ((marker?.due_count ?? 0) > 0 || (marker?.overdue_count ?? 0) > 0) tones.add("due");
  if ((marker?.drive_count ?? 0) > 0) tones.add("drive");
  if ((marker?.completed_count ?? 0) > 0) tones.add("history");
  if ((marker?.open_count ?? 0) > 0 && tones.size === 0) tones.add("other");

  const priority: CalendarMarkerTone[] = ["drive", "due", "deferred", "other", "history"];
  return priority.filter((tone) => tones.has(tone));
}
