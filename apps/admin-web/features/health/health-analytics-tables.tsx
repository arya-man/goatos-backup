"use client";

import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import type { SxProps, Theme } from "@mui/material/styles";

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

/**
 * The table region: the DataTable already scrolls in the template Scrollbar, so this only names the
 * region and gives the wide table its floor (was `.health-analytics-scroll` / `.health-analytics-table`
 * in mesha-theme.css): one line per cell, the lead and attribution columns wrap.
 */
const TABLE_REGION_SX: SxProps<Theme> = {
  minWidth: 0,
  maxWidth: 1,
  "& table": { width: 1, minWidth: { xs: 680, sm: 760 } },
  "& th, & td": { whiteSpace: "nowrap", verticalAlign: "top" },
  "& td:first-of-type, & td:nth-of-type(5)": { whiteSpace: "normal", overflowWrap: "anywhere" },
};

/** Machine keys (disease / rule codes) in the template monospace caption. */
const MONO_SX = { fontFamily: "monospace" } as const;

function TableRegion({ ariaLabel, children }: { ariaLabel: string; children: React.ReactNode }) {
  return (
    <Box tabIndex={0} role="region" aria-label={ariaLabel} sx={TABLE_REGION_SX}>
      {children}
    </Box>
  );
}

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
          <Typography variant="subtitle2" component="div">{row.label}</Typography>
          <Typography variant="caption" component="div" sx={{ ...MONO_SX, color: "text.secondary" }}>{row.key}</Typography>
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
    <TableRegion ariaLabel={ariaLabel}>
      <DataTable
        columns={columns}
        data={rows}
        getRowId={(row) => row.key}
        ariaLabel={ariaLabel}
        empty={empty}
      />
    </TableRegion>
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
  /** TRUE when the operator named the disease; FALSE when it was inferred from an open case. */
  causeRecorded: boolean;
  diseaseLabel: string;
  daysUnderTreatment: number | null;
};

export function DeathsTable({
  contract,
  rows,
  ariaLabel,
  empty,
  noDataLabel,
  inferredLabel,
}: {
  contract: AdminUiTableContract;
  rows: DeathRow[];
  ariaLabel: string;
  empty: React.ReactNode;
  /** Backend copy for a cell with nothing to show. Never composed here. */
  noDataLabel: string;
  /** Backend copy marking a disease that was inferred rather than recorded. */
  inferredLabel: string;
}) {
  const columns = columnsFromContract<DeathRow>(contract, {
    animal: {
      cell: (row) => (
        <div>
          <Typography variant="body2" component="span" sx={MONO_SX}>{row.animalLabel}</Typography>
          <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{row.displayId}</Typography>
        </div>
      ),
      sortValue: (row) => row.animalLabel,
    },
    pen: { cell: (row) => row.pen || <Typography variant="body2" component="span" sx={{ color: "text.secondary" }}>{noDataLabel}</Typography>, sortValue: (row) => row.pen },
    // Sorted on the ISO value, never the rendered DD-MM-YYYY, which orders by day-of-month.
    date: { cell: (row) => row.date, sortValue: (row) => row.sortDate },
    age_band: { cell: (row) => row.ageBandLabel, sortValue: (row) => row.ageBandLabel },
    attribution: {
      // An UNATTRIBUTED death shows the chip and NOTHING else. If the death has neither
      // a recorded cause nor the legacy open-case inference, a disease name must never appear.
      cell: (row) =>
        row.attributed ? (
          <div>
            {/* A RECORDED cause reads plainly; an INFERRED one is marked, because it says
                only that this case was open when the animal died — co-incidence, not
                causation, and the reader is owed the difference. */}
            <Tag tone={row.causeRecorded ? "teal" : "mut"}>{row.diseaseLabel}</Tag>
            {row.causeRecorded ? null : (
              <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{inferredLabel}</Typography>
            )}
          </div>
        ) : (
          <Tag tone="mut">{row.attributionLabel}</Tag>
        ),
      sortValue: (row) => (row.attributed ? row.diseaseLabel : row.attributionLabel),
    },
    days_treated: {
      cell: (row) => (row.daysUnderTreatment === null ? <Typography variant="body2" component="span" sx={{ color: "text.secondary" }}>—</Typography> : nf(row.daysUnderTreatment)),
      meta: { align: "right" },
      sortValue: (row) => row.daysUnderTreatment ?? undefined,
    },
  });

  return (
    <TableRegion ariaLabel={ariaLabel}>
      <DataTable
        columns={columns}
        data={rows}
        getRowId={(row) => row.goatId}
        ariaLabel={ariaLabel}
        empty={empty}
      />
    </TableRegion>
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
    medicine: { cell: (row) => <Typography variant="subtitle2" component="span">{row.name}</Typography>, sortValue: (row) => row.name },
    route: { cell: (row) => row.route, sortValue: (row) => row.route },
    doses: { cell: (row) => nf(row.doses), meta: { align: "right" }, sortValue: (row) => row.doses },
    animals: { cell: (row) => nf(row.animals), meta: { align: "right" }, sortValue: (row) => row.animals },
  });

  return (
    <TableRegion ariaLabel={ariaLabel}>
      <DataTable
        columns={columns}
        data={rows}
        getRowId={(row) => row.key}
        ariaLabel={ariaLabel}
        empty={empty}
      />
    </TableRegion>
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
    rule: { cell: (row) => <Typography variant="body2" component="span" sx={MONO_SX}>{row.key}</Typography>, sortValue: (row) => row.key },
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
    <TableRegion ariaLabel={ariaLabel}>
      <DataTable
        columns={columns}
        data={rows}
        getRowId={(row) => row.key}
        ariaLabel={ariaLabel}
        empty={empty}
      />
    </TableRegion>
  );
}
