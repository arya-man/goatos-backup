"use client";

import { useEffect, useState, useTransition } from "react";
import { Pencil } from "lucide-react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedConfigActionResult } from "./feed-config-actions";
import { afterSubmit, CLOSED_STATE, openIntent, type AuthoringIdempotencyState } from "@/lib/authoring-idempotency";
import { clearSavedRate, publishSavedRate, rationRateKey } from "./feed-rate-optimistic";

// Inline editors for the three writable Feed Config surfaces.
//
// THE BLANK-IS-NOT-ZERO RULE, ON THE INPUT SIDE.
//
// The numeric inputs below are UNCONTROLLED (`defaultValue`) on purpose. A controlled numeric input
// is where blank quietly becomes 0: `value={n}` with an `onChange` that does `Number(e.target.value)`
// turns a cleared box into 0 the moment the last character is deleted, and a React `useState(0)`
// default authors a business value nobody typed. Keeping them uncontrolled means the field holds
// exactly the characters the operator left in it — including none — and the server action decides
// what a blank means (see feed-config-actions.ts: it is REJECTED, never defaulted).
//
// `type="text"` with `inputMode="decimal"` rather than `type="number"` for the same reason: a number
// input silently reports an out-of-range or malformed value as "" in some browsers, which would turn
// a typo into a blank and a blank into a rejection the operator cannot explain. Out-of-range values
// must reach the backend verbatim so its field error is what the operator sees.
//
// There is no min/max/step clamping here, and no rounding. That is deliberate and must stay that way.

// Split across two aliases so the arrow type does not read as `> Promise<` — the contract-literal
// guard scans for `>text<` to catch visible JSX copy, and an inline generic return type trips it.
type SaveActionResult = Promise<FeedConfigActionResult>;
type SaveAction = (formData: FormData) => SaveActionResult;

/**
 * How long a success confirmation stays on screen once the form has closed, in ms.
 *
 * It clears itself so the confirmation cannot become permanent furniture: these controls live in
 * table cells, and a message that never goes away would grow the row of every combination the
 * operator has ever touched in this tab. A REJECTION is not on a timer — it stays inside the still
 * open form until the operator fixes the value, because it is the instruction for what to do next.
 */
const SUCCESS_NOTICE_MS = 8000;

function FeedConfigFormShell({
  pageContract,
  action,
  children,
  editLabel,
  openLabel,
  onSaved,
  onOptimistic,
  onRejected,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  children: React.ReactNode;
  editLabel: string;
  openLabel: string;
  /**
   * Called with the submitted form ONLY after a CONFIRMED save, so a caller can show the new value
   * before the route's re-render lands. Never called for a rejected write — the form already
   * surfaces that error, and echoing the refused value beside it would say the opposite.
   */
  onSaved?: (formData: FormData) => void;
  /**
   * Called with the submitted form BEFORE the action is awaited, so a caller can show the value
   * immediately. Paired with onRejected, which must undo it — at this point nothing is confirmed.
   */
  onOptimistic?: (formData: FormData) => void;
  /** Called when the write was REFUSED, so an optimistic display can be rolled back. */
  onRejected?: (formData: FormData) => void;
}) {
  const [pending, startTransition] = useTransition();
  const [result, setResult] = useState<FeedConfigActionResult | null>(null);
  // The idempotency-key lifecycle (mint on open, reuse across retries, rotate only after a
  // confirmed success) is a pure state machine in lib/authoring-idempotency.ts, tested there
  // directly so the retry/replay behavior does not require mounting this component.
  const [idem, setIdem] = useState<AuthoringIdempotencyState>(CLOSED_STATE);

  // Drop the confirmation after a while. Keyed on the result object identity, so each new success
  // restarts the clock and the timer is cancelled if the operator reopens the form first.
  const settled = result !== null && result.ok && !idem.open;
  useEffect(() => {
    if (!settled) return undefined;
    const timer = setTimeout(() => setResult(null), SUCCESS_NOTICE_MS);
    return () => clearTimeout(timer);
  }, [settled, result]);

  function handleOpen() {
    setResult(null);
    setIdem(openIntent(() => crypto.randomUUID()));
  }

  function onSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formData = new FormData(event.currentTarget);
    // BEFORE the await: the action's response carries the revalidated page, so a caller that waits
    // for it cannot show anything sooner than the re-render itself.
    onOptimistic?.(formData);
    startTransition(async () => {
      const outcome = await action(formData);
      setResult(outcome);
      if (outcome.ok) onSaved?.(formData);
      else onRejected?.(formData);
      setIdem((prev) => afterSubmit(prev, outcome.ok, () => crypto.randomUUID()));
      // A CONFIRMED write ENDS the editing intent, so the form closes — the same rule the sibling
      // authoring screen (/health/config) already follows, and three things depend on it:
      //
      //  1. The operator sees the write. The server action revalidates this route, so the row or
      //     the catalog behind the form is already showing the new value; an open form sitting on
      //     top of it, still holding the text that was typed, reads as "nothing happened" and is
      //     what sent people to the browser reload button.
      //  2. The screen stops disagreeing with the server. The inputs are UNCONTROLLED, so they keep
      //     the characters that were typed rather than the value that was stored — after saving
      //     "4.5" the form said 4.5 while the table said 4.500. Closing drops the stale copy; the
      //     next open is rendered from the refreshed server props.
      //  3. A stray second Apply cannot write again. The key rotates on success, so a resubmit of
      //     the SAME still-filled form is not an idempotent replay — it is a second, real write.
      //
      // A rejection deliberately does NOT close: nothing was written, the values are still the
      // operator's to fix, and the same key must be reused for that retry.
      if (outcome.ok) setIdem(CLOSED_STATE);
    });
  }

  if (!idem.open) {
    const openButton = (
      <button type="button" className="btn sm" onClick={handleOpen} title={openLabel}>
        <Pencil className="ic" aria-hidden="true" />
        {editLabel}
      </button>
    );
    // Nothing has been saved from this control yet — render exactly the bare button, so the closed
    // state stays byte-identical to what every table cell and section header lays out today.
    if (!result) return openButton;
    return (
      <span
        style={{ display: "inline-flex", flexDirection: "column", alignItems: "flex-start", gap: 6, maxWidth: 220 }}
      >
        {openButton}
        <Outcome result={result} pageContract={pageContract} />
      </span>
    );
  }

  return (
    <form onSubmit={onSubmit} style={{ display: "flex", flexDirection: "column", gap: 8, minWidth: 220 }}>
      <input type="hidden" name="idempotency_key" value={idem.key ?? ""} />
      {children}
      <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
        <button type="submit" className="btn sm p" disabled={pending}>
          {pending ? copy(pageContract, "state.loading") : copy(pageContract, "action.apply")}
        </button>
        <button type="button" className="btn sm" onClick={() => setIdem(CLOSED_STATE)} disabled={pending}>
          {copy(pageContract, "action.cancel")}
        </button>
      </div>
      <Outcome result={result} pageContract={pageContract} />
    </form>
  );
}

