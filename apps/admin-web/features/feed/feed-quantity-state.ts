// The blocked-vs-zero decision, isolated as a pure function so it can be tested directly.
//
// This is the single most consequential rendering rule in the Feed vertical, and the live seed does
// NOT currently contain a blocked cell (both CBE and CPT are fully configured). A path that real data
// never exercises is exactly the one that rots silently, so the decision lives here with tests rather
// than only inside JSX.
//
// The three states and why they must never collapse into each other:
//
//   blocked          No authored ration exists for this (ration group, shed tag, feed item). There is
//                    NO quantity — the API sends `quantity_kg: null` — and the shed will not be fed
//                    at all until someone authors a rate. Renders as a gap, never as a number.
//
//   configured_zero  An authored rate of exactly 0. A real feeding instruction meaning "these animals
//                    get none of this item", correct for K0/K1 kids on milk. The shed IS configured.
//
//   planned          An authored rate above zero.
//
// Reading a blocked cell as zero is the failure this function exists to make impossible: it turns
// "we do not know what to feed these animals" into "feed them nothing", and the sheet still looks
// complete.

export type FeedQuantityState = "blocked" | "configured_zero" | "planned";

export type FeedQuantityInput = {
  status: string;
  quantity_kg?: string | null;
};

/**
 * True when the string is an authored zero ("0", "0.0", "0.000"...).
 *
 * Decided on the STRING SHAPE, never by parsing. These are exact decimals the backend owns; calling
 * Number() on them is how a column total ends up reading 12.299999999.
 */
export function isConfiguredZeroQuantity(quantityKg: string | null | undefined): boolean {
  if (typeof quantityKg !== "string") return false;
  return /^-?0(\.0*)?$/.test(quantityKg.trim());
}

/**
 * Classifies one feed item cell.
 *
 * `blocked` wins on status alone. It is deliberately NOT inferred from a missing/falsy quantity, and
 * a resolved cell carrying no usable quantity string also lands on `blocked` rather than on zero:
 * if the API's own invariant (`quantity_kg` is null iff blocked) is ever violated, failing toward
 * "we do not know" is safe and failing toward "zero" starves a shed.
 */
export function classifyFeedQuantity(item: FeedQuantityInput): FeedQuantityState {
  if (item.status === "blocked") return "blocked";
  if (typeof item.quantity_kg !== "string" || item.quantity_kg.trim() === "") return "blocked";
  return isConfiguredZeroQuantity(item.quantity_kg) ? "configured_zero" : "planned";
}
