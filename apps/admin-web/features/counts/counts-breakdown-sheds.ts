import type { BreakdownFilterOption } from "./counts-breakdown-filters";
import { hasOperationalPartition, operationalLocationLabel } // Relative WITH an explicit .ts extension, not the "@/lib" alias. This module is imported
// directly by counts-breakdown-sheds.test.mjs under `node --test`, which resolves neither
// the tsconfig path alias nor an extensionless relative path -- so before
// allowImportingTsExtensions was enabled in tsconfig.json there was NO import form that
// satisfied both the typechecker and the test runner, and this file hand-rolled its own
// shed+partition composition instead. That copy reproduced the "Godel 1 1" defect the
// shared helper exists to prevent. Do not "tidy" this back to the alias without checking
// the test still loads.
from "../../lib/operational-location.ts";

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
  /** Parent physical shed uuid, sent explicitly so clients never parse the composite key. */
  shed_id?: string | null;
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
  /**
   * park_id -> park display label, taken from the SAME response's `facets.parks` (keyed by park
   * id, labelled with the park code). It exists because `park_label` on the shed facet is a field
   * NOTHING EVER FILLS: the frontend type declares it, this helper reads it, and neither the Go
   * struct nor the OpenAPI schema has it — so the disambiguation below could never fire and the
   * dropdown showed "Castro" twice, "Mandela 1 - Part 3" twice, and so on, with no way to tell
   * the two parks apart. Rather than widen the contract for a fact the response already carries,
   * the park vocabulary is passed in. `park_label` is still preferred when present, so a backend
   * that starts sending it wins.
   */
  parkLabels: ReadonlyMap<string, string> = new Map(),
): BreakdownFilterOption[] {
  const filtered = (sheds ?? [])
    .filter((shed) => shed.key !== "")
    .filter((shed) => selectedParkId === "" || shed.park_id === selectedParkId);

  // Group by shed_id to identify parent sheds and their partitions
  // The backend facet `key` is COMPOSITE for a partition row: "<shed_id>#<normalized_label>"
  // (the oploc.Key() convention), and bare "<shed_id>" for the parent-shed row. Treating that key
  // as a shed id put "<uuid>#2" into the option value, so the parent-aggregate option carried a
  // PARTITION key and the selected partition could never round-trip. Split it back apart here and
  // key every group on the real shed id.
  // shed_id now arrives EXPLICITLY on the facet; the composite-key split is only a fallback for a
  // backend that predates that field.
  const shedIdOf = (row: CountsBreakdownShedFacetLike): string =>
    (row.shed_id ?? "").trim() || ((row.key ?? "").split("#")[0] ?? "");
  const shedMap = new Map<string, CountsBreakdownShedFacetLike[]>();
  for (const shed of filtered) {
    const groupKey = `${shed.park_id}|${shedIdOf(shed)}`;
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
  // NEVER fall back to park_id here: it is a UUID, and rendering it would put a raw internal id in
  // front of a CEO (the copy-firewall rule in AGENTS.md). With no human park label available we
  // simply omit the suffix -- an ambiguous-but-clean label beats a leaked identifier.
  const parkLabelFor = new Map<string, string>(parkLabels);
  for (const shed of filtered) {
    const label = (shed.park_label ?? "").trim();
    if (shed.park_id && label) {
      parkLabelFor.set(shed.park_id, label);
    }
  }
  // Detect if a shed label appears in multiple parks (for display disambiguation).
  // Key by park + shed_id to avoid silent merging, then check dynamically.
  const needsParkSuffix = (shedLabel: string): boolean => {
    const parksWithLabel = new Set<string>();
    for (const shed of filtered) {
      if ((shed.label ?? "").trim() === shedLabel) {
        parksWithLabel.add(shed.park_id);
      }
    }
    return parksWithLabel.size > 1;
  };
  const withPark = (label: string, shedName: string, parkId: string): string => {
    if (!needsParkSuffix(shedName)) return label;
    const park = parkLabelFor.get(parkId);
    return park ? `${label} · ${park}` : label;
  };

  const options: BreakdownFilterOption[] = [];

  // `numeric` is the point: the facet arrives ordered by shed UUID — an artifact, not a decision —
  // and a plain string sort inside a shed gives "Part 1, Part 10, Part 2". Both read as noise in a
  // control the operator is scanning for one pen.
  const collator = new Intl.Collator("en", { numeric: true, sensitivity: "base" });

  // Sheds in name order, with a park tiebreak so two same-named sheds land next to each other in a
  // stable order instead of wherever their UUIDs happened to fall.
  const groupsInOrder = [...shedMap.entries()].sort(([leftKey, left], [rightKey, right]) => {
    const byName = collator.compare(left[0]?.label ?? "", right[0]?.label ?? "");
    return byName !== 0 ? byName : collator.compare(leftKey, rightKey);
  });

  // For each shed, add parent aggregate and partition-specific options
  for (const [groupKey, shedRows] of groupsInOrder) {
    const [parkId, shedId] = groupKey.split("|");

    // Find the parent shed row (non-partitioned or the first row for rollup)
    const parentRow = shedRows.find((r) => !r.partition_label || r.partition_label.toLowerCase() === "whole") ||
                      shedRows[0];

    // Add partition-specific options (for partitioned sheds)
    const partitionedRows = shedRows
      .filter((r) => r.partition_label && r.partition_label.toLowerCase() !== "whole")
      .sort((left, right) => collator.compare(left.partition_label ?? "", right.partition_label ?? ""));

    // A subdivided shed becomes an OPTGROUP holding its whole-shed option and one option per pen,
    // which is the shape the operational-location convention asks for ("group by shed_id first,
    // list partitions under it" — OL-2). Real data makes this the difference between a usable
    // control and an unusable one: 148 flat rows, twelve of them starting "Yashoda -", is a wall.
    // An UNDIVIDED shed gets no group — a one-option group is chrome around a single row.
    // Options keep their full "Yashoda - 3" label rather than a bare "3": a native select scrolls
    // its group header out of sight, and the convention requires both halves of a location to
    // render together.
    const group = partitionedRows.length ? withPark(parentRow.label, parentRow.label, parkId) : undefined;

    // Add parent aggregate option
    options.push({
      key: groupKey,
      value: shedId,
      label: withPark(parentRow.label, parentRow.label, parkId),
      group,
    });

    for (const row of partitionedRows) {
      options.push({
        key: `${parkId}|${shedId}|${row.partition_label}`,
        value: `${shedId}|${row.partition_label}`,
        label: withPark(
          row.operational_location_display || operationalLocationLabel({
            shedName: row.label,
            partitionLabel: row.partition_label,
          }),
          row.label,
          parkId,
        ),
        group,
      });
    }
  }

  return options;
}
