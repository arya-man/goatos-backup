"use client";
import { useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import type { AppApiComponents } from "@goatos/api-client";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { copy, optionGroup } from "@/lib/admin-ui-contract";
import {
  commonDriveName,
  driveSelectionValue,
  formatDateSpan,
  formatScheduledDriveDates,
  parseDriveSelectionValue,
  scheduledDriveCampaigns,
  scheduledDriveRows,
  type CommandBoardDriveOption,
} from "./command-board-future-drives";

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
  cells: Record<string, GridCell>;
}

interface AdministeredDateRange {
  min?: string | null;
  max?: string | null;
}

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

// The cohort ladder comes from the backend option group so the row set stays
// business-governed. The last rung is the catch-all: any live management stage that
// does not match an earlier rung folds into it.
function cohortBucket(managementStage: string, ladder: string[]): string {
  const stage = (managementStage || "").trim().toUpperCase();
  const fallback = ladder[ladder.length - 1] ?? "";
  for (const rung of ladder.slice(0, -1)) {
    const key = rung.toUpperCase();
    if (stage.startsWith(key) || stage.includes(key)) return rung;
  }
  return fallback;
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
  verified: Record<string, number>;
  administeredDates: Record<string, AdministeredDateRange>;
  // Per-vaccine day split and dose-sequence exceptions, both backend-owned. The grid shows the
  // exception COUNT (a clean 324 and a 321-with-3-missing must not read alike) and the drilldown
  // shows the days and the animals.
  days: Record<string, CohortDay[]>;
  exceptions: Record<string, { count: number; goats: CohortAnimal[] }>;
  // The real management stages that fold into this rung. They are CEO-level noise in the grid, so
  // they live in the drilldown only — the grid stays one row per cohort.
  members: CohortMember[];
}

interface CohortMember {
  label: string;
  animals: number;
  pending: Record<string, number>;
  submitted: Record<string, number>;
  verified: Record<string, number>;
  administeredDates: Record<string, AdministeredDateRange>;
  exceptions: Record<string, { count: number; goats: CohortAnimal[] }>;
}

type CohortDay = { date: string; animalCount: number };
type CohortAnimal = { goatId: string; displayId: string; tag?: string };

interface CohortCellInput {
  cohort: { parkName: string; managementStage: string; sex: string; animalCount: number };
  vaccineLabel: string;
  pendingCount: number;
  submittedCount: number;
  verifiedCount: number;
  minAdministeredDate?: string | null;
  maxAdministeredDate?: string | null;
  administeredDays?: CohortDay[];
  missingPriorDoseCount?: number;
  missingPriorDoseGoats?: CohortAnimal[];
}

// Day counts of the same vaccine coming from several (stage, sex) cohorts land on the same cohort
// row, so identical dates ADD rather than overwrite — otherwise "1 Jul: 237" would silently become
// whichever sub-cohort was folded last.
function mergeDays(target: Record<string, CohortDay[]>, vaccine: string, days?: CohortDay[]) {
  if (!days?.length) return;
  const list = target[vaccine] ?? [];
  days.forEach((day) => {
    const found = list.find((candidate) => candidate.date === day.date);
    if (found) found.animalCount += day.animalCount;
    else list.push({ date: day.date, animalCount: day.animalCount });
  });
  list.sort((a, b) => a.date.localeCompare(b.date));
  target[vaccine] = list;
}

function mergeExceptions(
  target: Record<string, { count: number; goats: CohortAnimal[] }>,
  vaccine: string,
  count?: number,
  goats?: CohortAnimal[],
) {
  if (!count) return;
  const current = target[vaccine] ?? { count: 0, goats: [] };
  current.count += count;
  (goats ?? []).forEach((goat) => {
    if (!current.goats.some((candidate) => candidate.goatId === goat.goatId)) current.goats.push(goat);
  });
  target[vaccine] = current;
}

// One matrix per FARM: leadership reads this farmwise, so Channapatna and Coimbatore never
// merge into one set of rows. Farms are ordered by name for a stable read.
function buildCohortFarms(
  matrix: CohortCellInput[],
  ladder: string[]
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
    .map(([farm, cells]) => ({ farm, ...buildCohortPivot(cells, ladder) }));
}

