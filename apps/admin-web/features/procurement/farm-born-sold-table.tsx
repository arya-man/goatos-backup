"use client";

import { DataTable, columnsFromContract } from "@/components/data-table";
import { useUrlSort } from "@/components/use-url-sort";
import type { AdminUiTableContract } from "@/lib/admin-ui-contract";
import type { FarmBornSoldRow } from "@/lib/api/procurement";
import { humanDate, inr } from "./sales-format";
import type { TableOrder } from "./table-order";

/** Backend copy handed down by the server component; this file names no label of its own. */
export type FarmBornSoldTableLabels = {
  ariaLabel: string;
  notRecorded: string;
  noDeal: string;
  /**
   * The farm's own name for each stored gender code (the page's farm_born_sexes group, compiled from
   * Configuration). A plain record because it crosses the server/client boundary.
   */
  sexLabels: Record<string, string>;
  empty: React.ReactNode;
  /** Screen-reader suffix for a sortable header ("sort all rows"). */
  sortAll: string;
};

/**
 * The sold-animal ledger on TanStack (the buyers-table shape). Columns and their sort
 * affordances come from the page's `sales-farm-born-sold` contract. Sorting reorders the served
 * page only -- the window is the backend's, which is why the pager stays outside -- and it opens
 * on the backend's own order: newest sale first.
 */
export function FarmBornSoldTable({
  contract,
  rows,
  labels,
  order,
}: {
  contract: AdminUiTableContract;
  rows: FarmBornSoldRow[];
  labels: FarmBornSoldTableLabels;
  /** The order the rows were served in; a header click asks the backend for a new one. */
  order: TableOrder;
}) {
  // WHOLE-RESULT sorting over every sold animal in the filter (2026-09-25), paged from its first row.
  const serverSort = useUrlSort({
    sort: order.sort,
    dir: order.dir,
    defaultSort: { id: "sale_date", desc: true },
    pageParams: ["offset"],
  });
  const orDash = (value: string) =>
    value ? value : <span className="muted">{labels.notRecorded}</span>;
  const columns = columnsFromContract<FarmBornSoldRow>(contract, {
    tag: {
      cell: (row) => (
        <>
          <b>{row.tag || row.display_id}</b>
          {row.tag ? <div className="muted small">{row.display_id}</div> : null}
        </>
      ),
      sortValue: (row) => (row.tag || row.display_id).toLocaleLowerCase(),
    },
    breed: {
      cell: (row) => orDash(row.breed),
      sortValue: (row) => row.breed.toLocaleLowerCase() || undefined,
    },
    sex: {
      cell: (row) =>
        row.sex ? (labels.sexLabels[row.sex] ?? row.sex) : orDash(""),
      sortValue: (row) => row.sex,
    },
    stage: {
      cell: (row) => orDash(row.stage),
      sortValue: (row) => row.stage.toLocaleLowerCase() || undefined,
    },
    pen: {
      cell: (row) => (
        <>
          {orDash(row.pen)}
          {row.park_name ? <div className="muted small">{row.park_name}</div> : null}
        </>
      ),
      sortValue: (row) => `${row.park_name} ${row.pen}`.toLocaleLowerCase(),
    },
    sale_date: {
      // humanDate on the CELL only; the ISO string sorts correctly as text where dd/mm/yyyy would not.
      cell: (row) => humanDate(row.sale_date),
      sortValue: (row) => row.sale_date,
    },
    buyer_name: {
      cell: (row) => orDash(row.buyer_name),
      sortValue: (row) => row.buyer_name.toLocaleLowerCase() || undefined,
    },
    sale_value: {
      cell: (row) =>
        row.sale_value != null ? inr(row.sale_value) : <span className="muted">{labels.noDeal}</span>,
      meta: { cellClassName: "num" },
      sortValue: (row) => row.sale_value ?? undefined,
    },
  });
  return (
    <DataTable
      columns={columns}
      data={rows}
      getRowId={(row) => row.goat_id}
      ariaLabel={labels.ariaLabel}
      className="tbl"
      empty={labels.empty}
      serverSort={{ ...serverSort, sortLabel: labels.sortAll }}
    />
  );
}