/**
 * The outcome of the last submit, in the contract's own words.
 *
 * The blank-is-not-zero rejection is the important one to show: it is the message that tells an
 * operator that clearing a field leaves the combination BLOCKED, and that feeding nothing requires
 * typing an explicit 0. Every message resolves through the page contract — this component never
 * composes visible prose of its own.
 *
 * It renders in BOTH states of the shell: inside the still-open form for a rejection, and beside
 * the closed control for a success, so a confirmed write is confirmed OUT LOUD rather than only by
 * a number changing somewhere else on a long screen.
 */
function Outcome({
  result,
  pageContract,
}: {
  result: FeedConfigActionResult | null;
  pageContract: AdminUiPageContract;
}) {
  if (!result) return null;
  return (
    <div
      className="small"
      style={{
        color: result.ok ? "var(--brand-d)" : "var(--danger)",
        lineHeight: 1.5,
        // These messages are SENTENCES, and they render inside the feed tables, whose cells are
        // `nowrap` (failure mode 4b — short business values must never shred into character
        // columns). Left at the table's default, "Saved. This pen is fed the absolute kg authored
        // here…" ran one line off the right edge and was clipped. A sentence-shaped message wraps;
        // the width cap is what keeps it from widening the column instead.
        whiteSpace: "normal",
        overflowWrap: "break-word",
        maxWidth: 220,
      }}
    >
      {copy(pageContract, result.messageKey)}
      {result.detail ? <div className="muted">{result.detail}</div> : null}
    </div>
  );
}

