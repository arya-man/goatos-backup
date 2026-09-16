// Shared constants for the Alerts page. Plain module (no "use client") so the server page and the
// client drawer read the SAME string: a constant exported from a client module reaches a server
// component as a client reference, not a value.
export const ALERTS_PATH = "/alerts";
export const PARAM_CONFIGURE = "configure";

// An empty filtered table is not evidence that every enabled check succeeded.
export function alertsEmptyState(input: {
  visibleRows: number;
  totalRows: number;
  allFailed: boolean;
  incomplete: boolean;
  rulesRun: number;
}): string | null {
  if (input.allFailed || input.visibleRows > 0) return null;
  if (input.incomplete) return "state.empty.incomplete";
  if (input.totalRows > 0) return "state.empty.filtered";
  if (input.rulesRun === 0) return "state.empty.no_rules";
  return "state.empty";
}
