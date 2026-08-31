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
  createSalesBenchmark,
  createSalesBuyerLead,
  createSalesDeal,
  recordSalesDealPayment,
  createSalesFpoLead,
  createSalesSoldTags,
  createSalesWeightCheck,
  setSalesBuyerLeadStatus,
  setSalesFpoLeadStatus,
} from "@/lib/api/procurement-server";
import type { SalesBuyerLeadWrite, SalesDealWrite, SalesSoldTagsWrite } from "@/lib/api/procurement";

const SALES_PATH = "/sales";

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
    // REQUIRED: every sale is made to a vendor on the register (maintainer decision 2026-08-27).
    // requiredString throws on a blank, so a form that somehow submits without a selection fails
    // here rather than posting a vendorless deal; the backend re-validates the same rule.
    buyer_vendor_id: requiredString(formData, "buyer_vendor_id"),
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

// ---- Pipeline & evidence entry (the retired Sales DB sheet's job, now done in the app). ----
// Same key discipline as recordSaleAction: a fresh UUID per submit.

function parseOptionalNumber(formData: FormData, key: string): number | null {
  const trimmed = (formData.get(key)?.toString() ?? "").trim();
  if (trimmed === "") return null;
  const parsed = Number(trimmed);
  return Number.isFinite(parsed) ? parsed : null;
}

export async function recordBuyerLeadAction(formData: FormData): Promise<void> {
  const result = await createSalesBuyerLead(
    {
      recorded_date: optionalString(formData, "recorded_date") ?? "",
      farm: (optionalString(formData, "farm") ?? "") as SalesBuyerLeadWrite["farm"],
      buyer_name: requiredString(formData, "buyer_name"),
      buyer_place: optionalString(formData, "buyer_place") ?? "",
      animal_type: optionalString(formData, "animal_type") ?? "",
      breed: optionalString(formData, "breed") ?? "",
      call_status: optionalString(formData, "call_status") ?? "",
    },
    randomUUID(),
  );
  if (!result.ok) {
    actionRedirect(formData, "error", "action.lead_record_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.lead_recorded");
}

export async function updateBuyerLeadStatusAction(formData: FormData): Promise<void> {
  const result = await setSalesBuyerLeadStatus(
    requiredString(formData, "lead_id"),
    { call_status: optionalString(formData, "call_status") ?? "" },
    randomUUID(),
  );
  if (!result.ok) {
    actionRedirect(formData, "error", "action.lead_status_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.lead_status_updated");
}

export async function recordFpoLeadAction(formData: FormData): Promise<void> {
  const result = await createSalesFpoLead(
    {
      fpo_name: requiredString(formData, "fpo_name"),
      crops: optionalString(formData, "crops") ?? "",
      district: optionalString(formData, "district") ?? "",
      taluk: optionalString(formData, "taluk") ?? "",
      state: optionalString(formData, "state") ?? "",
      call_status: optionalString(formData, "call_status") ?? "",
    },
    randomUUID(),
  );
  if (!result.ok) {
    actionRedirect(formData, "error", "action.fpo_record_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.fpo_recorded");
}

export async function updateFpoLeadStatusAction(formData: FormData): Promise<void> {
  const result = await setSalesFpoLeadStatus(
    requiredString(formData, "lead_id"),
    { call_status: optionalString(formData, "call_status") ?? "" },
    randomUUID(),
  );
  if (!result.ok) {
    actionRedirect(formData, "error", "action.lead_status_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.lead_status_updated");
}

export async function recordBenchmarkAction(formData: FormData): Promise<void> {
  const result = await createSalesBenchmark(
    {
      market: optionalString(formData, "market") ?? "",
      category: optionalString(formData, "category") ?? "",
      breed: requiredString(formData, "breed"),
      source: optionalString(formData, "source") ?? "",
      ex_farm_rate: optionalString(formData, "ex_farm_rate") ?? "",
      transport_rate: optionalString(formData, "transport_rate") ?? "",
      landing_cost_per_kg: parseOptionalNumber(formData, "landing_cost_per_kg"),
      market_price_per_kg: parseOptionalNumber(formData, "market_price_per_kg"),
    },
    randomUUID(),
  );
  if (!result.ok) {
    actionRedirect(formData, "error", "action.quote_record_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.quote_recorded");
}

/**
 * Parses the tag-list textarea: one animal per line, comma-separated as
 * "animal label, tag number, weight kg" — tag and weight optional.
 */
function parseTagRows(raw: string): SalesSoldTagsWrite["rows"] {
  return raw
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "")
    .map((line) => {
      const [label = "", tag = "", weight = ""] = line.split(",").map((part) => part.trim());
      const parsedWeight = weight === "" ? null : Number(weight);
      return {
        animal_label: label,
        tag_number: tag,
        weight_kg: parsedWeight != null && Number.isFinite(parsedWeight) ? parsedWeight : null,
      };
    });
}

export async function recordSoldTagsAction(formData: FormData): Promise<void> {
  const result = await createSalesSoldTags(
    {
      farm: (optionalString(formData, "farm") ?? "") as SalesSoldTagsWrite["farm"],
      rows: parseTagRows(requiredString(formData, "tag_rows")),
    },
    randomUUID(),
  );
  if (!result.ok) {
    actionRedirect(formData, "error", "action.tags_record_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.tags_recorded");
}

export async function recordWeightCheckAction(formData: FormData): Promise<void> {
  const result = await createSalesWeightCheck(
    {
      tag_number: optionalString(formData, "tag_number") ?? "",
      book_weight_kg: Number(requiredString(formData, "book_weight_kg")),
      video_weight_kg: Number(requiredString(formData, "video_weight_kg")),
      farm_born: formData.get("farm_born") === "on",
    },
    randomUUID(),
  );
  if (!result.ok) {
    actionRedirect(formData, "error", "action.weight_check_failed");
  }
  revalidatePath(SALES_PATH);
  actionRedirect(formData, "success", "action.weight_check_recorded");
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
