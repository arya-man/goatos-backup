"use server";

import { putGrowthAssumptions, type GrowthAssumptionsResponse, type GrowthAssumptionsUpdate } from "@/lib/api/server";

export type SaveAssumptionsResult =
  | { ok: true; data: GrowthAssumptionsResponse }
  | { ok: false; reason: "conflict" | "invalid" | "error"; message: string };

/**
 * Saves the Assumptions drawer (maintainer decision 2026-09-19). The backend owns the business
 * bands and the effective date; this action carries the drawer's figures and reports the outcome
 * in three shapes the drawer renders differently: a stale row_version (reload and decide again),
 * a figure the backend refused (its message names the band), or a plain failure.
 *
 * The action returns the saved rows; the client applies them locally and then performs the full
 * navigation that makes every server-rendered reader load the new figures.
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
  return { ok: true, data: result.data };
}
