"use client";
import { useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { copy, optionGroup } from "@/lib/admin-ui-contract";


// Chart series palette (theme tokens only). Assigned by response order, never by
// vaccine name, so the renderer stays label-agnostic.
const VACCINE_SERIES_COLORS = ["var(--ok)", "var(--info)", "var(--warn)", "var(--brand)"] as const;

// Top of the weekly plot area; bars are drawn from y=220 upward over 180px.
const PLOT_TOP = 40;

// Build colored grid heatmap from flat shed-dose matrix
interface GridCell {
  doseRule: string;
  state: string;
  animalCount: number;
  minDueDate?: string | null;
  maxDueDate?: string | null;
}

interface ShedGridRow {
  shedName: string;
  cells: Record<string, GridCell>;
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
  pending: Record<string, number>;
  verified: Record<string, number>;
  // The real management stages that fold into this rung, kept as their own sub-rows so the
  // ladder never hides the live detail — Adults still shows Non-Pregnant and Buck separately.
  members: Array<{ label: string; animals: number; pending: Record<string, number>; verified: Record<string, number> }>;
}

interface CohortCellInput {
  cohort: { parkName: string; managementStage: string; sex: string; animalCount: number };
  vaccineLabel: string;
  pendingCount: number;
  verifiedCount: number;
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
    const verified: Record<string, number> = {};
    const members = new Map<
      string,
      { label: string; animals: number; pending: Record<string, number>; verified: Record<string, number> }
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
      verified[cell.vaccineLabel] = (verified[cell.vaccineLabel] ?? 0) + cell.verifiedCount;

      let member = members.get(key);
      if (!member) {
        member = {
          label: `${cell.cohort.managementStage} · ${cell.cohort.sex}`,
          animals: 0,
          pending: {},
          verified: {},
        };
        members.set(key, member);
      }
      member.pending[cell.vaccineLabel] = (member.pending[cell.vaccineLabel] ?? 0) + cell.pendingCount;
      member.verified[cell.vaccineLabel] = (member.verified[cell.vaccineLabel] ?? 0) + cell.verifiedCount;

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
      verified,
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
  verifiedCount: number;
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
interface WeeklyRow {
  isoYear: number;
  isoWeek: number;
  vaccineLabel: string;
  completionStatus: string;
  count: number;
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
interface DriveOption {
  driveBatchId: string;
  label: string;
  status: string;
}
interface CommandBoard {
  kpis: CommandBoardKpis;
  cohortMatrix: CohortCell[];
  shedDoseMatrix: ShedDoseCell[];
  weeklyGiven: WeeklyRow[];
  verificationQueue: QueueRow[];
  driveOptions?: DriveOption[];
}

interface CommandBoardViewProps {
  board: CommandBoard;
  pageContract: AdminUiPageContract;
  driveBatchId?: string;
}

const STATUS_KEYS = ["verified", "awaiting", "overdue", "scheduled"] as const;
type StatusKey = (typeof STATUS_KEYS)[number];

export function CommandBoardView({ board, pageContract, driveBatchId }: CommandBoardViewProps) {
  // Vaccine + status filters operate on the fetched payload: the board is one bounded
  // read, so narrowing it client-side keeps every card, matrix and chart consistent
  // without a refetch. Drive scope is a server read (the drive selects which obligations
  // exist at all, which no client-side slice can reproduce). Park/date scope stays with
  // the shell top bar, which already owns it.
  const router = useRouter();
  const searchParams = useSearchParams();
  const driveOptions = board.driveOptions ?? [];

  const selectDrive = (next: string) => {
    if (!next) return;
    const params = new URLSearchParams(searchParams?.toString() ?? "");
    params.set("cb_drive", next);
    const query = params.toString();
    router.push(query ? `?${query}` : "?", { scroll: false });
  };

  const vaccineOptions = useMemo(() => {
    const labels = (board.cohortMatrix ?? []).map((c) => c.vaccineLabel).filter(Boolean);
    return Array.from(new Set(labels)).sort();
  }, [board]);
  const [vaccine, setVaccine] = useState<string>("");
  const [statuses, setStatuses] = useState<Set<StatusKey>>(new Set(STATUS_KEYS));
  // Hovered ISO week for the weekly chart tooltip.
  const [hoverWeek, setHoverWeek] = useState<string | null>(null);


  const view = useMemo(() => {
    const matchesVaccine = (label?: string) => !vaccine || (label ?? "").startsWith(vaccine);
    return {
    ...board,
    shedDoseMatrix: (board.shedDoseMatrix ?? []).filter(
      (c) => matchesVaccine(c.doseRule) && statuses.has(c.state as StatusKey),
    ),
    cohortMatrix: (board.cohortMatrix ?? []).filter((c) => matchesVaccine(c.vaccineLabel)),
    weeklyGiven: (board.weeklyGiven ?? []).filter((r) => matchesVaccine(r.vaccineLabel)),
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
          {copy(pageContract, "command_board.filter.drive")}
        </label>
        <select
          id="cbm-drive"
          className="cbm-select cbm-select-wide"
          value={driveBatchId ?? ""}
          onChange={(e) => selectDrive(e.target.value)}
          disabled={driveOptions.length === 0}
          aria-disabled={driveOptions.length === 0}
          title={driveOptions.length === 0 ? copy(pageContract, "command_board.filter.no_drives") : undefined}
        >
          {driveOptions.map((d) => (
            <option key={d.driveBatchId} value={d.driveBatchId}>{d.label}</option>
          ))}
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
                          const dateStr =
                            cell.state === "verified" || cell.state === "awaiting" ? cell.minDueDate || cell.maxDueDate : cell.minDueDate;
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
                                {dateStr ? dateStr.slice(0, 10) : ""}
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
                            const cells = (
                              pendingOf: Record<string, number>,
                              verifiedOf: Record<string, number>,
                              label: string,
                              present: boolean,
                            ) =>
                              vaccines.map((v) => {
                                const pending = pendingOf[v];
                                if (!present || pending === undefined) {
                                  return <td key={v} className="cbm-cell cbm-na">—</td>;
                                }
                                const done = verifiedOf[v] ?? 0;
                                return (
                                  <td
                                    key={v}
                                    className={`cbm-cell ${pending > 0 ? "cbm-pending" : "cbm-clear"}`}
                                    title={`${label} · ${v} · ${pending} ${copy(pageContract, "command_board.cohort_matrix.pending_word")}, ${done} ${copy(pageContract, "command_board.cohort_matrix.verified_word")}`}
                                  >
                                    {pending}
                                    <small>
                                      {done} {copy(pageContract, "command_board.cohort_matrix.verified_word")}
                                    </small>
                                  </td>
                                );
                              });

                            return [
                              <tr key={`${farm}-${row.cohort}`}>
                                <th className="cbm-rowh">{row.cohort}</th>
                                {cells(row.pending, row.verified, row.cohort, row.animals > 0)}
                                <td className="cbm-cell cbm-na">{row.animals > 0 ? row.animals : "—"}</td>
                              </tr>,
                              // The live stages inside this rung, so folding onto the ladder never
                              // hides the detail the herd actually carries.
                              ...(row.members.length > 1 || (row.members.length === 1 && row.members[0].label !== row.cohort)
                                ? row.members.map((member) => (
                                    <tr key={`${farm}-${row.cohort}-${member.label}`} className="cbm-cohort-sub">
                                      <th className="cbm-rowh cbm-rowh-sub">{member.label}</th>
                                      {cells(member.pending, member.verified, member.label, true)}
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

        {/* Weekly given - bar chart */}
        {view.weeklyGiven.length > 0 && (() => {
          // One series per (vaccine, completion status) pair. Keying by vaccine alone
          // dropped rows: a week holding both verified and awaiting doses of the SAME
          // vaccine kept only the last row (e.g. 114 verified ET+TT vanished behind 210
          // awaiting ET+TT in the same ISO week).
          const seriesKey = (vaccineLabel: string, status: string) => `${vaccineLabel}\u0000${status}`;
          const weekMap = new Map<string, Map<string, { count: number; status: string; vaccineLabel: string }>>();
          const weekOrder = new Map<string, number>();
          const seriesSet = new Map<string, { vaccineLabel: string; status: string }>();

          view.weeklyGiven.forEach((row) => {
            const weekKey = `${row.isoYear}-W${String(row.isoWeek).padStart(2, "0")}`;
            weekOrder.set(weekKey, row.isoYear * 100 + row.isoWeek);
            if (!weekMap.has(weekKey)) weekMap.set(weekKey, new Map());
            const key = seriesKey(row.vaccineLabel, row.completionStatus);
            const bucket = weekMap.get(weekKey)!;
            const existing = bucket.get(key);
            // same (week, vaccine, status) can legitimately arrive split across doses
            bucket.set(key, {
              count: (existing?.count ?? 0) + (row.count || 0),
              status: row.completionStatus,
              vaccineLabel: row.vaccineLabel,
            });
            if (!seriesSet.has(key)) seriesSet.set(key, { vaccineLabel: row.vaccineLabel, status: row.completionStatus });
          });

          // Chronological left-to-right; the backend returns newest-first for tables.
          const weeks = Array.from(weekMap.keys()).sort((a, b) => (weekOrder.get(a) ?? 0) - (weekOrder.get(b) ?? 0));
          // Verified series first so awaiting always stacks on top of its own vaccine.
          const series = Array.from(seriesSet.entries())
            .sort((a, b) => (a[1].status === b[1].status ? a[1].vaccineLabel.localeCompare(b[1].vaccineLabel) : a[1].status === "accepted" ? -1 : 1))
            .map(([key, meta]) => ({ key, ...meta }));
          const distinctVaccines = Array.from(new Set(view.weeklyGiven.map((r) => r.vaccineLabel)));
          // Axis must fit the tallest STACK, not the tallest single series.
          const maxCount = Math.max(
            ...weeks.map((w) => Array.from(weekMap.get(w)?.values() ?? []).reduce((sum, d) => sum + d.count, 0)),
            10,
          );
          const axisStep = Math.max(10, Math.ceil(maxCount / 4 / 10) * 10);
          const axisTicks: number[] = [];
          for (let t = 0; t <= maxCount; t += axisStep) axisTicks.push(t);

          return (
            <div className="cbm-weekly-section">
              <div className="cbm-section-head">
                <h3>{copy(pageContract, "command_board.weekly.title")}</h3>
                <span className="cbm-meta">{copy(pageContract, "command_board.weekly.meta")}</span>
              </div>
              <div className="cbm-chartbox">
                {/* Width tracks the week count so a 4-week chart does not carry ~400px of
                    dead space, and the aspect ratio is preserved with "meet" — "slice"
                    scales to FILL the box and crops, which inflated the plot height. */}
                <svg
                  className="cbm-chart"
                  viewBox={`0 0 ${70 + weeks.length * 60} 250`}
                  width={70 + weeks.length * 60}
                  height={250}
                  preserveAspectRatio="xMinYMid meet"
                >
                  {/* Y-axis labels and grid lines */}
                  {axisTicks.map((val) => (
                    <g key={`grid-${val}`}>
                      <line x1="40" x2={60 + weeks.length * 60} y1={220 - (val / maxCount) * 180} y2={220 - (val / maxCount) * 180} stroke="var(--line2)" strokeWidth="1" opacity="0.5" />
                      <text x="35" y={223 - (val / maxCount) * 180} textAnchor="end" fontSize="10" fill="var(--faint)">
                        {val}
                      </text>
                    </g>
                  ))}
                  {/* Bars */}
                  {weeks.map((week, widx) => {
                    const vaccineData = weekMap.get(week) || new Map();
                    let stackY = 220;
                    return (
                      <g
                        key={week}
                        onMouseEnter={() => setHoverWeek(week)}
                        onMouseLeave={() => setHoverWeek(null)}
                      >
                        {/* Full-height capture band: the hit target must cover the whole column,
                            not just the drawn bar, or thin/zero segments are unhoverable. */}
                        <rect x={50 + widx * 60 - 6} y={PLOT_TOP} width="48" height={220 - PLOT_TOP} fill="transparent" />
                        {series.map((s) => {
                          const data = vaccineData.get(s.key);
                          const count = data?.count || 0;
                          if (count === 0) return null;
                          const h = (count / maxCount) * 180;
                          const y = stackY - h;
                          // Amber is RESERVED for awaiting-verification, so it can never be
                          // confused with a verified vaccine series. Verified doses take a
                          // palette slot by the vaccine's position in the response order, so a
                          // newly protocolled vaccine renders without a code change and no
                          // vaccine label is hardcoded in this renderer.
                          const color = s.status === "recorded"
                            ? "var(--amber)"
                            : VACCINE_SERIES_COLORS[distinctVaccines.indexOf(s.vaccineLabel) % VACCINE_SERIES_COLORS.length];
                          const barX = 50 + widx * 60;
                          stackY = y;
                          return (
                            <g key={`${week}-${s.key}`}>
                              <rect x={barX} y={y} width="36" height={Math.max(h, 2)} rx="3" fill={color}>
                                <title>{`${s.vaccineLabel} - ${count}`}</title>
                              </rect>
                              {h >= 22 && count > 0 && (
                                <text x={barX + 18} y={y + h / 2 + 3} textAnchor="middle" fontSize="10" fontWeight="700" fill="var(--ink)" opacity="0.8">
                                  {count}
                                </text>
                              )}
                            </g>
                          );
                        })}
                        <text x={50 + widx * 60 + 18} y="240" textAnchor="middle" fontSize="10" fill="var(--muted)">
                          {week}
                        </text>
                      </g>
                    );
                  })}
                </svg>
                {hoverWeek
                  ? (() => {
                      const bucket = weekMap.get(hoverWeek);
                      const lines = series
                        .map((s) => ({ s, data: bucket?.get(s.key) }))
                        .filter((entry) => (entry.data?.count ?? 0) > 0);
                      const total = lines.reduce((sum, entry) => sum + (entry.data?.count ?? 0), 0);
                      return (
                        <div className="cbm-tip" role="status">
                          <b>{hoverWeek}</b>
                          {lines.map((entry) => (
                            <div key={entry.s.key} className="cbm-tip-row">
                              <span>
                                <i
                                  style={{
                                    background:
                                      entry.s.status === "recorded"
                                        ? "var(--amber)"
                                        : VACCINE_SERIES_COLORS[
                                            distinctVaccines.indexOf(entry.s.vaccineLabel) % VACCINE_SERIES_COLORS.length
                                          ],
                                  }}
                                />
                                {entry.s.vaccineLabel} ·{" "}
                                {copy(
                                  pageContract,
                                  entry.s.status === "recorded"
                                    ? "command_board.weekly.legend.pending"
                                    : "command_board.weekly.legend.verified",
                                )}
                              </span>
                              <b>{entry.data?.count ?? 0}</b>
                            </div>
                          ))}
                          <div className="cbm-tip-row cbm-tip-total">
                            <span>{copy(pageContract, "command_board.weekly.tooltip_total")}</span>
                            <b>{total}</b>
                          </div>
                        </div>
                      );
                    })()
                  : null}
              </div>
              <div className="cbm-legend">
                <span><i></i>{copy(pageContract, "command_board.weekly.legend.verified")}</span>
                <span><i></i>{copy(pageContract, "command_board.weekly.legend.pending")}</span>
              </div>
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
