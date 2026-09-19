import type { GrowthAssumptionValue } from "@/lib/api/server";

/**
 * The sale-ready line and the load-age alert are tenant assumptions (maintainer decision
 * 2026-09-19). These two helpers are the ONLY place a page turns them into copy: the value comes
 * from the assumptions read, the sentence from the backend contract's `{kg}` placeholder, and a
 * page with no read (the assumptions call failed) shows the default the constant carried rather
 * than a blank label -- the number beside it was counted against that default too.
 */
export const DEFAULT_SALE_READY_THRESHOLD_KG = 35;
export const DEFAULT_SALE_READY_LOWER_KG = 30;

export function assumptionValue(values: readonly GrowthAssumptionValue[], key: GrowthAssumptionValue["key"]): number | null {
  const row = values.find((value) => value.key === key);
  return row ? row.value : null;
}

/** Fills the backend's `{kg}` placeholder with the sale line the count was taken against. */
export function fillKg(template: string, thresholdKg: number | null | undefined): string {
  const kg = thresholdKg ?? DEFAULT_SALE_READY_THRESHOLD_KG;
  const shown = Number.isInteger(kg) ? String(kg) : kg.toLocaleString("en-IN", { maximumFractionDigits: 1 });
  return template.replaceAll("{kg}", shown);
}

/** The band edges as the demographics read takes them ("15,20,25,30,35"); undefined when unset. */
export function bandEdgesParam(values: readonly GrowthAssumptionValue[] | null | undefined): string | undefined {
  const row = values?.find((value) => value.key === "weight_band_edges_kg");
  if (!row || row.kind !== "number_list" || !row.values || row.values.length < 2) return undefined;
  return row.values.join(",");
}
