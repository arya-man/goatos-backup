"use server";

// Write flow for the sales board's record-sale drawer. The backend response (or its error
// envelope) drives the banner the operator sees — no optimistic success.
import { revalidatePath } from "next/cache";
import {
  actionRedirect,
  actionRedirectWithDetail,
  optionalString,
  requiredString,
} from "@/lib/action-helpers";
// NOTE: every actionKey below MUST start with "action." -- withActionFeedback silently rewrites
// anything else to "action.error_form" -- and each key needs matching page-contract copy, because
// actionFeedbackCopy throws on a missing key and takes the whole page down with it.
import {
  createSalesDeal,
  deleteSalesDealPayment,
  recordSalesDealPayment,
  setSalesDealStatus,
  deleteSellableProduct,
  saveSellableProduct,
  setLoadCost,
  updateSalesDealPayment,
} from "@/lib/api/procurement-server";
import type {
  SalesDealWrite,
  SalesDealStatusWrite,
  SalesProductOption,
  SellableProductWrite,
} from "@/lib/api/procurement";
import { MAX_SALE_LINES } from "./sales-format";
import { paymentIdempotencyKey } from "./payment-idempotency";

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
      // A line priced by the unit (feed, migration 000422) posts kilograms and a rate and NO
      // value: the backend computes it, so a stale figure on the form can never be recorded as
      // the money. Its value field is a readout, not an input, and is deliberately absent here.
      quantity: parseOptionalNumber(`line_quantity_${i}`),
      rate_per_unit: parseOptionalNumber(`line_rate_per_unit_${i}`),
      sales_value: Number(formData.get(`line_sales_value_${i}`)?.toString() ?? "0"),
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
    // Only ever true because a person ticked it after being shown what the store holds.
    stock_shortfall_acknowledged: formData.get("stock_shortfall_acknowledged") !== null,
    status: (optionalString(formData, "status") ?? "") as SalesDealWrite["status"],
    comments: optionalString(formData, "comments") ?? "",
  };
}

export async function recordSaleAction(formData: FormData): Promise<{ code: string; message: string } | undefined> {
  // The FORM's key (payment-idempotency.ts): minted when the record-sale form opens, so a double
  // click posts one key twice and the backend replays the first sale instead of recording two.
  const result = await createSalesDeal(readSaleForm(formData), paymentIdempotencyKey(formData));
  if (!result.ok) {
    // The short-feed-sale CONFIRMATION (maintainer decision 2026-09-23) is not a failure: the sale
    // may be right and the purchase ledger behind. Keyed on the backend's CODE, never on its
    // sentence -- the sentence is farm copy and may be reworded -- and that sentence is carried
    // through so the desk sees what the store actually holds, which is the question it is being
    // asked to answer.
    if (result.error.code === "feed_stock_confirmation_required") {
      return { code: "feed_stock_confirmation_required", message: result.error.message };
    }
    // The backend names the exact field and what is wrong with it ("Quantity must be more than
    // zero"); the banner used to drop that and say "check the fields", leaving the desk to hunt
    // through a form of ten. The sentence is backend-composed farm copy, carried through the same
    // way the stock confirmation's is.
    return { code: result.error.code ?? "sale_record_failed", message: result.error.message };
  }
  // interaction-guard:ignore: success revalidates then redirects; only the error/confirmation path returns, and it never revalidates.
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.sale_recorded");
}

/**
 * The outcome of one item edit, kept WITH its row (the market-config shape, 2026-09-15) -- and the
 * saved row itself, which the client applies in place.
 *
 * It RETURNS the row rather than revalidating the route: a returning action that also revalidates
 * re-renders the whole page on top of the row the client just applied, which is the flicker the
 * console's interaction rules ban. The section merges this into its own list, so the table and the
 * record-sale dropdown both update without the page moving under the reader.
 */
export type SellableProductActionState = {
  status: "idle" | "success" | "error";
  code: string;
  ticket: number;
  product?: SalesProductOption & { status: string; is_builtin: boolean; sort_order: number; species_code: string };
  /** The code that was removed, when the action was a delete. */
  deletedCode?: string;
};

/**
 * Adds an item the farm sells, edits one, or removes one (maintainer instruction 2026-09-23).
 *
 * The CODE is what makes this an edit rather than a second item: absent when adding, the backend
 * derives it from the name once; sent back when editing, so a rename keeps every sale already
 * recorded under the item. The form never composes it.
 */
