/**
 * Display words for lifecycle stage codes.
 *
 * The farm's stored stage codes still carry the legacy "F2" token for the fattening stage
 * ("F2", "F2-Male", "ICU-F2-Male"). Readers must never see "F2" (design rule, 2026-09-19): the
 * token reads as "Fattening" while every other part of the code stays the farm's own word, so
 * "F2-Male" reads "Fattening-Male" and "ICU-F2-Male" reads "ICU-Fattening-Male".
 *
 * PRESENTATION ONLY. Keys, filters, URL parameters, sort order and what the phone posts are
 * untouched -- call this at the point a stage is rendered, never where it is compared or sent.
 */
const FATTENING_TOKEN = /(^|[^A-Za-z0-9])F2(?![A-Za-z0-9])/gi;

export const FATTENING_STAGE_WORD = "Fattening";

export function stageLabel(raw: string | null | undefined): string {
  if (!raw) return raw ?? "";
  return raw.replace(FATTENING_TOKEN, (_match, lead: string) => `${lead}${FATTENING_STAGE_WORD}`);
}

/** `stageLabel` for anything that may or may not be text (table cells, chart labels). */
export function stageLabelNode<T>(value: T): T | string {
  return typeof value === "string" ? stageLabel(value) : value;
}