/** Authored g/head/day for one (park, ration group, shed tag, feed item) cell. */
export function RationRateEditor({
  pageContract,
  action,
  parkId,
  rationGroup,
  shedTag,
  feedItem,
  gramsPerHead,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  parkId: string;
  rationGroup: string;
  shedTag: string;
  feedItem: string;
  /** The currently in-force rate, or undefined when the combination is unconfigured. */
  gramsPerHead?: string;
}) {
  return (
    <FeedConfigFormShell
      pageContract={pageContract}
      action={action}
      editLabel={copy(pageContract, gramsPerHead === undefined ? "action.add_rate" : "action.edit_rate")}
      openLabel={copy(pageContract, "label.configured_zero_note")}
      // Shows the typed quantity in this row's cell at once, and takes it back if the write is
      // refused — the form's own error is then the only thing on screen, which is correct: nothing
      // was stored, so the cell must go back to the server's value.
      onOptimistic={(formData) =>
        publishSavedRate(
          rationRateKey(parkId, rationGroup, shedTag, feedItem),
          String(formData.get("grams_per_head") ?? ""),
        )
      }
      onRejected={() => clearSavedRate(rationRateKey(parkId, rationGroup, shedTag, feedItem))}
    >
      <input type="hidden" name="park_id" value={parkId} />
      <input type="hidden" name="ration_group" value={rationGroup} />
      <input type="hidden" name="shed_tag" value={shedTag} />
      <input type="hidden" name="feed_item" value={feedItem} />
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`grams-${parkId}-${rationGroup}-${shedTag}-${feedItem}`}>
          {copy(pageContract, "label.grams_noun")}
        </label>
        <input
          id={`grams-${parkId}-${rationGroup}-${shedTag}-${feedItem}`}
          name="grams_per_head"
          type="text"
          inputMode="decimal"
          // Uncontrolled: a cleared box stays cleared and is rejected server-side, never sent as 0.
          defaultValue={gramsPerHead ?? ""}
          aria-describedby={`grams-hint-${parkId}-${rationGroup}-${shedTag}-${feedItem}`}
        />
        <div
          id={`grams-hint-${parkId}-${rationGroup}-${shedTag}-${feedItem}`}
          className="small muted"
          style={{ marginTop: 4 }}
        >
          {copy(pageContract, "reason.blank_is_not_zero")}
        </div>
      </div>
    </FeedConfigFormShell>
  );
}

/** Authored multiplier for one (park, shed, feed item). No row at all reads as 1.0. */
export function ShedFactorEditor({
  pageContract,
  action,
  parkId,
  shedId,
  feedItem,
  multiplier,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  parkId: string;
  shedId: string;
  feedItem: string;
  multiplier?: string;
}) {
  return (
    <FeedConfigFormShell
      pageContract={pageContract}
      action={action}
      editLabel={copy(pageContract, multiplier === undefined ? "action.add_shed_factor" : "action.edit_shed_factor")}
      openLabel={copy(pageContract, "section.shed_factors.note")}
    >
      <input type="hidden" name="park_id" value={parkId} />
      <input type="hidden" name="shed_id" value={shedId} />
      <input type="hidden" name="feed_item" value={feedItem} />
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`factor-${shedId}-${feedItem}`}>{copy(pageContract, "section.shed_factors.caption")}</label>
        <input
          id={`factor-${shedId}-${feedItem}`}
          name="multiplier"
          type="text"
          inputMode="decimal"
          defaultValue={multiplier ?? ""}
        />
        <div className="small muted" style={{ marginTop: 4 }}>
          {copy(pageContract, "reason.blank_is_not_zero")}
        </div>
      </div>
    </FeedConfigFormShell>
  );
}

/**
 * Authored ABSOLUTE kg for one (park, shed, PEN, feed item) of an experiment shed.
 *
 * The pen is part of that key, not decoration. A partitioned shed authors one cell per pen, so a
 * write that omits partition_label targets the shed-wide row instead of the pen the author clicked
 * -- and the field ids below would collide across pens, wiring one pen's label to another's input.
 *
 * Same uncontrolled-input discipline as the ration rate editor, for a reason that is quieter here:
 * a blank rate on the grid BLOCKS the shed visibly, while a blank experiment cell just stops the
 * shed being fed its authored kg and lets the per-head grid feed it instead.
 *
 * The head count input is separate and OPTIONAL. It carries no `required`, and a cleared box is sent
 * as null ("not recorded") rather than 0 ("this shed is empty") — see feed-config-actions.ts. It is
 * labelled as informational at the input, not only in the section note, because this is the exact
 * spot where someone would otherwise assume it multiplies the kg.
 */
