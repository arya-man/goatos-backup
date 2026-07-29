"use server";

import { Suspense } from "react";
import { getVaccinationCommandBoard } from "@/lib/api/server";
import type { VaccinationCommandBoardResponse } from "@/lib/api/vaccination-command-board";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";

export function VaccinationCommandBoardSkeleton() {
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <h2>Command Board</h2>
      </div>
      <div className="bd">
        <div style={{ padding: "16px", color: "#666" }}>Loading...</div>
      </div>
    </section>
  );
}

interface VaccinationCommandBoardProps {
  pageContract: AdminUiPageContract;
}

async function VaccinationCommandBoardContent({ pageContract }: VaccinationCommandBoardProps) {
  const result = await getVaccinationCommandBoard();
  if (!result.ok) {
    return (
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h2>Command Board</h2>
        </div>
        <div className="bd">
          <div style={{ padding: "16px", color: "#999" }}>Unable to load command board</div>
        </div>
      </section>
    );
  }

  const board = result.data;

  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <h2>Command Board</h2>
      </div>
      <div className="bd">
        {/* KPI Row */}
        <div className="kpi-row" style={{ display: "flex", gap: "16px", marginBottom: "24px", flexWrap: "wrap" }}>
          <div className="kpi" style={{ flex: 1, minWidth: "120px" }}>
            <div className="lab">Targets</div>
            <div className="val">{board.kpis.targets}</div>
          </div>
          <div className="kpi" style={{ flex: 1, minWidth: "120px" }}>
            <div className="lab">Verified</div>
            <div className="val" style={{ color: "#4CAF50" }}>{board.kpis.dosesVerified}</div>
          </div>
          <div className="kpi" style={{ flex: 1, minWidth: "120px" }}>
            <div className="lab">Awaiting Verification</div>
            <div className="val" style={{ color: "#FF9800" }}>{board.kpis.awaitingVerification}</div>
          </div>
          <div className="kpi" style={{ flex: 1, minWidth: "120px" }}>
            <div className="lab">Overdue</div>
            <div className="val" style={{ color: "#F44336" }}>{board.kpis.overdueNotGiven}</div>
          </div>
          <div className="kpi" style={{ flex: 1, minWidth: "120px" }}>
            <div className="lab">Scheduled Ahead</div>
            <div className="val">{board.kpis.scheduledAhead}</div>
          </div>
        </div>

        {/* Cohort Matrix Table */}
        {board.cohortMatrix.length > 0 && (
          <div style={{ marginBottom: "24px" }}>
            <h3 style={{ marginBottom: "12px", fontSize: "14px", fontWeight: "600" }}>Cohort Vaccine Matrix</h3>
            <table style={{ width: "100%", fontSize: "13px", borderCollapse: "collapse" }}>
              <thead>
                <tr style={{ borderBottom: "1px solid #e0e0e0", backgroundColor: "#f5f5f5" }}>
                  <th style={{ padding: "8px", textAlign: "left" }}>Stage</th>
                  <th style={{ padding: "8px", textAlign: "left" }}>Sex</th>
                  <th style={{ padding: "8px", textAlign: "right" }}>Animals</th>
                  <th style={{ padding: "8px", textAlign: "left" }}>Vaccine</th>
                  <th style={{ padding: "8px", textAlign: "right" }}>Pending</th>
                </tr>
              </thead>
              <tbody>
                {board.cohortMatrix.map((cell, idx) => (
                  <tr key={idx} style={{ borderBottom: "1px solid #f0f0f0" }}>
                    <td style={{ padding: "8px" }}>{cell.cohort.managementStage}</td>
                    <td style={{ padding: "8px" }}>{cell.cohort.sex}</td>
                    <td style={{ padding: "8px", textAlign: "right" }}>{cell.cohort.animalCount}</td>
                    <td style={{ padding: "8px" }}>{cell.vaccineLabel}</td>
                    <td style={{ padding: "8px", textAlign: "right", color: cell.pendingCount > 0 ? "#FF9800" : "#999" }}>
                      {cell.pendingCount}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {/* Verification Queue */}
        {board.verificationQueue.length > 0 && (
          <div>
            <h3 style={{ marginBottom: "12px", fontSize: "14px", fontWeight: "600" }}>Verification Queue</h3>
            <table style={{ width: "100%", fontSize: "13px", borderCollapse: "collapse" }}>
              <thead>
                <tr style={{ borderBottom: "1px solid #e0e0e0", backgroundColor: "#f5f5f5" }}>
                  <th style={{ padding: "8px", textAlign: "left" }}>Shed</th>
                  <th style={{ padding: "8px", textAlign: "left" }}>Dose</th>
                  <th style={{ padding: "8px", textAlign: "right" }}>Awaiting</th>
                  <th style={{ padding: "8px", textAlign: "right" }}>Total</th>
                  <th style={{ padding: "8px", textAlign: "right" }}>Days in Queue</th>
                </tr>
              </thead>
              <tbody>
                {board.verificationQueue.map((row, idx) => (
                  <tr key={idx} style={{ borderBottom: "1px solid #f0f0f0" }}>
                    <td style={{ padding: "8px" }}>{row.shedName}</td>
                    <td style={{ padding: "8px" }}>{row.doseRule}</td>
                    <td style={{ padding: "8px", textAlign: "right", color: "#FF9800" }}>{row.awaitingCount}</td>
                    <td style={{ padding: "8px", textAlign: "right" }}>{row.totalCount}</td>
                    <td style={{ padding: "8px", textAlign: "right" }}>{row.daysInQueue ?? "-"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </section>
  );
}

export function VaccinationCommandBoard({ pageContract }: VaccinationCommandBoardProps) {
  return (
    <Suspense fallback={<VaccinationCommandBoardSkeleton />}>
      <VaccinationCommandBoardContent pageContract={pageContract} />
    </Suspense>
  );
}
