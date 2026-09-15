"use server";

// Write flow for the sales board's record-sale drawer. The backend response (or its error
// envelope) drives the banner the operator sees — no optimistic success.
import { randomUUID } from "node:crypto";
import { revalidatePath } from "next/cache";
import { actionRedirect, optionalString, requiredString } from "@/lib/action-helpers";
// NOTE: every actionKey below MUST start with "action." -- withActionFeedback silently rewrites
// anything else to "action.error_form" -- and each key needs matching page-contract copy, because
// actionFeedbackCopy throws on a missing key and takes the whole page down with it.
import {
  createSalesDeal,
  deleteSalesDealPayment,
  recordSalesDealPayment,
  setSalesDealStatus,
  setLoadCost,
  updateSalesDealPayment,
} from "@/lib/api/procurement-server";
import type { SalesDealWrite, SalesDealStatusWrite } from "@/lib/api/procurement";
import { MAX_SALE_LINES } from "./sales-format";

// Every sales write is submitted from /sales/config (maintainer decision 2026-09-01), so that is
// the page whose cache must be invalidated -- a save that revalidated only the read board would
// leave the operator looking at the ledger they just wrote to, unchanged. The read pages are
// force-dynamic and re-read on their own next visit.
const SALES_PATH = "/sales/config";

/**
 * Reads the record-sale fields off the form.
 *
 * ONE sale, MANY lines (maintainer decision 2026-09-12): the drawer posts its product lines as
 * indexed fields -- `line_product_type_0`, `line_breed_0`, `line_animal_count_0`,
 * `line_total_weight_kg_0`, `line_sales_value_0`, then `_1`, `_2`... -- and this reads them back
 * in order until the first index with no product. The deal's product/breed/counts/value are
 * NOT sent: the backend computes them as the rollup of the lines, so a stale client-side total
 * can never disagree with the lines.
 *
 * A CLEARED count/weight/advance is null, never 0: zero animals or zero advance is a real recorded
 * fact, and coercing blank into it would invent that fact. Each line's value is required and
 * parsed as a number for the contract; the backend re-validates it must be more than zero.
 */
function readSaleForm(formData: FormData): SalesDealWrite {
  const parseOptionalNumber = (key: string): number | null => {
    const trimmed = (formData.get(key)?.toString() ?? "").trim();
    if (trimmed === "") return null;
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) ? parsed : null;
  };

  const lines: NonNullable<SalesDealWrite["lines"]> = [];
  // Bounded by the backend's own cap so a hostile form cannot make this loop unbounded.
  for (let i = 0; i < MAX_SALE_LINES; i += 1) {
    const productType = (formData.get(`line_product_type_${i}`)?.toString() ?? "").trim();
    if (productType === "") break;
    lines.push({
      // The backend re-validates these against its closed vocabularies and REJECTS an
      // unrecognised value; the cast only satisfies the generated client's literal union.
      product_type: productType as NonNullable<SalesDealWrite["lines"]>[number]["product_type"],
      breed: requiredString(formData, `line_breed_${i}`),
      animal_count: parseOptionalNumber(`line_animal_count_${i}`),
      total_weight_kg: parseOptionalNumber(`line_total_weight_kg_${i}`),
      sales_value: Number(requiredString(formData, `line_sales_value_${i}`)),
    });
  }

  return {
    sale_date: requiredString(formData, "sale_date"),
    farm: requiredString(formData, "farm") as SalesDealWrite["farm"],
    lines,
    buyer_name: requiredString(formData, "buyer_name"),
    buyer_place: optionalString(formData, "buyer_place") ?? "",
    // REQUIRED: every sale is made to a vendor on the register (maintainer decision 2026-08-27).
    // requiredString throws on a blank, so a form that somehow submits without a selection fails
    // here rather than posting a vendorless deal; the backend re-validates the same rule.
    buyer_vendor_id: requiredString(formData, "buyer_vendor_id"),
    advance_amount: parseOptionalNumber("advance_amount"),
    status: (optionalString(formData, "status") ?? "") as SalesDealWrite["status"],
    comments: optionalString(formData, "comments") ?? "",
  };
}

