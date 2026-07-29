import { Suspense } from "react";
import { getVaccinationCommandBoard } from "@/lib/api/server";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { copy } from "@/lib/admin-ui-contract";


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

interface VaccinationCommandBoardSkeletonProps {
  pageContract: AdminUiPageContract;
}

export function VaccinationCommandBoardSkeleton({ pageContract }: VaccinationCommandBoardSkeletonProps) {
  return (
    <section className="card cbm">
      <div className="hd">
        <h2>{copy(pageContract, "section.command_board.title")}</h2>
      </div>
      <div className="bd">
        <div className="cbm-loading">{copy(pageContract, "section.command_board.loading")}</div>
      </div>
    </section>
  );
}

interface VaccinationCommandBoardProps {
  pageContract: AdminUiPageContract;
}

async function VaccinationCommandBoardContent({ pageContract }: VaccinationCommandBoardProps) {
  const result = await getVaccinationCommandBoard();
  // telemetry: covered by parent /vaccination page-level Faro tracking
  if (!result.ok) {
    return (
      <section className="card cbm">
        <div className="hd">
          <h2>{copy(pageContract, "section.command_board.title")}</h2>
        </div>
        <div className="bd">
          <div className="cbm-unavailable">{copy(pageContract, "section.command_board.unavailable")}</div>
        </div>
      </section>
    );
  }

  const board = result.data;

  return (
    <section className="card cbm">
      <div className="hd">
        <h2>{copy(pageContract, "section.command_board.title")}</h2>
      </div>
      <div className="bd">
        {/* KPI Row - 5 cards with colored stripes */}
        <div className="cbm-kpi-row">
          <div className={`kpi mut ${board.kpis.targets > 0 ? "mut" : "mut"}`}>
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.targets")}</div>
            <div className="val">{board.kpis.targets}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.targets_dl")}</div>
          </div>
          <div className="kpi ok">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.verified")}</div>
            <div className="val">{board.kpis.dosesVerified}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.verified_dl")}</div>
          </div>
          <div className="kpi warn">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.awaiting_verification")}</div>
            <div className="val">{board.kpis.awaitingVerification}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.awaiting_dl")}</div>
          </div>
          <div className="kpi danger">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.overdue")}</div>
            <div className="val">{board.kpis.overdueNotGiven}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.overdue_dl")}</div>
          </div>
          <div className="kpi info">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.scheduled_ahead")}</div>
            <div className="val">{board.kpis.scheduledAhead}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.scheduled_dl")}</div>
          </div>
        </div>

        {/* Vaccine × Shed status - colored grid heatmap */}
        {board.shedDoseMatrix.length > 0 && (() => {
          const grid = buildShedGrid(board.shedDoseMatrix);
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

        {/* Cohort Matrix Table - conditional coloring on pending count */}
        {board.cohortMatrix.length > 0 && (
          <div className="cbm-cohort-section">
            <h3 className="cbm-cohort-title">{copy(pageContract, "command_board.cohort_matrix.title")}</h3>
            <div style={{ overflowX: "auto" }}>
              <table className="cbm-cohort-table">
                <thead>
                  <tr>
                    <th>{copy(pageContract, "command_board.cohort_matrix.column.stage")}</th>
                    <th>{copy(pageContract, "command_board.cohort_matrix.column.sex")}</th>
                    <th>{copy(pageContract, "command_board.cohort_matrix.column.animals")}</th>
                    <th>{copy(pageContract, "command_board.cohort_matrix.column.vaccine")}</th>
                    <th>{copy(pageContract, "command_board.cohort_matrix.column.pending")}</th>
                  </tr>
                </thead>
                <tbody>
                  {board.cohortMatrix.map((cell, idx) => (
                    <tr key={idx}>
                      <td>{cell.cohort.managementStage}</td>
                      <td>{cell.cohort.sex}</td>
                      <td className="cbm-cohort-num">{cell.cohort.animalCount}</td>
                      <td>{cell.vaccineLabel}</td>
                      <td className={`cbm-cohort-num ${cell.pendingCount > 0 ? "cbm-cohort-pending" : ""}`}>{cell.pendingCount}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {/* Weekly given - bar chart */}
        {board.weeklyGiven.length > 0 && (() => {
          // Group by week and get vaccine vaccines with colors
          const weekMap = new Map<string, Map<string, { count: number; status: string }>>();
          const vaccineSet = new Set<string>();

          board.weeklyGiven.forEach((row) => {
            const weekKey = `${row.isoYear}-W${String(row.isoWeek).padStart(2, "0")}`;
            if (!weekMap.has(weekKey)) {
              weekMap.set(weekKey, new Map());
            }
            weekMap.get(weekKey)!.set(row.vaccineLabel, { count: row.count, status: row.completionStatus });
            vaccineSet.add(row.vaccineLabel);
          });

          const weeks = Array.from(weekMap.keys());
          const vaccines = Array.from(vaccineSet);
          const maxCount = Math.max(...board.weeklyGiven.map((r) => r.count || 0), 100);

          return (
            <div className="cbm-weekly-section">
              <div className="cbm-section-head">
                <h3>{copy(pageContract, "command_board.weekly.title")}</h3>
                <span className="cbm-meta">{copy(pageContract, "command_board.weekly.meta")}</span>
              </div>
              <div className="cbm-chartbox">
                <svg className="cbm-chart" viewBox={`0 0 ${400 + weeks.length * 60} 250`} preserveAspectRatio="xMidYMid slice">
                  {/* Y-axis labels and grid lines */}
                  {[0, 50, 100, 150, 200].map((val) => (
                    <g key={`grid-${val}`}>
                      <line x1="40" x2={400 + weeks.length * 60} y1={220 - (val / maxCount) * 180} y2={220 - (val / maxCount) * 180} stroke="var(--line2)" strokeWidth="1" opacity="0.5" />
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
                        {vaccines.map((vax) => {
                          const data = vaccineData.get(vax);
                          const count = data?.count || 0;
                          const h = (count / maxCount) * 180;
                          const y = stackY - h;
                          // Awaiting-verification doses are always amber; verified doses take a
                          // stable palette slot by the vaccine's position in the response order,
                          // so a newly protocolled vaccine gets a colour without a code change
                          // (and no vaccine label is hardcoded in this renderer).
                          const color = data?.status === "recorded"
                            ? "var(--amber)"
                            : VACCINE_SERIES_COLORS[vaccines.indexOf(vax) % VACCINE_SERIES_COLORS.length];
                          const barX = 50 + widx * 60;
                          stackY = y;
                          return (
                            <g key={`${week}-${vax}`}>
                              <rect x={barX} y={y} width="36" height={Math.max(h, 1)} rx="3" fill={color} opacity={data?.status === "pending_verification" ? "0.8" : "1"} />
                              {h >= 30 && count > 0 && (
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
        {board.verificationQueue.length > 0 && (
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
                  {board.verificationQueue.map((row, idx) => (
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

export function VaccinationCommandBoard({ pageContract }: VaccinationCommandBoardProps) {
  return (
    <Suspense fallback={<VaccinationCommandBoardSkeleton pageContract={pageContract} />}>
      <VaccinationCommandBoardContent pageContract={pageContract} />
    </Suspense>
  );
}
