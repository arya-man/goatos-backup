import type { TaskRow } from "./task-row";

/**
 * Which row the task drawer shows, given what it has cached and what the list now says.
 *
 * The drawer keeps the detail rows it has read (notes + activity, which the list does not carry).
 * It used to show that cache whenever it had one, so a task moved elsewhere -- the list row now
 * at v7 Done -- reopened as the cached v5 "In progress" and offered moves the backend would
 * refuse (2026-09-25). The row with the higher `rowVersion` wins; the cached detail-only fields
 * (feed, notes) ride along ONLY when the cache is the same version as the list row, because a
 * newer list row means that feed is missing whatever moved it.
 *
 * `detailLoaded` says whether the returned row carries a feed the drawer can show as final.
 */
export function pickDrawerRow(
  cached: TaskRow | undefined,
  summary: TaskRow | null,
): { row: TaskRow | null; detailLoaded: boolean } {
  if (!cached) return { row: summary, detailLoaded: false };
  if (!summary) return { row: cached, detailLoaded: true };
  if (summary.rowVersion > cached.rowVersion) return { row: summary, detailLoaded: false };
  return { row: cached, detailLoaded: true };
}

/** A published patch on top of the row, unless the patch is older than the row. */
export function withPatch(row: TaskRow, patch: Partial<TaskRow> | undefined): TaskRow {
  if (!patch) return row;
  if (typeof patch.rowVersion === "number" && patch.rowVersion < row.rowVersion) return row;
  return { ...row, ...patch };
}

/**
 * Whether a list island (the table, the filter chips) may lay a published patch over its server
 * row. The board already guards this (`task-board-dnd.tsx`): a patch older than the server row is
 * a leftover from before the last full render and must not repaint the row backwards.
 */
export function patchApplies(serverRowVersion: number, patch: Partial<TaskRow> | undefined): boolean {
  if (!patch) return false;
  return !(typeof patch.rowVersion === "number" && patch.rowVersion < serverRowVersion);
}

/**
 * The board's rows: the list page plus the first page of cancelled tasks read beside it, each
 * task once. Only CANCELLED rows are taken from the extra read, so a backend that ignored the
 * filter can never put a live task on the board twice.
 */
export function withCancelledRows(rows: readonly TaskRow[], cancelled: readonly TaskRow[]): TaskRow[] {
  if (!cancelled.length) return [...rows];
  const seen = new Set(rows.map((row) => row.id));
  return [...rows, ...cancelled.filter((row) => row.status === "cancelled" && !seen.has(row.id))];
}
