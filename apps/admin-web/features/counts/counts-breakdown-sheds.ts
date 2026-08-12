import type { BreakdownFilterOption } from "./counts-breakdown-filters";
import { operationalLocationLabel } // Relative WITH an explicit .ts extension, not the "@/lib" alias. This module is imported
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
 * park_id + shed_id and cascaded by park. partition_label may still arrive from legacy rows, but
 * the current option identity is the exact shed id only.
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
 *  - for each exact shed id, offer ONE option;
 *  - key each option by `park_id + shed_id` so same-named sheds in different parks stay distinct,
 *    while `value` is the exact shed UUID for the backend round-trip;
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

  const shedIdOf = (row: CountsBreakdownShedFacetLike): string =>
    (row.shed_id ?? "").trim() || ((row.key ?? "").split("#")[0] ?? "");
  const byExactShed = new Map<string, CountsBreakdownShedFacetLike>();
  for (const shed of filtered) {
    const shedId = shedIdOf(shed);
    if (!shedId) continue;
    const key = `${shed.park_id}|${shedId}`;
    const label = (shed.operational_location_display || shed.label || "").trim();
    const existing = byExactShed.get(key);
    if (!existing || (!existing.operational_location_display && label)) {
      byExactShed.set(key, shed);
    }
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

  const collator = new Intl.Collator("en", { numeric: true, sensitivity: "base" });

  // Sheds in name order, with a park tiebreak so two same-named sheds land next to each other in a
  // stable order instead of wherever their UUIDs happened to fall.
  const groupsInOrder = [...byExactShed.entries()].sort(([leftKey, left], [rightKey, right]) => {
    const byName = collator.compare(left.operational_location_display || left.label || "", right.operational_location_display || right.label || "");
    return byName !== 0 ? byName : collator.compare(leftKey, rightKey);
  });

  for (const [groupKey, row] of groupsInOrder) {
    const [parkId, shedId] = groupKey.split("|");
    const label = row.operational_location_display || operationalLocationLabel({
      shedName: row.label,
      partitionLabel: row.partition_label,
    });
    options.push({
      key: groupKey,
      value: shedId,
      label: withPark(label, row.label, parkId),
    });
  }

  return options;
}
