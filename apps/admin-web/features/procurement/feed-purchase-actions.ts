"use server";

// Write flow for the feed purchase ledger's record drawer. The backend response (or its error
// envelope) drives the banner the operator sees — no optimistic success.
import { randomUUID } from "node:crypto";
import { revalidatePath } from "next/cache";
import { actionRedirect, optionalString, requiredString } from "@/lib/action-helpers";
// NOTE: every actionKey below MUST start with "action." -- withActionFeedback silently rewrites
// anything else to "action.error_form" -- and each key needs matching page-contract copy, because
// actionFeedbackCopy throws on a missing key and takes the whole page down with it.
import { createFeedPurchase, recordFeedPurchasePayment, setFeedPurchasePaymentStatus } from "@/lib/api/procurement-server";
import type { FeedPurchaseStatusWrite, FeedPurchaseWrite } from "@/lib/api/procurement";

const FEED_PURCHASES_PATH = "/procurement/feed-purchases";

/**
 * Reads the record-purchase fields off the form.
 *
 * A CLEARED cost is null, never 0: a load bought with no transport charge and a load whose
 * transport charge has not been entered yet are different facts, and coercing blank into 0 would
 * invent the first. The same rule is why `batch_no` stays null when blank — that is what tells the
 * backend to assign the next load number itself.
 */
function readPurchaseForm(formData: FormData): FeedPurchaseWrite {
  const parseOptionalNumber = (key: string): number | null => {
    const trimmed = (formData.get(key)?.toString() ?? "").trim();
    if (trimmed === "") return null;
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) ? parsed : null;
  };

  return {
    purchase_date: requiredString(formData, "purchase_date"),
    // The backend re-validates these against its closed vocabularies and REJECTS an unrecognised
    // value; the casts only satisfy the generated client's literal unions, they are not a trust
    // boundary.
    farm: requiredString(formData, "farm") as FeedPurchaseWrite["farm"],
    feed_item: requiredString(formData, "feed_item"),
    batch_no: parseOptionalNumber("batch_no"),
    quantity_kg: Number(requiredString(formData, "quantity_kg")),
    feed_cost: parseOptionalNumber("feed_cost"),
    transport_cost: parseOptionalNumber("transport_cost"),
    loading_cost: parseOptionalNumber("loading_cost"),
    unloading_cost: parseOptionalNumber("unloading_cost"),
    total_cost: parseOptionalNumber("total_cost"),
    vendor: requiredString(formData, "vendor"),
    payment_released: parseOptionalNumber("payment_released"),
    payment_status: requiredString(formData, "payment_status") as FeedPurchaseWrite["payment_status"],
  };
}

export async function recordFeedPurchaseAction(formData: FormData): Promise<void> {
  // The real form mints this once when rendered and carries it as a hidden field. Reading it here
  // makes a double-submit or lost-response retry replay the SAME logical purchase instead of
  // creating another load with the next auto-assigned batch number. The fallback covers only
  // programmatic callers that did not post the field.
  const result = await createFeedPurchase(readPurchaseForm(formData), optionalString(formData, "idempotency_key") ?? randomUUID());
  if (!result.ok) {
    actionRedirect(formData, "error", "action.purchase_record_failed");
  }
  // The stock and days-left cards on Feed Analytics are counted from these rows, so a recorded
  // load must not leave a cached page behind showing the farm short of feed it now has.
  revalidatePath(FEED_PURCHASES_PATH);
  revalidatePath("/feed/analytics");
  actionRedirect(formData, "success", "action.purchase_recorded");
}

/**
 * Records one instalment paid against a load. The backend advances the running paid total and
 * re-derives the payment status in the same transaction; this action only reports the outcome.
 */
export async function recordFeedPurchasePaymentAction(formData: FormData): Promise<void> {
  const purchaseId = requiredString(formData, "feed_purchase_id");
  const note = (formData.get("note")?.toString() ?? "").trim();
  const result = await recordFeedPurchasePayment(
    purchaseId,
    {
      paid_on: requiredString(formData, "paid_on"),
      amount_rupees: Number(requiredString(formData, "amount_rupees")),
      ...(note ? { note } : {}),
    },
    // Same contract as the record form: the drawer mints this once when rendered and posts it as a
    // hidden field, so a double-submit or lost-response retry replays the SAME instalment instead
    // of handing the vendor the money twice. The fallback covers only programmatic callers.
    optionalString(formData, "idempotency_key") ?? randomUUID(),
  );
  if (!result.ok) {
    actionRedirect(formData, "error", "action.payment_record_failed");
  }
  revalidatePath(FEED_PURCHASES_PATH);
  actionRedirect(formData, "success", "action.payment_recorded");
}

/** Sets a load's payment status directly — the edit control for a state recorded wrong. */
export async function setFeedPurchasePaymentStatusAction(formData: FormData): Promise<void> {
  const purchaseId = requiredString(formData, "feed_purchase_id");
  // The backend validates against its closed vocabulary and REJECTS an unrecognised word; the
  // cast only satisfies the generated client's literal union, it is not a trust boundary.
  const status = requiredString(formData, "payment_status") as FeedPurchaseStatusWrite["payment_status"];
  const result = await setFeedPurchasePaymentStatus(purchaseId, { payment_status: status });
  if (!result.ok) {
    actionRedirect(formData, "error", "action.payment_status_update_failed");
  }
  revalidatePath(FEED_PURCHASES_PATH);
  actionRedirect(formData, "success", "action.payment_status_updated");
}
