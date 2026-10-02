"use server";

// Server action for the FARM VALUATION section on /sales/config (maintainer instruction
// 2026-09-19; the stage list became authored on 2026-09-24). One whole-set PUT: the stages the herd
// is valued in and every stage's four species x gender rows (2026-10-02), fenced on the
// row_version the form loaded. Lands in place (useActionState): the SAVED ROW comes back on the
// state and the section applies it -- new row_version, stored figures, the stage keys the backend
// assigned to rows that were added -- so the next save is fenced on what was just written without a
// route re-read. The backend's refusal message is returned verbatim because it names the field and
// the reason. The Farm value / Load wise pages re-read the assumptions per request, so nothing to
// revalidate.

import { putValuationAssumptions, type ValuationAssumptions, type ValuationBucket, type ValuationStage } from "@/lib/api/sales-valuation-server";

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
  // The stage list travels as one field because it is one decision: which stages exist, in what
  // order, covering which register entries. Splitting it across named inputs would let a half-read
  // form post a stage with no coverage.
  let stages: ValuationStage[] = [];
  try {
    stages = JSON.parse((formData.get("stages") as string | null) ?? "[]") as ValuationStage[];
  } catch {
    return { status: "error", code: "failed", message: "The stages could not be read.", ticket };
  }
  const buckets: ValuationBucket[] = [];
  stages.forEach((stage) => {
    // Species x gender, in the order the backend lists them (domain.BucketKeysForStages).
    ["goat_female", "goat_male", "sheep_female", "sheep_male"].forEach((group) => {
      // The inputs are NAMED after the row's field key, which for a stage being added is its row
      // id; the bucket is KEYED by the stage key it will be stored under. Reading by one and
      // posting by the other is what lets a new stage be typed without its inputs being renamed
      // mid-keystroke.
      const fieldKey = (stage as { field_key?: string }).field_key || stage.stage;
      const key = `${stage.stage}_${group}`;
      buckets.push({
        bucket: key,
        // The card's words are composed by the backend from the stage's own label, so a stage
        // renamed here renames both its cards and nothing has to be kept in step.
        label: "",
        fixed_weight_kg: num(formData.get(`weight_${fieldKey}_${group}`)),
        price_per_kg: num(formData.get(`price_${fieldKey}_${group}`)) ?? Number.NaN,
        display_order: buckets.length + 1,
      });
    });
  });
  const body = {
    // field_key is the SCREEN's business -- which inputs belong to which row -- and must not ride
    // to the backend as part of the authored stage.
    stages: stages.map((s, i) => {
      const { stage, label, matches } = s;
      return { stage, label, matches, display_order: i + 1 };
    }),
    buckets,
    // Retired 2026-10-02: Load wise values unsold animals by weight x the bucket ₹/kg above, so
    // the per-animal figure no longer prices anything and is cleared on save.
    unsold_stock_price_rupees: null,
    row_version: Number(formData.get("row_version") ?? 0),
  };
  // A field that is not a number is sent as-is so the backend names it; JSON has no NaN, so
  // refuse here with the same shape the backend would.
  const nan = buckets.find((b) => Number.isNaN(b.price_per_kg) || Number.isNaN(b.fixed_weight_kg ?? 0));
  if (nan) {
    return { status: "error", code: "failed", message: "Every figure must be a number.", ticket };
  }
  const result = await putValuationAssumptions(body);
  if (!result.ok) return { status: "error", code: "failed", message: result.error.message || "", ticket };
  return { status: "success", code: "saved", message: "", ticket, saved: result.data };
}
