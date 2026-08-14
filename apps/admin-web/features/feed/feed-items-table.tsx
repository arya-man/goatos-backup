"use client";

import { useEffect, useMemo, useRef, useState, useTransition } from "react";

import { DataTable, columnsFromContract } from "@/components/data-table";
import {
  copy,
  optionGroup,
  optionLabel,
  optionTitle,
  optionTone,
  type AdminUiPageContract,
  type AdminUiTableContract,
} from "@/lib/admin-ui-contract";
import type { FeedConfigFeedItem } from "@/lib/api/server";
import type { FeedConfigActionResult } from "./feed-config-actions";
import { type SaveAction } from "./feed-config-editor";

/** The two values `feed_item_catalog_status_check` allows. Storage vocabulary, never displayed. */
type FeedItemStatus = "active" | "retired";

const STATUS_GROUP = "feed_item_status";

/**
 * Shared cell styling for the four measured-attribute columns.
 *
 * Module-level, not built inside the component: a fresh object on every render is a new dependency
 * identity, which would rebuild the whole column model each time and make the `useMemo` below a
 * no-op.
 */
const NUMERIC_CELL = { cellClassName: "muted", cellStyle: { fontVariantNumeric: "tabular-nums" as const } };

/**
 * The status cell: a chip that becomes a picker on double-click, and applies the choice at once.
 *
 * This REPLACES a confirm-then-apply button (`FeedItemStatusSwitch`), and the tradeoff is worth
 * stating plainly rather than discovering later. That button named the item and spelled out the
 * consequence before writing, because retiring an item takes it off EVERY park's feed sheet — the
 * catalog is tenant-wide. An inline picker writes on the first change instead, so the consequence
 * now travels as the option's own `title` (the contract's per-option note, surfaced on both the
 * chip and each `<option>`) and the write stays undoable in one gesture: pick the other value back.
 * That is the mitigation, not an oversight. If a confirmation is wanted again, it belongs here as a
 * step before `apply`, not as a second control elsewhere.
 *
 * Double-click, not single: these rows sit in a scrollable table an operator drags and selects text
 * in, and a single click on a chip is far too easy to fire by accident for a write that changes what
 * every park is fed. Keyboard users get the same editor from Enter/Space on the focused cell, so the
 * gesture is not mouse-only.
 */
function FeedItemStatusCell({
  pageContract,
  action,
  feedItemId,
  feedItemLabel,
  status,
}: {
  pageContract: AdminUiPageContract;
  action: SaveAction;
  /** Keyed on the row's id, never the label, so a rename cannot misdirect the write. */
  feedItemId: string;
  feedItemLabel: string;
  status: FeedItemStatus;
}) {
  const [editing, setEditing] = useState(false);
  const [pending, startTransition] = useTransition();
  const [failure, setFailure] = useState<FeedConfigActionResult | null>(null);
  /**
   * The value shown while a write is in flight, so the chip reflects the choice immediately rather
   * than waiting for `revalidatePath` to re-render the route.
   *
   * It records the status it moved AWAY from, not just the new value, so it can retire itself by
   * DERIVATION: the moment the server prop stops being `from`, the write has landed and the prop is
   * authoritative again. Clearing it from an effect on `status` instead would be a setState inside
   * an effect — a cascading render, and one React's lint rule rejects outright.
   */
  const [optimistic, setOptimistic] = useState<{ from: FeedItemStatus; to: FeedItemStatus } | null>(null);
  const selectRef = useRef<HTMLSelectElement | null>(null);

  useEffect(() => {
    if (editing) selectRef.current?.focus();
  }, [editing]);

  const shown = optimistic && optimistic.from === status ? optimistic.to : status;
  const options = optionGroup(pageContract, STATUS_GROUP);

  function apply(next: string) {
    if (next !== "active" && next !== "retired") return;
    setEditing(false);
    if (next === shown) return; // Picking the value it already has is not a write.

    setFailure(null);
    setOptimistic({ from: status, to: next });
    const formData = new FormData();
    formData.set("feed_item_id", feedItemId);
    formData.set("status", next);
    // A fresh key per applied change. Each pick is its own intent, so a repeat of the same choice
    // must not replay as the earlier one — the same rule the authoring forms follow on success.
    formData.set("idempotency_key", crypto.randomUUID());

    startTransition(async () => {
      const outcome = await action(formData);
      // A REFUSAL must snap the chip back to what is actually stored at once — an optimistic value
      // left standing after a rejection is the screen lying about the database. A success leaves it
      // alone: `revalidatePath` is already re-rendering, and the derivation above drops it the
      // moment the new prop arrives, so clearing here would flash the old value in between.
      if (!outcome.ok) {
        setOptimistic(null);
        setFailure(outcome);
      }
    });
  }

  if (editing) {
    return (
      <select
        ref={selectRef}
        className="inp sm"
        defaultValue={shown}
        disabled={pending}
        aria-label={copy(pageContract, "hint.feed_item_status_edit")}
        onChange={(event) => apply(event.target.value)}
        // Leaving the cell without picking cancels: nothing is written until a value changes.
        onBlur={() => setEditing(false)}
        onKeyDown={(event) => {
          if (event.key === "Escape") setEditing(false);
        }}
      >
        {options.map((option) => (
          <option key={option.key} value={option.key} title={option.title || undefined}>
            {option.label}
          </option>
        ))}
      </select>
    );
  }

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 8, whiteSpace: "nowrap" }}>
      <span
        className={`tag t-${optionTone(pageContract, STATUS_GROUP, shown)} feed-item-status`}
        // Both sentences: what this state means, and that the cell can be changed.
        title={`${optionTitle(pageContract, STATUS_GROUP, shown)} ${copy(pageContract, "hint.feed_item_status_edit")}`}
        role="button"
        tabIndex={0}
        aria-label={`${feedItemLabel} ${optionLabel(pageContract, STATUS_GROUP, shown)}`}
        onDoubleClick={() => setEditing(true)}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            setEditing(true);
          }
        }}
      >
        {pending ? copy(pageContract, "state.loading") : optionLabel(pageContract, STATUS_GROUP, shown)}
      </span>
      {/* Only a REFUSAL is shown. A success needs no message here: the chip already carries the new
          state, which is the whole content of the confirmation. */}
      {failure ? (
        <span
          className="small"
          style={{ color: "var(--danger)", whiteSpace: "normal", overflowWrap: "break-word", maxWidth: 220 }}
        >
          {copy(pageContract, failure.messageKey)}
          {failure.detail ? <div className="muted">{failure.detail}</div> : null}
        </span>
      ) : null}
    </div>
  );
}

