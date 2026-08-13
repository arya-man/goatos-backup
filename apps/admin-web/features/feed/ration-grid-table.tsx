"use client";

import { useMemo } from "react";

import { DataTable, columnsFromContract } from "@/components/data-table";
import { copy, type AdminUiPageContract, type AdminUiTableContract } from "@/lib/admin-ui-contract";
import type { FeedConfigRationRate } from "@/lib/api/server";
import { RationRateEditor, type SaveAction } from "./feed-config-editor";
import { RationRateValue } from "./feed-rate-optimistic";

/**
 * In-force (`valid_to` absent) vs superseded by a later edit.
 *
 * Lives here rather than on the page because the row is now rendered on the client. It still shows
 * only backend-owned copy — `copy()` reads the same compiled page contract, which already crosses
 * this boundary for the rate value and the editor.
 */
function EffectiveWindow({
  validFrom,
  validTo,
  pageContract,
}: {
  validFrom: string;
  validTo?: string | null;
  pageContract: AdminUiPageContract;
}) {
  const open = !validTo;
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 3 }}>
      <span
        className={open ? "tag t-ok" : "tag t-mut"}
        title={copy(pageContract, open ? "label.effective_open_note" : "label.effective_closed_note")}
      >
        {copy(pageContract, open ? "label.effective_open" : "label.effective_closed")}
      </span>
      <span className="muted" style={{ fontSize: 11 }}>
        {validFrom}
        {validTo ? ` · ${validTo}` : ""}
      </span>
    </div>
  );
}

/**
 * The authored ration grid: g/head/day per (park, ration group, shed tag, feed item).
 *
 * Server-paginated by `fc_offset`/`fc_limit` exactly as before, so TanStack sorts only the rows
 * already on screen and never slices or refetches. Only the four value columns carry a sort
 * affordance — the contract marks `valid_from`/`valid_to` unsortable because they render as ONE
 * merged effective-window cell, reproduced here with `colSpan`/`spanned` so the header still shows
 * both contract labels.
 *
 * There is NO park column: every `/feed-config/*` read requires `park_id` and this page shares one
 * Park filter, so it would print the same value on every row.
 *
 * The edit column is appended AFTER the contract's columns rather than declared in it: it is an
 * action affordance, not a data column, and the backend contract declares its label separately as
 * `action.edit_rate`.
 */
export function RationGridTable({
  contract,
  pageContract,
  rows,
  ariaLabel,
  empty,
  action,
}: {
  contract: AdminUiTableContract;
  pageContract: AdminUiPageContract;
  rows: FeedConfigRationRate[];
  ariaLabel: string;
  empty: React.ReactNode;
  action: SaveAction;
}) {
  const columns = useMemo(() => {
    const dataColumns = columnsFromContract<FeedConfigRationRate>(contract, {
      ration_group: { cell: (row) => row.ration_group, sortValue: (row) => row.ration_group },
      shed_tag: {
        cell: (row) => row.shed_tag,
        sortValue: (row) => row.shed_tag,
        meta: { cellClassName: "muted" },
      },
      feed_item: { cell: (row) => row.feed_item, sortValue: (row) => row.feed_item },
      grams_per_head: {
        // A client cell so a just-saved quantity appears at once: saving writes in ~0.3s but the
        // number only lands when revalidatePath re-renders the route, and until then the cell
        // showed the OLD figure beside a form that had closed on success — which reads as
        // "nothing happened" on a screen whose numbers are feeding instructions.
        cell: (row) => (
          <RationRateValue
            pageContract={pageContract}
            parkId={row.park_id}
            rationGroup={row.ration_group}
            shedTag={row.shed_tag}
            feedItem={row.feed_item}
            gramsPerHead={row.grams_per_head}
          />
        ),
        // Sorts on the AUTHORED number, not on the optimistic display: an unsaved local edit must
        // not silently reorder the grid under the author's cursor.
        sortValue: (row) => row.grams_per_head ?? -1,
      },
      valid_from: {
        cell: (row) => (
          <EffectiveWindow validFrom={row.valid_from} validTo={row.valid_to} pageContract={pageContract} />
        ),
        meta: { colSpan: 2 },
      },
      valid_to: { cell: () => null, meta: { spanned: true } },
    });

    return [
      ...dataColumns,
      {
        id: "edit_rate",
        header: copy(pageContract, "action.edit_rate"),
        enableSorting: false,
        cell: ({ row }: { row: { original: FeedConfigRationRate } }) => (
          <RationRateEditor
            pageContract={pageContract}
            action={action}
            parkId={row.original.park_id}
            rationGroup={row.original.ration_group}
            shedTag={row.original.shed_tag}
            feedItem={row.original.feed_item}
            gramsPerHead={row.original.grams_per_head}
          />
        ),
      },
    ];
  }, [contract, pageContract, action]);

  return (
    <DataTable
      className="feed-table"
      ariaLabel={ariaLabel}
      columns={columns}
      data={rows}
      getRowId={(row) => row.ration_rate_id}
      empty={empty}
    />
  );
}
