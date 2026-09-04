"use client";

import { useMemo } from "react";

import { DataTable, columnsFromContract } from "@/components/data-table";
import type { AdminUiPageContract, AdminUiTableContract } from "@/lib/admin-ui-contract";
import type { CountsBreakdownResponse } from "@/lib/api/server";

import { CensusValueEditor, type CensusSlice } from "./census-value-editor";
import type { InlineChoice } from "./inline-cell-editor";
import { ShedTagEditor } from "./shed-tag-editor";
import type { StageOption } from "./shed-stage-actions";
import { operationalLocationLabel } from "@/lib/operational-location";

export type CountsBreakdownRow = CountsBreakdownResponse["items"][number];

// sliceOf names the row the way the correction write matches it. Every field participates in the
// predicate, so this must stay a faithful copy of the row rather than a convenient subset.
function sliceOf(row: CountsBreakdownRow): CensusSlice {
  return {
    shedId: row.shed_id ?? "",
    partitionLabel: row.partition_label ?? "",
    managementStage: row.management_stage,
    breed: row.breed,
    sex: row.sex === "male" ? "male" : "female",
  };
}

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
  breeds,
  genders,
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
  breeds: InlineChoice[];
  genders: InlineChoice[];
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

  const genderLabel = useMemo(() => {
    const labels = new Map(genders.map((choice) => [choice.value, choice.label] as const));
    return (value: string) => labels.get(value) ?? value;
  }, [genders]);

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
          // Editable, and scoped to THIS ROW's animals -- unlike the Stage cell beside it, which
          // moves the whole pen. A row with no shed (the unassigned bucket) has no slice to write
          // to and stays plain text.
          cell: (row) =>
            row.shed_id ? (
              <CensusValueEditor
                pageContract={pageContract}
                slice={sliceOf(row)}
                field="breed"
                current={row.breed}
                emptyLabel={noBreedLabel}
                choices={breeds}
                enabled={retagEnabled}
                disabledReason={retagDisabledReason}
              />
            ) : (
              row.breed || noBreedLabel
            ),
          sortValue: (row) => row.breed || noBreedLabel,
        },
        gender: {
          // The stored token ("female") reads as the contract's own gender label ("Female"), the
          // same word the filter beside the table and the pen line above it use. The editor still
          // sends the token verbatim; only the rendering changes.
          cell: (row) =>
            row.shed_id ? (
              <CensusValueEditor
                pageContract={pageContract}
                slice={sliceOf(row)}
                field="sex"
                current={row.sex}
                emptyLabel={noBreedLabel}
                choices={genders}
                enabled={retagEnabled}
                disabledReason={retagDisabledReason}
                renderCurrent={genderLabel}
              />
            ) : (
              genderLabel(row.sex)
            ),
          sortValue: (row) => row.sex,
        },
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
    [
      contract,
      pageContract,
      noParkLabel,
      noStageLabel,
      noBreedLabel,
      shedLabel,
      stages,
      breeds,
      genders,
      genderLabel,
      retagEnabled,
      retagDisabledReason,
    ],
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