export function ExperimentCellEditor({
  pageContract,
  action,
  parkId,
  shedId,
  partitionLabel,
  feedItem,
  experimentCategory,
  absoluteKg,
  headCount,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  parkId: string;
  shedId: string;
  /** The pen this cell belongs to; empty for an undivided shed. */
  partitionLabel: string;
  feedItem: string;
  experimentCategory: string;
  /** The currently authored kg, or undefined when this item has no row for the shed. */
  absoluteKg?: string;
  /** Absent means the population was not recorded — never rendered or sent as 0. */
  headCount?: number | null;
}) {
  // The pen is in the field id for the same reason it is in the form body: ten pens of one shed
  // render ten copies of this editor, and without it every copy shares one id -- so a <label
  // htmlFor> points at the first pen's input whichever pen the author opened.
  const fieldId = `exp-${shedId}-${partitionLabel}-${feedItem}`;
  return (
    <FeedConfigFormShell
      pageContract={pageContract}
      action={action}
      editLabel={copy(pageContract, "action.edit_experiment_cell")}
      openLabel={copy(pageContract, "label.experiment_absolute_kg_note")}
    >
      <input type="hidden" name="park_id" value={parkId} />
      <input type="hidden" name="shed_id" value={shedId} />
      <input type="hidden" name="partition_label" value={partitionLabel} />
      <input type="hidden" name="feed_item" value={feedItem} />
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`${fieldId}-kg`}>{copy(pageContract, "label.experiment_absolute_kg")}</label>
        <input
          id={`${fieldId}-kg`}
          name="absolute_kg"
          type="text"
          inputMode="decimal"
          // Uncontrolled: a cleared box stays cleared and is rejected server-side, never sent as 0.
          defaultValue={absoluteKg ?? ""}
          aria-describedby={`${fieldId}-kg-hint`}
        />
        <div id={`${fieldId}-kg-hint`} className="small muted" style={{ marginTop: 4 }}>
          {copy(pageContract, "reason.experiment_blank_is_not_zero")}
        </div>
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`${fieldId}-arm`}>{copy(pageContract, "label.experiment_category")}</label>
        <input
          id={`${fieldId}-arm`}
          name="experiment_category"
          type="text"
          defaultValue={experimentCategory}
          aria-describedby={`${fieldId}-arm-hint`}
        />
        <div id={`${fieldId}-arm-hint`} className="small muted" style={{ marginTop: 4 }}>
          {copy(pageContract, "label.experiment_category_note")}
        </div>
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`${fieldId}-count`}>{copy(pageContract, "label.experiment_head_count")}</label>
        <input
          id={`${fieldId}-count`}
          name="head_count"
          type="text"
          inputMode="numeric"
          // `?? ""` and not `?? 0`: a shed whose population was not recorded must not be shown as
          // empty, and must not be saved as empty either.
          defaultValue={headCount ?? ""}
          aria-describedby={`${fieldId}-count-hint`}
        />
        <div id={`${fieldId}-count-hint`} className="small muted" style={{ marginTop: 4 }}>
          {copy(pageContract, "label.experiment_head_count_note")}
        </div>
      </div>
    </FeedConfigFormShell>
  );
}

/**
 * Author a feed item this pen does NOT yet have a cell for.
 *
 * WHY THIS EXISTS AS ITS OWN CONTROL. ExperimentCellEditor edits an existing cell and
 * ExperimentPenEnroller offers only pens with no authored cell at all, so a pen that is already on
 * the experiment would otherwise have no way to gain a SIXTH feed item -- the only route was a
 * hand-written database write. The enroller covers "this pen is new"; this covers "this pen needs
 * one more item".
 *
 * It posts to the SAME single-cell upsert the editor uses. That write already inserts when no row
 * exists for (shed, pen, item), so nothing new is needed on the write path -- and because one cell
 * is one transaction, a failure here leaves the pen exactly as it was rather than half-authored.
 *
 * The item list is the catalog MINUS what the pen already has. Offering an authored item would make
 * this control a second, unlabelled way to overwrite a quantity that the row's own Edit button owns.
 * A pen holding every catalog item therefore has nothing to add, and says so rather than rendering
 * an empty select that looks broken.
 *
 * The arm and head count are prefilled from the pen and sent back, because the write authors the
 * whole row: leaving them blank would reject (the arm is required) or record "not recorded" over a
 * population the pen already had.
 */
