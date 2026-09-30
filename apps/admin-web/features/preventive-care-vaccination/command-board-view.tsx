"use client";
import Table from "@mui/material/Table";
import { CB_DRIVE_FIELD_MIN, CB_KPI_SIZE, CB_STATUS_FIELD_MIN, CB_VACCINE_FIELD_MIN, STATUS_KEYS } from "./command-board-layout";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { Fragment, useEffect, useMemo, useState, useTransition, type KeyboardEvent as ReactKeyboardEvent, useCallback } from "react";
import { useBackCloses } from "@/components/use-back-closes";

import type { CommandBoardCohortMatrixPage } from "@/lib/api/server";
import {
  useClosedWithoutDoseAnimals,
  useCohortMatrix,
  useShedDoseMatrix,
  type ShedDoseCellRow,
  useCohortCellDetail,
  useDriveCatalogue,
  useShedVaccineAnimals,
  type CohortCellRef,
} from "./command-board-drilldowns";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { MinimalDrawer } from "@/components/app/drawer";
import { DrawerTableScroll } from "@/components/app/detail-drawer";
import { KpiWidget, type KpiIcon } from "@/components/app/kpi-widget";
import type { PaletteColorKey } from "@/theme/core";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import type { LabelColor } from "@/components/minimal/label";
import { Label } from "@/components/minimal/label";
import { EmptyRow, HeadCell, MatrixCard, RowHeadCell, StateCell, StateLegend } from "./command-board-cards";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import Checkbox from "@mui/material/Checkbox";
import { useRouter, useSearchParams } from "next/navigation";
import type { AppApiComponents } from "@goatos/api-client";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { copy, optionGroup } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { operationalLocationLabel } from "@/lib/operational-location";
import {
  driveSelectionValue,
  executedDriveCampaigns,
  formatDateSpan,
  formatScheduledDriveDates,
  parseDriveSelectionValue,
  scheduledDriveCampaigns,
  scheduledDriveRows,
  sortDriveCampaignsChronological,
  type CommandBoardDriveOption,
} from "./command-board-future-drives";
import { TAP_MIN } from "@/theme/tap-target";

// Build colored grid heatmap from flat shed-dose matrix
interface GridCell {
  doseRule: string;
  state: string;
  animalCount: number;
  minAdministeredDate?: string | null;
  maxAdministeredDate?: string | null;
  minDueDate?: string | null;
  maxDueDate?: string | null;
}

interface ShedGridRow {
  shedName: string;
  shedId: string;
  partitionLabel: string | null;
  operational_location_display: string | null;
  parkName: string | null;
  cells: Record<string, GridCell>;
}

interface AdministeredDateRange {
  min?: string | null;
  max?: string | null;
}

// KPI tile fills its grid cell; a drill-in tile is a keyboard button with a visible focus ring.
const KPI_TILE_SX = { height: 1 } as const;
const KPI_BUTTON_SX = { height: 1, cursor: "pointer", borderRadius: 1.5, "&:focus-visible": { outlineStyle: "solid", outlineWidth: 2, outlineColor: "primary.main", outlineOffset: 2 } } as const;

function mergeAdministeredDateRange(
  ranges: Record<string, AdministeredDateRange>,
  vaccine: string,
  min?: string | null,
  max?: string | null,
) {
  const nextMin = min?.slice(0, 10) ?? "";
  const nextMax = (max ?? min)?.slice(0, 10) ?? "";
  if (!nextMin && !nextMax) return;
  const current = ranges[vaccine] ?? {};
  if (nextMin && (!current.min || nextMin < current.min.slice(0, 10))) current.min = min;
  if (nextMax && (!current.max || nextMax > current.max.slice(0, 10))) current.max = max ?? min;
  ranges[vaccine] = current;
}

// Stage -> cohort row, by DECLARED backend membership. Never prefix matching, and Adults is no
// longer a catch-all.
//
// Prefix matching with a trailing catch-all put F2-Female/F2-Male in the adult herd (372 against a
// true 324) and left ICU-Kid — a KID carrying a health-state prefix — in Adults as well. Both are
// the same failure: a label the ladder did not recognise fell through to the last rung and
// silently inflated it.
//
// An unmapped stage now returns ITSELF, so it renders as its own visible row. Warmup is an
// arrival/acclimation state that is neither adult nor kid by label, and it stays visible under its
// own name rather than being guessed into a cohort.
function cohortBucket(managementStage: string, stageMap: Map<string, string>): string {
  const stage = (managementStage || "").trim();
  return stageMap.get(stage.toUpperCase()) ?? stage;
}

interface CohortPivotRow {
  cohort: string;
  animals: number;
  // Three DISJOINT buckets from the backend, by WHO OWES THE NEXT MOVE: the operator (pending),
  // the verifier (submitted), nobody (verified). The cell must show all three — showing only
  // "pending" is what made a fully vaccinated, fully submitted park read identically to a park
  // nobody had touched, and left the CEO with "40 pending" under "40 awaiting verification".
  pending: Record<string, number>;
  submitted: Record<string, number>;
  rejectedRework: Record<string, number>;
  verified: Record<string, number>;
  administeredDates: Record<string, AdministeredDateRange>;
  // The BACKEND cells that fold into each displayed vaccine column of this row.
  //
  // A row is a display bucket: it can roll several (stage, sex) cohorts and several parks together.
  // The day split is fetched per cell, so the drawer needs to know which cells it is made of —
  // one page per contributing cell, merged the way the summary numbers above were merged.
  cellRefs: Record<string, CohortCellRef[]>;
  // The real management stages that fold into this rung. They are CEO-level noise in the grid, so
  // they live in the drilldown only — the grid stays one row per cohort.
  members: CohortMember[];
}

interface CohortMember {
  label: string;
  animals: number;
  pending: Record<string, number>;
  submitted: Record<string, number>;
  rejectedRework: Record<string, number>;
  verified: Record<string, number>;
  administeredDates: Record<string, AdministeredDateRange>;
}

interface CohortCellInput {
  cohort: { parkId?: string; parkName: string; managementStage: string; sex: string; animalCount: number };
  vaccineLabel: string;
  pendingCount: number;
  submittedCount?: number;
  rejectedReworkCount?: number;
  verifiedCount: number;
  minAdministeredDate?: string | null;
  maxAdministeredDate?: string | null;
  // The raw dose codes behind this cell's single displayed vaccineLabel. They ADDRESS the cell in
  // the drilldown endpoints: the board collapses several codes onto one column, so the label alone
  // cannot identify it, and re-deriving the mapping here would put a second, drifting copy of the
  // dose-labelling table in the frontend.
  doseCodes?: string[];
}

// Day counts of the same vaccine coming from several (stage, sex) cohorts land on the same cohort
// row, so identical dates ADD rather than overwrite — otherwise "1 Jul: 237" would silently become
// whichever sub-cohort was folded last.
//
// CALLER CONTRACT: only ever feed this the day rows of ONE vaccine label. Repeated dates across
// sub-cohorts are DIFFERENT animals dosed on the same day and must sum; repeated dates from the
// same sub-cohort would double it. The backend already emits one day list per cohort x dose, so
// the caller must not merge two dose codes into one call.
function buildCohortFarms(
  matrix: CohortCellInput[],
  ladder: string[],
  stageMap: Map<string, string>,
): Array<{ farm: string; vaccines: string[]; rows: CohortPivotRow[] }> {
  const byFarm = new Map<string, CohortCellInput[]>();
  matrix.forEach((cell) => {
    const farm = cell.cohort.parkName || "";
    const bucket = byFarm.get(farm);
    if (bucket) bucket.push(cell);
    else byFarm.set(farm, [cell]);
  });
  return Array.from(byFarm.entries())
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([farm, cells]) => ({ farm, ...buildCohortPivot(cells, ladder, stageMap) }));
}

function buildCohortPivot(
  matrix: CohortCellInput[],
  ladder: string[],
  stageMap: Map<string, string>,
): { vaccines: string[]; rows: CohortPivotRow[] } {
  const vaccines = Array.from(new Set(matrix.map((c) => c.vaccineLabel).filter(Boolean))).sort();
  // Declared rows PLUS any stage this farm holds that the map does not classify. An unmapped stage
  // gets its own visible row instead of disappearing into another cohort, so a newly introduced or
  // still-undefined label (Warmup today) is something the CEO can see and ask about.
  const unmapped = Array.from(
    new Set(
      matrix
        .map((cell) => cohortBucket(cell.cohort.managementStage, stageMap))
        .filter((row) => row && !ladder.includes(row)),
    ),
  ).sort();
  const rows = [...ladder, ...unmapped].map((cohort) => {
    const pending: Record<string, number> = {};
    const submitted: Record<string, number> = {};
    const rejectedRework: Record<string, number> = {};
    const verified: Record<string, number> = {};
    const administeredDates: Record<string, AdministeredDateRange> = {};
    const cellRefs: Record<string, CohortCellRef[]> = {};
    const members = new Map<string, CohortMember>();
    // Animals are per (stage, sex) cohort and the source repeats a cohort once per vaccine, so
    // head counts accumulate per DISTINCT cohort key — summing the rows directly would multiply
    // the head count by the number of vaccines.
    const counted = new Set<string>();
    let animals = 0;
    matrix.forEach((cell) => {
      if (cohortBucket(cell.cohort.managementStage, stageMap) !== cohort) return;
      const key = `${cell.cohort.managementStage}|${cell.cohort.sex}`;
      pending[cell.vaccineLabel] = (pending[cell.vaccineLabel] ?? 0) + cell.pendingCount;
      submitted[cell.vaccineLabel] = (submitted[cell.vaccineLabel] ?? 0) + (cell.submittedCount ?? 0);
      rejectedRework[cell.vaccineLabel] =
        (rejectedRework[cell.vaccineLabel] ?? 0) + (cell.rejectedReworkCount ?? 0);
      verified[cell.vaccineLabel] = (verified[cell.vaccineLabel] ?? 0) + cell.verifiedCount;
      mergeAdministeredDateRange(
        administeredDates,
        cell.vaccineLabel,
        cell.minAdministeredDate,
        cell.maxAdministeredDate,
      );
      // Record the backend cell so the drawer can fetch this column's actual vaccination days.
      const refs = cellRefs[cell.vaccineLabel] ?? [];
      refs.push({
        cohortParkId: cell.cohort.parkId ?? "",
        managementStage: cell.cohort.managementStage,
        sex: cell.cohort.sex,
        doseCodes: cell.doseCodes ?? [],
      });
      cellRefs[cell.vaccineLabel] = refs;

      let member = members.get(key);
      if (!member) {
        member = {
          label: `${cell.cohort.managementStage} · ${cell.cohort.sex}`,
          animals: 0,
          pending: {},
          submitted: {},
          rejectedRework: {},
          verified: {},
          administeredDates: {},
        };
        members.set(key, member);
      }
      member.pending[cell.vaccineLabel] = (member.pending[cell.vaccineLabel] ?? 0) + cell.pendingCount;
      member.submitted[cell.vaccineLabel] =
        (member.submitted[cell.vaccineLabel] ?? 0) + (cell.submittedCount ?? 0);
      member.rejectedRework[cell.vaccineLabel] =
        (member.rejectedRework[cell.vaccineLabel] ?? 0) + (cell.rejectedReworkCount ?? 0);
      member.verified[cell.vaccineLabel] = (member.verified[cell.vaccineLabel] ?? 0) + cell.verifiedCount;
      mergeAdministeredDateRange(
        member.administeredDates,
        cell.vaccineLabel,
        cell.minAdministeredDate,
        cell.maxAdministeredDate,
      );

      if (!counted.has(key)) {
        counted.add(key);
        animals += cell.cohort.animalCount;
        member.animals = cell.cohort.animalCount;
      }
    });
    return {
      cohort,
      animals,
      pending,
      submitted,
      rejectedRework,
      verified,
      administeredDates,
      cellRefs,
      members: Array.from(members.values()).sort((a, b) => b.animals - a.animals),
    };
  });
  return { vaccines, rows };
}

