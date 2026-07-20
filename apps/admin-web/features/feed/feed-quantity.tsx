import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedDirectionItemQuantity } from "@/lib/api/server";
import { classifyFeedQuantity, isConfiguredZeroQuantity } from "./feed-quantity-state";

// =================================================================================================
// THE ONE RULE THIS WHOLE VERTICAL TURNS ON: A BLOCKED CELL IS NOT A ZERO.
//
// `/feed-direction/preview` returns `quantity_kg: null` if and only if `status === "blocked"`, which
// means NO ration was ever authored for that (ration group, shed tag, feed item). It is not a
// quantity. It is the absence of an instruction, and the consequence is that the shed does not get
// fed at all.
//
// An authored `"0.000"` with `status: "resolved"` is the OPPOSITE state: someone deliberately said
// "these animals get none of this item" — correct and normal for K0/K1 kids on milk. The shed is
// configured; it simply eats nothing of that item.
//
// The two therefore MUST render differently, and neither may render as the other:
//
//   blocked         -> a DANGER tag naming the gap, and NO NUMBER AT ALL. Not "0", not "0.00", and
//                      deliberately not an em-dash either: a dash in a kg column reads as "nothing
//                      to feed", which is exactly the wrong conclusion.
//   configured zero -> the real authored number "0.000" plus an INFO tag saying it is deliberate.
//   otherwise       -> the number.
//
// Both the tag text and its hover explanation come from the page contract (`label.blocked` /
// `label.blocked_note` / `label.configured_zero` / `label.configured_zero_note`), which each of the
// three feed pages defines with wording tuned to its own surface.
//
// Quantities stay EXACT DECIMAL STRINGS from the API to the DOM. Nothing here calls Number() on a
// quantity: a float round trip is how a column total ends up reading 12.299999999. The one place a
// quantity is inspected numerically is `isConfiguredZero`, which does it by string shape precisely so
// no parse happens.
// =================================================================================================

// The classification itself lives in feed-quantity-state.ts as a pure, unit-tested function — the
// live seed contains no blocked cells, so this path is proven by test rather than by clicking.
export { isConfiguredZeroQuantity as isConfiguredZero };

/** True only for the blocked state — never inferred from a falsy/absent quantity. */
export function isBlockedItem(item: Pick<FeedDirectionItemQuantity, "status">): boolean {
  return item.status === "blocked";
}

export function FeedQuantityCell({
  item,
  pageContract,
}: {
  item: FeedDirectionItemQuantity;
  pageContract: AdminUiPageContract;
}) {
  // BLOCKED: no number is rendered, in any form. The tag is the value.
  //
  // `blocked_reason.detail` names the exact missing coordinate ("no authored rate for CBE / Kid /
  // F2-Male / COFS"), which is what makes the gap closable; the contract's own note explains why a
  // blank here is not zero. Both are shown, reason first, because a gap an operator cannot locate is
  // a gap they cannot close.
  const state = classifyFeedQuantity(item);

  if (state === "blocked") {
    const reasonDetail = item.blocked_reason?.detail;
    const blockedNote = copy(pageContract, "label.blocked_note");
    return (
      <span
        className="tag t-dng"
        title={reasonDetail ? `${reasonDetail} — ${blockedNote}` : blockedNote}
      >
        {copy(pageContract, "label.blocked_short")}
      </span>
    );
  }

  // RESOLVED. The quantity is a real authored number, including a real authored zero.
  // classifyFeedQuantity already routed any unusable quantity to `blocked` above, so this is a
  // string by construction.
  const quantity = item.quantity_kg as string;
  const configuredZero = state === "configured_zero";
  // The number and its unit are ONE token and stay on one line — "0.000" orphaned from "kg" is a
  // misreadable quantity. The configured-zero badge is a separate annotation, so it is allowed to
  // wrap underneath when the column is tight instead of holding the whole column open at the width
  // of "0.000 kg Configured zero" (which is what pushed Feed Direction's session total off-screen).
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>
        <span
          style={{
            fontVariantNumeric: "tabular-nums",
            fontWeight: configuredZero ? 500 : 700,
            color: configuredZero ? "var(--muted)" : "var(--brand-d)",
          }}
        >
          {quantity}
        </span>
        <span className="muted" style={{ fontSize: 11 }}>
          {copy(pageContract, "label.kg_noun")}
        </span>
      </span>
      {configuredZero ? (
        <span className="tag t-info" title={copy(pageContract, "label.configured_zero_note")}>
          {copy(pageContract, "label.configured_zero")}
        </span>
      ) : null}
    </span>
  );
}

/**
 * The per-item status tag. `label.ok` / `label.blocked` are each page's own wording for the two
 * states — Feed Direction calls the good one "Planned", Feed Packing calls it "Ready to pack".
 */
export function FeedItemStatusTag({
  item,
  pageContract,
}: {
  item: FeedDirectionItemQuantity;
  pageContract: AdminUiPageContract;
}) {
  if (isBlockedItem(item)) {
    return (
      <span className="tag t-dng" title={copy(pageContract, "label.blocked_note")}>
        {copy(pageContract, "label.blocked")}
      </span>
    );
  }
  return (
    <span className="tag t-ok" title={copy(pageContract, "label.ok_note")}>
      {copy(pageContract, "label.ok")}
    </span>
  );
}

/** normal = derived per head from the grid; experiment = hand-entered absolute kg for the shed. */
export function FeedWorkflowTag({
  workflow,
  pageContract,
}: {
  workflow: string;
  pageContract: AdminUiPageContract;
}) {
  const experiment = workflow === "experiment";
  return (
    <span
      className={experiment ? "tag t-pur" : "tag t-ok"}
      title={copy(pageContract, experiment ? "label.workflow_experiment_note" : "label.workflow_normal_note")}
    >
      {copy(pageContract, experiment ? "label.workflow_experiment" : "label.workflow_normal")}
    </span>
  );
}

/**
 * A movement the projected head count already assumes has happened, whose feed-effective date has
 * passed with the animals still not moved. It keeps counting toward the shed every day until it is
 * executed or cancelled, so it is surfaced on the row rather than left silent.
 */
export function FeedOverdueShiftingChip({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <span className="tag t-warn" title={copy(pageContract, "label.overdue_shifting_note")}>
      {copy(pageContract, "label.overdue_shifting_chip")}
    </span>
  );
}
