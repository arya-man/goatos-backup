"use server";

// Server actions behind "Tag animals to sale".
//
// The drawer is a CLIENT component (multi-select, running count, review step), so it cannot call
// the API directly — these actions are the seam. They return DATA rather than redirecting, because
// the whole flow happens inside one open overlay and a redirect would close it.
//
// NOTHING here decides which animals may be sold. Every verdict is the backend's, rendered
// verbatim; a second opinion computed in the browser is how a blocked animal becomes selectable on
// one surface only.
import { randomUUID } from "node:crypto";
import { revalidatePath } from "next/cache";

import {
  confirmSaleAllocation,
  listSaleCandidates,
  previewSaleAllocation,
  type SaleAllocationConfirmResponse,
  type SaleAllocationPreviewResponse,
  type SaleCandidateListResponse,
} from "@/lib/api/server";

const SALES_PATH = "/procurement/sales";

/** A failed call carries the backend's own message, so the drawer never invents copy for an error. */
export type SaleAllocationActionResult<T> = { ok: true; data: T } | { ok: false; message: string };

function failure(message: string | undefined, fallback: string): { ok: false; message: string } {
  const trimmed = (message ?? "").trim();
  return { ok: false, message: trimmed === "" ? fallback : trimmed };
}

/** One keyset page of the picker. */
export async function fetchSaleCandidatesAction(params: {
  parkId: string;
  shedId?: string;
  partitionLabels?: string[];
  query?: string;
  cursor?: string;
}): Promise<SaleAllocationActionResult<SaleCandidateListResponse>> {
  const result = await listSaleCandidates({
    park_id: params.parkId,
    shed_id: params.shedId || undefined,
    partition_label: params.partitionLabels?.length ? params.partitionLabels : undefined,
    q: params.query || undefined,
    cursor: params.cursor || undefined,
  });
  if (!result.ok) {
    return failure(result.error?.message, "Could not load animals for this shed.");
  }
  return { ok: true, data: result.data };
}

/** The review step: what was picked, shed by shed, plus every refused animal and its reason. */
export async function previewSaleAllocationAction(params: {
  salesDealId: string;
  goatIds: string[];
}): Promise<SaleAllocationActionResult<SaleAllocationPreviewResponse>> {
  const result = await previewSaleAllocation({
    sales_deal_id: params.salesDealId,
    goat_ids: params.goatIds,
  });
  if (!result.ok) {
    return failure(result.error?.message, "Could not check the selected animals.");
  }
  return { ok: true, data: result.data };
}

/**
 * The confirm: tag the animals to the sale and mark them sold.
 *
 * The idempotency key is minted HERE, per submission, rather than in the browser. A key generated
 * in the component would be reused by a double-click on the same render, which is the one case it
 * exists to distinguish from a genuine second confirmation.
 */
export async function confirmSaleAllocationAction(params: {
  salesDealId: string;
  goatIds: string[];
  reason?: string;
}): Promise<SaleAllocationActionResult<SaleAllocationConfirmResponse>> {
  const result = await confirmSaleAllocation(
    {
      sales_deal_id: params.salesDealId,
      goat_ids: params.goatIds,
      reason: params.reason?.trim() || undefined,
    },
    randomUUID(),
  );
  if (!result.ok) {
    // The backend's `animals_blocked` message names each refused animal and why, so it is shown
    // as-is. Replacing it with a generic sentence would send the person hunting.
    return failure(result.error?.message, "Could not tag these animals to the sale.");
  }
  // The ledger now shows different animals against this deal, and the herd is smaller.
  revalidatePath(SALES_PATH);
  return { ok: true, data: result.data };
}
