"use client";
import { useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
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
  // The real management stages that fold into this rung, kept as their own sub-rows so the
  // ladder never hides the live detail — Adults still shows Non-Pregnant and Buck separately.
  members: Array<{
    label: string;
    animals: number;
    pending: Record<string, number>;
    submitted: Record<string, number>;
    verified: Record<string, number>;
    administeredDates: Record<string, AdministeredDateRange>;
  }>;
}

interface CohortCellInput {
  cohort: { parkName: string; managementStage: string; sex: string; animalCount: number };
  vaccineLabel: string;
  pendingCount: number;
  submittedCount: number;
  verifiedCount: number;
  minAdministeredDate?: string | null;
  maxAdministeredDate?: string | null;
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
    const members = new Map<
      string,
      {
        label: string;
        animals: number;
        pending: Record<string, number>;
        submitted: Record<string, number>;
        verified: Record<string, number>;
        administeredDates: Record<string, AdministeredDateRange>;
      }
    >();
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

      let member = members.get(key);
      if (!member) {
        member = {
          label: `${cell.cohort.managementStage} · ${cell.cohort.sex}`,
          animals: 0,
          pending: {},
          submitted: {},
          verified: {},
          administeredDates: {},
        };
        members.set(key, member);
      }
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

interface CommandBoardKpis {
  targets: number;
  dosesVerified: number;
  awaitingVerification: number;
  overdueNotGiven: number;
  scheduledAhead: number;
}
interface CohortCell {
  cohort: { parkId: string; parkName: string; managementStage: string; sex: string; animalCount: number };
  vaccineLabel: string;
  pendingCount: number;
  submittedCount: number;
  verifiedCount: number;
  minAdministeredDate?: string | null;
  maxAdministeredDate?: string | null;
}
interface ShedDoseCell {
  shedId?: string;
  shedName: string;
  doseRule: string;
  state: string;
  animalCount: number;
  minAdministeredDate?: string | null;
  maxAdministeredDate?: string | null;
  minDueDate?: string | null;
  maxDueDate?: string | null;
}
interface QueueRow {
  shedId?: string;
  shedName: string;
  doseRule: string;
  awaitingCount: number;
  totalCount: number;
  lastGivenOnDate?: string | null;
  daysInQueue?: number | null;
}
interface CommandBoard {
  kpis: CommandBoardKpis;
  cohortMatrix: CohortCell[];
  shedDoseMatrix: ShedDoseCell[];
  verificationQueue: QueueRow[];
  driveOptions?: CommandBoardDriveOption[];
}

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
          const ladder = optionGroup(pageContract, "command_board_cohort_ladder").map((o) => o.label);
          const farms = buildCohortFarms(view.cohortMatrix, ladder);
          return (
            <div className="cbm-cohort-section">
              <div className="cbm-section-head">
                <h3>{copy(pageContract, "command_board.cohort_matrix.title")}</h3>
                <span className="cbm-meta">{copy(pageContract, "command_board.cohort_matrix.meta")}</span>
              </div>
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
                          {rows.flatMap((row) => {
                            // Three DISJOINT buckets, rendered together: the big number is what
                            // the OPERATOR still owes, and the sub-line carries what the VERIFIER
                            // owes (submitted) plus what is closed (verified). Showing pending
                            // alone made a fully vaccinated, fully submitted park read identically
                            // to an untouched one, and contradicted the "awaiting verification"
                            // KPI directly above this table.
                            const cells = (
                              pendingOf: Record<string, number>,
                              submittedOf: Record<string, number>,
                              verifiedOf: Record<string, number>,
                              administeredDatesOf: Record<string, AdministeredDateRange>,
                              label: string,
                              present: boolean,
                            ) =>
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
                                return (
                                  <td
                                    key={v}
                                    className={`cbm-cell ${pending > 0 ? "cbm-pending" : awaiting > 0 ? "cbm-awaiting" : "cbm-clear"}`}
                                    title={`${label} · ${v} · ${pending} ${pendingWord}, ${awaiting} ${submittedWord}, ${done} ${verifiedWord}${dateSuffix}`}
                                  >
                                    {headline}
                                    <small>
                                      {awaiting} {submittedWord} · {done} {verifiedWord}{dateSuffix}
                                    </small>
                                  </td>
                                );
                              });

                            return [
                              <tr key={`${farm}-${row.cohort}`}>
                                <th className="cbm-rowh">{row.cohort}</th>
                                {cells(row.pending, row.submitted, row.verified, row.administeredDates, row.cohort, row.animals > 0)}
                                <td className="cbm-cell cbm-na">{row.animals > 0 ? row.animals : "—"}</td>
                              </tr>,
                              // The live stages inside this rung, so folding onto the ladder never
                              // hides the detail the herd actually carries.
                              ...(row.members.length > 1 || (row.members.length === 1 && row.members[0].label !== row.cohort)
                                ? row.members.map((member) => (
                                    <tr key={`${farm}-${row.cohort}-${member.label}`} className="cbm-cohort-sub">
                                      <th className="cbm-rowh cbm-rowh-sub">{member.label}</th>
                                      {cells(member.pending, member.submitted, member.verified, member.administeredDates, member.label, true)}
                                      <td className="cbm-cell cbm-na">{member.animals}</td>
                                    </tr>
                                  ))
                                : []),
                            ];
                          })}
                        </tbody>
                      </table>
                    </div>
                  </div>
                ))
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
