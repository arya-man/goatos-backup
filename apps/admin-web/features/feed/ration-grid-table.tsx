"use client";

import { createContext, useContext, useMemo } from "react";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";

import { DataTable, columnsFromContract } from "@/components/data-table";
import { fmtDate } from "@/lib/format";
import { copy, type AdminUiPageContract, type AdminUiTableContract } from "@/lib/admin-ui-contract";
import type { FeedConfigRationRate } from "@/lib/api/server";
import { RationRateEditor, type SaveAction } from "./feed-config-editor";
import { RationRateValue } from "./feed-rate-optimistic";
import { stageLabel } from "@/lib/stage-labels";
import { EmptyState } from "@/components/app/empty-state";
import { Label } from "@/components/minimal/label";

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
    <Stack spacing={0.5} sx={{ alignItems: "flex-start" }}>
      <Label
        variant="soft"
        color={open ? "success" : "default"}
        title={copy(pageContract, open ? "label.effective_open_note" : "label.effective_closed_note")}
      >
        {copy(pageContract, open ? "label.effective_open" : "label.effective_closed")}
      </Label>
      <Typography variant="caption" sx={{ color: "text.secondary" }}>
        {fmtDate(validFrom)}
        {validTo ? ` · ${fmtDate(validTo)}` : ""}
      </Typography>
    </Stack>
  );
}

// The page copy and the save action the cells need, handed down by CONTEXT rather than captured in
// the column definitions.
//
// THE DEFECT THIS CLOSES: TanStack's flexRender renders a column's `cell` function AS A COMPONENT, so
// a new function is a new component type and React remounts the cell. Every server action re-renders
// this route with fresh props (the write marker cookie alone does that), and a column model rebuilt
// from them remounted every Edit rate cell -- an open editor lost the number being typed and the
// "Rate rejected" message two seconds after it appeared. The column model is now keyed on what
// actually shapes it (the table contract and the edit header), and the cells read the rest from here.
const RationGridCellContext = createContext<{ pageContract: AdminUiPageContract; action: SaveAction } | null>(null);

function useCellContext() {
  const value = useContext(RationGridCellContext);
  if (!value) throw new Error("ration grid cell rendered outside RationGridTable");
  return value;
}

function RateValueCell({ row }: { row: FeedConfigRationRate }) {
  const { pageContract } = useCellContext();
  return (
    <RationRateValue
      pageContract={pageContract}
      parkId={row.park_id}
      rationGroup={row.ration_group}
      shedTag={row.shed_tag}
      feedItem={row.feed_item}
      gramsPerHead={row.grams_per_head}
    />
  );
}

function EffectiveWindowCell({ row }: { row: FeedConfigRationRate }) {
  const { pageContract } = useCellContext();
  return <EffectiveWindow validFrom={row.valid_from} validTo={row.valid_to} pageContract={pageContract} />;
}