export function ExperimentCellAdder({
  pageContract,
  action,
  parkId,
  shedId,
  partitionLabel,
  experimentCategory,
  headCount,
  availableItems,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  parkId: string;
  shedId: string;
  /** The pen gaining the item; empty for an undivided shed. */
  partitionLabel: string;
  /** Carried from the pen's existing rows so the new cell joins the same arm. */
  experimentCategory: string;
  /** Absent means the population was not recorded — never rendered or sent as 0. */
  headCount?: number | null;
  /** Catalog items with no authored cell on THIS pen. Empty means there is nothing to add. */
  availableItems: string[];
}) {
  if (availableItems.length === 0) {
    return <span className="small muted">{copy(pageContract, "empty.experiment_items_authored")}</span>;
  }
  // Pen-scoped id for the same reason ExperimentCellEditor's is: one shed renders one of these per
  // pen, and a shared id points every label at the first pen's control.
  const fieldId = `exp-add-${shedId}-${partitionLabel}`;
  return (
    <FeedConfigFormShell
      pageContract={pageContract}
      action={action}
      editLabel={copy(pageContract, "action.add_experiment_item")}
      openLabel={copy(pageContract, "label.experiment_absolute_kg_note")}
    >
      <input type="hidden" name="park_id" value={parkId} />
      <input type="hidden" name="shed_id" value={shedId} />
      <input type="hidden" name="partition_label" value={partitionLabel} />
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`${fieldId}-item`}>{copy(pageContract, "filter.feed_item_label")}</label>
        <select id={`${fieldId}-item`} name="feed_item" defaultValue="">
          {availableItems.map((item) => (
            <option key={item} value={item}>
              {item}
            </option>
          ))}
        </select>
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`${fieldId}-kg`}>{copy(pageContract, "label.experiment_absolute_kg")}</label>
        <input
          id={`${fieldId}-kg`}
          name="absolute_kg"
          type="text"
          inputMode="decimal"
          // Uncontrolled and blank: a new cell has no prior value, and a cleared box must stay
          // cleared so the server rejects it rather than authoring 0 kg for a pen nobody costed.
          defaultValue=""
          aria-describedby={`${fieldId}-kg-hint`}
        />
        <div id={`${fieldId}-kg-hint`} className="small muted" style={{ marginTop: 4 }}>
          {copy(pageContract, "reason.experiment_blank_is_not_zero")}
        </div>
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`${fieldId}-arm`}>{copy(pageContract, "label.experiment_category")}</label>
        <input
          id={`${fieldId}-arm`}
          name="experiment_category"
          type="text"
          defaultValue={experimentCategory}
          aria-describedby={`${fieldId}-arm-hint`}
        />
        <div id={`${fieldId}-arm-hint`} className="small muted" style={{ marginTop: 4 }}>
          {copy(pageContract, "label.experiment_category_note")}
        </div>
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`${fieldId}-count`}>{copy(pageContract, "label.experiment_head_count")}</label>
        <input
          id={`${fieldId}-count`}
          name="head_count"
          type="text"
          inputMode="numeric"
          defaultValue={headCount ?? ""}
          aria-describedby={`${fieldId}-count-hint`}
        />
        <div id={`${fieldId}-count-hint`} className="small muted" style={{ marginTop: 4 }}>
          {copy(pageContract, "label.experiment_head_count_note")}
        </div>
      </div>
    </FeedConfigFormShell>
  );
}

/**
 * The workflow switch: move one pen onto the experiment, or return it to the ration grid.
 *
 * Deliberately NOT a checkbox or a toggle. This control changes what a pen's animals are fed, so it
 * is a labelled button that states the direction of the change and carries the consequence in its
 * hint. A toggle would read as a
 * view preference and could be flipped by a stray click with no statement of what it did.
 *
 * The target status is a hidden literal rather than something derived at submit time, so there is no
 * code path where an unparsed value could fall through to a default — both defaults would silently
 * re-plan the pen's feed.
 */
export function ExperimentShedSwitch({
  pageContract,
  action,
  parkId,
  shedId,
  shedName,
  partitionLabel,
  targetStatus,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  parkId: string;
  shedId: string;
  /** The PEN's display name, exactly as the row above shows it ("Godel 1 - Part 3"). */
  shedName: string;
  /**
   * The raw authored pen this switch applies to; blank for an undivided shed.
   *
   * Load-bearing, not decorative: without it the write was shed-wide while this control was
   * captioned with one pen's name, so retiring "Godel 1 - Part 3" retired all ten Godel 1 pens.
   */
  partitionLabel: string;
  /** "active" enrols the pen onto absolute kg; "retired" returns it to the per-head grid. */
  targetStatus: "active" | "retired";
}) {
  const labelKey =
    targetStatus === "active" ? "action.restore_experiment_shed" : "action.withdraw_experiment_shed";
  const outcomeKey =
    targetStatus === "active" ? "label.experiment_active_note" : "label.experiment_retired_note";
  return (
    <FeedConfigFormShell
      pageContract={pageContract}
      action={action}
      editLabel={copy(pageContract, labelKey)}
      openLabel={copy(pageContract, "reason.experiment_switch_consequence")}
    >
      <input type="hidden" name="park_id" value={parkId} />
      <input type="hidden" name="shed_id" value={shedId} />
      <input type="hidden" name="partition_label" value={partitionLabel} />
      <input type="hidden" name="status" value={targetStatus} />
      {/* The pen is named back to the operator before they apply. This control is one click away
          from changing a park's feed plan, so it confirms WHICH pen and WHAT will happen — and the
          name shown is now the same scope the write touches. */}
      <div className="small" style={{ lineHeight: 1.5 }}>
        <b>{shedName}</b>
        <div className="muted">{copy(pageContract, outcomeKey)}</div>
      </div>
    </FeedConfigFormShell>
  );
}

/**
 * Remove ONE feed item from feeding, or put it back.
 *
 * The only way to remove a feed item, and deliberately a RETIRE rather than a delete. The item's
 * authored rates, shed factors and experiment cells are kept exactly as they are, so putting it back
 * restores them without re-entering anything — and every past feed sheet stays explainable. A delete
 * would take the rates with it, and a restore would then return an item whose every combination is
 * UNCONFIGURED, which on this screen means BLOCKED: those sheds would not be fed.
 *
 * It sits on the STATUS cell rather than in a trailing action column, because the status is the
 * thing being changed and is what an author looks at to decide.
 *
 * Behind the same confirm shell as every other write here, which is not ceremony: this is
 * TENANT-wide (the catalog is shared by both parks) and it changes what animals eat from the next
 * issued sheet onward, so it is the widest-reaching control on the page.
 */