/**
 * The tenant-wide feed item catalog.
 *
 * Not park-scoped and not paginated by the page's Park filter — `feed_item_catalog` is keyed on
 * (tenant, item) and both parks author against one list, so every column sorts over the whole
 * catalog that is on screen.
 *
 * A blank attribute renders as the contract's placeholder, never as 0 and never as an empty cell.
 * Both of those read as a measured value on a table whose other columns are numbers: a 0 claims
 * someone measured none, and a blank reads as one too.
 */
export function FeedItemsTable({
  contract,
  pageContract,
  rows,
  ariaLabel,
  empty,
  statusAction,
}: {
  contract: AdminUiTableContract;
  pageContract: AdminUiPageContract;
  rows: FeedConfigFeedItem[];
  ariaLabel: string;
  empty: React.ReactNode;
  statusAction: SaveAction;
}) {
  const placeholder = copy(pageContract, "label.placeholder");

  const columns = useMemo(
    () =>
      columnsFromContract<FeedConfigFeedItem>(contract, {
        feed_item: { cell: (row) => row.feed_item, sortValue: (row) => row.feed_item },
        energy_kcal_per_kg: {
          cell: (row) => row.energy_kcal_per_kg ?? placeholder,
          // An unmeasured attribute sorts BELOW every measured one rather than as 0, which would
          // rank "nobody measured this" alongside "measured as carrying none".
          sortValue: (row) => (row.energy_kcal_per_kg == null ? -1 : Number(row.energy_kcal_per_kg)),
          meta: NUMERIC_CELL,
        },
        dry_matter_factor: {
          cell: (row) => row.dry_matter_factor ?? placeholder,
          sortValue: (row) => (row.dry_matter_factor == null ? -1 : Number(row.dry_matter_factor)),
          meta: NUMERIC_CELL,
        },
        wastage_factor: {
          cell: (row) => row.wastage_factor ?? placeholder,
          sortValue: (row) => (row.wastage_factor == null ? -1 : Number(row.wastage_factor)),
          meta: NUMERIC_CELL,
        },
        display_order: {
          cell: (row) => row.display_order,
          sortValue: (row) => row.display_order,
          meta: NUMERIC_CELL,
        },
        status: {
          cell: (row) => (
            <FeedItemStatusCell
              pageContract={pageContract}
              action={statusAction}
              feedItemId={row.feed_item_id}
              feedItemLabel={row.feed_item}
              status={row.status === "active" ? "active" : "retired"}
            />
          ),
          // Sorts on the STORED value, so the two states group together predictably regardless of
          // what the contract labels them.
          sortValue: (row) => row.status,
        },
      }),
    [contract, pageContract, statusAction, placeholder],
  );

  return (
    <DataTable
      className="feed-table"
      ariaLabel={ariaLabel}
      columns={columns}
      data={rows}
      getRowId={(row) => row.feed_item_id}
      empty={empty}
    />
  );
}
