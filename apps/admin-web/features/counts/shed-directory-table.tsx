"use client";

import { useMemo } from "react";

import { DataTable, columnsFromContract } from "@/components/data-table";
import type { AdminUiTableContract, AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ShedDirectoryResponse } from "@/lib/api/server";

import { ShedTagEditor } from "./shed-tag-editor";
import type { StageOption } from "./shed-stage-drawer";

export type ShedDirectoryRow = ShedDirectoryResponse["items"][number];

/**
 * The Sheds directory table: one row per operational location (a pen, or a shed with no pens), one
 * Tag + Capacity column pair per park.
 *
 * The park columns are NOT declared in this file. The backend compiles them into the page contract
 * from the live park rows, keyed `tag:<park_id>` / `capacity:<park_id>`, and this component builds
 * a cell renderer for whatever keys the contract shipped. That is what lets a third park appear
 * with no frontend change, and it is why no park code is written here — the header text is the
 * contract's, and the values are looked up by the park id the key carries.
 *
 * Three states have to stay visually distinct, because they are three different facts:
 *
 *   - the park has no shed of this name at all  -> `notInParkLabel` ("—")
 *   - the shed exists but has no configured tag -> `noTagLabel`
 *   - the shed exists but no capacity is on record -> `noCapacityLabel`, NOT "0"
 *
 * A recorded capacity of 0 is a real answer and renders as 0.
 */
export function ShedDirectoryTable({
  contract,
  pageContract,
  rows,
  ariaLabel,
  empty,
  noTagLabel,
  noCapacityLabel,
  notInParkLabel,
  stages,
  retagEnabled,
  retagDisabledReason,
}: {
  contract: AdminUiTableContract;
  pageContract: AdminUiPageContract;
  rows: ShedDirectoryRow[];
  ariaLabel: string;
  empty: React.ReactNode;
  noTagLabel: string;
  noCapacityLabel: string;
  notInParkLabel: string;
  stages: StageOption[];
  retagEnabled: boolean;
  retagDisabledReason: string;
}) {
  const columns = useMemo(() => {
    const cells: Parameters<typeof columnsFromContract<ShedDirectoryRow>>[1] = {
      shed: {
        // The backend-composed display ("Godel 1 - Part 3", bare "Q1"), rendered verbatim. Never a
        // local join of name + pen: that is the hand-rolled composition that shipped "Godel 1 1".
        cell: (row) => row.operational_location_display,
        sortValue: (row) => row.operational_location_display,
        // NOT `.celllink`: nothing opens from this cell, and that class is a block-level link
        // affordance whose own padding pushes the name out of line with the rest of the row.
        meta: { cellStyle: { fontWeight: 600 } },
      },
    };

    for (const column of contract.columns) {
      const [kind, parkId] = column.key.split(":");
      if (!parkId) continue;

      if (kind === "tag") {
        cells[column.key] = {
          // The tag is the one EDITABLE cell on this screen. A park that has no location of this
          // label has nothing to edit, so it stays a plain dash -- an editor there would offer to
          // retag a pen that does not exist.
          cell: (row) => {
            const cell = row.cells[parkId];
            if (!cell) return <span className="muted">{notInParkLabel}</span>;
            return (
              <ShedTagEditor
                pageContract={pageContract}
                shedId={cell.shed_id}
                partitionLabel={row.partition_label}
                currentTag={cell.tag}
                emptyLabel={noTagLabel}
                stages={stages}
                enabled={retagEnabled}
                disabledReason={retagDisabledReason}
              />
            );
          },
        };
      }

      if (kind === "capacity") {
        cells[column.key] = {
          // A null capacity is "nobody recorded it" and must not read as a number. `?? ` rather
          // than a falsy check on purpose: 0 is a recorded answer and has to survive.
          cell: (row) => {
            const cell = row.cells[parkId];
            if (!cell) return <span className="muted">{notInParkLabel}</span>;
            return cell.capacity === null || cell.capacity === undefined ? (
              <span className="muted small">{noCapacityLabel}</span>
            ) : (
              cell.capacity
            );
          },
          meta: { align: "right", cellStyle: { fontWeight: 700, color: "var(--brand-d)" } },
        };
      }
    }

    return columnsFromContract<ShedDirectoryRow>(contract, cells);
  }, [contract, pageContract, noTagLabel, noCapacityLabel, notInParkLabel, stages, retagEnabled, retagDisabledReason]);

  return (
    <DataTable
      className="shed-directory-table"
      ariaLabel={ariaLabel}
      columns={columns}
      data={rows}
      // The operational-location LABEL is the row identity here, and only here: pairing the two
      // parks is what this screen is for, and every cell still carries its own park's shed_id
      // underneath. Shed name alone would merge a shed's ten pens into one React identity.
      getRowId={(row) => row.operational_location_display}
      empty={empty}
    />
  );
}
