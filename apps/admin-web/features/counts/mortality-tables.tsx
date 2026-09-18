"use client";

import { DataTable, columnsFromContract } from "@/components/data-table";
import { Tag } from "@/components/ui-primitives";
import type { AdminUiTableContract } from "@/lib/admin-ui-contract";

/**
 * The Mortality page's recent-deaths table. Every column comes from the page's own table
 * contract, so a column the backend drops never renders here and this file names no column
 * label of its own. Rows arrive RESOLVED from the server component: every piece of copy is
 * backend text handed down, and nothing below composes a sentence.
 */

export type RecentDeathRow = {
  goatId: string;
  /** Already DD-MM-YYYY; the ISO value rides on sortDate so the column orders by date. */
  diedOn: string;
  sortDate: string;
  tag: string;
  displayId: string;
  breed: string;
  sex: string;
  stage: string;
  ageDays: number | null;
  ageBandLabel: string;
  park: string;
  pen: string;
  loadRef: string;
  causeLabel: string;
  causeBasis: "recorded" | "inferred" | "none";
  basisLabel: string;
};

export function RecentDeathsTable({
  contract,
  rows,
  ariaLabel,
  empty,
  noDataLabel,
  daysSuffix,
}: {
  contract: AdminUiTableContract;
  rows: RecentDeathRow[];
  ariaLabel: string;
  empty: React.ReactNode;
  noDataLabel: string;
  daysSuffix: string;
}) {
  const muted = <span className="muted">{noDataLabel}</span>;
  const columns = columnsFromContract<RecentDeathRow>(contract, {
    died_on: { cell: (row) => row.diedOn, sortValue: (row) => row.sortDate },
    tag: {
      cell: (row) => (
        <div>
          <span className="mono">{row.tag || row.displayId}</span>
          {row.tag ? <div className="muted small">{row.displayId}</div> : null}
        </div>
      ),
      sortValue: (row) => row.tag || row.displayId,
    },
    breed: { cell: (row) => row.breed || muted, sortValue: (row) => row.breed },
    sex: { cell: (row) => row.sex || muted, sortValue: (row) => row.sex },
    stage: { cell: (row) => row.stage || muted, sortValue: (row) => row.stage },
    age_at_death: {
      cell: (row) =>
        row.ageDays == null ? (
          <span className="muted">{row.ageBandLabel}</span>
        ) : (
          <div>
            <span>{`${row.ageDays.toLocaleString("en-IN")} ${daysSuffix}`}</span>
            <div className="muted small">{row.ageBandLabel}</div>
          </div>
        ),
      meta: { align: "right" },
      sortValue: (row) => row.ageDays ?? undefined,
    },
    farm: { cell: (row) => row.park || muted, sortValue: (row) => row.park },
    shed: { cell: (row) => row.pen || muted, sortValue: (row) => row.pen },
    load: { cell: (row) => row.loadRef || muted, sortValue: (row) => row.loadRef },
    cause: {
      cell: (row) => (
        <div>
          {/* A RECORDED cause reads plainly; an INFERRED one is marked, because it says only
              that a case was open when the animal died — co-incidence, not causation, and the
              reader is owed the difference. A death with neither shows the no-cause chip alone
              and never a disease. */}
          <Tag tone={row.causeBasis === "recorded" ? "teal" : row.causeBasis === "inferred" ? "info" : "mut"}>{row.causeLabel}</Tag>
          {row.causeBasis === "inferred" ? <div className="muted small">{row.basisLabel}</div> : null}
        </div>
      ),
      sortValue: (row) => row.causeLabel,
    },
  });

  return (
    <div className="health-analytics-scroll" tabIndex={0} role="region" aria-label={ariaLabel}>
      <DataTable
        columns={columns}
        data={rows}
        getRowId={(row) => row.goatId}
        ariaLabel={ariaLabel}
        className="health-analytics-table"
        empty={empty}
      />
    </div>
  );
}
