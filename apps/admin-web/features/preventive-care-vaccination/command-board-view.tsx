"use client";
import { useMemo, useState } from "react";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { copy, optionGroup } from "@/lib/admin-ui-contract";


// Chart series palette (theme tokens only). Assigned by response order, never by
// vaccine name, so the renderer stays label-agnostic.
const VACCINE_SERIES_COLORS = ["var(--ok)", "var(--info)", "var(--warn)", "var(--brand)"] as const;

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
}

function buildCohortPivot(
  matrix: Array<{ cohort: { managementStage: string; animalCount: number }; vaccineLabel: string; pendingCount: number }>,
  ladder: string[]
): { vaccines: string[]; rows: CohortPivotRow[] } {
  const vaccines = Array.from(new Set(matrix.map((c) => c.vaccineLabel).filter(Boolean))).sort();
  const rows = ladder.map((cohort) => {
    const pending: Record<string, number> = {};
    // Animals are per (stage, sex) cohort, so summing the DISTINCT stage/sex pairs that
    // fold into this bucket avoids double counting the same cohort once per vaccine.
    const countedCohorts = new Set<string>();
    let animals = 0;
    matrix.forEach((cell) => {
      if (cohortBucket(cell.cohort.managementStage, ladder) !== cohort) return;
      pending[cell.vaccineLabel] = (pending[cell.vaccineLabel] ?? 0) + cell.pendingCount;
      if (!countedCohorts.has(cell.cohort.managementStage)) {
        countedCohorts.add(cell.cohort.managementStage);
        animals += cell.cohort.animalCount;
      }
    });
    return { cohort, animals, pending };
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
  cohort: { managementStage: string; sex: string; animalCount: number };
  vaccineLabel: string;
  pendingCount: number;
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
interface CommandBoard {
  kpis: CommandBoardKpis;
  cohortMatrix: CohortCell[];
  shedDoseMatrix: ShedDoseCell[];
  weeklyGiven: WeeklyRow[];
  verificationQueue: QueueRow[];
}

interface CommandBoardViewProps {
  board: CommandBoard;
  pageContract: AdminUiPageContract;
}

const STATUS_KEYS = ["verified", "awaiting", "overdue", "scheduled"] as const;
type StatusKey = (typeof STATUS_KEYS)[number];

export function CommandBoardView({ board, pageContract }: CommandBoardViewProps) {
  // Vaccine + status filters operate on the fetched payload: the board is one bounded
  // read, so narrowing it client-side keeps every card, matrix and chart consistent
  // without a refetch. Park/date scope stays with the shell top bar.
  const vaccineOptions = useMemo(() => {
    const labels = (board.cohortMatrix ?? []).map((c) => c.vaccineLabel).filter(Boolean);
    return Array.from(new Set(labels)).sort();
  }, [board]);
  const [vaccine, setVaccine] = useState<string>("");
  const [statuses, setStatuses] = useState<Set<StatusKey>>(new Set(STATUS_KEYS));


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
                          return (
                            <td key={dose} className={`cbm-cell cbm-${cell.state}`} title={`${row.shedName} · ${cell.animalCount} animals`}>
                              {cell.animalCount}
                              <small>{dateStr ? dateStr.slice(0, 10) : ""}</small>
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

        {/* Cohort matrix: fixed cohort ladder down the side, vaccines across the top,
            pending count in the cell, red when > 0. */}
        {(() => {
          const ladder = optionGroup(pageContract, "command_board_cohort_ladder").map((o) => o.label);
          const pivot = buildCohortPivot(view.cohortMatrix, ladder);
          return (
            <div className="cbm-cohort-section">
              <h3 className="cbm-cohort-title">{copy(pageContract, "command_board.cohort_matrix.title")}</h3>
              <div className="cbm-hm">
                <table className="cbm-heat cbm-cohort-heat">
                  <thead>
                    <tr>
                      <th className="cbm-rowh">{copy(pageContract, "command_board.cohort_matrix.column.stage")}</th>
                      {pivot.vaccines.map((v) => (
                        <th key={v}>{v}</th>
                      ))}
                      <th>{copy(pageContract, "command_board.cohort_matrix.column.animals")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pivot.rows.map((row) => (
                      <tr key={row.cohort}>
                        <th className="cbm-rowh">{row.cohort}</th>
                        {pivot.vaccines.map((v) => {
                          const pending = row.pending[v];
                          if (row.animals === 0 || pending === undefined) {
                            return <td key={v} className="cbm-cell cbm-na">—</td>;
                          }
                          return (
                            <td
                              key={v}
                              className={`cbm-cell ${pending > 0 ? "cbm-pending" : "cbm-clear"}`}
                              title={`${row.cohort} · ${v} · ${pending} pending`}
                            >
                              {pending}
                            </td>
                          );
                        })}
                        <td className="cbm-cell cbm-na">{row.animals > 0 ? row.animals : "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
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
                      <g key={week}>
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
              </div>
              <div className="cbm-legend">
                <span><i></i>{copy(pageContract, "command_board.weekly.legend.verified")}</span>
                <span><i></i>{copy(pageContract, "command_board.weekly.legend.pending")}</span>
              </div>
            </div>
          );
        })()}

        {/* Verification Queue - styled table with status pills */}
        {view.verificationQueue.length > 0 && (
          <div className="cbm-queue-section">
            <div className="cbm-section-head">
              <h3>{copy(pageContract, "command_board.verification_queue.title")}</h3>
              <span className="cbm-meta">{copy(pageContract, "command_board.verification_queue.meta")}</span>
            </div>
            <div style={{ overflowX: "auto" }}>
              <table className="cbm-queue-table">
                <thead>
                  <tr>
                    <th>{copy(pageContract, "command_board.verification_queue.column.shed")}</th>
                    <th>{copy(pageContract, "command_board.verification_queue.column.dose")}</th>
                    <th>{copy(pageContract, "command_board.verification_queue.column.awaiting")}</th>
                    <th>{copy(pageContract, "command_board.verification_queue.column.total")}</th>
                    <th>{copy(pageContract, "command_board.verification_queue.column.status")}</th>
                    <th>{copy(pageContract, "command_board.verification_queue.column.days")}</th>
                  </tr>
                </thead>
                <tbody>
                  {view.verificationQueue.map((row, idx) => (
                    <tr key={idx}>
                      <td className="cbm-celllink">{row.shedName}</td>
                      <td>{row.doseRule}</td>
                      <td className="cbm-queue-num">{row.awaitingCount}</td>
                      <td className="cbm-queue-num">{row.totalCount}</td>
                      <td>
                        <span className="cbm-pill cbm-pill-warn">{copy(pageContract, "command_board.verification_queue.status.awaiting")}</span>
                      </td>
                      <td className={`cbm-queue-num ${row.daysInQueue && row.daysInQueue > 0 ? "cbm-aging" : ""}`}>{row.daysInQueue ?? "-"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>
    </section>
  );

}