function buildShedGrid(
  matrix: Array<{
    doseKey: string;
    shedId: string;
    shedName: string;
    partition_label?: string | null;
    operational_location_display?: string | null;
    parkName?: string | null;
    doseRule: string;
    state: string;
    animalCount: number;
    minAdministeredDate?: string | null;
    maxAdministeredDate?: string | null;
    minDueDate?: string | null;
    maxDueDate?: string | null;
  }>
): {
  byDose: Array<{ key: string; label: string }>;
  byShed: ShedGridRow[];
} {
  const doseSet = new Set<string>();
  // BUG FIX: Key by shedId + partition_label (using shedId|partition_label format) instead of shedName.
  // Two same-named sheds in different parks and two partitions of the same shed must remain as separate rows
  // with separate animal counts. Keying by shedName alone caused them to merge, silently summing counts.
  const shedMap = new Map<string, ShedGridRow>();

  // Keyed on doseKey (the matrix's dose index), NOT on the display label. Two dose codes can share
  // a label -- DoseQualifiedDisplayLabel leaves both et_tt_kid_4w and et_tt_kid_7w as "ET+TT" -- so
  // keying on the label merged two real doses into one column and dropped one of their counts,
  // exactly the way keying sheds by shedName used to merge two sheds. Two columns may therefore
  // carry the same header until that labeller disambiguates those suffixes; showing the same label
  // twice is a cosmetic wart, losing an animal count is a wrong number.
  const doseLabels = new Map<string, string>();
  matrix.forEach((cell) => {
    doseSet.add(cell.doseKey);
    doseLabels.set(cell.doseKey, cell.doseRule);
    const shedKey = `${cell.shedId}|${cell.partition_label ?? ""}`;
    if (!shedMap.has(shedKey)) {
      shedMap.set(shedKey, {
        shedName: cell.shedName,
        shedId: cell.shedId,
        partitionLabel: cell.partition_label ?? null,
        operational_location_display: cell.operational_location_display ?? null,
        parkName: cell.parkName ?? null,
        cells: {},
      });
    }
    shedMap.get(shedKey)!.cells[cell.doseKey] = {
      doseRule: cell.doseRule,
      state: cell.state,
      animalCount: cell.animalCount,
      minAdministeredDate: cell.minAdministeredDate,
      maxAdministeredDate: cell.maxAdministeredDate,
      minDueDate: cell.minDueDate,
      maxDueDate: cell.maxDueDate,
    };
  });

  return {
    byDose: Array.from(doseSet).map((key) => ({ key, label: doseLabels.get(key) ?? "" })),
    byShed: Array.from(shedMap.values()),
  };
}

// Derived from the generated client rather than hand-declared. Local mirrors of the response
// schema are why the compiler stayed green while closedWithoutDose and driveOptionsTruncated --
// both REQUIRED by the contract -- were dropped before they reached the render. Deriving makes the
// next dropped field a type error instead of a silent hole in the page.
type CommandBoardResponse = AppApiComponents["schemas"]["VaccinationCommandBoardResponse"];
// The cohort cell shape. It is no longer on the board response -- the matrix is its own section --
// so it is taken from that section's page type.
type CohortCell = NonNullable<CommandBoardCohortMatrixPage["cells"]>[number];
type ShedDoseCell = ShedDoseCellRow;
type CommandBoardKpis = CommandBoardResponse["kpis"] & {
  missedNotGiven?: number;
  reworkNeeded?: number;
  closedWithoutDose?: number;
};
type CommandBoardExtras = {
  kpis: CommandBoardKpis;
  shedVaccineMatrix?: ShedVaccineCell[];
  shedVaccineColumns?: Array<{ code: string; label: string }>;
  driveOptionsTruncated?: boolean;
};
// driveOptions is the one field the view widens: enrichDriveOptions reconstructs counts the skinny
// API catalogue omits and tags them, so the rendered option carries more than the wire schema does.
type CommandBoard = Omit<CommandBoardResponse, "driveOptions" | "kpis" | "shedVaccineMatrix" | "shedVaccineColumns"> & CommandBoardExtras & {
  driveOptions?: CommandBoardDriveOption[];
};

type ShedVaccineCell = {
  shedId: string;
  shedName: string;
  partition_label?: string | null;
  operational_location_display?: string | null;
  parkName?: string | null;
  vaccineCode: string;
  state: "behind" | "rework" | "verifying" | "ok" | "not_planned";
  behindAnimals: number;
  verifyingAnimals?: number;
  reworkAnimals?: number;
  totalAnimals: number;
  // No proofVideos / flaggedAnimals here on purpose: both are fetched per cell when the drawer
  // opens (command-board-drilldowns.ts). Declaring them optional would let a future edit read a
  // field the board never sends and render a silently empty drawer.
};
type ShedVaccineDrawerState = "behind" | "rework" | "verifying";
type OpenableShedVaccineCell = ShedVaccineCell & { state: ShedVaccineDrawerState };

function isOpenableShedVaccineCell(cell: ShedVaccineCell | undefined): cell is OpenableShedVaccineCell {
  return cell?.state === "behind" || cell?.state === "rework" || cell?.state === "verifying";
}

interface CommandBoardViewProps {
  board: CommandBoard;
  pageContract: AdminUiPageContract;
  driveBatchId?: string;
  // Park of the selected drive. The API's drive-option grain is (batch, park), so the batch id
  // alone does not identify a row once the same batch runs in two parks.
  driveParkId?: string;
}

/** Detail drawers: the template MinimalDrawer paper width from sm up (phones get the full width). */
const DRAWER_WIDTH = 480;

type StatusKey = (typeof STATUS_KEYS)[number];
/** Each status filter's colour: the matrix cell colour of that state. */
const STATUS_COLOR: Record<StatusKey, "success" | "warning" | "secondary" | "error" | "info"> = {
  verified: "success",
  awaiting: "warning",
  rework: "secondary",
  overdue: "error",
  scheduled: "info",
};

/** Pen × Vaccine cell state -> template Label colour (no colour: the quiet "not planned" dash). */
const PEN_VACCINE_COLOR: Record<string, LabelColor | undefined> = {
  behind: "error",
  rework: "secondary",
  verifying: "warning",
  ok: "success",
  not_planned: undefined,
};
/** Pending-by-pen chip colour per bucket (the colour of the same state in the matrix). */
const PENDING_COLOR: Record<string, "error" | "secondary" | "warning"> = {
  behind: "error",
  rework: "secondary",
  verifying: "warning",
};
/** Dose-matrix cell state -> template Label colour (legend and cells share it). */
const SHED_DOSE_COLOR: Record<string, LabelColor> = {
  verified: "success",
  awaiting: "warning",
  rework: "secondary",
  overdue: "error",
  scheduled: "info",
  not_scoped: "default",
};

function keyDate(value?: string | null): string {
  return value?.match(/^(\d{4}-\d{2}-\d{2})/)?.[1] ?? "";
}

function splitDriveDoseRules(label: string): string[] {
  const head = label.split(" — ")[0] ?? label;
  return head.split(" + ").map((part) => part.trim()).filter(Boolean);
}

