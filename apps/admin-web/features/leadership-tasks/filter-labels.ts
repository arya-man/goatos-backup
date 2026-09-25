import { fmtDate } from "../../lib/format.ts";

/**
 * A date span as a filter chip / the Dates disclosure states it, in the console's one date shape
 * (DD/MM/YYYY -- AGENTS.md "Every Visible Date Is DD/MM/YYYY"). The URL keeps ISO business dates;
 * only the words change. A one-day span names the day once. An EN DASH joins the two ends.
 */
export function dateSpanLabel(from: string, to: string): string {
  const a = from ? fmtDate(from) : "";
  const b = to ? fmtDate(to) : "";
  if (a && b) return a === b ? a : `${a} – ${b}`;
  return a || b;
}

/**
 * The names for a person filter's picked ids ("Dinakar, Manju"). An id with no matching option --
 * a person no longer on the roster -- reads as `unknownLabel`, never as the raw id.
 */
export function personFilterLabel(
  options: ReadonlyArray<{ value: string; label: string }>,
  ids: readonly string[],
  unknownLabel: string,
): string {
  return ids.map((id) => options.find((option) => option.value === id)?.label ?? unknownLabel).join(", ");
}
