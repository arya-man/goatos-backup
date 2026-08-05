import type { BreakdownFilterOption } from "./counts-breakdown-filters";

/**
 * One entry of the backend's park-scoped, live-herd shed filter vocabulary
 * (`CountsBreakdownFacets.sheds` / `CountsBreakdownShedFacet` in the generated api-client).
 * `key` is the shed UUID, `park_id` is the park the animals sit in. Shed NAMES repeat across
 * parks (two thirds of them in real data), which is exactly why the option must be keyed by
 * park_id + shed_id and cascaded by park. For partitioned sheds, one row per partition is
 * provided, with partition_label and operational_location_display populated.
 */
export type CountsBreakdownShedFacetLike = {
  key: string;
  label: string;
  count: number;
  park_id: string;
  /** Park display label, used to disambiguate shed names that repeat across parks. */
  park_label?: string | null;
  partition_label?: string | null;
  operational_location_display?: string | null;
};

/**
 * Build the Shed filter dropdown options from the backend shed facet.
 *
 * Rules, mirroring the park facet and the Android CountsViewModel:
 *  - drop the blank-key "no shed assigned" bucket (its `<option value="">` would collide with
 *    the "All" sentinel);
 *  - when a park is selected, show only that park's sheds (Park -> Shed cascade); with no park
 *    selected, show the whole live-herd shed vocabulary;
 *  - for each shed, offer ONE parent aggregate option (all partitions combined);
 *  - for each partition within a shed, offer ONE partition-specific option;
 *  - key each option by `park_id + shed_id + partition_label` so same-named sheds in different
 *    parks and partition-specific rows stay distinct, while `value` is `shed_id|partition_label`
 *    (or just `shed_id` for non-partitioned) to round-trip to the backend;
 *  - partition option counts must SUM to the parent option count (verified in tests);
 *  - never emit an inactive partition-alias row as a 0-animal shed.
 */
export function buildShedFilterOptions(
  sheds: readonly CountsBreakdownShedFacetLike[] | undefined,
  selectedParkId: string,
): BreakdownFilterOption[] {
  const filtered = (sheds ?? [])
    .filter((shed) => shed.key !== "")
    .filter((shed) => selectedParkId === "" || shed.park_id === selectedParkId);

  // Group by shed_id to identify parent sheds and their partitions
  const shedMap = new Map<string, CountsBreakdownShedFacetLike[]>();
  for (const shed of filtered) {
    const groupKey = `${shed.park_id}|${shed.key}`;
    if (!shedMap.has(groupKey)) {
      shedMap.set(groupKey, []);
    }
    shedMap.get(groupKey)!.push(shed);
  }

  // Shed NAMES repeat across parks -- there are two "Castro", two "Gandhi", two "Yashoda". Keying
  // by park_id + shed_id already keeps them distinct in the DATA, but the rendered LABELS were
  // identical, so the dropdown showed "Yashoda", "Yashoda 1", ... twice with nothing to tell a CEO
  // which park each belonged to. Only names that actually collide get a park suffix, so the common
  // case stays clean.
  const parksPerShedName = new Map<string, Set<string>>();
  for (const shed of filtered) {
    const name = (shed.label ?? "").trim();
    if (!name) continue;
    if (!parksPerShedName.has(name)) parksPerShedName.set(name, new Set());
    parksPerShedName.get(name)!.add(shed.park_id);
  }
  const parkLabelFor = new Map<string, string>();
  for (const shed of filtered) {
    if (shed.park_id && !parkLabelFor.has(shed.park_id)) {
      parkLabelFor.set(shed.park_id, shed.park_label ?? shed.park_id);
    }
  }
  const needsParkSuffix = (shedName: string): boolean =>
    (parksPerShedName.get((shedName ?? "").trim())?.size ?? 0) > 1;
  const withPark = (label: string, shedName: string, parkId: string): string => {
    if (!needsParkSuffix(shedName)) return label;
    const park = parkLabelFor.get(parkId);
    return park ? `${label} · ${park}` : label;
  };

  const options: BreakdownFilterOption[] = [];

  // For each shed, add parent aggregate and partition-specific options
  for (const [groupKey, shedRows] of shedMap) {
    const [parkId, shedId] = groupKey.split("|");

    // Find the parent shed row (non-partitioned or the first row for rollup)
    const parentRow = shedRows.find((r) => !r.partition_label || r.partition_label.toLowerCase() === "whole") ||
                      shedRows[0];

    // Add parent aggregate option
    options.push({
      key: groupKey,
      value: shedId,
      label: withPark(parentRow.label, parentRow.label, parkId),
    });

    // Add partition-specific options (for partitioned sheds)
    const partitionedRows = shedRows.filter((r) => r.partition_label && r.partition_label.toLowerCase() !== "whole");
    for (const row of partitionedRows) {
      options.push({
        key: `${parkId}|${shedId}|${row.partition_label}`,
        value: `${shedId}|${row.partition_label}`,
        label: withPark(
          row.operational_location_display || `${row.label} ${row.partition_label}`,
          row.label,
          parkId,
        ),
      });
    }
  }

  return options;
}