function buildCohortPivot(
  matrix: CohortCellInput[],
  ladder: string[]
): { vaccines: string[]; rows: CohortPivotRow[] } {
  const vaccines = Array.from(new Set(matrix.map((c) => c.vaccineLabel).filter(Boolean))).sort();
  const rows = ladder.map((cohort) => {
    const pending: Record<string, number> = {};
    const submitted: Record<string, number> = {};
    const verified: Record<string, number> = {};
    const administeredDates: Record<string, AdministeredDateRange> = {};
    const days: Record<string, CohortDay[]> = {};
    const exceptions: Record<string, { count: number; goats: CohortAnimal[] }> = {};
    const members = new Map<string, CohortMember>();
    // Animals are per (stage, sex) cohort and the source repeats a cohort once per vaccine, so
    // head counts accumulate per DISTINCT cohort key — summing the rows directly would multiply
    // the head count by the number of vaccines.
    const counted = new Set<string>();
    let animals = 0;
    matrix.forEach((cell) => {
      if (cohortBucket(cell.cohort.managementStage, ladder) !== cohort) return;
      const key = `${cell.cohort.managementStage}|${cell.cohort.sex}`;
      pending[cell.vaccineLabel] = (pending[cell.vaccineLabel] ?? 0) + cell.pendingCount;
      submitted[cell.vaccineLabel] = (submitted[cell.vaccineLabel] ?? 0) + (cell.submittedCount ?? 0);
      verified[cell.vaccineLabel] = (verified[cell.vaccineLabel] ?? 0) + cell.verifiedCount;
      mergeAdministeredDateRange(
        administeredDates,
        cell.vaccineLabel,
        cell.minAdministeredDate,
        cell.maxAdministeredDate,
      );
      mergeDays(days, cell.vaccineLabel, cell.administeredDays);
      mergeExceptions(exceptions, cell.vaccineLabel, cell.missingPriorDoseCount, cell.missingPriorDoseGoats);

      let member = members.get(key);
      if (!member) {
        member = {
          label: `${cell.cohort.managementStage} · ${cell.cohort.sex}`,
          animals: 0,
          pending: {},
          submitted: {},
          verified: {},
          administeredDates: {},
          exceptions: {},
        };
        members.set(key, member);
      }
      mergeExceptions(member.exceptions, cell.vaccineLabel, cell.missingPriorDoseCount, cell.missingPriorDoseGoats);
      member.pending[cell.vaccineLabel] = (member.pending[cell.vaccineLabel] ?? 0) + cell.pendingCount;
      member.submitted[cell.vaccineLabel] =
        (member.submitted[cell.vaccineLabel] ?? 0) + (cell.submittedCount ?? 0);
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
      verified,
      administeredDates,
      days,
      exceptions,
      members: Array.from(members.values()).sort((a, b) => b.animals - a.animals),
    };
  });
  return { vaccines, rows };
}

function buildShedGrid(
  matrix: Array<{
    shedName: string;
    doseRule: string;
    state: string;
    animalCount: number;
    minAdministeredDate?: string | null;
    maxAdministeredDate?: string | null;
    minDueDate?: string | null;
    maxDueDate?: string | null;
  }>
): {
  byDose: string[];
  byShed: ShedGridRow[];
} {
  const doseSet = new Set<string>();
  const shedMap = new Map<string, ShedGridRow>();

  matrix.forEach((cell) => {
    doseSet.add(cell.doseRule);
    if (!shedMap.has(cell.shedName)) {
      shedMap.set(cell.shedName, { shedName: cell.shedName, cells: {} });
    }
    shedMap.get(cell.shedName)!.cells[cell.doseRule] = {
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
    byDose: Array.from(doseSet),
    byShed: Array.from(shedMap.values()),
  };
}

// Derived from the generated client rather than hand-declared. Local mirrors of the response
// schema are why the compiler stayed green while closedWithoutDose and driveOptionsTruncated --
// both REQUIRED by the contract -- were dropped before they reached the render. Deriving makes the
// next dropped field a type error instead of a silent hole in the page.
type CommandBoardResponse = AppApiComponents["schemas"]["VaccinationCommandBoardResponse"];
type CohortCell = CommandBoardResponse["cohortMatrix"][number];
type ShedDoseCell = CommandBoardResponse["shedDoseMatrix"][number];
// driveOptions is the one field the view widens: enrichDriveOptions reconstructs counts the skinny
// API catalogue omits and tags them, so the rendered option carries more than the wire schema does.
type CommandBoard = Omit<CommandBoardResponse, "driveOptions"> & {
  driveOptions?: CommandBoardDriveOption[];
};

interface CommandBoardViewProps {
  board: CommandBoard;
  pageContract: AdminUiPageContract;
  driveBatchId?: string;
  // Park of the selected drive. The API's drive-option grain is (batch, park), so the batch id
  // alone does not identify a row once the same batch runs in two parks.
  driveParkId?: string;
}

const STATUS_KEYS = ["verified", "awaiting", "overdue", "scheduled"] as const;
type StatusKey = (typeof STATUS_KEYS)[number];

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
    const shedNames = Array.from(new Set(cells.map((cell) => cell.shedName).filter(Boolean))).sort();
    const doseCount = cells.reduce((sum, cell) => sum + (cell.animalCount ?? 0), 0);
    let targetCount = 0;
    if ((option.driveName || option.label).includes(" + ")) {
      const byShed = new Map<string, number>();
      cells.forEach((cell) => byShed.set(cell.shedName, Math.max(byShed.get(cell.shedName) ?? 0, cell.animalCount ?? 0)));
      targetCount = Array.from(byShed.values()).reduce((sum, count) => sum + count, 0);
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
  verified: number;
  dateSpan: string;
  days: CohortDay[];
  exceptionCount: number;
  exceptionGoats: CohortAnimal[];
  members: Array<{
    label: string;
    animals: number;
    pending: number;
    submitted: number;
    verified: number;
    exceptions: number;
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
  const driveOptions = useMemo(
    () => enrichDriveOptions(board.driveOptions ?? [], board.shedDoseMatrix ?? [], board.cohortMatrix ?? [], board.kpis.targets),
    [board.driveOptions, board.shedDoseMatrix, board.cohortMatrix, board.kpis.targets],
  );
  const futureDrives = useMemo(() => scheduledDriveRows(driveOptions), [driveOptions]);
  const completedDriveOptions = useMemo(
    () => driveOptions.filter((drive) => drive.status !== "planned"),
    [driveOptions],
  );

  const selectDrive = (next: string) => {
    const params = new URLSearchParams(searchParams?.toString() ?? "");
    const selection = next ? parseDriveSelectionValue(next) : undefined;
    if (selection?.driveBatchId) params.set("cb_drive", selection.driveBatchId); else params.delete("cb_drive");
    if (selection?.parkId) params.set("cb_drive_park", selection.parkId); else params.delete("cb_drive_park");
    const query = params.toString();
    router.push(query ? `?${query}` : "?", { scroll: false });
  };

  const vaccineOptions = useMemo(() => {
    const labels = (board.cohortMatrix ?? []).map((c) => c.vaccineLabel).filter(Boolean);
    return Array.from(new Set(labels)).sort();
  }, [board]);
  const [vaccine, setVaccine] = useState<string>("");
  const [statuses, setStatuses] = useState<Set<StatusKey>>(new Set(STATUS_KEYS));
  // Cell drilldown is client-local overlay state: the cohort row already carries its sub-cohorts,
  // so opening a cell must not re-run the route (local-overlay rule).
  const [selectedCell, setSelectedCell] = useState<SelectedCohortCell | null>(null);
  const futureCampaigns = useMemo(
    () => statuses.has("scheduled") ? scheduledDriveCampaigns(futureDrives) : [],
    [futureDrives, statuses],
  );

  const view = useMemo(() => {
    const matchesVaccine = (label?: string) => !vaccine || (label ?? "").startsWith(vaccine);
    return {
    ...board,
    shedDoseMatrix: (board.shedDoseMatrix ?? []).filter(
      (c) => matchesVaccine(c.doseRule) && statuses.has(c.state as StatusKey),
    ),
    cohortMatrix: (board.cohortMatrix ?? []).filter((c) => matchesVaccine(c.vaccineLabel)),
    verificationQueue: (board.verificationQueue ?? []).filter((r) => matchesVaccine(r.doseRule)),
    };
  }, [board, vaccine, statuses]);

  const toggleStatus = (key: StatusKey) => setStatuses((prev) => {
    const next = new Set(prev);
    if (next.has(key)) next.delete(key); else next.add(key);
    return next;
  });

  const filterBar = (
    <div className="cbm-filters">
      <div className="cbm-filter-row">
        <label className="cbm-filter-label" htmlFor="cbm-vaccine">
          {copy(pageContract, "command_board.filter.vaccine")}
        </label>
        <select id="cbm-vaccine" className="cbm-select" value={vaccine} onChange={(e) => setVaccine(e.target.value)}>
          <option value="">{copy(pageContract, "command_board.filter.all_vaccines")}</option>
          {vaccineOptions.map((v) => (
            <option key={v} value={v}>{v}</option>
          ))}
        </select>
        <label className="cbm-filter-label" htmlFor="cbm-drive">
          {copy(pageContract, "command_board.filter.operator_day")}
        </label>
        <select
          id="cbm-drive"
          className="cbm-select cbm-select-wide"
          value={driveBatchId ? driveSelectionValue(driveBatchId, driveParkId) : ""}
          onChange={(e) => selectDrive(e.target.value)}
          disabled={driveOptions.length === 0}
          aria-disabled={driveOptions.length === 0}
          title={driveOptions.length === 0 ? copy(pageContract, "command_board.filter.no_drives") : undefined}
        >
          <option value="">{copy(pageContract, "command_board.filter.all_common_drives")}</option>
          {futureCampaigns.map((campaign) => (
            <optgroup
              key={campaign.key}
              label={`${campaign.name} · ${formatScheduledDriveDates(campaign.dateKeys)} · ${campaign.targetCount} animals`}
            >
              {campaign.treatments.map((drive, index) => (
                  <option
                    key={driveSelectionValue(drive.batchIds[0] ?? drive.key, drive.parkId)}
                    value={driveSelectionValue(drive.batchIds[0] ?? drive.key, drive.parkId)}
                  >
                    {`Operator day ${index + 1} · ${formatScheduledDriveDates(drive.dateKeys)} · ${drive.targetCount} animals`}
                  </option>
                ))}
            </optgroup>
          ))}
          {completedDriveOptions.length > 0 && (
            <optgroup label={copy(pageContract, "command_board.filter.completed_history")}>
              {completedDriveOptions.map((drive) => (
                <option
                  key={driveSelectionValue(drive.driveBatchId, drive.parkId)}
                  value={driveSelectionValue(drive.driveBatchId, drive.parkId)}
                >
                  {`${commonDriveName(drive.driveName || drive.label, drive.parkName)} · ${formatDateSpan(drive.plannedDate, drive.plannedDate)} · ${drive.targetCount} animals`}
                </option>
              ))}
            </optgroup>
          )}
        </select>
        {/* The catalogue is bounded, so a drive past the bound is otherwise indistinguishable from a
            drive that was never planned. Say the picker is partial rather than let it read as the
            whole programme. */}
        {board.driveOptionsTruncated && (
          <span className="cbm-filter-note" role="status">
            {copy(pageContract, "command_board.filter.drives_truncated")}
          </span>
        )}
      </div>
      <div className="cbm-filter-row">
        {STATUS_KEYS.map((key) => (
          <button
            key={key}
            type="button"
            className={`cbm-chip cbm-chip-${key}${statuses.has(key) ? " cbm-chip-on" : ""}`}
            aria-pressed={statuses.has(key)}
            onClick={() => toggleStatus(key)}
          >
            <i />
            {copy(pageContract, `command_board.shed_matrix.state.${key}`)}
          </button>
        ))}
      </div>
    </div>
  );

  return (
    <section className="card cbm">
      <div className="hd">
        <h2>{copy(pageContract, "section.command_board.title")}</h2>
      </div>
      <div className="bd">
        {filterBar}
        {/* KPI Row - 5 cards with colored stripes */}
        <div className="cbm-kpi-row">
          <div className={`kpi mut ${view.kpis.targets > 0 ? "mut" : "mut"}`}>
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.targets")}</div>
            <div className="val">{view.kpis.targets}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.targets_dl")}</div>
          </div>
          <div className="kpi ok">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.verified")}</div>
            <div className="val">{view.kpis.dosesVerified}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.verified_dl")}</div>
          </div>
          <div className="kpi warn">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.awaiting_verification")}</div>
            <div className="val">{view.kpis.awaitingVerification}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.awaiting_dl")}</div>
          </div>
          <div className="kpi danger">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.overdue")}</div>
            <div className="val">{view.kpis.overdueNotGiven}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.overdue_dl")}</div>
          </div>
          <div className="kpi info">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.scheduled_ahead")}</div>
            <div className="val">{view.kpis.scheduledAhead}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.scheduled_dl")}</div>
          </div>
          {/* The five buckets are a disjoint, EXHAUSTIVE partition of targets. Rendering only four
              left the tiles summing to less than the total, so a reader could not tell a projection
              bug from animals whose obligations genuinely closed with no dose. */}
          <div className="kpi mut">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.closed_without_dose")}</div>
            <div className="val">{view.kpis.closedWithoutDose}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.closed_without_dose_dl")}</div>
          </div>
        </div>

        {/* Vaccine × Shed status - colored grid heatmap */}
        {view.shedDoseMatrix.length > 0 && (() => {
          const grid = buildShedGrid(view.shedDoseMatrix);
          // Queue age keyed by the same (shed, dose) grain the matrix cells use, so the number
          // lands on the cell it describes rather than being matched by position.
          const queueAgeDays = new Map<string, number>();
          (view.verificationQueue ?? []).forEach((q) => {
            if (q.daysInQueue !== undefined && q.daysInQueue !== null) {
              queueAgeDays.set(`${q.shedName}|${q.doseRule}`, q.daysInQueue);
            }
          });
          return (
            <div className="cbm-shed-section">
              <div className="cbm-section-head">
                <h3>{copy(pageContract, "command_board.shed_matrix.title")}</h3>
                <span className="cbm-meta">{copy(pageContract, "command_board.shed_matrix.meta")}</span>
              </div>
              <div className="cbm-hm">
                <table className="cbm-heat">
                  <thead>
                    <tr>
                      <th className="cbm-rowh">{copy(pageContract, "command_board.shed_matrix.column.shed")}</th>
                      {grid.byDose.map((dose) => (
                        <th key={dose}>{dose}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {grid.byShed.map((row) => (
                      <tr key={row.shedName}>
                        <th className="cbm-rowh">{row.shedName}</th>
                        {grid.byDose.map((dose) => {
                          const cell = row.cells[dose];
                          if (!cell) {
                            return <td key={dose} className="cbm-cell cbm-na">—</td>;
                          }
                          // Completed cells show the operator's actual administration date. Verification
                          // can happen days later and must never replace the medical date. Scheduled and
                          // overdue cells continue to show their rule-derived due date.
                          const dateStr = cell.state === "verified" || cell.state === "awaiting"
                            ? formatDateSpan(cell.minAdministeredDate, cell.maxAdministeredDate)
                            : formatDateSpan(cell.minDueDate, cell.maxDueDate);
                          // An awaiting cell also carries how long it has been sitting with the
                          // verifier — the one fact the removed queue table added.
                          const waiting = cell.state === "awaiting" ? queueAgeDays.get(`${row.shedName}|${dose}`) : undefined;
                          return (
                            <td
                              key={dose}
                              className={`cbm-cell cbm-${cell.state}`}
                              title={`${row.shedName} · ${cell.animalCount} animals${
                                waiting !== undefined ? ` · ${waiting}${copy(pageContract, "command_board.shed_matrix.waiting_suffix")}` : ""
                              }`}
                            >
                              {cell.animalCount}
                              <small>
                                {dateStr}
                                {waiting !== undefined ? ` · ${waiting}${copy(pageContract, "command_board.shed_matrix.waiting_suffix")}` : ""}
                              </small>
                            </td>
                          );
                        })}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div className="cbm-legend">
                <span><i></i>{copy(pageContract, "command_board.shed_matrix.legend.verified")}</span>
                <span><i></i>{copy(pageContract, "command_board.shed_matrix.legend.awaiting")}</span>
                <span><i></i>{copy(pageContract, "command_board.shed_matrix.legend.overdue")}</span>
                <span><i></i>{copy(pageContract, "command_board.shed_matrix.legend.scheduled")}</span>
                <span><i></i>{copy(pageContract, "command_board.shed_matrix.legend.not_scoped")}</span>
              </div>
            </div>
          );
        })()}

        {futureCampaigns.length > 0 && (
          <div className="cbm-future-section">
            <div className="cbm-section-head">
              <h3>{copy(pageContract, "command_board.future_drives.title")}</h3>
              <span className="cbm-meta">
                {futureCampaigns.length} {copy(pageContract, "command_board.future_drives.count_suffix")} · {futureDrives.length} {copy(pageContract, "command_board.future_drives.lines_suffix")}
              </span>
            </div>
            <div className="cbm-future-table-wrap">
              <table className="cbm-future-table">
                <thead>
                  <tr>
                    <th>{copy(pageContract, "command_board.future_drives.column.campaign")}</th>
                    <th>{copy(pageContract, "command_board.future_drives.column.drive")}</th>
                    <th>{copy(pageContract, "command_board.future_drives.column.dates")}</th>
                    <th>{copy(pageContract, "command_board.future_drives.column.sheds")}</th>
                    <th>{copy(pageContract, "command_board.future_drives.column.animals")}</th>
                    <th>{copy(pageContract, "command_board.future_drives.column.doses")}</th>
                  </tr>
                </thead>
                <tbody>
                  {futureCampaigns.flatMap((campaign) => campaign.treatments.map((drive, index) => (
                    <tr key={drive.key} className={driveBatchId && drive.batchIds.includes(driveBatchId) ? "is-selected" : undefined}>
                      {index === 0 && (
                        <td rowSpan={campaign.treatments.length} className="cbm-campaign-cell">
                          <strong>{campaign.name}</strong>
                          <small>
                            {campaign.targetCount} {copy(pageContract, "command_board.future_drives.campaign_animals")} · {campaign.doseCount} {copy(pageContract, "command_board.future_drives.campaign_doses")}
                          </small>
                        </td>
                      )}
                      <td><strong>{drive.driveName}</strong></td>
                      <td>{formatScheduledDriveDates(drive.dateKeys)}</td>
                      <td>{drive.shedNames.join(", ") || "—"}</td>
                      <td><strong>{drive.targetCount}</strong></td>
                      <td><strong>{drive.doseCount}</strong></td>
                    </tr>
                  )))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {/* Cohort matrix, FARMWISE: one table per farm, cohort ladder down the side, vaccines
            across the top, pending count in the cell (red when > 0) with the verified count
            beneath it so closure is readable without subtracting from the head count. */}
        {(() => {
          // MATCHING ladder (catch-all last) and READING order are different backend lists: the
          // Adults catch-all must stay last for bucketing, but the CEO reads Adults before the
          // not-adult cohorts.
          const ladder = optionGroup(pageContract, "command_board_cohort_ladder").map((o) => o.label);
          const readingOrder = cohortRowOrder(pageContract);
          const farms = buildCohortFarms(view.cohortMatrix, ladder).map((farmBlock) => ({
            ...farmBlock,
            rows: [...farmBlock.rows].sort((a, b) => {
              const ai = readingOrder.indexOf(a.cohort);
              const bi = readingOrder.indexOf(b.cohort);
              return (ai < 0 ? readingOrder.length : ai) - (bi < 0 ? readingOrder.length : bi);
            }),
          }));
          return (
            <div className="cbm-cohort-section">
              <div className="cbm-section-head">
                <h3>{copy(pageContract, "command_board.cohort_matrix.title")}</h3>
                <span className="cbm-meta">{copy(pageContract, "command_board.cohort_matrix.meta")}</span>
                <span className="cbm-meta cbm-cohort-hint">{copy(pageContract, "command_board.cohort_matrix.row_hint")}</span>
              </div>
              <div className="cbm-cohort-note">{copy(pageContract, "command_board.cohort_matrix.note")}</div>
              {farms.length === 0 ? (
                <div className="cbm-empty">{copy(pageContract, "command_board.cohort_matrix.empty")}</div>
              ) : (
                farms.map(({ farm, vaccines, rows }) => (
                  <div key={farm} className="cbm-farm-block">
                    <h4 className="cbm-farm-name">{farm || copy(pageContract, "command_board.cohort_matrix.no_farm")}</h4>
                    <div className="cbm-hm">
                      <table className="cbm-heat cbm-cohort-heat">
                        <thead>
                          <tr>
                            <th className="cbm-rowh">{copy(pageContract, "command_board.cohort_matrix.column.stage")}</th>
                            {vaccines.map((v) => (
                              <th key={v}>{v}</th>
                            ))}
                            <th>{copy(pageContract, "command_board.cohort_matrix.column.animals")}</th>
                          </tr>
                        </thead>
                        <tbody>
                          {rows.map((row) => {
                            // Three DISJOINT buckets, rendered together: the big number is what
                            // the OPERATOR still owes, and the sub-line carries what the VERIFIER
                            // owes (submitted) plus what is closed (verified). Showing pending
                            // alone made a fully vaccinated, fully submitted park read identically
                            // to an untouched one, and contradicted the "awaiting verification"
                            // KPI directly above this table.
                            const label = row.cohort;
                            const present = row.animals > 0;
                            const animals = row.animals;
                            const pendingOf = row.pending;
                            const submittedOf = row.submitted;
                            const verifiedOf = row.verified;
                            const administeredDatesOf = row.administeredDates;
                            const cells =
                              vaccines.map((v) => {
                                const pending = pendingOf[v];
                                if (!present || pending === undefined) {
                                  return <td key={v} className="cbm-cell cbm-na">—</td>;
                                }
                                const awaiting = submittedOf[v] ?? 0;
                                const done = verifiedOf[v] ?? 0;
                                // Nothing owed and nothing done: stay neutral rather than
                                // pretend work was completed. `awaiting` is part of the guard —
                                // a submitted-but-unverified cell is real work and must render.
                                if (pending === 0 && awaiting === 0 && done === 0) {
                                  return <td key={v} className="cbm-cell cbm-na">—</td>;
                                }
                                const pendingWord = copy(pageContract, "command_board.cohort_matrix.pending_word");
                                const submittedWord = copy(pageContract, "command_board.cohort_matrix.submitted_word");
                                const verifiedWord = copy(pageContract, "command_board.cohort_matrix.verified_word");
                                const administered = administeredDatesOf[v];
                                const administeredDate = formatDateSpan(administered?.min, administered?.max);
                                // The headline number is the count of the state the cell colour
                                // denotes, so colour and number can never disagree.
                                const headline = pending > 0 ? pending : awaiting > 0 ? awaiting : done;
                                // Actual medical dates belong to the VERIFIED doses only; show the
                                // honest "date unavailable" rather than borrowing the drive's
                                // planned date.
                                const dateSuffix = administeredDate
                                  ? ` · ${administeredDate}`
                                  : done > 0
                                    ? ` · ${copy(pageContract, "command_board.cohort_matrix.date_unavailable")}`
                                    : "";
                                const exception = row.exceptions[v];
                                const exceptionCount = exception?.count ?? 0;
                                const exceptionWord = copy(
                                  pageContract,
                                  exceptionCount === 1
                                    ? "command_board.cohort_matrix.exception_word_one"
                                    : "command_board.cohort_matrix.exception_word",
                                );
                                // Only the buckets that carry work are spelled out. A CEO cell that
                                // prints "0 pending · 0 submitted · 324 verified" makes the reader
                                // subtract zeroes to find the one fact that matters.
                                const parts: string[] = [];
                                if (pending > 0) parts.push(`${pending} ${pendingWord}`);
                                if (awaiting > 0) parts.push(`${awaiting} ${submittedWord}`);
                                if (done > 0) parts.push(`${done} ${verifiedWord}`);
                                const cellKey = `${farm}|${label}|${v}`;
                                const selection: SelectedCohortCell = {
                                  key: cellKey,
                                  farm,
                                  cohort: label,
                                  vaccine: v,
                                  animals,
                                  pending,
                                  submitted: awaiting,
                                  verified: done,
                                  dateSpan: administeredDate,
                                  days: row.days[v] ?? [],
                                  exceptionCount,
                                  exceptionGoats: exception?.goats ?? [],
                                  members: row.members.map((member) => ({
                                    label: member.label,
                                    animals: member.animals,
                                    pending: member.pending[v] ?? 0,
                                    submitted: member.submitted[v] ?? 0,
                                    verified: member.verified[v] ?? 0,
                                    exceptions: member.exceptions[v]?.count ?? 0,
                                    dateSpan: formatDateSpan(member.administeredDates[v]?.min, member.administeredDates[v]?.max),
                                  })),
                                };
                                // Colour follows who owes the next move; an exception rides ON TOP of
                                // that colour as its own chip, because "324 verified" and "321
                                // verified with 3 animals missing this dose" are different medical
                                // facts that must not render as the same green block.
                                return (
                                  <td
                                    key={v}
                                    className={`cbm-cell cbm-cohort-cell ${pending > 0 ? "cbm-pending" : awaiting > 0 ? "cbm-awaiting" : "cbm-clear"}${
                                      exceptionCount > 0 ? " cbm-cell-exception" : ""
                                    }${selectedCell?.key === cellKey ? " cbm-cell-on" : ""}`}
                                    title={`${label} · ${v} · ${pending} ${pendingWord}, ${awaiting} ${submittedWord}, ${done} ${verifiedWord}${dateSuffix}${
                                      exceptionCount > 0 ? ` · ${exceptionCount} ${exceptionWord}` : ""
                                    }`}
                                    role="button"
                                    tabIndex={0}
                                    aria-pressed={selectedCell?.key === cellKey}
                                    onClick={() => setSelectedCell((prev) => (prev?.key === cellKey ? null : selection))}
                                    onKeyDown={(e) => {
                                      if (e.key === "Enter" || e.key === " ") {
                                        e.preventDefault();
                                        setSelectedCell((prev) => (prev?.key === cellKey ? null : selection));
                                      }
                                    }}
                                  >
                                    <span className="cbm-cell-head">{headline}</span>
                                    <small>{parts.join(" · ")}</small>
                                    {administeredDate ? (
                                      <small className="cbm-cell-date">{administeredDate}</small>
                                    ) : done > 0 ? (
                                      <small className="cbm-cell-date">{copy(pageContract, "command_board.cohort_matrix.date_unavailable")}</small>
                                    ) : null}
                                    {exceptionCount > 0 ? (
                                      <span className="cbm-cell-exception-chip">
                                        {exceptionCount} {exceptionWord}
                                      </span>
                                    ) : null}
                                  </td>
                                );
                              });

                            return (
                              <tr key={`${farm}-${row.cohort}`}>
                                <th className="cbm-rowh">
                                  {row.cohort}
                                  {rowQualifier(pageContract, row.cohort) ? (
                                    <span className="cbm-rowh-note">{rowQualifier(pageContract, row.cohort)}</span>
                                  ) : null}
                                </th>
                                {cells}
                                <td className="cbm-cell cbm-na">{row.animals > 0 ? row.animals : "—"}</td>
                              </tr>
                            );
                          })}
                        </tbody>
                      </table>
                    </div>
                  </div>
                ))
              )}
              {selectedCell ? (
                <div className="cbm-cohort-detail">
                  <div className="cbm-cohort-detail-hd">
                    <b>
                      {selectedCell.farm || copy(pageContract, "command_board.cohort_matrix.no_farm")} · {selectedCell.cohort} ×{" "}
                      {selectedCell.vaccine}
                    </b>
                    <button type="button" className="btn sm" onClick={() => setSelectedCell(null)}>
                      {copy(pageContract, "command_board.cohort_matrix.detail.close")}
                    </button>
                  </div>
                  <div className="cbm-cohort-detail-grid">
                    <div>
                      <span className="k">{copy(pageContract, "command_board.cohort_matrix.detail.animals")}</span>
                      <span className="v">{selectedCell.animals}</span>
                    </div>
                    <div>
                      <span className="k">{copy(pageContract, "command_board.cohort_matrix.pending_word")}</span>
                      <span className="v">{selectedCell.pending}</span>
                    </div>
                    <div>
                      <span className="k">{copy(pageContract, "command_board.cohort_matrix.submitted_word")}</span>
                      <span className="v">{selectedCell.submitted}</span>
                    </div>
                    <div>
                      <span className="k">{copy(pageContract, "command_board.cohort_matrix.verified_word")}</span>
                      <span className="v">{selectedCell.verified}</span>
                    </div>
                    <div>
                      <span className="k">{copy(pageContract, "command_board.cohort_matrix.detail.dates")}</span>
                      <span className="v">
                        {selectedCell.dateSpan || copy(pageContract, "command_board.cohort_matrix.date_unavailable")}
                      </span>
                    </div>
                  </div>
                  <div className="cbm-cohort-detail-cols">
                    {/* The day story: which day the operator actually dosed how many animals. */}
                    <div className="cbm-cohort-detail-block">
                      <b>{copy(pageContract, "command_board.cohort_matrix.detail.per_day")}</b>
                      {selectedCell.days.length > 0 ? (
                        <ul className="cbm-daylist">
                          {selectedCell.days.map((day) => (
                            <li key={day.date}>
                              {/* Same farm-readable date wording the cell span uses ("30 Jun 2026"),
                                  never the stored ISO value. */}
                              <span className="d">{formatDateSpan(day.date, day.date)}</span>
                              <span className="n">{day.animalCount}</span>
                              <span className="u">{copy(pageContract, "command_board.cohort_matrix.detail.animals_word")}</span>
                            </li>
                          ))}
                        </ul>
                      ) : (
                        <span className="cbm-cohort-detail-muted">
                          {copy(pageContract, "command_board.cohort_matrix.date_unavailable")}
                        </span>
                      )}
                    </div>
                    {/* The exception story: animals whose later dose is accepted while THIS dose is
                        not. Named, so the CEO can hand the list to a park head. */}
                    <div className="cbm-cohort-detail-block">
                      <b>{copy(pageContract, "command_board.cohort_matrix.detail.exceptions")}</b>
                      {selectedCell.exceptionCount > 0 ? (
                        <>
                          <span className="cbm-cohort-detail-exception-count">{selectedCell.exceptionCount}</span>
                          <ul className="cbm-goatlist">
                            {selectedCell.exceptionGoats.map((goat) => (
                              <li key={goat.goatId}>
                                {goat.displayId}
                                {goat.tag ? <span className="t">{goat.tag}</span> : null}
                              </li>
                            ))}
                          </ul>
                          {selectedCell.exceptionCount > selectedCell.exceptionGoats.length ? (
                            <span className="cbm-cohort-detail-muted">
                              {copy(pageContract, "command_board.cohort_matrix.detail.capped")} {selectedCell.exceptionCount}
                            </span>
                          ) : null}
                        </>
                      ) : (
                        <span className="cbm-cohort-detail-muted">
                          {copy(pageContract, "command_board.cohort_matrix.detail.clean")}
                        </span>
                      )}
                    </div>
                  </div>
                  {selectedCell.members.length > 0 ? (
                    <table className="cbm-cohort-detail-table">
                      <thead>
                        <tr>
                          <th>{copy(pageContract, "command_board.cohort_matrix.detail.breakdown")}</th>
                          <th>{copy(pageContract, "command_board.cohort_matrix.column.animals")}</th>
                          <th>{copy(pageContract, "command_board.cohort_matrix.pending_word")}</th>
                          <th>{copy(pageContract, "command_board.cohort_matrix.submitted_word")}</th>
                          <th>{copy(pageContract, "command_board.cohort_matrix.verified_word")}</th>
                          <th>{copy(pageContract, "command_board.cohort_matrix.exception_word")}</th>
                          <th>{copy(pageContract, "command_board.cohort_matrix.detail.dates")}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {selectedCell.members.map((member) => (
                          <tr key={member.label}>
                            <td>{member.label}</td>
                            <td>{member.animals}</td>
                            <td>{member.pending}</td>
                            <td>{member.submitted}</td>
                            <td>{member.verified}</td>
                            <td>{member.exceptions}</td>
                            <td>{member.dateSpan || copy(pageContract, "command_board.cohort_matrix.date_unavailable")}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  ) : null}
                </div>
              ) : (
                <div className="cbm-cohort-detail cbm-cohort-detail-empty">
                  {copy(pageContract, "command_board.cohort_matrix.detail.empty")}
                </div>
              )}
            </div>
          );
        })()}
        {/* The verification queue used to render here as its own table, but every count in it
            (shed, dose, awaiting) is already an amber cell in the shed matrix above. Only the
            queue age was unique, so it now rides along in that cell and the duplicate table is
            gone. */}
      </div>
    </section>
  );

}