function enrichDriveOptions(
  options: CommandBoardDriveOption[],
  matrix: ShedDoseCell[],
  cohortMatrix: CohortCell[],
  targetCap: number,
): CommandBoardDriveOption[] {
  const defaultParkId = cohortMatrix.find((cell) => cell.cohort.parkId)?.cohort.parkId ?? "";
  const defaultParkName = cohortMatrix.find((cell) => cell.cohort.parkName)?.cohort.parkName ?? "";
  return options.map((option) => {
    if (Number.isFinite(option.targetCount) && option.shedNames) return option;
    const doseRules = splitDriveDoseRules(option.driveName || option.label);
    const start = keyDate(option.windowStart || option.plannedDate);
    const end = keyDate(option.windowEnd || option.windowStart || option.plannedDate);
    const cells = matrix.filter((cell) => {
      if (!doseRules.includes(cell.doseRule)) return false;
      const date =
        option.status === "planned"
          ? keyDate(cell.minDueDate)
          : keyDate(cell.minAdministeredDate);
      const expectedState = option.status === "planned" ? "scheduled" : "verified";
      if (cell.state !== expectedState || !date) return false;
      if (option.status !== "planned") return true;
      return (!start || date >= start) && (!end || date <= end);
    });
    // BUG FIX (2026-08-07): OL-2 partition collapse. Key by shedId + partition_label instead of shedName.
    // Two same-named sheds across parks and two partitions of one shed must contribute separate counts.
    // Keying by shedName alone merged them, silently summing counts from disjoint physical locations.
    const byShedKey = new Map<string, { shedId: string; shedName: string; partitionLabel: string | null; operationalLocationDisplay: string | null; animalCount: number }>();
    cells.forEach((cell) => {
      const shedKey = `${cell.shedId}|${cell.partition_label ?? ""}`;
      const existing = byShedKey.get(shedKey);
      if (!existing || cell.animalCount > existing.animalCount) {
        byShedKey.set(shedKey, {
          shedId: cell.shedId,
          shedName: cell.shedName,
          partitionLabel: cell.partition_label ?? null,
          operationalLocationDisplay: cell.operational_location_display ?? null,
          animalCount: cell.animalCount,
        });
      }
    });
    const shedNames = Array.from(byShedKey.values())
      .map((entry) => entry.operationalLocationDisplay || operationalLocationLabel({ shedName: entry.shedName, partitionLabel: entry.partitionLabel }))
      .sort();
    const shedLocations = Array.from(byShedKey.values())
      .map((entry) => ({
        shedId: entry.shedId,
        shedName: entry.shedName,
        ...(entry.partitionLabel ? { partition_label: entry.partitionLabel } : {}),
        operational_location_display: entry.operationalLocationDisplay || operationalLocationLabel({ shedName: entry.shedName, partitionLabel: entry.partitionLabel }),
      }))
      .sort((a, b) => `${a.shedId}|${a.partition_label ?? ""}`.localeCompare(`${b.shedId}|${b.partition_label ?? ""}`));
    const doseCount = cells.reduce((sum, cell) => sum + (cell.animalCount ?? 0), 0);
    let targetCount = 0;
    if ((option.driveName || option.label).includes(" + ")) {
      targetCount = Array.from(byShedKey.values()).reduce((sum, entry) => sum + entry.animalCount, 0);
    } else {
      targetCount = doseCount;
    }
    return {
      ...option,
      driveName: option.driveName || option.label,
      parkId: option.parkId ?? defaultParkId,
      parkName: option.parkName ?? defaultParkName,
      plannedDate: option.plannedDate ?? option.windowStart,
      targetCount: targetCap > 0 ? Math.min(targetCount, targetCap) : targetCount,
      doseCount,
      shedNames,
      shedIds: shedLocations.map((location) => location.shedId),
      shedLocations,
      derivedFromMatrix: true,
    };
  }).filter((option) => option.status !== "planned" || (option.targetCount ?? 0) > 0);
}

// One selected cohort × dose cell, resolved entirely from the row already rendered.
interface SelectedCohortCell {
  key: string;
  farm: string;
  cohort: string;
  vaccine: string;
  animals: number;
  pending: number;
  submitted: number;
  rejectedRework: number;
  verified: number;
  dateSpan: string;
  // The BACKEND cells this display cell is made of. The drawer fetches its day split from these.
  cellRefs: CohortCellRef[];
  members: Array<{
    label: string;
    animals: number;
    pending: number;
    submitted: number;
    rejectedRework: number;
    verified: number;
    dateSpan: string;
  }>;
}

// Reading order and the "not adult" qualifier are backend-owned (the cohort row-order option
// group), so the grid never re-sorts business rows or invents its own qualifier text.
function cohortRowOrder(pageContract: AdminUiPageContract): string[] {
  return optionGroup(pageContract, "command_board_cohort_row_order").map((option) => option.label);
}

function rowQualifier(pageContract: AdminUiPageContract, cohort: string): string {
  const match = optionGroup(pageContract, "command_board_cohort_row_order").find((option) => option.label === cohort);
  return match?.title ?? "";
}

