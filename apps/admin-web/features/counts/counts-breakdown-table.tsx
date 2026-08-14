"use client";

import { useMemo } from "react";

import { DataTable, columnsFromContract } from "@/components/data-table";
import type { AdminUiPageContract, AdminUiTableContract } from "@/lib/admin-ui-contract";
import type { CountsBreakdownResponse } from "@/lib/api/server";

import { ShedTagEditor } from "./shed-tag-editor";
import type { StageOption } from "./shed-stage-actions";
import { operationalLocationLabel } from "@/lib/operational-location";

export type CountsBreakdownRow = CountsBreakdownResponse["items"][number];

/**
 * The Counts Breakdown census table.
 *
 * A client component ONLY so the operator can reorder the visible page; the rows, the filters, the
 * pager and every total still come from the server render. Sorting reorders THIS PAGE and nothing
 * else — the `<tfoot>` total is passed in already computed from the response's whole-result window
 * totals, so no ordering the operator applies can change what that number means.
 *
 * The fallback labels are props rather than `copy()` calls because they are backend-owned page copy
 * resolved during the server render; re-resolving them here would need the whole page contract on
 * the client for four strings.
 */
export function CountsBreakdownTable({
  contract,
  pageContract,
  rows,
  ariaLabel,
  empty,
  footer,
  noParkLabel,
  noStageLabel,
  noBreedLabel,
  noShedLabel,
  stages,
  retagEnabled,
  retagDisabledReason,
}: {
  contract: AdminUiTableContract;
  pageContract: AdminUiPageContract;
  rows: CountsBreakdownRow[];
  ariaLabel: string;
  empty: React.ReactNode;
  footer?: React.ReactNode;
  noParkLabel: string;
  noStageLabel: string;
  noBreedLabel: string;
  noShedLabel: string;
  stages: StageOption[];
  retagEnabled: boolean;
  retagDisabledReason: string;
}) {
  // The shed cell prefers the backend-composed `operational_location_display` and only falls back
  // to the shared helper — never a local join of name + partition.
  const shedLabel = useMemo(
    () => (row: CountsBreakdownRow) =>
      row.operational_location_display ||
      operationalLocationLabel({ shedName: row.shed_label, partitionLabel: row.partition_label }) || // operational-location:ignore: owner=ravi issue=OL-FE-HELPER scope=shared-helper-call-not-local-sql-case expiry=2026-11-30
      noShedLabel,
    [noShedLabel],
  );

  const columns = useMemo(
    () =>
      columnsFromContract<CountsBreakdownRow>(contract, {
        farm: {
          cell: (row) => row.park_label || noParkLabel,
          sortValue: (row) => row.park_label || noParkLabel,
          meta: { cellClassName: "muted" },
        },
        stage: {
          // The one EDITABLE cell: double-click retags the row's whole PEN. A row with no shed has
          // no pen to write to (the unassigned bucket), so it stays plain text -- an editor there
          // would offer to retag nothing.
          cell: (row) =>
            row.shed_id ? (
              <ShedTagEditor
                pageContract={pageContract}
                shedId={row.shed_id}
                partitionLabel={row.partition_label ?? ""}
                currentTag={row.management_stage}
                emptyLabel={noStageLabel}
                stages={stages}
                enabled={retagEnabled}
                disabledReason={retagDisabledReason}
              />
            ) : (
              row.management_stage || noStageLabel
            ),
          sortValue: (row) => row.management_stage || noStageLabel,
        },
        breed: {
          cell: (row) => row.breed || noBreedLabel,
          sortValue: (row) => row.breed || noBreedLabel,
        },
        gender: { cell: (row) => row.sex, sortValue: (row) => row.sex },
        shed: {
          cell: shedLabel,
          sortValue: shedLabel,
          meta: { cellClassName: "muted" },
        },
        count: {
          cell: (row) => row.count,
          // Numeric, so it sorts by magnitude rather than by "10" < "9" string order.
          sortValue: (row) => row.count,
          meta: { align: "right", cellStyle: { fontWeight: 700, color: "var(--brand-d)" } },
        },
      }),
    [contract, pageContract, noParkLabel, noStageLabel, noBreedLabel, shedLabel, stages, retagEnabled, retagDisabledReason],
  );

  return (
    <DataTable
      className="counts-breakdown-table"
      ariaLabel={ariaLabel}
      columns={columns}
      data={rows}
      // The full grain key. Dropping any part of it collapses two genuinely different census rows
      // into one React identity — and shed_id alone is not the ground location when a pen exists.
      getRowId={(row) =>
        `${row.park_id ?? ""}|${row.shed_id ?? ""}|${row.partition_label ?? ""}|${row.management_stage}|${row.breed}|${row.sex}`
      }
      empty={empty}
      footer={footer}
    />
  );
}