export async function recordSaleAction(formData: FormData): Promise<void> {
  // A fresh key per submit: retries of THIS action invocation cannot duplicate the deal, while a
  // deliberate second submit records a second deal, which is what the operator asked for.
  const result = await createSalesDeal(readSaleForm(formData), randomUUID());
  if (!result.ok) {
    actionRedirect(formData, "error", "action.sale_record_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.sale_recorded");
}

/**
 * Records (or clears) one load's landed cost from the load-wise cost drawer.
 *
 * Blank stays null, never 0: a load bought with no transport charge and one whose charge has not
 * been entered yet are different facts. The backend rejects negatives and detail-without-animal-
 * cost; this action only reports the outcome.
 */
export async function recordLoadCostAction(formData: FormData): Promise<void> {
  const parseCost = (key: string): number | null => {
    const trimmed = (formData.get(key)?.toString() ?? "").trim();
    if (trimmed === "") return null;
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) ? parsed : null;
  };
  const result = await setLoadCost(requiredString(formData, "load_id"), {
    animal_cost: parseCost("animal_cost"),
    transport_cost: parseCost("transport_cost"),
    other_cost: parseCost("other_cost"),
  });
  if (!result.ok) {
    actionRedirect(formData, "error", "action.load_cost_record_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.load_cost_recorded");
}

/**
 * Records one amount received from the buyer against a deal. The backend advances the running
 * received total in the same transaction; deal status stays a human decision.
 */
export async function recordSalesDealPaymentAction(formData: FormData): Promise<void> {
  const dealId = requiredString(formData, "deal_id");
  const note = (formData.get("note")?.toString() ?? "").trim();
  const result = await recordSalesDealPayment(
    dealId,
    {
      received_on: requiredString(formData, "received_on"),
      amount_rupees: Number(requiredString(formData, "amount_rupees")),
      ...(note ? { note } : {}),
    },
    // A fresh key per submit, like every sales write: retries of THIS invocation cannot count the
    // same money twice, while a deliberate second submit records a second receipt.
    randomUUID(),
  );
  if (!result.ok) {
    actionRedirect(formData, "error", "action.payment_record_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.payment_recorded");
}

export async function updateSalesDealPaymentAction(formData: FormData): Promise<void> {
  const dealId = requiredString(formData, "deal_id");
  const paymentId = requiredString(formData, "payment_id");
  const note = (formData.get("note")?.toString() ?? "").trim();
  const receivedOn = requiredString(formData, "received_on");
  const amountRupees = Number(requiredString(formData, "amount_rupees"));
  const result = await updateSalesDealPayment(
    dealId,
    paymentId,
    {
      received_on: receivedOn,
      amount_rupees: amountRupees,
      note,
    },
    // A FRESH key per submit (guard test in sales-actions.test.mjs, landed on main as d015e8480 and
    // reintroduced as a stable hash by the merge): a deterministic key turns a later legitimate edit
    // back to an earlier value, or a repeated delete, into an idempotent replay the backend skips.
    randomUUID(),
  );
  if (!result.ok) {
    actionRedirect(formData, "error", "action.payment_update_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.payment_updated");
}

export async function deleteSalesDealPaymentAction(formData: FormData): Promise<void> {
  const dealId = requiredString(formData, "deal_id");
  const paymentId = requiredString(formData, "payment_id");
  const result = await deleteSalesDealPayment(
    dealId,
    paymentId,
    randomUUID(),
  );
  if (!result.ok) {
    actionRedirect(formData, "error", "action.payment_delete_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.payment_deleted");
}

/** Sets a deal's lifecycle status — the edit that closes an expected sale on the day it happens. */
export async function setSalesDealStatusAction(formData: FormData): Promise<void> {
  const dealId = requiredString(formData, "deal_id");
  // The backend validates against its closed vocabulary and REJECTS an unrecognised word; the
  // cast only satisfies the generated client's literal union, it is not a trust boundary.
  const status = requiredString(formData, "status") as SalesDealStatusWrite["status"];
  const result = await setSalesDealStatus(dealId, { status });
  if (!result.ok) {
    actionRedirect(formData, "error", "action.deal_status_update_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.deal_status_updated");
}
