"use client";

import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { DataTable, columnsFromContract } from "@/components/data-table";
import { STICKY_FIRST_COLUMN_SX } from "@/components/app/table";
import { Tag } from "@/components/ui-primitives";
import { cohortWord } from "./fcr-labels";
import type { AdminUiTableContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import type { GrowthFCRPen } from "@/lib/api/server";

/**
 * The FCR tab's pen table on TanStack, columns from the page's `fcr-pens` contract so a column the
 * backend drops never renders. Every figure is the backend's; this file divides nothing. Absent
 * figures (a pen weighed once, a pen with no feed rows, a pen that did not gain) render the contract's
 * dash rather than 0 -- a zero FCR would claim a pen that converts feed for free.
 */
export type FCRPensTableLabels = {
  ariaLabel: string;
  noValue: string;
  mixedBreed: string;
  mixedSex: string;
  unknown: string;
  /** The page's own Male / Female words: the backend sends the register's lower-case sex key. */
  male: string;
  female: string;
  wholePen: string;
  scanned: string;
  status: { ok: string; blocked: string; weighed_once: string; no_feed: string; no_gain: string };
  unpriced: string;
  wasted: string;
  rupee: string;
  empty: React.ReactNode;
};

/** A second line under a figure (the pen's weighing modes, the wasted / unpriced kilograms). */
function SubLine({ children }: { children: React.ReactNode }) {
  return (
    <Typography component="span" variant="caption" sx={{ display: "block", color: "text.secondary" }}>
      {children}
    </Typography>
  );
}

const num = (value: number, digits = 1) =>
  value.toLocaleString("en-IN", { minimumFractionDigits: digits, maximumFractionDigits: digits });


export function FCRPensTable({
  contract,
  rows,
  labels,
}: {
  contract: AdminUiTableContract;
  rows: GrowthFCRPen[];
  labels: FCRPensTableLabels;
}) {
  const absent = <Box component="span" sx={{ color: "text.secondary" }}>{labels.noValue}</Box>;
  const money = (value: number | null | undefined) =>
    value == null ? absent : `${labels.rupee}${num(value, 0)}`;
  const columns = columnsFromContract<GrowthFCRPen>(contract, {
    pen: {
      cell: (row) => (
        <span>
          <b>{row.operational_location_display}</b>
          <SubLine>
            {row.weighing_modes.map((mode) => (mode === "individual_animal" ? labels.scanned : labels.wholePen)).join(" · ")}
          </SubLine>
        </span>
      ),
    },
    cohort: {
      cell: (row) => (
        <Box component="span" sx={{ display: "inline-flex", gap: 0.5, flexWrap: "wrap" }}>
          <Tag tone={row.breed === "mixed" ? "info" : "mut"}>{cohortWord(row.breed, labels, "breed")}</Tag>
          <Tag tone={row.sex === "mixed" ? "info" : "mut"}>{cohortWord(row.sex, labels, "sex")}</Tag>
        </Box>
      ),
    },
    animals: { cell: (row) => row.animals.toLocaleString("en-IN"), meta: { align: "right" }, sortValue: (row) => row.animals },
    weighed: {
      cell: (row) =>
        row.rounds < 2 ? (
          <Box component="span" sx={{ color: "text.secondary" }}>{fmtDate(row.last_weigh_date)}</Box>
        ) : (
          // First and last weigh on two lines: on one line the span was the table's widest cell and
          // pushed fifteen columns ~250px past a 1440 card (PR #294 W7).
          <span>
            {fmtDate(row.first_weigh_date)} →
            <Box component="span" sx={{ display: "block" }}>
              {fmtDate(row.last_weigh_date)}
            </Box>
          </span>
        ),
    },
    daily_gain: {
      cell: (row) => (row.adg_g_per_day == null ? absent : `${Math.round(row.adg_g_per_day).toLocaleString("en-IN")} g`),
      meta: { align: "right" },
      sortValue: (row) => row.adg_g_per_day ?? undefined,
    },
    head_days: {
      cell: (row) => (row.head_days == null ? absent : num(row.head_days, 0)),
      meta: { align: "right" },
      sortValue: (row) => row.head_days ?? undefined,
    },
    gain_kg: {
      cell: (row) => (row.gain_kg == null ? absent : num(row.gain_kg)),
      meta: { align: "right" },
      sortValue: (row) => row.gain_kg ?? undefined,
    },
    feed_kg: {
      // Feed is what the pen ATE: the sheet's kilograms less the leftover the verifier weighed.
      // The wasted kilograms are printed under it so the reader can see what was taken off.
      cell: (row) =>
        row.feed_kg == null ? (
          absent
        ) : (
          <span>
            {num(row.feed_kg)}
            {row.wastage_kg != null && row.wastage_kg > 0 ? (
              <SubLine>
                {num(row.wastage_kg)} {labels.wasted}
              </SubLine>
            ) : null}
          </span>
        ),
      meta: { align: "right" },
      sortValue: (row) => row.feed_kg ?? undefined,
    },
    fcr: {
      cell: (row) => (row.fcr == null ? absent : <b>{num(row.fcr, 2)}</b>),
      meta: { align: "right" },
      sortValue: (row) => row.fcr ?? undefined,
    },
    feed_cost: {
      cell: (row) => (
        <span>
          {money(row.feed_cost_inr)}
          {row.unpriced_feed_kg > 0 ? (
            <SubLine>
              {num(row.unpriced_feed_kg, 0)} {labels.unpriced}
            </SubLine>
          ) : null}
        </span>
      ),
      meta: { align: "right" },
      sortValue: (row) => row.feed_cost_inr ?? undefined,
    },
    gain_value: {
      cell: (row) => money(row.gain_value_inr),
      meta: { align: "right" },
      sortValue: (row) => row.gain_value_inr ?? undefined,
    },
    margin: {
      cell: (row) =>
        row.margin_inr == null ? absent : <b>{money(row.margin_inr)}</b>,
      meta: { align: "right" },
      sortValue: (row) => row.margin_inr ?? undefined,
    },
    feed_cost_per_kg_gain: {
      cell: (row) => money(row.feed_cost_per_kg_gain_inr),
      meta: { align: "right" },
      sortValue: (row) => row.feed_cost_per_kg_gain_inr ?? undefined,
    },
    feed_sheet: {
      cell: (row) => {
        if (row.status !== "ok") {
          return <Tag tone="mut">{labels.status[row.status]}</Tag>;
        }
        if (row.blocked_cells > 0) {
          return (
            <Tag tone="warn">
              {row.blocked_cells} {labels.status.blocked}
            </Tag>
          );
        }
        return <Tag tone="ok">{labels.status.ok}</Tag>;
      },
    },
  });

  return (
    // Twelve figures per pen: cells hold one line (no mid-word breaks), the table scrolls sideways
    // inside its card with the pen name pinned.
    <Box sx={{ "& .MuiTableCell-root": { whiteSpace: "nowrap", overflowWrap: "normal", wordBreak: "normal" }, ...STICKY_FIRST_COLUMN_SX }}>
      <DataTable<GrowthFCRPen>
        columns={columns}
        data={rows}
        getRowId={(row) => `${row.location_id}|${row.partition_label}`}
        ariaLabel={labels.ariaLabel}
        empty={labels.empty}
      />
    </Box>
  );
}
