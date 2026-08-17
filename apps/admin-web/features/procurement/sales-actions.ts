"use server";

// Write flow for the sales board's record-sale drawer. The backend response (or its error
// envelope) drives the banner the operator sees — no optimistic success.
import { randomUUID } from "node:crypto";
import { revalidatePath } from "next/cache";
import { actionRedirect, optionalString, requiredString } from "@/lib/action-helpers";
// NOTE: every actionKey below MUST start with "action." -- withActionFeedback silently rewrites
// anything else to "action.error_form" -- and each key needs matching page-contract copy, because
// actionFeedbackCopy throws on a missing key and takes the whole page down with it.
import { createSalesDeal } from "@/lib/api/procurement-server";
import type { SalesDealWrite } from "@/lib/api/procurement";

const SALES_PATH = "/procurement/sales";

/**
 * Reads the record-sale fields off the form.
 *
 * A CLEARED count/weight/advance is null, never 0: zero animals or zero advance is a real recorded
 * fact, and coercing blank into it would invent that fact. Sale value is required and parsed as a
 * number for the contract; the backend re-validates it must be more than zero.
 */
function readSaleForm(formData: FormData): SalesDealWrite {
  const parseOptionalNumber = (key: string): number | null => {
    const trimmed = (formData.get(key)?.toString() ?? "").trim();
    if (trimmed === "") return null;
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) ? parsed : null;
  };

  return {
    sale_date: requiredString(formData, "sale_date"),
    // The backend re-validates these against its closed vocabularies and REJECTS an unrecognised
    // value; the casts only satisfy the generated client's literal unions, they are not a trust
    // boundary.
    farm: requiredString(formData, "farm") as SalesDealWrite["farm"],
    product_type: requiredString(formData, "product_type") as SalesDealWrite["product_type"],
    breed: requiredString(formData, "breed"),
    buyer_name: requiredString(formData, "buyer_name"),
    buyer_place: optionalString(formData, "buyer_place") ?? "",
    animal_count: parseOptionalNumber("animal_count"),
    total_weight_kg: parseOptionalNumber("total_weight_kg"),
    sales_value: Number(requiredString(formData, "sales_value")),
    advance_amount: parseOptionalNumber("advance_amount"),
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