export function FeedItemStatusSwitch({
  pageContract,
  action,
  feedItemId,
  feedItemLabel,
  targetStatus,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  /** The catalog row's own id. Keyed on the id, never the label, so a rename cannot misdirect it. */
  feedItemId: string;
  /** The item's name, shown back to the author before they apply. */
  feedItemLabel: string;
  /** "retired" removes it from feeding; "active" puts it back. */
  targetStatus: "active" | "retired";
}) {
  const labelKey = targetStatus === "retired" ? "action.retire_feed_item" : "action.restore_feed_item";
  const consequenceKey = targetStatus === "retired" ? "reason.retire_feed_item" : "reason.restore_feed_item";
  return (
    <FeedConfigFormShell
      pageContract={pageContract}
      action={action}
      editLabel={copy(pageContract, labelKey)}
      openLabel={copy(pageContract, consequenceKey)}
    >
      <input type="hidden" name="feed_item_id" value={feedItemId} />
      <input type="hidden" name="status" value={targetStatus} />
      {/* The item is named back before the write applies. One click from here changes what every
          park is fed, so the control states WHICH item and WHAT will happen to its rates. */}
      <div className="small" style={{ lineHeight: 1.5 }}>
        <b>{feedItemLabel}</b>
        <div className="muted">{copy(pageContract, consequenceKey)}</div>
      </div>
    </FeedConfigFormShell>
  );
}

/**
 * Enrol ONE PEN onto the experiment workflow, authoring every feed item of it in one atomic write.
 *
 * Enrolment happens through QUANTITIES, not a status flip, and that is the backend contract rather
 * than a UI choice: membership in the table is the workflow flag, so a pen cannot be "on the
 * experiment" with nothing authored. Creating empty rows to carry a status would author cells nobody
 * entered — and the pen would be enrolled while being fed nothing it was configured for.
 *
 * Three defects in the shed-level predecessor, all fixed here:
 *
 *  1. IT OFFERED SHEDS. A shed with some pens already enrolled was excluded wholesale, so a NEW pen
 *     of that shed (Godel 1 - Part 8) could not be added from this screen at all — the complaint
 *     this control exists to answer.
 *  2. IT DERIVED CANDIDATES FROM THE PAGINATED CELL LIST. A pen whose cells sat on another page
 *     read as unconfigured. Candidates now come from the pen CATALOG, which states per pen whether
 *     it is already configured, so paging cannot change the answer.
 *  3. IT AUTHORED ONE FEED ITEM. A pen is fed several; entering them one at a time could leave the
 *     pen enrolled after the first write and fed a fraction of what was intended.
 *
 * THE PARK IS CHOSEN FIRST, and the pen list is empty until it is. When the top bar reads
 * company-wide this table spans both parks, so an enroller that silently assumed one of them would
 * put a pen on the experiment in a park nobody picked.
 */
