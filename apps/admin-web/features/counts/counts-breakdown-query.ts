// The URL a Counts Breakdown filter Apply navigates to, kept out of the client component so the
// park rules below are tested directly.
//
// Farm writes the SHARED `park` parameter, the one lib/scope.ts parses and every other page reads.
// The top-bar park chip is hidden on this page (mesha-shell PAGES_WITH_LOCAL_OR_NO_PARK_SCOPE), so
// this control IS the park control, on the same parameter — the maintainer's 2026-08-18 rule for a
// page that owns its park. A page-private key (the retired `bd_farm`) meant a park picked on Herd
// Analytics rendered here as a greyed-out "All" over one park's numbers, and a farm picked here
// was forgotten by the next Counts page.

/** The shared park parameter the top bar and every other page read (lib/scope.ts). */
export const PARK_PARAM = "park";
/** The retired page-private park key. Read for old links; always dropped on apply. */
export const LEGACY_FARM_PARAM = "bd_farm";
const SHED_PARAM = "bd_shed";
const PAGE_PARAM = "bd_page";

/**
 * `current` is the page's query string, `params` the filter params on the bar, `values` what each
 * one is set to. Returns the next query string (without "?").
 */
export function breakdownFilterQuery(
  current: string,
  params: readonly string[],
  values: Record<string, readonly string[]>,
  /**
   * Pen value -> the park id that pen sits in, for EVERY park (the pen facet is not narrowed by
   * park on the server). Used only when the park changes, to keep the pens of the new park.
   */
  penParks: Readonly<Record<string, string>> = {},
): string {
  const next = new URLSearchParams(current);
  const previousPark = next.get(PARK_PARAM) || next.get(LEGACY_FARM_PARAM) || "";
  // A filter change must reset paging, or the reader lands on an offset that no longer exists in
  // the newly-filtered result set and sees an empty page.
  next.delete(PAGE_PARAM);
  next.delete(LEGACY_FARM_PARAM);
  for (const param of params) {
    next.delete(param);
    for (const value of values[param] ?? []) {
      if (value) next.append(param, value);
    }
  }
  if (params.includes(PARK_PARAM)) {
    const park = next.get(PARK_PARAM) || "";
    // The mode moves with the park exactly as the top-bar picker writes it: a park means
    // scope_mode=park, no park means company-wide.
    next.set("scope_mode", park ? "park" : "company");
    // A pen belongs to one park. On a park change, a pen of ANOTHER park would AND with the new
    // park and return an empty table with no visible reason, so it is dropped -- but a pen of the
    // NEW park is kept, because the reader may have picked the farm and one of its pens in the
    // same Apply. All farms keeps every pen. A pen whose park is unknown is dropped.
    if (park !== previousPark && park) {
      const pens = next.getAll(SHED_PARAM);
      next.delete(SHED_PARAM);
      for (const pen of pens) {
        if (penParks[pen] === park) next.append(SHED_PARAM, pen);
      }
    }
  }
  return next.toString();
}

/**
 * A dropdown's options, plus every SELECTED value the options no longer carry, so a selection can
 * always be unticked. The Stage, Breed and Pen options follow the selected park, so a value picked
 * under one farm can be missing under the next; left out of the list it would stay applied with no
 * checkbox to clear it, and the table would read empty for no visible reason. A kept value reads
 * with the label `labelFor` gives it, else the value itself.
 */
export function withSelectedOptions<T extends { value: string; label: string }>(
  options: readonly T[],
  selected: readonly string[],
  labelFor: (value: string) => string | undefined = () => undefined,
): Array<T | { value: string; label: string }> {
  const present = new Set(options.map((option) => option.value));
  const kept = selected
    .filter((value) => value && !present.has(value))
    .map((value) => ({ value, label: labelFor(value) || value }));
  return [...options, ...kept];
}
