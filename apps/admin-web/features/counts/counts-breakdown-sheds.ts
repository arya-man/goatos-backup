import type { BreakdownFilterOption } from "./counts-breakdown-filters";

/**
 * One entry of the backend's park-scoped, live-herd shed filter vocabulary
 * (`CountsBreakdownFacets.sheds` / `CountsBreakdownShedFacet` in the generated api-client).
 * `key` is the shed UUID, `park_id` is the park the animals sit in. Shed NAMES repeat across
 * parks (two thirds of them in real data), which is exactly why the option must be keyed by
 * park_id + shed_id and cascaded by park.
 */
export type CountsBreakdownShedFacetLike = {
  key: string;
  label: string;
  park_id: string;
};

/**
 * Build the Shed filter dropdown options from the backend shed facet.
 *
 * Rules, mirroring the park facet and the Android CountsViewModel:
 *  - drop the blank-key "no shed assigned" bucket (its `<option value="">` would collide with
 *    the "All" sentinel);
 *  - when a park is selected, show only that park's sheds (Park -> Shed cascade); with no park
 *    selected, show the whole live-herd shed vocabulary;
 *  - key each option by `park_id + shed_id` so two same-named sheds in different parks stay
 *    distinct, while `value` stays the bare shed UUID that round-trips to the backend as
 *    `shed_id`.
 */
export function buildShedFilterOptions(
  sheds: readonly CountsBreakdownShedFacetLike[] | undefined,
  selectedParkId: string,
): BreakdownFilterOption[] {
  return (sheds ?? [])
    .filter((shed) => shed.key !== "")
    .filter((shed) => selectedParkId === "" || shed.park_id === selectedParkId)
    .map((shed) => ({
      key: `${shed.park_id}|${shed.key}`,
      value: shed.key,
      label: shed.label,
    }));
}
