"use client";

import { useState, useTransition } from "react";
import { Pencil } from "lucide-react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedConfigActionResult } from "./feed-config-actions";
import { afterSubmit, CLOSED_STATE, openIntent, type AuthoringIdempotencyState } from "@/lib/authoring-idempotency";

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

function FeedConfigFormShell({
  pageContract,
  action,
  children,
  editLabel,
  openLabel,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  children: React.ReactNode;
  editLabel: string;
  openLabel: string;
}) {
  const [pending, startTransition] = useTransition();
  const [result, setResult] = useState<FeedConfigActionResult | null>(null);
  // The idempotency-key lifecycle (mint on open, reuse across retries, rotate only after a
  // confirmed success) is a pure state machine in lib/authoring-idempotency.ts, tested there
  // directly so the retry/replay behavior does not require mounting this component.
  const [idem, setIdem] = useState<AuthoringIdempotencyState>(CLOSED_STATE);

  function handleOpen() {
    setResult(null);
    setIdem(openIntent(() => crypto.randomUUID()));
  }

  function onSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formData = new FormData(event.currentTarget);
    startTransition(async () => {
      const outcome = await action(formData);
      setResult(outcome);
      setIdem((prev) => afterSubmit(prev, outcome.ok, () => crypto.randomUUID()));
    });
  }

  if (!idem.open) {
    return (
      <button type="button" className="btn sm" onClick={handleOpen} title={openLabel}>
        <Pencil className="ic" aria-hidden="true" />
        {editLabel}
      </button>
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
      {/* The blank-is-not-zero rejection is the important one to show: it is the message that tells
          an operator that clearing a field leaves the combination BLOCKED, and that feeding nothing
          requires typing an explicit 0. Every message resolves through the page contract. */}
      {result ? (
        <div className={result.ok ? "small" : "small"} style={{ color: result.ok ? "var(--brand-d)" : "var(--danger)" }}>
          {copy(pageContract, result.messageKey)}
          {result.detail ? <div className="muted">{result.detail}</div> : null}
        </div>
      ) : null}
    </form>
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
 * The workflow switch: move a whole shed onto the experiment, or return it to the ration grid.
 *
 * Deliberately NOT a checkbox or a toggle. This control changes what a shed's animals are fed, so it
 * is a labelled button that states the direction of the change ("Move shed to experiment" /
 * "Return shed to normal grid") and carries the consequence in its hint. A toggle would read as a
 * view preference and could be flipped by a stray click with no statement of what it did.
 *
 * The target status is a hidden literal rather than something derived at submit time, so there is no
 * code path where an unparsed value could fall through to a default — both defaults would silently
 * re-plan the shed's feed.
 */
export function ExperimentShedSwitch({
  pageContract,
  action,
  parkId,
  shedId,
  shedName,
  targetStatus,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  parkId: string;
  shedId: string;
  shedName: string;
  /** "active" enrols the shed onto absolute kg; "retired" returns it to the per-head grid. */
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
      <input type="hidden" name="status" value={targetStatus} />
      {/* The shed is named back to the operator before they apply. This control is one click away
          from changing a park's feed plan, so it confirms WHICH shed and WHAT will happen. */}
      <div className="small" style={{ lineHeight: 1.5 }}>
        <b>{shedName}</b>
        <div className="muted">{copy(pageContract, outcomeKey)}</div>
      </div>
    </FeedConfigFormShell>
  );
}

/**
 * Enrol a shed that has no experiment rows yet, by authoring its first cell.
 *
 * Enrolment happens through a QUANTITY, not a status flip, and that is the backend contract rather
 * than a UI choice: membership in the table is the workflow flag, so a shed cannot be "on the
 * experiment" with nothing authored. Creating empty rows to carry a status would author cells nobody
 * entered — and the shed would be enrolled while being fed nothing it was configured for.
 */
export function ExperimentShedEnroller({
  pageContract,
  action,
  parkId,
  sheds,
  feedItems,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  parkId: string;
  /** Sheds in this park with no experiment rows at all. */
  sheds: { id: string; name: string }[];
  /** The tenant's feed vocabulary, from the catalog — never a local literal list. */
  feedItems: string[];
}) {
  if (sheds.length === 0) {
    return <div className="small muted">{copy(pageContract, "empty.experiment_candidates")}</div>;
  }
  return (
    <FeedConfigFormShell
      pageContract={pageContract}
      action={action}
      editLabel={copy(pageContract, "action.add_experiment_shed")}
      openLabel={copy(pageContract, "section.experiment.switch_note")}
    >
      <input type="hidden" name="park_id" value={parkId} />
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`exp-new-shed-${parkId}`}>{copy(pageContract, "filter.shed_label")}</label>
        <select id={`exp-new-shed-${parkId}`} name="shed_id" defaultValue="">
          {sheds.map((shed) => (
            <option key={shed.id} value={shed.id}>
              {shed.name}
            </option>
          ))}
        </select>
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`exp-new-item-${parkId}`}>{copy(pageContract, "filter.feed_item_label")}</label>
        <select id={`exp-new-item-${parkId}`} name="feed_item" defaultValue="">
          {feedItems.map((item) => (
            <option key={item} value={item}>
              {item}
            </option>
          ))}
        </select>
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`exp-new-arm-${parkId}`}>{copy(pageContract, "label.experiment_category")}</label>
        <input id={`exp-new-arm-${parkId}`} name="experiment_category" type="text" defaultValue="" />
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`exp-new-kg-${parkId}`}>{copy(pageContract, "label.experiment_absolute_kg")}</label>
        <input
          id={`exp-new-kg-${parkId}`}
          name="absolute_kg"
          type="text"
          inputMode="decimal"
          defaultValue=""
          aria-describedby={`exp-new-kg-hint-${parkId}`}
        />
        <div id={`exp-new-kg-hint-${parkId}`} className="small muted" style={{ marginTop: 4 }}>
          {copy(pageContract, "label.experiment_absolute_kg_note")}
        </div>
      </div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={`exp-new-count-${parkId}`}>{copy(pageContract, "label.experiment_head_count")}</label>
        <input
          id={`exp-new-count-${parkId}`}
          name="head_count"
          type="text"
          inputMode="numeric"
          defaultValue=""
          aria-describedby={`exp-new-count-hint-${parkId}`}
        />
        <div id={`exp-new-count-hint-${parkId}`} className="small muted" style={{ marginTop: 4 }}>
          {copy(pageContract, "label.experiment_head_count_note")}
        </div>
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