export async function saveSellableProductAction(
  previous: SellableProductActionState,
  formData: FormData,
): Promise<SellableProductActionState> {
  const ticket = previous.ticket + 1;
  const code = (formData.get("code")?.toString() ?? "").trim();

  // The same form carries both buttons, so the intent is read from the one that was pressed.
  if (formData.get("intent") === "delete") {
    if (code === "") return { status: "error", code: "sellable_product_save_failed", ticket };
    const removed = await deleteSellableProduct(code);
    if (!removed.ok) {
      return {
        status: "error",
        code: removed.error.code === "product_has_sales" ? "sellable_product_has_sales" : "sellable_product_save_failed",
        ticket,
      };
    }
    return { status: "success", code: "sellable_product_deleted", ticket, deletedCode: code };
  }

  const name = (formData.get("name")?.toString() ?? "").trim();
  const kind = (formData.get("kind")?.toString() ?? "").trim();
  const unit = (formData.get("unit")?.toString() ?? "").trim();
  if (name === "" || kind === "" || unit === "") {
    return { status: "error", code: "sellable_product_save_failed", ticket };
  }
  const speciesCode = (formData.get("species_code")?.toString() ?? "").trim();
  // Absent when adding: the backend appends the item after the last one, so nobody is asked to
  // number the list. An edit sends back the place the row already has.
  const sortOrder = Number((formData.get("sort_order")?.toString() ?? "0").trim() || "0");
  // A blank tick is an ARCHIVED item: the box asks whether it is in use, and an unticked box is a
  // person saying it is not, never a missing answer.
  const status = formData.get("in_use") !== null ? "active" : "archived";

  const result = await saveSellableProduct({
    code,
    name,
    kind: kind as SellableProductWrite["kind"],
    unit: unit as SellableProductWrite["unit"],
    species_code: speciesCode,
    sort_order: sortOrder,
    status,
  });
  if (!result.ok) {
    // The one refusal worth its own sentence; everything else is "check the fields".
    return {
      status: "error",
      code: result.error.code === "product_name_taken" ? "sellable_product_name_taken" : "sellable_product_save_failed",
      ticket,
    };
  }
  return {
    status: "success",
    code: "sellable_product_saved",
    ticket,
    // Built from what the BACKEND saved, never from what the form sent: the two agree only if the
    // write did what the client expected, and the row the client then shows must be the stored one.
    product: {
      name: result.data.name,
      code: result.data.code,
      kind: result.data.kind,
      unit: result.data.unit,
      priced_per_unit: result.data.priced_per_unit,
      status,
      is_builtin: false,
      sort_order: sortOrder,
      species_code: speciesCode,
    },
  };
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
 * What a refused payment write hands back to its form. `message` is the backend's own sentence and
 * is carried only for a named FIELD refusal ("Received on cannot be in the future.") -- anything
 * else (network, a 5xx) carries none, and the form shows its contract copy instead, because a
 * transport error's text is not farm copy.
 */
export type SalesPaymentActionError = { code: string; message: string };

function paymentRefusal(error: { code?: string; message: string }): SalesPaymentActionError {
  const code = error.code ?? "";
  return { code, message: code.startsWith("sales_invalid_") ? error.message : "" };
}

/**
 * Records one amount received from the buyer against a deal. The backend advances the running
 * received total in the same transaction; deal status stays a human decision.
 *
 * The idempotency key is the FORM's (payment-idempotency.ts): minted once when the drawer shows the
 * form, so a double click posts the same key twice and the backend replays the first receipt
 * instead of recording the money again. A refusal returns in place -- no redirect, no revalidate --
 * so the form keeps what was typed and shows the backend's reason beside it.
 */
export async function recordSalesDealPaymentAction(formData: FormData): Promise<SalesPaymentActionError | undefined> {
  const dealId = requiredString(formData, "deal_id");
  const note = (formData.get("note")?.toString() ?? "").trim();
  const result = await recordSalesDealPayment(
    dealId,
    {
      received_on: requiredString(formData, "received_on"),
      amount_rupees: Number(requiredString(formData, "amount_rupees")),
      ...(note ? { note } : {}),
    },
    paymentIdempotencyKey(formData),
  );
  if (!result.ok) {
    return paymentRefusal(result.error);
  }
  // interaction-guard:ignore: success revalidates then redirects; only the refusal path returns, and it never revalidates.
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.payment_recorded");
}

export async function updateSalesDealPaymentAction(formData: FormData): Promise<SalesPaymentActionError | undefined> {
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
    // The FORM's key, never a hash of its content (d015e8480): a content-derived key would replay a
    // later edit back to an earlier value as the old edit. The form's key rotates once an edit
    // lands, so each edit is its own intent while a double click stays one.
    paymentIdempotencyKey(formData),
  );
  if (!result.ok) {
    return paymentRefusal(result.error);
  }
  // interaction-guard:ignore: success revalidates then redirects; only the refusal path returns, and it never revalidates.
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.payment_updated");
}

export async function deleteSalesDealPaymentAction(formData: FormData): Promise<SalesPaymentActionError | undefined> {
  const dealId = requiredString(formData, "deal_id");
  const paymentId = requiredString(formData, "payment_id");
  const result = await deleteSalesDealPayment(dealId, paymentId, paymentIdempotencyKey(formData));
  if (!result.ok) {
    return paymentRefusal(result.error);
  }
  // interaction-guard:ignore: success revalidates then redirects; only the refusal path returns, and it never revalidates.
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.payment_deleted");
}

/** Sets a deal's lifecycle status — the edit that closes an expected sale on the day it happens. */
export async function setSalesDealStatusAction(formData: FormData): Promise<void> {
  const dealId = requiredString(formData, "deal_id");
  // The backend validates against its closed vocabulary and REJECTS an unrecognised word; the
  // cast only satisfies the generated client's literal union, it is not a trust boundary.
  const status = requiredString(formData, "status") as SalesDealStatusWrite["status"];
  const result = await setSalesDealStatus(dealId, {
    status,
    // Only ever true because a person ticked it after being shown what the store holds.
    stock_shortfall_acknowledged: formData.get("stock_shortfall_acknowledged") !== null,
  });
  if (!result.ok) {
    // CLOSING an expected sale is the moment its feed leaves the store, so it is weighed against
    // today's balance exactly as recording one is -- and the answer is the same: show the desk what
    // the store holds and let it close the sale anyway. Keyed on the backend's CODE, never on its
    // sentence, which is farm copy and may be reworded.
    if (result.error.code === "feed_stock_confirmation_required") {
      actionRedirectWithDetail(formData, "error", "action.deal_status_feed_stock_confirm", result.error.message);
    }
    actionRedirect(formData, "error", "action.deal_status_update_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.deal_status_updated");
}
