"use server";

// The Over 35 kg card's error-margin Apply (flicker fix, 2026-09-25). Apply used to re-render the
// WHOLE Farm value page -- the valuation read, the category breakdown and the chrome -- to change
// one number in one card. It now asks for that number alone and the card updates in place; the
// rest of the page never moves. server-action-read-only: GET-backed count fetch; it writes nothing
// and revalidates nothing.
import { getGrowthAssumptions, getShedWeights } from "@/lib/api/server";
import { assumptionValue, DEFAULT_SALE_READY_THRESHOLD_KG } from "@/features/weighing/assumption-copy";
import { istDayPlus, todayIso } from "@/lib/format";
import { OVER35_MAX_TOLERANCE_G, OVER35_WINDOW_DAYS } from "./over35-window";

export type Over35CountResult = { ok: true; count: number | null } | { ok: false; code: string };

export async function countOver35Action(parkId: string, toleranceG: number): Promise<Over35CountResult> {
  // The client's numbers are bounded here, never trusted: a hand-built call cannot ask for a
  // margin the card does not offer.
  const g = Math.min(Math.max(Math.round(Number.isFinite(toleranceG) ? toleranceG : 0), 0), OVER35_MAX_TOLERANCE_G);
  const assumptions = await getGrowthAssumptions();
  if (!assumptions.ok) return { ok: false, code: assumptions.error.code ?? assumptions.error.kind };
  const lineKg = assumptionValue(assumptions.data.values, "sale_ready_threshold_kg") ?? DEFAULT_SALE_READY_THRESHOLD_KG;
  const lowerKg = assumptionValue(assumptions.data.values, "sale_ready_lower_kg") ?? undefined;
  const to = todayIso();
  const result = await getShedWeights({
    from: istDayPlus(to, -OVER35_WINDOW_DAYS),
    to,
    sale_threshold_tolerance_g: String(g),
    sale_threshold_kg: lineKg,
    sale_lower_kg: lowerKg,
    ...(parkId ? { park_id: parkId } : {}),
  });
  if (!result.ok) return { ok: false, code: result.error.code ?? result.error.kind };
  return { ok: true, count: result.data.summary.at_or_above_35kg };
}