export function ExperimentPenEnroller({
  pageContract,
  action,
  parks,
  pens,
  feedItems,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  /** The parks in scope. One entry when a park is selected; both when the top bar is company-wide. */
  parks: { id: string; name: string }[];
  /** Pens with NO authored experiment cell, from the pen catalog — never from the cell page. */
  pens: { parkId: string; shedId: string; partitionLabel: string; display: string }[];
  /** The tenant's feed vocabulary, from the catalog — never a local literal list. */
  feedItems: string[];
}) {
  // Preselected when there is exactly one park, so the common single-park case is not made to click
  // a select with one option. With two, it starts empty on purpose: see the kdoc.
  const [parkId, setParkId] = useState(parks.length === 1 ? parks[0].id : "");
  const parkPens = parkId ? pens.filter((pen) => pen.parkId === parkId) : [];

  if (pens.length === 0) {
    return <div className="small muted">{copy(pageContract, "empty.experiment_candidates")}</div>;
  }
  return (
    <FeedConfigFormShell
      pageContract={pageContract}
      action={action}
      editLabel={copy(pageContract, "action.add_experiment_pen")}
      openLabel={copy(pageContract, "section.experiment.switch_note")}
    >
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor="exp-new-park">{copy(pageContract, "filter.park_label")}</label>
        <select
          id="exp-new-park"
          name="park_id"
          value={parkId}
          onChange={(event) => setParkId(event.target.value)}
          aria-describedby="exp-new-park-hint"
        >
          {/* An explicit empty option when there is a real choice to make. Defaulting to the first
              park would be the silent assumption this control exists to prevent. */}
          {parks.length > 1 ? <option value="" /> : null}
          {parks.map((park) => (
            <option key={park.id} value={park.id}>
              {park.name}
            </option>
          ))}
        </select>
        <div id="exp-new-park-hint" className="small muted" style={{ marginTop: 4 }}>
          {copy(pageContract, "reason.experiment_enrol_park")}
        </div>
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor="exp-new-pen">{copy(pageContract, "filter.pen_label")}</label>
        {/* The pen carries its shed id and its RAW partition label as one JSON value. A delimiter
            would be unsafe — a partition label is free text and may contain spaces or hyphens, so
            any separator character could occur inside it. The label travels verbatim; the backend
            normalizes and validates it against the shed's own catalog. */}
        <select id="exp-new-pen" name="pen" defaultValue="" disabled={parkPens.length === 0}>
          {parkPens.map((pen) => (
            <option
              key={`${pen.shedId}#${pen.partitionLabel}`}
              value={JSON.stringify({ s: pen.shedId, p: pen.partitionLabel })}
            >
              {pen.display}
            </option>
          ))}
        </select>
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor="exp-new-arm">{copy(pageContract, "label.experiment_category")}</label>
        <input id="exp-new-arm" name="experiment_category" type="text" defaultValue="" />
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor="exp-new-count">{copy(pageContract, "label.experiment_head_count")}</label>
        <input
          id="exp-new-count"
          name="head_count"
          type="text"
          inputMode="numeric"
          defaultValue=""
          aria-describedby="exp-new-count-hint"
        />
        <div id="exp-new-count-hint" className="small muted" style={{ marginTop: 4 }}>
          {copy(pageContract, "label.experiment_head_count_note")}
        </div>
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <div className="small" style={{ fontWeight: 600 }}>
          {copy(pageContract, "label.experiment_enrol_items")}
        </div>
        <div className="small muted" style={{ marginTop: 4, marginBottom: 8, lineHeight: 1.5 }}>
          {copy(pageContract, "label.experiment_enrol_items_note")}
        </div>
        {/* One row per catalog item, every row always rendered. The two fields are paired by their
            shared INDEX in the name, not by position in two arrays: a conditionally-rendered field
            would shift a parallel array and pair a quantity with the wrong feed item, which here
            means feeding a pen the wrong thing. */}
        {feedItems.map((item, index) => (
          <div
            key={item}
            style={{ display: "flex", gap: 8, alignItems: "center", marginBottom: 6 }}
          >
            <input type="hidden" name={`item_label_${index}`} value={item} />
            <label htmlFor={`exp-new-kg-${index}`} className="small" style={{ flex: 1 }}>
              {item}
            </label>
            <input
              id={`exp-new-kg-${index}`}
              name={`item_kg_${index}`}
              type="text"
              inputMode="decimal"
              defaultValue=""
              style={{ width: 96 }}
            />
          </div>
        ))}
      </div>
      <div className="small muted" style={{ lineHeight: 1.5 }}>
        {copy(pageContract, "section.experiment.switch_note")}
      </div>
    </FeedConfigFormShell>
  );
}

/**
 * Add a feed item to the tenant's catalog — the "Add feed type" control.
 *
 * THE ONE CONTROL ON THIS SCREEN WHERE A BLANK IS NOT A REJECTION.
 *
 * Every other editor here treats a cleared numeric box as an error, because a missing quantity is a
 * blocking state. The four attributes below are genuinely optional: a blank one is omitted from the
 * request and stored as "not measured", which is honest and consequence-free — it blocks a
 * nutritional rollup, never a feeding decision. An explicit 0 still means a measured zero. The
 * inputs stay UNCONTROLLED and `type="text"` for exactly the same reason as the rest of the file:
 * so a cleared box holds no characters rather than quietly becoming 0.
 *
 * AND IT AUTHORS NO QUANTITY. The hint under the name says so, because the expectation this control
 * invites — "I added the item, so it will be on tomorrow's sheet" — is wrong: the item becomes
 * SELECTABLE, and every combination using it stays unconfigured until a rate is authored.
 */
