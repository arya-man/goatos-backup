"use client";

import { fmtDate } from "@/lib/format";
import Box from "@mui/material/Box";
import { DataTable, columnsFromContract } from "@/components/data-table";
import { Tag } from "@/components/ui-primitives";
import type { AdminUiTableContract } from "@/lib/admin-ui-contract";

/**
 * One pen of the Weights analytics General tab, already RESOLVED by the server component: the
 * figures are numbers so the table can sort them, and every label is backend copy handed down.
 * The gain is nullable on purpose -- a pen weighed once in the window has no gain, and it must
 * read as "no data" rather than 0 g/day (which would claim the pen stopped growing).
 */
export type PensTableRow = {
  key: string;
  park: string;
  pen: string;
  /** The pen's resident breed, or the backend's "Mixed breeds" word. Null = nothing to name. */
  breed: string | null;
  weighingCategory: string;
  animals: number;
  averageKg: number;
  gainGPerDay: number | null;
  totalKg: number;
  lastWeighed: string | null;
};

export type PensTableLabels = {
  ariaLabel: string;
  individual: string;
  lump: string;
  noData: string;
  neverWeighed: string;
  empty: React.ReactNode;
};

const kg = (value: number, digits = 1) =>
  value.toLocaleString("en-IN", { minimumFractionDigits: digits, maximumFractionDigits: digits });

/**
 * The pens table on TanStack (maintainer request 2026-09-03). Columns come from the page's
 * `shed-weights` contract, so a column the backend drops never renders and a column the backend
 * marks sortable gets its sort toggle; this file names no column label of its own. Sorting on
 * average weight and daily gain is client-side over the rows the server already served -- the
 * page window is still the backend's, which is why the pager stays outside.
 */
export function PensTable({
  contract,
  rows,
  labels,
}: {
  contract: AdminUiTableContract;
  rows: PensTableRow[];
  labels: PensTableLabels;
}) {
  const columns = columnsFromContract<PensTableRow>(contract, {
    park: { cell: (row) => row.park },
    shed: { cell: (row) => <Box component="b" sx={{ fontWeight: 600, color: "text.primary" }}>{row.pen}</Box> },
    // A pen holding more than one breed reads "Mixed breeds" (backend copy) and is never split
    // across them: one pen average cannot be divided between two cohorts.
    breed: {
      cell: (row) =>
        row.breed == null ? <Box component="span" sx={{ color: "text.secondary" }}>{labels.noData}</Box> : <span>{row.breed}</span>,
      sortValue: (row) => row.breed ?? undefined,
    },
    weighing: {
      cell: (row) => (
        <Tag tone={row.weighingCategory === "individual_animal" ? "info" : "mut"}>
          {row.weighingCategory === "individual_animal" ? labels.individual : labels.lump}
        </Tag>
      ),
    },
    animals_weighed: {
      cell: (row) => row.animals.toLocaleString("en-IN"),
      meta: { align: "right" },
      sortValue: (row) => row.animals,
    },
    average_weight: {
      cell: (row) => (
        <Box component="span" sx={{ fontVariantNumeric: "tabular-nums", fontWeight: 600 }}>
          {kg(row.averageKg)} <Box component="span" sx={{ color: "text.secondary" }}>kg</Box>
        </Box>
      ),
      meta: { align: "right" },
      sortValue: (row) => row.averageKg,
    },
    daily_gain: {
      cell: (row) =>
        row.gainGPerDay == null ? (
          <Box component="span" sx={{ color: "text.secondary" }}>{labels.noData}</Box>
        ) : (
          <Tag tone={row.gainGPerDay < 0 ? "dng" : row.gainGPerDay === 0 ? "mut" : "ok"}>
            {row.gainGPerDay > 0 ? "+" : ""}
            {Math.round(row.gainGPerDay).toLocaleString("en-IN")} g
          </Tag>
        ),
      meta: { align: "right" },
      // A pen with no gain sorts below every measured one in either direction (the table's
      // sortUndefined: "last"): it is absent, not slow, and must never sit between a 30 g and
      // a 40 g pen as if it were 0.
      sortValue: (row) => row.gainGPerDay ?? undefined,
    },
    total_weight: {
      cell: (row) => `${kg(row.totalKg, 0)} kg`,
      meta: { align: "right" },
      sortValue: (row) => row.totalKg,
    },
    last_weighed: {
      // fmtDate on the CELL only: sortValue below keeps the raw ISO string, which sorts
      // correctly as text where "08/09/2026" would not.
      cell: (row) => (row.lastWeighed ? fmtDate(row.lastWeighed) : <Box component="span" sx={{ color: "text.secondary" }}>{labels.neverWeighed}</Box>),
      meta: { align: "right" },
      sortValue: (row) => row.lastWeighed ?? undefined,
    },
  });

  return (
    <DataTable<PensTableRow>
      columns={columns}
      data={rows}
      getRowId={(row) => row.key}
      ariaLabel={labels.ariaLabel}
      empty={labels.empty}
    />
  );
}
