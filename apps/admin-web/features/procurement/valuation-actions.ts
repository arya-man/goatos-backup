"use server";

// Server action for the FARM VALUATION section on /sales/config (maintainer instruction
// 2026-09-19). One whole-set PUT: every bucket's weight and price, the sale-ready line and the
// unsold-stock price, fenced on the row_version the form loaded. Lands in place (useActionState):
// the SAVED ROW comes back on the state and the section applies it -- new row_version, stored
// figures -- so the next save is fenced on what was just written without a route re-read. The
// backend's refusal message is returned verbatim because it names the field and the band. The
// Farm value / Load wise pages re-read the assumptions per request, so nothing to revalidate.

import { putValuationAssumptions, type ValuationAssumptions, type ValuationBucket } from "@/lib/api/sales-valuation-server";

export type ValuationActionState = {
  status: "idle" | "success" | "error";
  /** `saved` / `failed`, resolved to copy by the section; `message` is the backend's own words on a refusal. */
  code: string;
  message: string;
  ticket: number;
  /** The row as stored, on success; the section renders from it. */
  saved?: ValuationAssumptions;
};

function num(raw: FormDataEntryValue | null): number | null {
  const s = (typeof raw === "string" ? raw : "").trim();
  if (s === "") return null;
  const n = Number(s);
  return Number.isFinite(n) ? n : Number.NaN;
}

export async function saveValuationAction(previous: ValuationActionState, formData: FormData): Promise<ValuationActionState> {
  const ticket = previous.ticket + 1;
  const keys = (formData.get("bucket_keys") as string | null)?.split(",").filter(Boolean) ?? [];
  const buckets: ValuationBucket[] = keys.map((key, i) => ({
    bucket: key,
    label: ((formData.get(`label_${key}`) as string | null) ?? "").trim(),
    fixed_weight_kg: num(formData.get(`weight_${key}`)),
    price_per_kg: num(formData.get(`price_${key}`)) ?? Number.NaN,
    display_order: i + 1,
  }));
  const body = {
    buckets,
    sale_ready_kg: num(formData.get("sale_ready_kg")) ?? Number.NaN,
    unsold_stock_price_rupees: num(formData.get("unsold_stock_price_rupees")),
    row_version: Number(formData.get("row_version") ?? 0),
  };
  // A field that is not a number is sent as-is so the backend names it; JSON has no NaN, so
  // refuse here with the same shape the backend would.
  const nan = buckets.find((b) => Number.isNaN(b.price_per_kg) || Number.isNaN(b.fixed_weight_kg ?? 0));
  if (nan || Number.isNaN(body.sale_ready_kg) || Number.isNaN(body.unsold_stock_price_rupees ?? 0)) {
    return { status: "error", code: "failed", message: "Every figure must be a number.", ticket };
  }
  const result = await putValuationAssumptions(body);
  if (!result.ok) return { status: "error", code: "failed", message: result.error.message || "", ticket };
  return { status: "success", code: "saved", message: "", ticket, saved: result.data };
}