export function FeedItemCreator({
  pageContract,
  action,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
}) {
  return (
    // Bounded width. The shell's form is a flex ITEM of the section header, and with five fields it
    // grew to the full card width — which squeezed the section title into a two-line sliver on the
    // left and stretched every input to ~800px for values like "0.9". The cap keeps the form the
    // size of the values it collects and leaves the header readable while it is open. maxWidth
    // rather than width so it still shrinks on a narrow viewport.
    <div style={{ maxWidth: 380, width: "100%" }}>
      <FeedConfigFormShell
        pageContract={pageContract}
        action={action}
        editLabel={copy(pageContract, "action.add_feed_item")}
        openLabel={copy(pageContract, "action.add_feed_item_open")}
      >
        <div className="fld" style={{ marginBottom: 0 }}>
          <label htmlFor="feed-item-new-name">{copy(pageContract, "label.feed_item_name")}</label>
          <input
            id="feed-item-new-name"
            name="feed_item"
            type="text"
            defaultValue=""
            aria-describedby="feed-item-new-name-hint"
          />
          <div id="feed-item-new-name-hint" className="small muted" style={{ marginTop: 4 }}>
            {copy(pageContract, "label.feed_item_name_note")}
          </div>
        </div>
        {/* The four optional attributes. Each carries its own hint naming what a blank means, rather
            than relying on one note at the bottom of the form — the blank-vs-zero distinction is
            per-field, and the operator is deciding it one box at a time. */}
        <div className="fld" style={{ marginBottom: 0 }}>
          <label htmlFor="feed-item-new-energy">{copy(pageContract, "label.energy_kcal_per_kg")}</label>
          <input
            id="feed-item-new-energy"
            name="energy_kcal_per_kg"
            type="text"
            inputMode="decimal"
            defaultValue=""
            aria-describedby="feed-item-new-energy-hint"
          />
          <div id="feed-item-new-energy-hint" className="small muted" style={{ marginTop: 4 }}>
            {copy(pageContract, "label.energy_kcal_per_kg_note")}
          </div>
        </div>
        <div className="fld" style={{ marginBottom: 0 }}>
          <label htmlFor="feed-item-new-dry-matter">{copy(pageContract, "label.dry_matter_factor")}</label>
          <input
            id="feed-item-new-dry-matter"
            name="dry_matter_factor"
            type="text"
            inputMode="decimal"
            defaultValue=""
            aria-describedby="feed-item-new-dry-matter-hint"
          />
          <div id="feed-item-new-dry-matter-hint" className="small muted" style={{ marginTop: 4 }}>
            {copy(pageContract, "label.dry_matter_factor_note")}
          </div>
        </div>
        <div className="fld" style={{ marginBottom: 0 }}>
          <label htmlFor="feed-item-new-wastage">{copy(pageContract, "label.wastage_factor")}</label>
          <input
            id="feed-item-new-wastage"
            name="wastage_factor"
            type="text"
            inputMode="decimal"
            defaultValue=""
            aria-describedby="feed-item-new-wastage-hint"
          />
          <div id="feed-item-new-wastage-hint" className="small muted" style={{ marginTop: 4 }}>
            {copy(pageContract, "label.wastage_factor_note")}
          </div>
        </div>
        <div className="fld" style={{ marginBottom: 0 }}>
          <label htmlFor="feed-item-new-order">{copy(pageContract, "label.display_order")}</label>
          <input
            id="feed-item-new-order"
            name="display_order"
            type="text"
            inputMode="numeric"
            defaultValue=""
            aria-describedby="feed-item-new-order-hint"
          />
          <div id="feed-item-new-order-hint" className="small muted" style={{ marginTop: 4 }}>
            {copy(pageContract, "label.display_order_note")}
          </div>
        </div>
        {/* The consequence, stated at the point of action: this adds a NAME. */}
        <div className="small muted" style={{ lineHeight: 1.5 }}>
          {copy(pageContract, "section.feed_items.note")}
        </div>
      </FeedConfigFormShell>
    </div>
  );
}

/** A park's dispatch clock for one workflow. Wall-clock Asia/Kolkata values, no offset. */
export function ScheduleEditor({
  pageContract,
  action,
  parkId,
  workflow,
  directionTime,
  correctionTime,
  transportTime,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  parkId: string;
  workflow: string;
  directionTime: string;
  correctionTime: string;
  /** Absent means the park declared NO cutoff — unknown, never "no deadline". */
  transportTime?: string;
}) {
  return (
    <FeedConfigFormShell
      pageContract={pageContract}
      action={action}
      editLabel={copy(pageContract, "action.edit_schedule")}
      openLabel={copy(pageContract, "section.schedule.note")}
    >
      <input type="hidden" name="park_id" value={parkId} />
      <input type="hidden" name="workflow" value={workflow} />
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`direction-${parkId}-${workflow}`}>{copy(pageContract, "section.schedule.title")}</label>
        <input
          id={`direction-${parkId}-${workflow}`}
          name="direction_time"
          type="text"
          defaultValue={directionTime}
        />
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`correction-${parkId}-${workflow}`}>{copy(pageContract, "section.schedule.caption")}</label>
        <input
          id={`correction-${parkId}-${workflow}`}
          name="correction_time"
          type="text"
          defaultValue={correctionTime}
        />
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`transport-${parkId}-${workflow}`}>{copy(pageContract, "filter.effective_label")}</label>
        {/* Cleared records the explicit "no declared cutoff" the contract allows as null. It is the
            one blank on this screen that has an authored meaning, because the API models it. */}
        <input
          id={`transport-${parkId}-${workflow}`}
          name="transport_time"
          type="text"
          defaultValue={transportTime ?? ""}
        />
      </div>
    </FeedConfigFormShell>
  );
}
