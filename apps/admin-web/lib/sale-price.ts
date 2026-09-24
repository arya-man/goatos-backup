import type { GrowthSalePrice } from "@/lib/api/server";

/**
 * One animal's assumed live-weight sale price (maintainer decision 2026-09-24): its own
 * (species, stage, sex) override when one is in force, else its species default (stage and sex
 * both ""). Mirrors backend `growthdirector/domain.SalePrices.PriceForAnimal` exactly -- the FCR
 * tab values gain through that one and the Load-wise tab values stock through this one, and the
 * two must price the same animal at the same figure. Null when the species has no price at all.
 */
export function salePriceForAnimal(
  prices: readonly GrowthSalePrice[],
  species: string,
  stage: string,
  sex: string,
): number | null {
  const sp = species.trim().toLowerCase();
  const st = stage.trim().toLowerCase();
  const sx = sex.trim().toLowerCase();
  if (st !== "" && sx !== "") {
    const override = prices.find(
      (p) => p.management_stage !== "" && p.species.toLowerCase() === sp && p.management_stage.toLowerCase() === st && p.sex.toLowerCase() === sx,
    );
    if (override) return override.price_per_kg_inr;
  }
  const fallback = prices.find((p) => p.management_stage === "" && p.sex === "" && p.species.toLowerCase() === sp);
  return fallback ? fallback.price_per_kg_inr : null;
}

/** A group of animals sharing one (species, stage, sex), as a load or a pen reports it. */
export type HeadMix = { species: string; management_stage: string; sex: string; animals: number };

/**
 * The rupee value of a head mix at one average live weight: Σ animals × weight × own price. Null
 * when any animal in the mix has no price, because valuing only the priced part would understate
 * the stock without saying so.
 */
export function valueHeadMix(prices: readonly GrowthSalePrice[], mix: readonly HeadMix[], avgKg: number): number | null {
  let total = 0;
  for (const group of mix) {
    if (group.animals <= 0) continue;
    const price = salePriceForAnimal(prices, group.species, group.management_stage, group.sex);
    if (price === null) return null;
    total += group.animals * avgKg * price;
  }
  return total;
}
