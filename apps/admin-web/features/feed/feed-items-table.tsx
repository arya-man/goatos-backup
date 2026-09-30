"use client";

import { createContext, useContext, useEffect, useMemo, useRef, useState, useTransition } from "react";

import { DataTable, columnsFromContract } from "@/components/data-table";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { Label } from "@/components/minimal/label";
import { TONE_COLOR, type Tone } from "@/components/ui-primitives";
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
  // The MUI TextField select renders a role="combobox" element, so the editor is focused by reaching into the
  // wrapper for it rather than by holding a ref to a native <select>.
  const editorRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (editing) editorRef.current?.querySelector<HTMLElement>('[role="combobox"]')?.focus();
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
      <div
        ref={editorRef}
        // Leaving the cell without picking cancels: nothing is written until a value changes.
        // `relatedTarget` inside the wrapper means focus only moved between the trigger and its
        // listbox, which is not a cancel.
        onBlur={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setEditing(false);
        }}
        onKeyDown={(event) => {
          // The field itself consumes Escape to close its menu; this closes the editor when the
          // menu is already shut.
          if (event.key === "Escape" && event.target === event.currentTarget.querySelector("button")) {
            setEditing(false);
          }
        }}
      >
        <TextField
          select
          label={copy(pageContract, "hint.feed_item_status_edit")}
          value={shown}
          disabled={pending}
          title={optionTitle(pageContract, STATUS_GROUP, shown) || undefined}
          onChange={(event) => apply(event.target.value)}
          sx={{ minWidth: { xs: 0, sm: 148 }, flexShrink: 0, maxWidth: 1 }}
          slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
        >
          {options.map((option) => (
            <MenuItem key={option.key} value={option.key}>
              {option.label}
            </MenuItem>
          ))}
        </TextField>
      </div>
    );
  }

  return (
    <Box sx={{ display: "flex", alignItems: "center", gap: 1, whiteSpace: "nowrap" }}>
      <Label
        variant="soft"
        color={TONE_COLOR[optionTone(pageContract, STATUS_GROUP, shown) as Tone] ?? "default"}
        sx={{
          cursor: "pointer",
          textDecoration: "underline dashed",
          textUnderlineOffset: 3,
          "&:focus-visible": { outline: "2px solid", outlineColor: "primary.main", outlineOffset: 2 },
        }}
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
      </Label>
      {/* Only a REFUSAL is shown. A success needs no message here: the chip already carries the new
          state, which is the whole content of the confirmation. */}
      {failure ? (
        <Typography component="span" variant="body2" sx={{ color: "error.main", whiteSpace: "normal", overflowWrap: "break-word", maxWidth: 220 }}>
          {copy(pageContract, failure.messageKey)}
          {failure.detail ? (
            <Box component="span" sx={{ display: "block", color: "text.secondary" }}>
              {failure.detail}
            </Box>
          ) : null}
        </Typography>
      ) : null}
    </Box>
  );
}

const FeedItemCellContext = createContext<{ pageContract: AdminUiPageContract; statusAction: SaveAction } | null>(null);

function StatusCellFromContext({ row }: { row: FeedConfigFeedItem }) {
  const value = useContext(FeedItemCellContext);
  if (!value) throw new Error("feed item status cell rendered outside FeedItemsTable");
  return (
    <FeedItemStatusCell
      pageContract={value.pageContract}
      action={value.statusAction}
      feedItemId={row.feed_item_id}
      feedItemLabel={row.feed_item}
      status={row.status === "active" ? "active" : "retired"}
    />
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
  // Keyed on CONTENT, not on the props' identity: every server action re-renders this route with a
  // new `contract`/`pageContract`, and TanStack renders a cell function AS A COMPONENT, so a rebuilt
  // column model remounted the status cell and threw away a half-done confirm. The status cell reads
  // the live copy and action from context instead (see RationGridTable for the same fix).
  const contractKey = JSON.stringify(contract);

  const columns = useMemo(
    () =>
      columnsFromContract<FeedConfigFeedItem>(JSON.parse(contractKey) as AdminUiTableContract, {
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
          cell: (row) => <StatusCellFromContext row={row} />,
          // Sorts on the STORED value, so the two states group together predictably regardless of
          // what the contract labels them.
          sortValue: (row) => row.status,
        },
      }),
    [contractKey, placeholder],
  );
  const cellContext = useMemo(() => ({ pageContract, statusAction }), [pageContract, statusAction]);

  return (
    <FeedItemCellContext.Provider value={cellContext}>
      <DataTable
        ariaLabel={ariaLabel}
        columns={columns}
        data={rows}
        getRowId={(row) => row.feed_item_id}
        empty={empty}
      />
    </FeedItemCellContext.Provider>
  );
}
