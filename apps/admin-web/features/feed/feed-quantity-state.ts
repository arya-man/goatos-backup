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

// -------------------------------------------------------------------------------------------------
// OPERATIONAL-SHEET VISIBILITY
//
// Feed Direction and Feed Packing are the sheets someone works FROM. A line saying "pack 0.000 kg of
// RGS Concentrate" is not an instruction — it is an instruction to do nothing, printed among the
// instructions to do something, and it lengthens the sheet in proportion to how many items the park
// has authored as zero. Castro 1 lists 5 items of which 3 are configured zero; the packer needs 2.
//
// So configured zero is hidden on the operational sheets. It is NOT hidden on Feed Config: that is the
// AUTHORING surface, where a rate of 0 is the thing being edited and "Configured zero" is the useful
// fact on screen.
//
// The predicate below is deliberately written as an equality against the `configured_zero` CLASS, not
// as a test on the quantity. The tempting version —
//
//     if (!item.quantity_kg) hide            // WRONG
//     if (isConfiguredZeroQuantity(...)) hide // WRONG on its own
//
// — hides BLOCKED cells too, because blocked carries `quantity_kg: null`. That inverts the entire
// point of this module: a blocked cell is the loud, locatable "nobody has said what to feed these
// animals" marker, and hiding it converts a visible gap into a silent one, on the exact sheet an
// operator uses to decide a shed is done. Routing through classifyFeedQuantity makes that
// unreachable: `blocked` is returned before any quantity is inspected, so no quantity shape — null,
// empty, "0.000", or absent — can ever produce a hidden blocked cell.
// -------------------------------------------------------------------------------------------------

/**
 * True only for an authored zero on a resolved cell — the one state hidden from Feed Direction and
 * Feed Packing. Blocked is structurally excluded: it classifies as `blocked` before its (null)
 * quantity is ever looked at.
 */
export function isHiddenOnOperationalSheet(item: FeedQuantityInput): boolean {
  return classifyFeedQuantity(item) === "configured_zero";
}

/**
 * The item lines an operational sheet renders: everything except configured zero.
 *
 * Returns the same array identity-wise unfiltered when nothing is hidden, and preserves order.
 * Blocked and planned lines always survive.
 */
export function visibleOperationalFeedItems<T extends FeedQuantityInput>(items: readonly T[]): T[] {
  return items.filter((item) => !isHiddenOnOperationalSheet(item));
}

/**
 * True when a shed/session genuinely had item lines but every one of them was a configured zero —
 * the "nothing to feed this session" state.
 *
 * Distinct from "no items at all", which is a different (backend) condition, and distinct from a row
 * whose items are blocked: a blocked row always has at least one visible line, so it can never land
 * here.
 */
export function isNothingToFeed(items: readonly FeedQuantityInput[]): boolean {
  return items.length > 0 && visibleOperationalFeedItems(items).length === 0;
}
