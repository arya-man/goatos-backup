"use client";

import { DataTable, columnsFromContract } from "@/components/data-table";
import { Tag } from "@/components/ui-primitives";
import type { AdminUiTableContract } from "@/lib/admin-ui-contract";
import type { BuyerAnalyticsRow } from "@/lib/api/procurement";
import { humanDate, inr, num } from "./sales-format";

/** Backend copy handed down by the server component; this file names no label of its own. */
export type BuyerTableLabels = {
  ariaLabel: string;
  none: string;
  repeat: string;
  oneTime: string;
  settled: string;
  empty: React.ReactNode;
};

/**
 * A served buyer row plus the two backend-templated lines the server component fills in
 * (functions cannot cross into a client component, so the sentences travel as data).
 */
export type BuyerTableRow = BuyerAnalyticsRow & {
  /** Short lines under the repeat chip: "2 more after the first", "about every 17 days". */
  cadence_lines: string[];
  /** "7 days ago" / "today" under the last sale, or "" when unknown. */
  recency: string;
};

/**
 * The buyers table on TanStack (maintainer request 2026-09-16, the pens-table shape). Columns and
 * their sort affordances come from the page's `sales-buyer-analytics` contract; the phone column
 * is dropped here when the payload withheld phones, so the header and the body agree. Sorting
 * reorders the served page only -- the window is the backend's, which is why the pager stays
 * outside -- and it opens on the backend's own order: newest last sale first.
 */
export function BuyerTable({
  contract,
  rows,
  showPhones,
  labels,
}: {
  contract: AdminUiTableContract;
  rows: BuyerTableRow[];
  showPhones: boolean;
  labels: BuyerTableLabels;
}) {
  const visibleContract: AdminUiTableContract = {
    ...contract,
    // Older contracts declared these as columns; this screen already renders them as
    // buyer/revenue cell detail. Keep rollback compatibility without hiding unknown keys.
    columns: contract.columns.filter((column) =>
      !["category", "place", "share_pct"].includes(column.key),
    ).map((column) =>
      column.key === "phone_number"
        ? { ...column, visible: column.visible && showPhones }
        : column,
    ),
  };
  const columns = columnsFromContract<BuyerTableRow>(visibleContract, {
    buyer_name: {
      cell: (row) => (
        <>
          <b>{row.buyer_name}</b>
          <div className="muted small">
            {[row.category, row.place].filter(Boolean).join(" · ") ||
              labels.none}
          </div>
        </>
      ),
      sortValue: (row) => row.buyer_name.toLocaleLowerCase(),
    },
    phone_number: {
      cell: (row) => row.phone_number || labels.none,
    },
    purchases: {
      cell: (row) => (
        <>
          <b>{num(row.purchases)}</b>
          {row.product_types.length > 0 ? (
            <div className="muted small">{row.product_types.join(" · ")}</div>
          ) : null}
        </>
      ),
      meta: { cellClassName: "num" },
      sortValue: (row) => row.purchases,
    },
    animals: {
      cell: (row) => num(row.animals),
      meta: { cellClassName: "num" },
      sortValue: (row) => row.animals,
    },
    revenue: {
      cell: (row) => (
        <>
          {inr(row.revenue)}
          <div className="muted small">{num(row.share_pct, 1)}%</div>
        </>
      ),
      meta: { cellClassName: "num" },
      sortValue: (row) => row.revenue,
    },
    repeat: {
      cell: (row) => (
        <>
          <Tag tone={row.repeat ? "ok" : "mut"}>
            {row.repeat ? labels.repeat : labels.oneTime}
          </Tag>
          {row.cadence_lines.map((line) => (
            <div className="muted small" key={line}>
              {line}
            </div>
          ))}
        </>
      ),
      // Ordered by how many times they came back, so "Repeat" buyers group above "One-time".
      sortValue: (row) => row.repeat_purchases,
    },
    first_sale_date: {
      // humanDate on the CELL only; the ISO string sorts correctly as text where dd/mm/yyyy would not.
      cell: (row) => humanDate(row.first_sale_date),
      sortValue: (row) => row.first_sale_date,
    },
    last_sale_date: {
      cell: (row) => (
        <>
          {humanDate(row.last_sale_date)}
          {row.recency ? (
            <div className="muted small">{row.recency}</div>
          ) : null}
        </>
      ),
      sortValue: (row) => row.last_sale_date,
    },
    outstanding: {
      cell: (row) =>
        row.outstanding > 0 ? (
          inr(row.outstanding)
        ) : (
          <span className="muted">{labels.settled}</span>
        ),
      meta: { cellClassName: "num" },
      sortValue: (row) => row.outstanding,
    },
  });

  return (
    <DataTable<BuyerTableRow>
      columns={columns}
      data={rows}
      getRowId={(row) => row.buyer_key}
      ariaLabel={labels.ariaLabel}
      empty={labels.empty}
      initialSorting={[{ id: "last_sale_date", desc: true }]}
    />
  );
}
