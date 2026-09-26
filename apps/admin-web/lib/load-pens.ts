// Shared "which pens is this load in" label, for every chart on the dashboard that names a LOAD
// (maintainer request 2026-09-22). A load number on its own tells a reader which invoice the
// animals came on; it does not tell them where to walk. Every load chart therefore carries its
// pens in a bracket beside the load — "131 (CPT Castro 1, CPT Castro 2)".
//
// This is the ONE composition, for the same reason `operational-location.ts` is: the load charts
// live in three features (Weights, ADG Load-wise, Sales › Purchase and born, Counts Breakdown)
// and a hand-rolled bracket in each would drift in separator, order and overflow rule — the OL-7
// defect class, one noun over. It generalises the `penList` helper that lived privately inside
// load-comparison-tab.tsx and keeps that file's rules verbatim.
//
// TWO RULES THAT MUST NOT BE RELAXED:
//
//  1. THE PARK IS PART OF THE PEN NAME HERE (maintainer, 2026-09-05). "Castro 1" exists in BOTH
//     parks, so an unqualified bracket on a chart that mixes parks names a pen that could be
//     either one — the OL-1 name-merge defect. The park code is prefixed whenever the source
//     knows it; a pen whose park could not be resolved is still named rather than dropped.
//  2. THE BRACKET IS NEVER INVENTED. A load whose pens are unknown (the weighing read was not
//     fetched for this principal, the load has no tagged pen, or its animals have left) gets an
//     EMPTY string and renders exactly as it did before this helper existed. It must never fall
//     back to the park, the vendor, or a guess.
//
// What a pen head count in this bracket MEANS is the caller's business, and the callers differ on
// purpose: the weighing-backed charts count animals at the pen's LATEST WEIGH, while Counts
// Breakdown counts the pen's live residents. This helper therefore takes names only and never a
// count — a number whose meaning changes by caller has no place in a shared label.

export type LoadPen = {
  /** Park short code (CBE, CPT), or "" when the source cannot say. */
  park: string;
  /** The backend-composed operational location display ("Castro 1", "Godel 2 - Part 1"). */
  pen: string;
  /** Ordering weight only — the bigger placement reads first. Absent sorts last. */
  animals?: number;
};

/** The default number of pens a bracket spells out before it counts the rest. */
export const LOAD_PEN_BRACKET_MAX = 2;

/**
 * The park-qualified pen names for one load: biggest placement first, deduped, blanks dropped.
 * Ties keep the order the caller received, which is the backend's own (park, then shed).
 */
export function loadPenNames(pens: readonly LoadPen[] | null | undefined): string[] {
  const ordered = (pens ?? [])
    .map((pen, index) => ({ pen, index }))
    .sort((a, b) => (b.pen.animals ?? -1) - (a.pen.animals ?? -1) || a.index - b.index);
  const seen = new Set<string>();
  const out: string[] = [];
  for (const { pen } of ordered) {
    const name = dedupeRepeatedWords(pen.pen.trim());
    if (name === "") continue;
    const park = pen.park.trim();
    const label = park === "" ? name : `${park} ${name}`;
    if (seen.has(label)) continue;
    seen.add(label);
    out.push(label);
  }
  return out;
}

/**
 * A served pen display that repeats itself ("Fattening Fattening", "Castro 1 Castro 1") names the
 * pen once: the same doubled-partition defect class as 475312f5d, caught where the bracket is
 * composed so no load chart can print it whichever read supplied the name.
 */
function dedupeRepeatedWords(name: string): string {
  const m = /^(.+?)(?:\s+-)?\s+\1$/i.exec(name);
  return m ? m[1] : name;
}

/**
 * " (CBE Castro 1, CBE Castro 2 +1)" — the bracket to append to a load's chart label, or "" when
 * the load has no known pen.
 *
 * `max` caps how many names are spelled out; the remainder is COUNTED rather than dropped,
 * because a bracket that silently showed two of five pens would read as the whole answer. Pass
 * `Number.POSITIVE_INFINITY` for a tooltip or a table cell, which has the room the axis does not.
 */
export function loadPenBracket(
  pens: readonly LoadPen[] | null | undefined,
  max: number = LOAD_PEN_BRACKET_MAX,
): string {
  const names = loadPenNames(pens);
  if (names.length === 0) return "";
  if (names.length <= max) return ` (${names.join(", ")})`;
  const shown = names.slice(0, Math.max(1, max));
  return ` (${shown.join(", ")} +${names.length - shown.length})`;
}

/** `label` with the pen bracket appended — the form every caller actually wants. */
export function withLoadPens(
  label: string,
  pens: readonly LoadPen[] | null | undefined,
  max: number = LOAD_PEN_BRACKET_MAX,
): string {
  return `${label}${loadPenBracket(pens, max)}`;
}

/**
 * The weighing reads all carry the same placement shape (`by_load[].placements`), so every
 * weighing-backed chart maps it the same way rather than each writing the same three lines.
 */
export function pensFromPlacements(
  placements:
    | readonly { park_name: string; operational_location_display: string; animals: number }[]
    | null
    | undefined,
): LoadPen[] {
  return (placements ?? []).map((placement) => ({
    park: placement.park_name,
    pen: placement.operational_location_display,
    animals: placement.animals,
  }));
}
