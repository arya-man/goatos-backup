"use client";

import { DataTable, columnsFromContract } from "@/components/data-table";
import { Tag } from "@/components/ui-primitives";
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
  mixed: string;
  unknown: string;
  wholePen: string;
  scanned: string;
  status: { ok: string; blocked: string; weighed_once: string; no_feed: string; no_gain: string };
  unpriced: string;
  rupee: string;
  empty: React.ReactNode;
};

const num = (value: number, digits = 1) =>
  value.toLocaleString("en-IN", { minimumFractionDigits: digits, maximumFractionDigits: digits });

function cohortWord(value: string, labels: FCRPensTableLabels): string {
  if (value === "mixed") return labels.mixed;
  if (value === "unknown" || value === "") return labels.unknown;
  return value;
}

export function FCRPensTable({
  contract,
  rows,
  labels,
}: {
  contract: AdminUiTableContract;
  rows: GrowthFCRPen[];
  labels: FCRPensTableLabels;
}) {
  const absent = <span className="muted">{labels.noValue}</span>;
  const money = (value: number | null | undefined) =>
    value == null ? absent : `${labels.rupee}${num(value, 0)}`;
  const columns = columnsFromContract<GrowthFCRPen>(contract, {
    pen: {
      cell: (row) => (
        <span>
          <b>{row.operational_location_display}</b>
          <span className="muted small" style={{ display: "block" }}>
            {row.weighing_modes.map((mode) => (mode === "individual_animal" ? labels.scanned : labels.wholePen)).join(" · ")}
          </span>
        </span>
      ),
    },
    cohort: {
      cell: (row) => (
        <span style={{ display: "inline-flex", gap: 4, flexWrap: "wrap" }}>
          <Tag tone={row.breed === "mixed" ? "info" : "mut"}>{cohortWord(row.breed, labels)}</Tag>
          <Tag tone={row.sex === "mixed" ? "info" : "mut"}>{cohortWord(row.sex, labels)}</Tag>
        </span>
      ),
    },
    animals: { cell: (row) => row.animals.toLocaleString("en-IN"), meta: { cellClassName: "num" }, sortValue: (row) => row.animals },
    weighed: {
      cell: (row) =>
        row.rounds < 2 ? (
          <span className="muted">{fmtDate(row.last_weigh_date)}</span>
        ) : (
          `${fmtDate(row.first_weigh_date)} → ${fmtDate(row.last_weigh_date)}`
        ),
    },
    daily_gain: {
      cell: (row) => (row.adg_g_per_day == null ? absent : `${Math.round(row.adg_g_per_day).toLocaleString("en-IN")} g`),
      meta: { cellClassName: "num" },
      sortValue: (row) => row.adg_g_per_day ?? undefined,
    },
    head_days: {
      cell: (row) => (row.head_days == null ? absent : num(row.head_days, 0)),
      meta: { cellClassName: "num" },
      sortValue: (row) => row.head_days ?? undefined,
    },
    gain_kg: {
      cell: (row) => (row.gain_kg == null ? absent : num(row.gain_kg)),
      meta: { cellClassName: "num" },
      sortValue: (row) => row.gain_kg ?? undefined,
    },
    feed_kg: {
      cell: (row) => (row.feed_kg == null ? absent : num(row.feed_kg)),
      meta: { cellClassName: "num" },
      sortValue: (row) => row.feed_kg ?? undefined,
    },
    fcr: {
      cell: (row) => (row.fcr == null ? absent : <b>{num(row.fcr, 2)}</b>),
      meta: { cellClassName: "num" },
      sortValue: (row) => row.fcr ?? undefined,
    },
    feed_cost: {
      cell: (row) => (
        <span>
          {money(row.feed_cost_inr)}
          {row.unpriced_feed_kg > 0 ? (
            <span className="muted small" style={{ display: "block" }}>
              {num(row.unpriced_feed_kg, 0)} {labels.unpriced}
            </span>
          ) : null}
        </span>
      ),
      meta: { cellClassName: "num" },
      sortValue: (row) => row.feed_cost_inr ?? undefined,
    },
    gain_value: {
      cell: (row) => money(row.gain_value_inr),
      meta: { cellClassName: "num" },
      sortValue: (row) => row.gain_value_inr ?? undefined,
    },
    margin: {
      cell: (row) =>
        row.margin_inr == null ? absent : <b className={row.margin_inr < 0 ? "neg" : undefined}>{money(row.margin_inr)}</b>,
      meta: { cellClassName: "num" },
      sortValue: (row) => row.margin_inr ?? undefined,
    },
    feed_cost_per_kg_gain: {
      cell: (row) => money(row.feed_cost_per_kg_gain_inr),
      meta: { cellClassName: "num" },
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
    <DataTable<GrowthFCRPen>
      className="tbl loadwise-table"
      columns={columns}
      data={rows}
      getRowId={(row) => `${row.location_id}|${row.partition_label}`}
      ariaLabel={labels.ariaLabel}
      empty={labels.empty}
    />
  );
}
