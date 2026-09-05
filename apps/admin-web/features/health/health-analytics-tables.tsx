"use client";

import { DataTable, columnsFromContract } from "@/components/data-table";
import { Tag } from "@/components/ui-primitives";
import type { AdminUiTableContract } from "@/lib/admin-ui-contract";

/**
 * The four Health Analytics tables. Every column comes from the page's own table contract, so a
 * column the backend drops never renders here and this file names no column label of its own.
 *
 * Rows arrive RESOLVED from the server component: figures are numbers so the table can sort
 * them, and every piece of copy is backend text handed down. Nothing below composes a sentence.
 */

const nf = (value: number) => value.toLocaleString("en-IN");
const pct = (value: number) => `${value.toLocaleString("en-IN", { maximumFractionDigits: 1 })}%`;

export type DiseaseRow = {
  key: string;
  label: string;
  ageBandLabel: string;
  newCases: number;
  openCases: number;
  recovered: number;
  died: number;
  caseFatalityPct: number;
};

export function DiseaseBoardTable({
  contract,
  rows,
  ariaLabel,
  empty,
}: {
  contract: AdminUiTableContract;
  rows: DiseaseRow[];
  ariaLabel: string;
  empty: React.ReactNode;
}) {
  const columns = columnsFromContract<DiseaseRow>(contract, {
    disease: {
      cell: (row) => (
        <div>
          <b>{row.label}</b>
          <div className="muted small mono">{row.key}</div>
        </div>
      ),
      sortValue: (row) => row.label,
    },
    age_band: { cell: (row) => <Tag tone="mut">{row.ageBandLabel}</Tag>, sortValue: (row) => row.ageBandLabel },
    new_cases: { cell: (row) => nf(row.newCases), meta: { align: "right" }, sortValue: (row) => row.newCases },
    open_cases: { cell: (row) => nf(row.openCases), meta: { align: "right" }, sortValue: (row) => row.openCases },
    recovered: { cell: (row) => nf(row.recovered), meta: { align: "right" }, sortValue: (row) => row.recovered },
    died: { cell: (row) => nf(row.died), meta: { align: "right" }, sortValue: (row) => row.died },
    case_fatality: {
      // Tone follows the RATE, not the row's rank: a disease nothing died of stays green
      // however it is sorted.
      cell: (row) => (
        <Tag tone={row.died === 0 ? "ok" : row.caseFatalityPct >= 15 ? "dng" : "warn"}>
          {pct(row.caseFatalityPct)}
        </Tag>
      ),
      meta: { align: "right" },
      sortValue: (row) => row.caseFatalityPct,
    },
  });

  return (
    <DataTable
      columns={columns}
      data={rows}
      getRowId={(row) => row.key}
      ariaLabel={ariaLabel}
      empty={empty}
    />
  );
}

export type DeathRow = {
  goatId: string;
  animalLabel: string;
  displayId: string;
  pen: string;
  /** Already DD-MM-YYYY. The ISO value rides on `sortDate` so the column still orders by date. */
  date: string;
  sortDate: string;
  ageBandLabel: string;
  attributionLabel: string;
  attributed: boolean;
  diseaseLabel: string;
  daysUnderTreatment: number | null;
};

export function DeathsTable({
  contract,
  rows,
  ariaLabel,
  empty,
  noDataLabel,
}: {
  contract: AdminUiTableContract;
  rows: DeathRow[];
  ariaLabel: string;
  empty: React.ReactNode;
  /** Backend copy for a cell with nothing to show. Never composed here. */
  noDataLabel: string;
}) {
  const columns = columnsFromContract<DeathRow>(contract, {
    animal: {
      cell: (row) => (
        <div>
          <span className="mono">{row.animalLabel}</span>
          <div className="muted small">{row.displayId}</div>
        </div>
      ),
      sortValue: (row) => row.animalLabel,
    },
    pen: { cell: (row) => row.pen || <span className="muted">{noDataLabel}</span>, sortValue: (row) => row.pen },
    // Sorted on the ISO value, never the rendered DD-MM-YYYY, which orders by day-of-month.
    date: { cell: (row) => row.date, sortValue: (row) => row.sortDate },
    age_band: { cell: (row) => row.ageBandLabel, sortValue: (row) => row.ageBandLabel },
    attribution: {
      // An UNATTRIBUTED death shows the chip and NOTHING else. Goat OS records no cause of
      // death, so a disease name must never appear on a row that had no case open.
      cell: (row) =>
        row.attributed ? (
          <div>
            <Tag tone="teal">{row.diseaseLabel}</Tag>
          </div>
        ) : (
          <Tag tone="mut">{row.attributionLabel}</Tag>
        ),
      sortValue: (row) => (row.attributed ? row.diseaseLabel : row.attributionLabel),
    },
    days_treated: {
      cell: (row) => (row.daysUnderTreatment === null ? <span className="muted">—</span> : nf(row.daysUnderTreatment)),
      meta: { align: "right" },
      sortValue: (row) => row.daysUnderTreatment ?? undefined,
    },
  });

  return (
    <DataTable
      columns={columns}
      data={rows}
      getRowId={(row) => row.goatId}
      ariaLabel={ariaLabel}
      empty={empty}
    />
  );
}

export type MedicineRow = {
  key: string;
  name: string;
  route: string;
  doses: number;
  animals: number;
};

export function MedicinesTable({
  contract,
  rows,
  ariaLabel,
  empty,
}: {
  contract: AdminUiTableContract;
  rows: MedicineRow[];
  ariaLabel: string;
  empty: React.ReactNode;
}) {
  const columns = columnsFromContract<MedicineRow>(contract, {
    medicine: { cell: (row) => <b>{row.name}</b>, sortValue: (row) => row.name },
    route: { cell: (row) => row.route, sortValue: (row) => row.route },
    doses: { cell: (row) => nf(row.doses), meta: { align: "right" }, sortValue: (row) => row.doses },
    animals: { cell: (row) => nf(row.animals), meta: { align: "right" }, sortValue: (row) => row.animals },
  });

  return (
    <DataTable
      columns={columns}
      data={rows}
      getRowId={(row) => row.key}
      ariaLabel={ariaLabel}
      empty={empty}
    />
  );
}

export type EngineRuleRow = {
  key: string;
  proposed: number;
  opened: number;
  notTakenUpPct: number;
};

export function EngineRulesTable({
  contract,
  rows,
  ariaLabel,
  empty,
}: {
  contract: AdminUiTableContract;
  rows: EngineRuleRow[];
  ariaLabel: string;
  empty: React.ReactNode;
}) {
  const columns = columnsFromContract<EngineRuleRow>(contract, {
    rule: { cell: (row) => <span className="mono">{row.key}</span>, sortValue: (row) => row.key },
    proposed: { cell: (row) => nf(row.proposed), meta: { align: "right" }, sortValue: (row) => row.proposed },
    opened: { cell: (row) => nf(row.opened), meta: { align: "right" }, sortValue: (row) => row.opened },
    not_taken_up: {
      cell: (row) => (
        <Tag tone={row.notTakenUpPct >= 30 ? "dng" : row.notTakenUpPct > 0 ? "warn" : "ok"}>
          {pct(row.notTakenUpPct)}
        </Tag>
      ),
      meta: { align: "right" },
      sortValue: (row) => row.notTakenUpPct,
    },
  });

  return (
    <DataTable
      columns={columns}
      data={rows}
      getRowId={(row) => row.key}
      ariaLabel={ariaLabel}
      empty={empty}
    />
  );
}
