"use server";

import { revalidatePath } from "next/cache";

import { putGrowthAssumptions, type GrowthAssumptionsResponse, type GrowthAssumptionsUpdate } from "@/lib/api/server";

export type SaveAssumptionsResult =
  | { ok: true; data: GrowthAssumptionsResponse }
  | { ok: false; reason: "conflict" | "invalid" | "error"; message: string };

const READER_PATHS = ["/weighing/sops", "/weighing/analytics", "/weighing/weights", "/sales/farm-value"];

/**
 * Saves the Assumptions drawer (maintainer decision 2026-09-19). The backend owns the business
 * bands and the effective date; this action carries the drawer's figures and reports the outcome
 * in three shapes the drawer renders differently: a stale row_version (reload and decide again),
 * a figure the backend refused (its message names the band), or a plain failure.
 *
 * Every reader page is revalidated on success so the FCR tab, the Load-wise tab, the Weights
 * cards and Farm value re-read the new figures on their next load -- "real time" here is the next
 * render, because every consumer re-reads the rows on each request rather than caching them.
 *
 * Idempotent on the backend: replaying the same body lands the same rows (an unchanged price is
 * not re-appended; an unchanged figure is not re-versioned), so no client replay key is minted.
 */
export async function saveWeighingAssumptionsAction(body: GrowthAssumptionsUpdate): Promise<SaveAssumptionsResult> {
  const result = await putGrowthAssumptions(body);
  if (!result.ok) {
    if (result.error.status === 409) return { ok: false, reason: "conflict", message: result.error.message };
    if (result.error.status === 400) return { ok: false, reason: "invalid", message: result.error.message };
    return { ok: false, reason: "error", message: result.error.message };
  }
  for (const path of READER_PATHS) revalidatePath(path);
  return { ok: true, data: result.data };
}