function EditRateCell({ row }: { row: FeedConfigRationRate }) {
  const { pageContract, action } = useCellContext();
  return (
    <RationRateEditor
      pageContract={pageContract}
      action={action}
      parkId={row.park_id}
      rationGroup={row.ration_group}
      shedTag={row.shed_tag}
      feedItem={row.feed_item}
      gramsPerHead={row.grams_per_head}
    />
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
  const editHeader = copy(pageContract, "action.edit_rate");
  // Keyed on CONTENT: every re-render hands this a new `contract` object with the same columns.
  const contractKey = JSON.stringify(contract);
  const columns = useMemo(() => {
    const dataColumns = columnsFromContract<FeedConfigRationRate>(JSON.parse(contractKey) as AdminUiTableContract, {
      ration_group: { cell: (row) => row.ration_group, sortValue: (row) => row.ration_group },
      shed_tag: {
        cell: (row) => stageLabel(row.shed_tag),
        sortValue: (row) => row.shed_tag,
        meta: { cellStyle: { color: "var(--palette-text-secondary)" } },
      },
      feed_item: {
        // A name may wrap inside its cell but never widen the table: the feed tables are
        // `nowrap` by default, and one long name pushed the quantity and edit columns of EVERY
        // row off the screen. The catalog now bounds new names at 80 characters; this holds the
        // grid together for a name authored before that bound existed.
        cell: (row) => (
          <Box component="span" sx={{ display: "inline-block", maxWidth: 260, whiteSpace: "normal", overflowWrap: "anywhere" }}>
            {row.feed_item}
          </Box>
        ),
        sortValue: (row) => row.feed_item,
      },
      grams_per_head: {
        // A client cell so a just-saved quantity appears at once (see RationRateValue).
        cell: (row) => <RateValueCell row={row} />,
        // Sorts on the AUTHORED number, not on the optimistic display: an unsaved local edit must
        // not silently reorder the grid under the author's cursor.
        // Numerically: as strings "12.5" sorted before "12.25".
        sortValue: (row) => (row.grams_per_head === undefined || row.grams_per_head === null ? -1 : Number(row.grams_per_head)),
      },
      valid_from: {
        cell: (row) => <EffectiveWindowCell row={row} />,
        meta: { colSpan: 2 },
      },
      valid_to: { cell: () => null, meta: { spanned: true } },
    });

    return [
      ...dataColumns,
      {
        id: "edit_rate",
        header: editHeader,
        enableSorting: false,
        cell: ({ row }: { row: { original: FeedConfigRationRate } }) => <EditRateCell row={row.original} />,
      },
    ];
  }, [contractKey, editHeader]);
  const labelFor = (key: string) => contract.columns.find((column) => column.key === key)?.label ?? key;

  const cellContext = useMemo(() => ({ pageContract, action }), [pageContract, action]);

  return (
    <RationGridCellContext.Provider value={cellContext}>
      {/* Phone: one row card per rate; md+: the contract table. Breakpoint display via sx, so the
          DataTable's own styles cannot override a stylesheet hide (AUDIT1 P0-6). */}
      <Box aria-label={ariaLabel} sx={{ display: { xs: "grid", md: "none" } }}>
        {rows.length === 0 ? (
          typeof empty === "string" ? <EmptyState title={empty} filled /> : empty
        ) : (
          rows.map((row) => (
            <Box
              component="article"
              key={row.ration_rate_id}
              sx={{
                display: "grid",
                gridTemplateColumns: "minmax(0, 1.15fr) minmax(112px, 0.85fr) 44px",
                gap: 1.25,
                alignItems: "center",
                px: 2,
                py: 1.75,
                borderBottom: 1,
                borderColor: "divider",
                "&:nth-of-type(even)": { bgcolor: "background.neutral" },
              }}
            >
              <Stack spacing={0.5} sx={{ minWidth: 0 }}>
                <Typography variant="overline" sx={{ color: "text.disabled", lineHeight: 1.1 }}>
                  {labelFor("feed_item")}
                </Typography>
                <Typography variant="subtitle2" sx={{ overflowWrap: "anywhere" }}>
                  {row.feed_item}
                </Typography>
                <Typography variant="caption" sx={{ color: "text.secondary", overflowWrap: "anywhere" }}>
                  {row.ration_group} · {stageLabel(row.shed_tag)}
                </Typography>
              </Stack>
              <Stack spacing={0.5} sx={{ minWidth: 0, alignItems: "flex-start", typography: "subtitle2" }}>
                <Typography variant="overline" sx={{ color: "text.disabled", lineHeight: 1.1 }}>
                  {labelFor("grams_per_head")}
                </Typography>
                <RationRateValue
                  pageContract={pageContract}
                  parkId={row.park_id}
                  rationGroup={row.ration_group}
                  shedTag={row.shed_tag}
                  feedItem={row.feed_item}
                  gramsPerHead={row.grams_per_head}
                />
              </Stack>
              <Box
                aria-label={copy(pageContract, "action.edit_rate")}
                sx={{ display: "flex", justifyContent: "flex-end", alignItems: "center" }}
              >
                <RationRateEditor
                  pageContract={pageContract}
                  action={action}
                  parkId={row.park_id}
                  rationGroup={row.ration_group}
                  shedTag={row.shed_tag}
                  feedItem={row.feed_item}
                  gramsPerHead={row.grams_per_head}
                />
              </Box>
            </Box>
          ))
        )}
      </Box>
      <Box sx={{ display: { xs: "none", md: "block" } }}>
        <DataTable
          ariaLabel={ariaLabel}
          columns={columns}
          data={rows}
          getRowId={(row) => row.ration_rate_id}
          empty={empty}
        />
      </Box>
    </RationGridCellContext.Provider>
  );
}