export function CommandBoardView({ board, pageContract, driveBatchId, driveParkId }: CommandBoardViewProps) {
  // Vaccine + status filters operate on the fetched payload. Drive scope is a server read, but
  // blank selection deliberately keeps the all-drives board so leadership sees the full programme.
  const router = useRouter();
  const searchParams = useSearchParams();
  const [isPending, startTransition] = useTransition();
  const currentSearch = searchParams?.toString() ?? "";
  const [optimisticDrive, setOptimisticDrive] = useState<{ from: string; value: string } | null>(null);
  // The scope every drilldown is resolved under: the SAME filter the board was rendered with, so a
  // drawer explains the number the reader actually clicked. drive_park_id wins over park_id for the
  // same reason it does on the board's own sections — a selected drive is one park's operator day.
  const drilldownScope = useMemo(
    () => ({
      driveBatchId: driveBatchId || undefined,
      parkId: driveParkId || searchParams?.get("park_id") || undefined,
      asOf: searchParams?.get("as_of") || undefined,
    }),
    [driveBatchId, driveParkId, searchParams],
  );

  // The cohort grid is a SECTION loaded after first paint, not a drawer. See useCohortMatrix.
  const cohortSection = useCohortMatrix<CohortCell>(drilldownScope);
  const cohortMatrix = cohortSection.data;
  // The shed grid is likewise a SECTION loaded after first paint. See useShedDoseMatrix: interning
  // its payload was not enough on its own, so the section itself moved off the board.
  const shedDoseSection = useShedDoseMatrix(drilldownScope);
  const shedDoseMatrix = shedDoseSection.data;

  // The board carries only the FIRST PAGE of drives (20). The catalogue used to ship whole and was
  // 448ms and 753 KB — more than the endpoint's entire 512 KB budget — for a dropdown, and it was
  // the board's critical path once the drilldowns had moved off it. This completes it in the
  // background after first paint, so the picker and the future-drive rows stay whole without the
  // reader waiting for them.
  const driveCatalogue = useDriveCatalogue(
    board.driveOptions ?? [],
    Boolean(board.driveOptionsTruncated),
    searchParams?.get("park_id") || undefined,
  );
  const driveOptions = useMemo(
    () => enrichDriveOptions(driveCatalogue.options, shedDoseMatrix, cohortMatrix, board.kpis.targets),
    [driveCatalogue.options, shedDoseMatrix, cohortMatrix, board.kpis.targets],
  );
  const futureDrives = useMemo(() => scheduledDriveRows(driveOptions), [driveOptions]);
  const executedCampaigns = useMemo(() => executedDriveCampaigns(driveOptions), [driveOptions]);
  const selectedDrive = optimisticDrive?.from === currentSearch
    ? optimisticDrive.value
    : driveBatchId
      ? driveSelectionValue(driveBatchId, driveParkId)
      : "";

  const selectDrive = (next: string) => {
    setOptimisticDrive({ from: currentSearch, value: next });
    const params = new URLSearchParams(currentSearch);
    const selection = next ? parseDriveSelectionValue(next) : undefined;
    if (selection?.driveBatchId) params.set("cb_drive", selection.driveBatchId); else params.delete("cb_drive");
    if (selection?.parkId) params.set("cb_drive_park", selection.parkId); else params.delete("cb_drive_park");
    const query = params.toString();
    startTransition(() => {
      router.push(query ? `?${query}` : "?", { scroll: false });
    });
  };

  const vaccineOptions = useMemo(() => {
    // Sourced from the SHED DOSE matrix. Both matrices are now lazy sections, so this filter
    // fills in when the shed grid lands; it is built from the shed grid rather than the cohort
    // grid because the shed grid is the cheaper of the two and therefore lands first.
    const labels = shedDoseMatrix.map((c) => c.doseRule).filter(Boolean);
    return Array.from(new Set(labels)).sort();
  }, [shedDoseMatrix]);
  const [vaccine, setVaccine] = useState<string>("");
  // EMPTY means "no filter, show everything" — it does NOT mean "hide everything". The chips used
  // to initialise to the full set and a click DELETED that status, so pressing "Overdue" hid the
  // overdue cells and left the other three: a control that reads "show me this" did the exact
  // opposite. Selecting into an empty set makes the chip mean what its label says, and keeps the
  // unfiltered board reachable by deselecting rather than by re-selecting all four.
  const [statuses, setStatuses] = useState<Set<StatusKey>>(new Set());
  // Cell drilldown is client-local overlay state: the cohort row already carries its sub-cohorts,
  // so opening a cell must not re-run the route (local-overlay rule).
  const [selectedCell, setSelectedCell] = useState<SelectedCohortCell | null>(null);
  // Client-local overlay state: the animals are already in the rendered payload, so opening the
  // drawer must not re-run the route.
  const [closedDrawerOpen, setClosedDrawerOpen] = useState(false);
  // The behind cell's animals travel IN the board payload, so opening a red cell is a local
  // overlay, not a second fetch (local-overlay rule).
  const [selectedShedVaccine, setSelectedShedVaccine] = useState<OpenableShedVaccineCell | null>(null);
  // The tile's animals, fetched when the drawer opens. They used to ship on the board payload,
  // computed tenant-wide on every render; that statement is the one that exhausted the pool timeout
  // and returned the 500 this whole change exists to fix.
  const closedDrilldown = useClosedWithoutDoseAnimals(closedDrawerOpen, drilldownScope);
  const closedAnimals = closedDrilldown.data;
  // Whole-scope truth from the board, and the only thing the tile's affordance may be gated on.
  const closedWithoutDoseCount = board.kpis.closedWithoutDose ?? 0;
  const shedVaccineDrilldown = useShedVaccineAnimals(
    selectedShedVaccine
      ? {
          shedId: selectedShedVaccine.shedId,
          vaccineCode: selectedShedVaccine.vaccineCode,
          state: selectedShedVaccine.state,
          partitionLabel: selectedShedVaccine.partition_label,
        }
      : null,
    drilldownScope,
  );
  const selectedShedVaccineCount = selectedShedVaccine
    ? selectedShedVaccine.state === "verifying"
      ? selectedShedVaccine.verifyingAnimals ?? 0
      : selectedShedVaccine.state === "rework"
        ? selectedShedVaccine.reworkAnimals ?? 0
        : selectedShedVaccine.behindAnimals ?? 0
    : 0;
  const cohortDrilldown = useCohortCellDetail(selectedCell?.cellRefs ?? null, drilldownScope);
  const futureCampaigns = useMemo(
    () => (statuses.size === 0 || statuses.has("scheduled")) ? scheduledDriveCampaigns(futureDrives) : [],
    [futureDrives, statuses],
  );
  const driveCampaigns = useMemo(
    () => sortDriveCampaignsChronological([...executedCampaigns, ...futureCampaigns]),
    [executedCampaigns, futureCampaigns],
  );
  const driveChoices = useMemo(
    () => driveCampaigns
      .flatMap((campaign) => campaign.treatments.map((drive, index) => ({ campaign, drive, index })))
      .sort((a, b) => {
        const dateOrder = (a.drive.dateKeys[0] ?? "").localeCompare(b.drive.dateKeys[0] ?? "");
        if (dateOrder !== 0) return dateOrder;
        return a.campaign.name.localeCompare(b.campaign.name);
      }),
    [driveCampaigns],
  );

  const view = useMemo(() => {
    const matchesVaccine = (label?: string) => !vaccine || (label ?? "").startsWith(vaccine);
    const isStatusVisible = (key: string) => statuses.size === 0 || statuses.has(key as StatusKey);
    const shedVaccineColumns = (board.shedVaccineColumns ?? []).filter((column) =>
      matchesVaccine(column.label || column.code),
    );
    const shedVaccineCodes = new Set(shedVaccineColumns.map((column) => column.code));
    return {
      ...board,
      shedVaccineMatrix: (board.shedVaccineMatrix ?? []).filter((cell) => shedVaccineCodes.has(cell.vaccineCode)),
      shedVaccineColumns,
      shedDoseMatrix: shedDoseMatrix.filter(
        (c) => matchesVaccine(c.doseRule) && isStatusVisible(c.state),
      ),
      cohortMatrix: cohortMatrix.filter((c) => matchesVaccine(c.vaccineLabel)),
      verificationQueue: (board.verificationQueue ?? []).filter((r) => matchesVaccine(r.doseRule)),
    };
  }, [board, shedDoseMatrix, cohortMatrix, vaccine, statuses]);
  const pendingVaccinesByShed = useMemo(() => {
    const vaccineLabels = new Map((view.shedVaccineColumns ?? []).map((c) => [c.code, c.label || c.code]));
    type PendingShed = {
      key: string;
      name: string;
      park?: string;
      cells: Array<OpenableShedVaccineCell & { label: string; bucketCount: number }>;
    };
    const byShed = new Map<string, PendingShed>();
    (view.shedVaccineMatrix ?? []).forEach((cell) => {
      const key = `${cell.shedId}|${cell.partition_label ?? ""}`;
      const row = byShed.get(key) ?? {
        key,
        name: cell.operational_location_display || operationalLocationLabel({ shedName: cell.shedName, partitionLabel: cell.partition_label }),
        park: cell.parkName ?? undefined,
        cells: [],
      };
      const label = vaccineLabels.get(cell.vaccineCode) ?? cell.vaccineCode;
      const addBucket = (state: ShedVaccineDrawerState, bucketCount: number | undefined) => {
        if (!bucketCount || bucketCount <= 0) return;
        row.cells.push({ ...cell, state, label, bucketCount });
      };
      addBucket("behind", cell.behindAnimals);
      addBucket("rework", cell.reworkAnimals);
      addBucket("verifying", cell.verifyingAnimals);
      byShed.set(key, row);
    });
    return Array.from(byShed.values()).sort((a, b) =>
      `${a.park ?? ""}:${a.name}`.localeCompare(`${b.park ?? ""}:${b.name}`),
    );
  }, [view.shedVaccineColumns, view.shedVaccineMatrix]);

  const activateStatusKpi = (key: StatusKey) => {
    setSelectedCell(null);
    setClosedDrawerOpen(false);
    setSelectedShedVaccine(null);
    setStatuses(new Set([key]));
    document.getElementById("cbm-shed-dose-matrix")?.scrollIntoView({ block: "start", behavior: "smooth" });
  };

  const statusKpiClick = (key: StatusKey, count: number) =>
    count > 0 ? () => activateStatusKpi(key) : undefined;

  const openCohortDrawer = (cell: SelectedCohortCell) => {
    setClosedDrawerOpen(false);
    setSelectedCell(cell);
  };

  const openClosedDrawer = () => {
    setSelectedCell(null);
    setClosedDrawerOpen(true);
  };

  // Back closes whichever drawer is open (the house drawer rule); these drawers are state, not URL.
  const anyDrawerOpen = Boolean(selectedCell || closedDrawerOpen || selectedShedVaccine);
  const closeAllDrawers = useCallback(() => {
    setSelectedCell(null);
    setClosedDrawerOpen(false);
    setSelectedShedVaccine(null);
  }, []);
  useBackCloses(anyDrawerOpen, closeAllDrawers);

  // Escape, the scrim and X are the MinimalDrawer's own onClose (MUI Drawer: it focuses itself on
  // open, so Escape works right after a mouse click too, and focus returns to the cell on close).

  // One option per (batch, park) identity. Two operator days of one batch in one park share a
  // selection value -- and selecting either narrows the board the same way -- so the second would
  // only duplicate the first (and its React key). The first wins; the dates in its label still
  // name a day the reader can recognise.
  const seenDriveValues = new Set<string>();
  const driveSelectOptions = [
    { value: "", label: copy(pageContract, "command_board.filter.all_common_drives") },
    ...driveChoices.flatMap(({ campaign, drive, index }) => {
      const value = driveSelectionValue(drive.batchIds[0] ?? drive.key, drive.parkId);
      if (seenDriveValues.has(value)) return [];
      seenDriveValues.add(value);
      return [{
        value,
        label: `${campaign.name} · Operator day ${index + 1} · ${formatScheduledDriveDates(drive.dateKeys)} · ${drive.targetCount} animals`,
      }];
    }),
  ];

  // Cohort matrix farm tab: client state (a presentation switch over the rows already loaded).
  const [farmTab, setFarmTab] = useState(0);

  const filterBar = (
    <Stack spacing={2} sx={{ px: 3, pb: 3 }}>
      <Stack direction={{ xs: "column", sm: "row" }} spacing={2} useFlexGap sx={{ flexWrap: "wrap" }}>
        <TextField
          select
          label={copy(pageContract, "command_board.filter.vaccine")}
          value={vaccine}
          onChange={(event) => setVaccine(event.target.value)}
          sx={{ minWidth: { xs: 0, sm: CB_VACCINE_FIELD_MIN }, flexShrink: 0, maxWidth: 1 }}
          slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
        >
          <MenuItem value="">{copy(pageContract, "command_board.filter.all_vaccines")}</MenuItem>
          {vaccineOptions.map((v) => (
            <MenuItem key={v} value={v}>
              {v}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          select
          label={copy(pageContract, "command_board.filter.operator_day")}
          value={selectedDrive}
          disabled={driveOptions.length === 0}
          title={driveOptions.length === 0
              ? copy(pageContract, "command_board.filter.no_drives")
              : isPending
                ? copy(pageContract, "state.loading")
                : undefined}
          onChange={(event) => selectDrive(event.target.value)}
          sx={{ minWidth: { xs: 0, sm: CB_DRIVE_FIELD_MIN }, flexShrink: 0, maxWidth: 1 }}
          slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
        >
          {driveSelectOptions.map((option) => (
            <MenuItem key={option.value} value={option.value}>
              {option.label}
            </MenuItem>
          ))}
        </TextField>
        {/* Status filter: the template UserTableToolbar multi Select (checkbox items, the state dot
            in the colour the matrix cells use), in the filter row beside Vaccine / Operator day,
            not a wrapping cloud of dot pills (TR1-#21, TR2-P2-6; guard: vaccination-status-select). */}
        <TextField
          select
          label={copy(pageContract, "command_board.filter.status", "Status")}
          value={STATUS_KEYS.filter((key) => statuses.has(key))}
          onChange={(event) => {
            const raw = event.target.value as unknown;
            const picked = (typeof raw === "string" ? raw.split(",") : (raw as string[])) as StatusKey[];
            setStatuses(new Set(picked.filter((key) => (STATUS_KEYS as readonly string[]).includes(key))));
          }}
          sx={{ minWidth: { xs: 0, sm: CB_STATUS_FIELD_MIN }, flexShrink: 0, maxWidth: 1 }}
          slotProps={{
            inputLabel: { shrink: true },
            select: {
              multiple: true,
              displayEmpty: true,
              renderValue: (selected) => {
                const keys = selected as StatusKey[];
                return keys.length === 0
                  ? copy(pageContract, "command_board.filter.all_statuses", "All statuses")
                  : keys.map((key) => copy(pageContract, `command_board.shed_matrix.state.${key}`)).join(", ");
              },
              MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } },
            },
          }}
        >
          {STATUS_KEYS.map((key) => (
            <MenuItem key={key} value={key}>
              <Checkbox disableRipple size="small" checked={statuses.has(key)} slotProps={{ input: { "aria-label": copy(pageContract, `command_board.shed_matrix.state.${key}`) } }} />
              <Box component="span" aria-hidden sx={{ width: "var(--sp-1)", height: "var(--sp-1)", borderRadius: "50%", bgcolor: `${STATUS_COLOR[key]}.main`, flexShrink: 0, mr: 1 }} />
              {copy(pageContract, `command_board.shed_matrix.state.${key}`)}
            </MenuItem>
          ))}
        </TextField>
        {/* The catalogue is bounded, so a drive past the bound is otherwise indistinguishable from a
            drive that was never planned. Say the picker is partial rather than let it read as the
            whole programme. */}
        {board.driveOptionsTruncated && (
          <Typography variant="caption" role="status" sx={{ color: "warning.main", alignSelf: "center" }}>
            {copy(pageContract, "command_board.filter.drives_truncated")}
          </Typography>
        )}
      </Stack>
    </Stack>
  );

  // One tile of the KPI deck: the shared KpiWidget adapter (template CourseWidgetSummary) with the
  // backend explanation as its caption sub-line (REVIEW-6/9: Missed vs Overdue must stay readable).
  // A tile with an action is a keyboard button (Enter / Space) that drills into the matrix; a
  // zero-count tile stays inert.
  const kpiTile = (key: string, title: string, total: number, hint: string, color: PaletteColorKey, icon: KpiIcon, onClick?: () => void) => (
    <Grid key={key} size={CB_KPI_SIZE}>
      {onClick ? (
        <Box
          role="button"
          tabIndex={0}
          aria-label={`${title}: ${total}. ${hint}`}
          onClick={onClick}
          onKeyDown={(event: ReactKeyboardEvent<HTMLDivElement>) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              onClick();
            }
          }}
          sx={KPI_BUTTON_SX}
        >
          <KpiWidget title={title} total={total} caption={hint} color={color} icon={icon} sx={KPI_TILE_SX} />
        </Box>
      ) : (
        <KpiWidget title={title} total={total} caption={hint} color={color} icon={icon} sx={KPI_TILE_SX} />
      )}
    </Grid>
  );

  // ---- Pen × Vaccine (dose collapsed) ------------------------------------------------------------
  // This sits ABOVE the dose-qualified matrix on purpose. The dose matrix answers "how much of each
  // dose", which is the follow-up; this one answers "is anything behind at all", which is the
  // question actually asked walking into a pen. It is deliberately not filtered by the status chips:
  // the chips select cell STATES of the dose matrix, and a red/green roll-up filtered to "scheduled"
  // would be a contradiction.
  const penVaccineCard = (() => {
    if (!(view.shedVaccineMatrix.length > 0 && view.shedVaccineColumns.length > 0)) return null;
    // Keyed by the operational location, not just parent shed. Partitioned sheds emit one backend
    // row per physical pen, so shedId alone would overwrite sibling partitions.
    const cellsByShed = new Map<string, Map<string, typeof view.shedVaccineMatrix[number]>>();
    const shedOrder: string[] = [];
    const shedLabel = new Map<string, { name: string; park?: string }>();
    const nameCount = new Map<string, Set<string>>();
    view.shedVaccineMatrix.forEach((cell) => {
      const opKey = `${cell.shedId}|${cell.partition_label ?? ""}`;
      let row = cellsByShed.get(opKey);
      if (!row) {
        row = new Map();
        cellsByShed.set(opKey, row);
        shedOrder.push(opKey);
        shedLabel.set(opKey, {
          name: cell.operational_location_display || operationalLocationLabel({ shedName: cell.shedName, partitionLabel: cell.partition_label }),
          park: cell.parkName ?? undefined,
        });
      }
      row.set(cell.vaccineCode, cell);
      const cellLabel = cell.operational_location_display || operationalLocationLabel({ shedName: cell.shedName, partitionLabel: cell.partition_label });
      const ids = nameCount.get(cellLabel) ?? new Set<string>();
      ids.add(opKey);
      nameCount.set(cellLabel, ids);
    });
    // Counts pens needing ANY attention (behind, rework, verifying), not just red ones: amber pens
    // with doses waiting on a verifier are not "up to date".
    const flaggedSheds = shedOrder.filter((shedId) =>
      Array.from(cellsByShed.get(shedId)?.values() ?? []).some(
        (c) => c.state === "behind" || c.state === "rework" || c.state === "verifying",
      ),
    ).length;
    const rows = shedOrder.map((shedPartitionKey) => {
      const label = shedLabel.get(shedPartitionKey);
      // Park is shown ONLY when the pen name is ambiguous in this payload.
      const ambiguous = (nameCount.get(label?.name ?? "")?.size ?? 0) > 1;
      return (
        <TableRow key={shedPartitionKey} hover>
          <RowHeadCell primary={label?.name} secondary={ambiguous ? label?.park : undefined} />
          {view.shedVaccineColumns.map((column) => {
            const code = column.code;
            const cell = cellsByShed.get(shedPartitionKey)?.get(code);
            const state = cell?.state ?? "not_planned";
            const openable = isOpenableShedVaccineCell(cell);
            const title =
              state === "behind"
                ? `${cell?.behindAnimals ?? 0} of ${cell?.totalAnimals ?? 0} behind`
                : state === "verifying"
                  ? `${cell?.verifyingAnimals ?? 0} of ${cell?.totalAnimals ?? 0} given, video verification pending`
                  : state === "rework"
                    ? `${cell?.reworkAnimals ?? 0} of ${cell?.totalAnimals ?? 0} rejected proof, rework needed`
                    : state === "ok"
                      ? `${cell?.totalAnimals ?? 0} on track`
                      : copy(pageContract, "command_board.shed_vaccine.state.not_planned");
            // Only a cell that needs a person carries a count; a clean cell is a quiet tick and an
            // unplanned one a dash, so the eye lands on the work first.
            const value =
              state === "behind"
                ? `${cell?.behindAnimals ?? 0} ${copy(pageContract, "command_board.shed_vaccine.cell.behind_unit")}`
                : state === "verifying"
                  ? `${cell?.verifyingAnimals ?? 0} ${copy(pageContract, "command_board.shed_vaccine.cell.verifying_unit")}`
                  : state === "rework"
                    ? `${cell?.reworkAnimals ?? 0} ${copy(pageContract, "command_board.shed_vaccine.cell.rework_unit")}`
                    : state === "ok"
                      ? "✓"
                      : "–";
            return (
              <StateCell
                key={code}
                color={PEN_VACCINE_COLOR[state]}
                value={value}
                title={title}
                aria-label={`${label?.name ?? ""} · ${column.label || column.code}: ${title}`}
                onActivate={openable ? () => setSelectedShedVaccine(cell) : undefined}
              />
            );
          })}
        </TableRow>
      );
    });
    return (
      <MatrixCard
        title={copy(pageContract, "command_board.shed_vaccine.title")}
        info={copy(pageContract, "command_board.shed_vaccine.meta")}
        subheader={flaggedSheds > 0
          ? `${flaggedSheds} / ${shedOrder.length} ${copy(pageContract, "command_board.shed_vaccine.summary_behind")}`
          : copy(pageContract, "command_board.shed_vaccine.summary_clean")}
        legend={
          <StateLegend
            items={(["behind", "rework", "verifying", "ok", "not_planned"] as const).map((state) => ({
              key: state,
              color: PEN_VACCINE_COLOR[state] ?? "default",
              label: copy(pageContract, `command_board.shed_vaccine.state.${state}`),
            }))}
          />
        }
        ariaLabel={copy(pageContract, "command_board.shed_vaccine.title")}
        minWidth={160 + view.shedVaccineColumns.length * 110}
        head={
          <TableRow>
            <HeadCell>{copy(pageContract, "command_board.shed_vaccine.column.shed")}</HeadCell>
            {/* Header text is SERVER copy: the label travels with the column so the client holds
                no vaccine-name table of its own. Falling back to the code keeps an unlabelled
                catalogue vaccine visible instead of blank. */}
            {view.shedVaccineColumns.map((column) => (
              <HeadCell key={column.code}>{column.label || column.code}</HeadCell>
            ))}
          </TableRow>
        }
        rows={rows}
      />
    );
  })();

  // ---- Pending vaccines by pen ------------------------------------------------------------------
  const pendingCard = (
    <MatrixCard
      title={copy(pageContract, "command_board.pending_sheds.title")}
      info={copy(pageContract, "command_board.pending_sheds.meta")}
      ariaLabel={copy(pageContract, "command_board.pending_sheds.title")}
      minWidth={560}
      head={
        <TableRow>
          <HeadCell>{copy(pageContract, "command_board.pending_sheds.column.shed")}</HeadCell>
          <HeadCell>{copy(pageContract, "command_board.pending_sheds.column.park")}</HeadCell>
          <HeadCell>{copy(pageContract, "command_board.pending_sheds.column.vaccines")}</HeadCell>
        </TableRow>
      }
      empty={<EmptyRow colSpan={3}>{copy(pageContract, "command_board.pending_sheds.empty")}</EmptyRow>}
      rows={pendingVaccinesByShed.map((row) => (
        <TableRow key={row.key} hover>
          <RowHeadCell primary={row.name} />
          <TableCell sx={{ whiteSpace: "nowrap" }}>{row.park ?? "—"}</TableCell>
          <TableCell>
            <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap" }}>
              {[...row.cells]
                .sort((a, b) => a.label.localeCompare(b.label))
                .map((cell) => (
                  <Chip
                    key={`${cell.vaccineCode}:${cell.state}`}
                    size="small"
                    variant="soft"
                    clickable
                    color={PENDING_COLOR[cell.state]}
                    onClick={() => setSelectedShedVaccine(cell)}
                    label={`${cell.label} · ${cell.bucketCount} ${copy(pageContract, `command_board.pending_sheds.state.${cell.state}`)}`}
                    sx={{ minHeight: { xs: TAP_MIN, sm: "auto" } }}
                  />
                ))}
            </Stack>
          </TableCell>
        </TableRow>
      ))}
    />
  );

  // ---- Vaccine × Pen status (dose-qualified) -----------------------------------------------------
  // The shed grid is loaded AFTER first paint, like the cohort grid. An explicit loading state
  // matters: an empty grid during the gap would read as "this tenant has no pens".
  const shedDoseState = shedDoseSection.loading && shedDoseMatrix.length === 0
    ? copy(pageContract, "command_board.shed_dose_matrix.loading")
    : shedDoseSection.error
      ? copy(pageContract, "command_board.shed_dose_matrix.unavailable")
      : shedDoseMatrix.length === 0
        ? copy(pageContract, "command_board.shed_dose_matrix.empty")
        : "";
  const shedDoseCard = (() => {
    const grid = buildShedGrid(view.shedDoseMatrix);
    // Queue age keyed by the same (pen, dose) grain the matrix cells use, so the number lands on
    // the cell it describes rather than being matched by position.
    const queueAgeDays = new Map<string, number>();
    (view.verificationQueue ?? []).forEach((q) => {
      if (q.daysInQueue !== undefined && q.daysInQueue !== null) {
        const shedKey = `${q.shedId}|${q.partition_label ?? ""}`;
        queueAgeDays.set(`${shedKey}|${q.doseRule}`, q.daysInQueue);
      }
    });
    const waitingSuffix = copy(pageContract, "command_board.shed_matrix.waiting_suffix");
    const rows = grid.byShed.map((row) => {
      // shedId + partition keys the row; the label is the operational location display.
      const shedKey = `${row.shedId}|${row.partitionLabel ?? ""}`;
      const shedLabel = row.operational_location_display || operationalLocationLabel({
        shedName: row.shedName,
        partitionLabel: row.partitionLabel,
      });
      return (
        <TableRow key={shedKey} hover>
          <RowHeadCell primary={shedLabel} secondary={row.parkName ? row.parkName : null} />
          {grid.byDose.map((dose) => {
            const cell = row.cells[dose.key];
            if (!cell) return <StateCell key={dose.key} value="—" />;
            const locationTitle = row.parkName ? `${shedLabel} · ${row.parkName}` : shedLabel;
            // Completed cells show the operator's actual administration date (verification can
            // happen days later and never replaces the medical date); scheduled and overdue cells
            // show their rule-derived due date.
            const dateStr = cell.state === "verified" || cell.state === "awaiting"
              ? formatDateSpan(cell.minAdministeredDate, cell.maxAdministeredDate)
              : formatDateSpan(cell.minDueDate, cell.maxDueDate);
            // An awaiting cell also carries how long it has been sitting with the verifier.
            const waiting = cell.state === "awaiting" ? queueAgeDays.get(`${shedKey}|${dose.label}`) : undefined;
            return (
              <StateCell
                key={dose.key}
                color={SHED_DOSE_COLOR[cell.state] ?? "default"}
                value={cell.animalCount}
                title={`${locationTitle} · ${cell.animalCount} animals${waiting !== undefined ? ` · ${waiting}${waitingSuffix}` : ""}`}
                caption={`${dateStr}${waiting !== undefined ? ` · ${waiting}${waitingSuffix}` : ""}`}
              />
            );
          })}
        </TableRow>
      );
    });
    return (
      <MatrixCard
        id="cbm-shed-dose-matrix"
        title={copy(pageContract, "command_board.shed_matrix.title")}
        info={copy(pageContract, "command_board.shed_matrix.meta")}
        legend={
          <StateLegend
            items={(["verified", "awaiting", "rework", "overdue", "scheduled", "not_scoped"] as const).map((state) => ({
              key: state,
              color: SHED_DOSE_COLOR[state] ?? "default",
              label: copy(pageContract, `command_board.shed_matrix.legend.${state}`),
            }))}
          />
        }
        ariaLabel={copy(pageContract, "command_board.shed_matrix.title")}
        minWidth={160 + grid.byDose.length * 150}
        head={
          <TableRow>
            <HeadCell>{copy(pageContract, "command_board.shed_matrix.column.shed")}</HeadCell>
            {grid.byDose.map((dose) => (
              <HeadCell key={dose.key}>{dose.label}</HeadCell>
            ))}
          </TableRow>
        }
        empty={shedDoseState ? <EmptyRow colSpan={grid.byDose.length + 1}>{shedDoseState}</EmptyRow> : undefined}
        rows={rows}
      />
    );
  })();

  // ---- Cohort matrix, FARMWISE -------------------------------------------------------------------
  // One tab per farm, cohort ladder down the side, vaccines across the top. The headline number is
  // the count of the state the Label colour denotes (who owes the next move), with the buckets that
  // carry work spelled out under it. Loaded AFTER first paint (its own section, see
  // useCohortMatrix), so an explicit loading state keeps an empty gap from reading as "no cohorts".
  const cohortState = cohortSection.loading && cohortMatrix.length === 0
    ? copy(pageContract, "command_board.cohort_matrix.loading")
    : cohortSection.error
      ? copy(pageContract, "command_board.cohort_matrix.unavailable")
      : cohortMatrix.length === 0
        ? copy(pageContract, "command_board.cohort_matrix.empty")
        : "";
  const cohortCard = (() => {
    // MATCHING ladder (catch-all last) and READING order are different backend lists.
    const ladder = optionGroup(pageContract, "command_board_cohort_ladder").map((o) => o.label);
    // Declared stage -> row membership. Adults is a normal rung here, never a fallback.
    const stageMap = new Map(
      optionGroup(pageContract, "command_board_cohort_stage_map").map((o) => [o.key.toUpperCase(), o.label]),
    );
    const readingOrder = cohortRowOrder(pageContract);
    const farms = cohortMatrix.length === 0 ? [] : buildCohortFarms(view.cohortMatrix, ladder, stageMap).map((farmBlock) => ({
      ...farmBlock,
      rows: [...farmBlock.rows].sort((a, b) => {
        const ai = readingOrder.indexOf(a.cohort);
        const bi = readingOrder.indexOf(b.cohort);
        return (ai < 0 ? readingOrder.length : ai) - (bi < 0 ? readingOrder.length : bi);
      }),
    }));
    const activeFarm = Math.min(farmTab, Math.max(0, farms.length - 1));
    const current = farms[activeFarm];
    const vaccines = current?.vaccines ?? [];
    const farm = current?.farm ?? "";
    const pendingWord = copy(pageContract, "command_board.cohort_matrix.pending_word");
    const reworkWord = copy(pageContract, "command_board.cohort_matrix.rework_word");
    const submittedWord = copy(pageContract, "command_board.cohort_matrix.submitted_word");
    const verifiedWord = copy(pageContract, "command_board.cohort_matrix.verified_word");
    const rows = (current?.rows ?? []).map((row, rowIndex) => {
      const label = row.cohort;
      const present = row.animals > 0;
      const cells = vaccines.map((v) => {
        const pending = row.pending[v];
        if (!present || pending === undefined) return <StateCell key={v} value="—" />;
        const awaiting = row.submitted[v] ?? 0;
        const rework = row.rejectedRework[v] ?? 0;
        const done = row.verified[v] ?? 0;
        // Nothing owed and nothing done: stay neutral rather than pretend work was completed.
        if (pending === 0 && awaiting === 0 && rework === 0 && done === 0) return <StateCell key={v} value="—" />;
        const administered = row.administeredDates[v];
        const administeredDate = formatDateSpan(administered?.min, administered?.max);
        const headline = pending > 0 ? pending : rework > 0 ? rework : awaiting > 0 ? awaiting : done;
        const dateLine = administeredDate
          ? administeredDate
          : done > 0
            ? copy(pageContract, "command_board.cohort_matrix.date_unavailable")
            : "";
        // Only the buckets that carry work are spelled out.
        const parts: string[] = [];
        if (pending > 0) parts.push(`${pending} ${pendingWord}`);
        if (rework > 0) parts.push(`${rework} ${reworkWord}`);
        if (awaiting > 0) parts.push(`${awaiting} ${submittedWord}`);
        if (done > 0) parts.push(`${done} ${verifiedWord}`);
        const cellKey = `${farm}|${label}|${v}`;
        const selection: SelectedCohortCell = {
          key: cellKey,
          farm,
          cohort: label,
          vaccine: v,
          animals: row.animals,
          pending,
          submitted: awaiting,
          rejectedRework: rework,
          verified: done,
          dateSpan: administeredDate,
          cellRefs: row.cellRefs[v] ?? [],
          members: row.members.map((member) => ({
            label: member.label,
            animals: member.animals,
            pending: member.pending[v] ?? 0,
            submitted: member.submitted[v] ?? 0,
            rejectedRework: member.rejectedRework[v] ?? 0,
            verified: member.verified[v] ?? 0,
            dateSpan: formatDateSpan(member.administeredDates[v]?.min, member.administeredDates[v]?.max),
          })),
        };
        const isOn = selectedCell?.key === cellKey;
        return (
          <StateCell
            key={v}
            // Colour follows who owes the next move.
            color={pending > 0 ? "error" : rework > 0 ? "secondary" : awaiting > 0 ? "warning" : "success"}
            value={headline}
            caption={
              <>
                <Box component="span" sx={{ display: "block" }}>{parts.join(" · ")}</Box>
                {dateLine ? <Box component="span" sx={{ display: "block" }}>{dateLine}</Box> : null}
              </>
            }
            title={`${label} · ${v} · ${pending} ${pendingWord}, ${rework} ${reworkWord}, ${awaiting} ${submittedWord}, ${done} ${verifiedWord}${dateLine ? ` · ${dateLine}` : ""}`}
            selected={isOn}
            onActivate={() => (isOn ? setSelectedCell(null) : openCohortDrawer(selection))}
          />
        );
      });
      // Two rows of one farm can share a cohort label, so the row's position disambiguates.
      return (
        <TableRow key={`${farm || "no-farm"}|${row.cohort}|${rowIndex}`} hover>
          <RowHeadCell primary={row.cohort} secondary={rowQualifier(pageContract, row.cohort) || undefined} />
          {cells}
          <TableCell sx={{ whiteSpace: "nowrap" }}>{row.animals > 0 ? row.animals : "—"}</TableCell>
        </TableRow>
      );
    });
    const farmPending = (block: (typeof farms)[number]) =>
      block.rows.reduce((sum, row) => sum + Object.values(row.pending).reduce((a, n) => a + (n ?? 0), 0), 0);
    return (
      <MatrixCard
        title={copy(pageContract, "command_board.cohort_matrix.title")}
        ariaLabel={copy(pageContract, "command_board.cohort_matrix.title")}
        minWidth={200 + vaccines.length * 150}
        tabs={farms.length > 0 ? (
          <Tabs
            value={activeFarm}
            onChange={(_, next: number) => {
              setSelectedCell(null);
              setFarmTab(next);
            }}
            variant="scrollable"
            allowScrollButtonsMobile
            sx={{ px: 2.5, boxShadow: (theme) => `inset 0 -2px 0 0 ${theme.vars.palette.divider}` }}
          >
            {farms.map((block, index) => {
              const owed = farmPending(block);
              return (
                <Tab
                  key={`${block.farm || "no-farm"}|${index}`}
                  value={index}
                  iconPosition="end"
                  label={block.farm || copy(pageContract, "command_board.cohort_matrix.no_farm")}
                  icon={
                    <Label variant={index === activeFarm ? "filled" : "soft"} color={owed > 0 ? "error" : "success"}>
                      <>{owed}</>
                    </Label>
                  }
                />
              );
            })}
          </Tabs>
        ) : null}
        head={
          <TableRow>
            <HeadCell>{copy(pageContract, "command_board.cohort_matrix.column.stage")}</HeadCell>
            {vaccines.map((v) => (
              <HeadCell key={v}>{v}</HeadCell>
            ))}
            <HeadCell>{copy(pageContract, "command_board.cohort_matrix.column.animals")}</HeadCell>
          </TableRow>
        }
        empty={<EmptyRow colSpan={vaccines.length + 2}>{cohortState || copy(pageContract, "command_board.cohort_matrix.empty")}</EmptyRow>}
        rows={rows}
      />
    );
  })();

  // ---- Scheduled ahead ---------------------------------------------------------------------------
  // Paged by campaign: a campaign's operator days stay together (the campaign cell spans them).
  const futureCard = futureCampaigns.length > 0 ? (
    <MatrixCard
      title={copy(pageContract, "command_board.future_drives.title")}
      subheader={`${futureCampaigns.length} ${copy(pageContract, "command_board.future_drives.count_suffix")} · ${futureDrives.length} ${copy(pageContract, "command_board.future_drives.lines_suffix")}`}
      ariaLabel={copy(pageContract, "command_board.future_drives.title")}
      minWidth={880}
      head={
        <TableRow>
          <HeadCell>{copy(pageContract, "command_board.future_drives.column.campaign")}</HeadCell>
          <HeadCell>{copy(pageContract, "command_board.future_drives.column.drive")}</HeadCell>
          <HeadCell>{copy(pageContract, "command_board.future_drives.column.dates")}</HeadCell>
          <HeadCell>{copy(pageContract, "command_board.future_drives.column.sheds")}</HeadCell>
          <HeadCell>{copy(pageContract, "command_board.future_drives.column.animals")}</HeadCell>
          <HeadCell>{copy(pageContract, "command_board.future_drives.column.doses")}</HeadCell>
        </TableRow>
      }
      rows={futureCampaigns.map((campaign, campaignIndex) => (
        // Two campaigns can share a name (one per operator window), so position disambiguates.
        <Fragment key={`${campaign.name}|${campaignIndex}`}>
          {campaign.treatments.map((drive, index) => (
            <TableRow key={drive.key} hover selected={Boolean(driveBatchId && drive.batchIds.includes(driveBatchId))}>
              {index === 0 && (
                <TableCell rowSpan={campaign.treatments.length} sx={{ verticalAlign: "top", minWidth: 200 }}>
                  <Typography variant="subtitle2" component="div">{campaign.name}</Typography>
                  <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>
                    {campaign.targetCount} {copy(pageContract, "command_board.future_drives.campaign_animals")} · {campaign.doseCount} {copy(pageContract, "command_board.future_drives.campaign_doses")}
                  </Typography>
                </TableCell>
              )}
              <TableCell sx={{ whiteSpace: "nowrap" }}><Typography variant="subtitle2" component="span">{drive.driveName}</Typography></TableCell>
              <TableCell sx={{ whiteSpace: "nowrap" }}>{formatScheduledDriveDates(drive.dateKeys)}</TableCell>
              <TableCell sx={{ minWidth: 240 }}>{drive.shedNames.join(", ") || "—"}</TableCell>
              <TableCell><Typography variant="subtitle2" component="span">{drive.targetCount}</Typography></TableCell>
              <TableCell><Typography variant="subtitle2" component="span">{drive.doseCount}</Typography></TableCell>
            </TableRow>
          ))}
        </Fragment>
      ))}
    />
  ) : null;

  // Page-level blocks, never a card inside a card (AUDIT1 P1-14): the Command Board filter card, the
  // KPI deck as page-level template widgets, then one template table card per matrix.
  return (
    <Stack spacing={3} sx={{ minWidth: 0 }}>
    <Card>
      <CardHeader title={copy(pageContract, "section.command_board.title")} sx={{ mb: 2.5 }} />
      {filterBar}
    </Card>
        {/* KPI deck: template overview/course widget tiles; a click drills into the matrix. */}
        <Grid container spacing={3}>
          {kpiTile("command_board.kpi.targets", copy(pageContract, "command_board.kpi.targets"), view.kpis.targets, copy(pageContract, "command_board.kpi.targets_dl"), "info", "completed", undefined)}
          {/* Missed sits FIRST after the roster total and ahead of Verified: it is the one tile that
              reports a failure rather than progress. */}
          {kpiTile("command_board.kpi.missed", copy(pageContract, "command_board.kpi.missed"), view.kpis.missedNotGiven, copy(pageContract, "command_board.kpi.missed_dl"), "error", "certificates", statusKpiClick("overdue", view.kpis.missedNotGiven))}
          {kpiTile("command_board.kpi.verified", copy(pageContract, "command_board.kpi.verified"), view.kpis.dosesVerified, copy(pageContract, "command_board.kpi.verified_dl"), "success", "completed", statusKpiClick("verified", view.kpis.dosesVerified))}
          {kpiTile("command_board.kpi.awaiting_verification", copy(pageContract, "command_board.kpi.awaiting_verification"), view.kpis.awaitingVerification, copy(pageContract, "command_board.kpi.awaiting_dl"), "warning", "progress", statusKpiClick("awaiting", view.kpis.awaitingVerification))}
          {kpiTile("command_board.kpi.rework_needed", copy(pageContract, "command_board.kpi.rework_needed"), view.kpis.reworkNeeded ?? 0, copy(pageContract, "command_board.kpi.rework_dl"), "secondary", "progress", statusKpiClick("rework", view.kpis.reworkNeeded ?? 0))}
          {kpiTile("command_board.kpi.overdue", copy(pageContract, "command_board.kpi.overdue"), view.kpis.overdueNotGiven, copy(pageContract, "command_board.kpi.overdue_dl"), "error", "certificates", statusKpiClick("overdue", view.kpis.overdueNotGiven))}
          {kpiTile("command_board.kpi.scheduled_ahead", copy(pageContract, "command_board.kpi.scheduled_ahead"), view.kpis.scheduledAhead, copy(pageContract, "command_board.kpi.scheduled_dl"), "info", "progress", statusKpiClick("scheduled", view.kpis.scheduledAhead))}
          {/* The five buckets are a disjoint, EXHAUSTIVE partition of targets; the fifth opens the
              animals that closed with no dose. Gated on the COUNT, not on the list — the animals are
              fetched when the drawer opens. */}
          {kpiTile("command_board.kpi.closed_without_dose", copy(pageContract, "command_board.kpi.closed_without_dose"), view.kpis.closedWithoutDose, copy(pageContract, "command_board.kpi.closed_without_dose_dl"), "info", "certificates", closedWithoutDoseCount > 0 ? openClosedDrawer : undefined)}
        </Grid>
      {penVaccineCard}
      {pendingCard}
      {shedDoseCard}
      {cohortCard}
      {futureCard}
      {/* The verification queue used to render as its own table, but every count in it (pen, dose,
          awaiting) is already an amber cell in the dose matrix; its queue age rides in that cell. */}

      {/* The three drawers are the template MinimalDrawer (portalled MUI Drawer): focus trapped
          and restored, X / Escape / scrim close, and Back closes via useBackCloses above. */}
      {/* Pen x Vaccine drawer: a red / amber / purple cell states the alarm, this names the animals. */}
      {selectedShedVaccine && (
        <MinimalDrawer
          open
          onClose={() => setSelectedShedVaccine(null)}
          title={`${selectedShedVaccine.operational_location_display || operationalLocationLabel({
            shedName: selectedShedVaccine.shedName,
            partitionLabel: selectedShedVaccine.partition_label,
          })} · ${view.shedVaccineColumns.find((c) => c.code === selectedShedVaccine.vaccineCode)?.label
            || selectedShedVaccine.vaccineCode}`}
          closeLabel={copy(pageContract, "command_board.cohort_matrix.detail.close")}
          width={DRAWER_WIDTH}
          role="dialog"
          aria-label={copy(pageContract, "command_board.shed_vaccine.title")}
        >
            <Typography variant="body2" sx={{ color: "text.secondary", px: 2.5, pt: 2, pb: 1 }}>
              {selectedShedVaccine.state === "verifying"
                ? `${selectedShedVaccine.verifyingAnimals} ${copy(pageContract, "command_board.shed_vaccine.drawer.verifying_of")} ${selectedShedVaccine.totalAnimals}`
                : selectedShedVaccine.state === "rework"
                  ? `${selectedShedVaccine.reworkAnimals} ${copy(pageContract, "command_board.shed_vaccine.drawer.rework_of")} ${selectedShedVaccine.totalAnimals}`
                : `${selectedShedVaccine.behindAnimals} ${copy(pageContract, "command_board.shed_vaccine.drawer.behind_of")} ${selectedShedVaccine.totalAnimals}`}
              {selectedShedVaccine.parkName ? ` · ${selectedShedVaccine.parkName}` : ""}
            </Typography>
            {/* The videos are PEN-and-day proof covering every animal below, so they belong once in
                the header, not on every row (which implied per-goat footage). */}
            <Stack direction="row" spacing={1} useFlexGap sx={{ px: 2.5, pb: 1.5, flexWrap: "wrap", alignItems: "center" }}>
              {shedVaccineDrilldown.loading ? (
                <Typography variant="caption" sx={{ color: "text.secondary" }}>
                  {copy(pageContract, "command_board.shed_vaccine.drawer.loading")}
                </Typography>
              ) : shedVaccineDrilldown.error ? (
                <Typography variant="caption" sx={{ color: "text.secondary" }}>
                  {copy(pageContract, "command_board.shed_vaccine.drawer.unavailable")}
                </Typography>
              ) : shedVaccineDrilldown.data.proofVideos.length > 0 ? (
                <>
                  <Typography variant="caption" sx={{ color: "text.secondary" }}>{copy(pageContract, "command_board.shed_vaccine.drawer.shed_videos")}</Typography>
                  {shedVaccineDrilldown.data.proofVideos.map((video, index) => (
                    <Chip
                      key={video.path}
                      size="small"
                      variant="soft"
                      color="info"
                      clickable
                      component="a"
                      href={video.path}
                      target="_blank"
                      rel="noreferrer"
                      label={`${copy(pageContract, "command_board.shed_vaccine.drawer.clip")} ${index + 1}`}
                      sx={{ minHeight: { xs: TAP_MIN, sm: "auto" } }}
                    />
                  ))}
                </>
              ) : (
                <Typography variant="caption" sx={{ color: "text.secondary" }}>
                  {copy(pageContract, "command_board.shed_vaccine.drawer.no_video")}
                </Typography>
              )}
            </Stack>
            <Box sx={{ pb: 2 }}>
              {/* One row per animal as a two-line row, not five columns (five overflowed the drawer
                  and hid the animal identity). */}
              {shedVaccineDrilldown.loading ? (
                <Typography variant="body2" role="status" sx={{ color: "text.secondary", px: 2.5 }}>
                  {copy(pageContract, "command_board.shed_vaccine.drawer.loading")}
                </Typography>
              ) : shedVaccineDrilldown.error ? (
                <Typography variant="body2" role="status" sx={{ color: "text.secondary", px: 2.5 }}>
                  {copy(pageContract, "command_board.shed_vaccine.drawer.unavailable")}
                </Typography>
              ) : (
                <>
                  <Table size="small">
                    <TableBody>
                      {shedVaccineDrilldown.data.animals.map((animal) => (
                        <TableRow key={animal.goatId}>
                          <TableCell>
                            {/* EAR TAGS lead, both of them: an operator may be reading either ear.
                                The internal id appears only for an animal with no active tag. */}
                            <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap" }}>
                              {animal.tag || animal.tag2 ? (
                                <>
                                  {animal.tag ? <Typography variant="subtitle2" component="span">{animal.tag}</Typography> : null}
                                  {animal.tag2 ? <Typography variant="subtitle2" component="span">{animal.tag2}</Typography> : null}
                                </>
                              ) : (
                                <Typography variant="subtitle2" component="span">{animal.displayId}</Typography>
                              )}
                            </Stack>
                            {/* The PEN and the date, nothing else: park and pen are constant for every
                                row of this cell and already sit in the drawer header. */}
                            <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>
                              {animal.partitionLabel ? `${animal.partitionLabel} · ` : ""}
                              {copy(pageContract, "command_board.shed_vaccine.drawer.column.due")}{" "}
                              {fmtDate(animal.dueAt ?? undefined)}
                            </Typography>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                  {/* The COUNT is whole-scope truth and the list is capped, so a shorter list must
                      say so rather than read as the complete set. */}
                  {shedVaccineDrilldown.data.animals.length < selectedShedVaccineCount && (
                    <Typography variant="caption" component="p" sx={{ color: "text.secondary", px: 2.5, pt: 1.5 }}>
                      {copy(pageContract, "command_board.shed_vaccine.drawer.truncated")}
                    </Typography>
                  )}
                </>
              )}
            </Box>
        </MinimalDrawer>
      )}

      {closedDrawerOpen && (
        <MinimalDrawer
          open
          onClose={() => setClosedDrawerOpen(false)}
          title={copy(pageContract, "command_board.kpi.closed_without_dose")}
          closeLabel={copy(pageContract, "command_board.cohort_matrix.detail.close")}
          width={DRAWER_WIDTH}
          role="dialog"
          aria-label={copy(pageContract, "command_board.kpi.closed_without_dose")}
        >
            <Typography variant="body2" sx={{ color: "text.secondary", px: 2.5, pt: 2 }}>
              {view.kpis.closedWithoutDose} {copy(pageContract, "command_board.closed_drawer.animals_word")}
            </Typography>
            <Box sx={{ p: 2.5 }}>
              {/* Four columns scroll sideways inside their own template Scrollbar, never clipped. */}
              <DrawerTableScroll>
              <Table sx={{ minWidth: 440 }}>
                <TableHead>
                  <TableRow>
                    <TableCell component="th">{copy(pageContract, "command_board.closed_drawer.column.animal")}</TableCell>
                    <TableCell component="th">{copy(pageContract, "command_board.closed_drawer.column.location")}</TableCell>
                    <TableCell component="th">{copy(pageContract, "command_board.closed_drawer.column.vaccine")}</TableCell>
                    <TableCell component="th">{copy(pageContract, "command_board.closed_drawer.column.reason")}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {closedAnimals.map((animal) => (
                    <TableRow key={animal.goatId}>
                      {/* The ear tags identify the animal on the farm, so they lead and the internal
                          id sits under them; both tags are shown so either ear matches. */}
                      <TableCell>
                        {animal.tag1 || animal.tag2 ? (
                          <>
                            {animal.tag1 ? <Typography variant="subtitle2" component="div">{animal.tag1}</Typography> : null}
                            {animal.tag2 ? <Typography variant="subtitle2" component="div">{animal.tag2}</Typography> : null}
                            <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{animal.displayId}</Typography>
                          </>
                        ) : (
                          <Typography variant="subtitle2" component="div">{animal.displayId}</Typography>
                        )}
                      </TableCell>
                      {/* Ground location, partition included (locationDisplay is the wire name of
                          VaccinationCommandBoardClosedWithoutDoseAnimal). */}
                      <TableCell>
                        <Typography variant="body2" component="div">{animal.locationDisplay}</Typography>
                        <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{animal.parkName}</Typography>
                      </TableCell>
                      <TableCell>{animal.vaccineLabel}</TableCell>
                      <TableCell>{animal.reason}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              </DrawerTableScroll>
              {(view.kpis.closedWithoutDose ?? 0) > closedAnimals.length ? (
                <Typography variant="caption" component="div" sx={{ color: "text.secondary", mt: 1.25 }}>
                  {copy(pageContract, "command_board.closed_drawer.capped")} {view.kpis.closedWithoutDose}
                </Typography>
              ) : null}
            </Box>
        </MinimalDrawer>
      )}

      {/* Cohort matrix cell detail drawer, from the data already in the rendered row. */}
      {selectedCell && (
        <MinimalDrawer
          open
          onClose={() => setSelectedCell(null)}
          title={`${selectedCell.farm || copy(pageContract, "command_board.cohort_matrix.no_farm")} · ${selectedCell.cohort} × ${selectedCell.vaccine}`}
          closeLabel={copy(pageContract, "command_board.cohort_matrix.detail.close")}
          width={DRAWER_WIDTH}
          role="dialog"
          aria-label={`${selectedCell.farm || copy(pageContract, "command_board.cohort_matrix.no_farm")} · ${selectedCell.cohort} × ${selectedCell.vaccine}`}
        >
            <Box sx={{ p: 2.5, display: "flex", flexDirection: "column", gap: 2 }}>
              <Box sx={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 2 }}>
                {([
                  ["command_board.cohort_matrix.detail.animals", selectedCell.animals],
                  ["command_board.cohort_matrix.pending_word", selectedCell.pending],
                  ["command_board.cohort_matrix.rework_word", selectedCell.rejectedRework],
                  ["command_board.cohort_matrix.submitted_word", selectedCell.submitted],
                  ["command_board.cohort_matrix.verified_word", selectedCell.verified],
                  ["command_board.cohort_matrix.detail.dates", selectedCell.dateSpan || copy(pageContract, "command_board.cohort_matrix.date_unavailable")],
                ] as const).map(([key, value]) => (
                  <Box key={key}>
                    <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{copy(pageContract, key)}</Typography>
                    <Typography variant="subtitle2" component="div">{value}</Typography>
                  </Box>
                ))}
              </Box>

              {/* The day story: which day the operator actually dosed how many animals. */}
              <Box>
                <Typography variant="overline" component="div" sx={{ color: "text.secondary", mb: 0.5 }}>{copy(pageContract, "command_board.cohort_matrix.detail.per_day")}</Typography>
                {cohortDrilldown.data.days.length > 0 ? (
                  <Stack divider={<Divider flexItem sx={{ borderStyle: "dashed" }} />}>
                    {cohortDrilldown.data.days.map((day) => (
                      <Stack key={day.date} direction="row" spacing={1.5} sx={{ py: 1, alignItems: "baseline" }}>
                        <Typography variant="body2" sx={{ flex: "1 1 auto" }}>{formatDateSpan(day.date, day.date)}</Typography>
                        <Typography variant="subtitle2">{day.animalCount}</Typography>
                        <Typography variant="caption" sx={{ color: "text.secondary" }}>{copy(pageContract, "command_board.cohort_matrix.detail.animals_word")}</Typography>
                      </Stack>
                    ))}
                  </Stack>
                ) : (
                  <Typography variant="body2" sx={{ color: "text.secondary" }}>
                    {copy(pageContract, "command_board.cohort_matrix.date_unavailable")}
                  </Typography>
                )}
              </Box>

              {/* Sub-cohorts breakdown table */}
              {selectedCell.members.length > 0 ? (
                <DrawerTableScroll>
                <Table sx={{ minWidth: 640 }}>
                  <TableHead>
                    <TableRow>
                      <TableCell component="th">{copy(pageContract, "command_board.cohort_matrix.detail.breakdown")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "command_board.cohort_matrix.column.animals")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "command_board.cohort_matrix.pending_word")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "command_board.cohort_matrix.rework_word")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "command_board.cohort_matrix.submitted_word")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "command_board.cohort_matrix.verified_word")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "command_board.cohort_matrix.detail.dates")}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {selectedCell.members.map((member, index) => (
                      <TableRow key={`${member.label}|${index}`}>
                        <TableCell>{member.label}</TableCell>
                        <TableCell>{member.animals}</TableCell>
                        <TableCell>{member.pending}</TableCell>
                        <TableCell>{member.rejectedRework}</TableCell>
                        <TableCell>{member.submitted}</TableCell>
                        <TableCell>{member.verified}</TableCell>
                        <TableCell>{member.dateSpan || copy(pageContract, "command_board.cohort_matrix.date_unavailable")}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                </DrawerTableScroll>
              ) : null}
            </Box>
        </MinimalDrawer>
      )}
    </Stack>
  );

}
