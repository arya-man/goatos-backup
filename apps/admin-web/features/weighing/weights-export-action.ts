"use server";

import { exportWeighingWeightsCsv as getWeighingWeightsCsvExport } from "@/lib/api/server";

export type WeightsCsvExport = { ok: true; csv: string } | { ok: false };

/**
 * Fetches the Weights CSV for the download drawer's selection.
 *
 * Fetched on CLICK rather than rendered with the page: the file can span a year of
 * weighings, and paying that on every page load for a file most viewers never ask
 * for is the over-fetch the drawer exists to avoid. The backend composes the file —
 * columns, identity resolution, approval wording — so this action only carries the
 * caller's selection and returns the finished text.
 *
 * A server action rather than a route handler: this is a READ that needs the
 * caller's session, and the action gets it from the same authenticated server
 * client every other read on this page uses. The backend gates it on
 * permissions.WeighingMonitor exactly as it gates the page, so this opens no path a
 * caller did not already have.
 *
 * server-action-read-only: GET-backed export; no mutation replay key required.
 */
export async function exportWeightsCsvAction(input: {
  from: string;
  to: string;
  parkId?: string;
  shedIds?: string[];
}): Promise<WeightsCsvExport> {
  const result = await getWeighingWeightsCsvExport({
    from: input.from,
    to: input.to,
    park_id: input.parkId,
    shed_id: input.shedIds,
  });
  if (!result.ok || typeof result.data !== "string") return { ok: false };
  return { ok: true, csv: result.data };
}
