"use server";

import { Suspense } from "react";
import { getVaccinationCommandBoard } from "@/lib/api/server";
import type { VaccinationCommandBoardResponse } from "@/lib/api/vaccination-command-board";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { copy } from "@/lib/admin-ui-contract";

export function VaccinationCommandBoardSkeleton() {
  return (
    <section className="card cbm">
      <div className="hd">
        <h2>Command Board</h2>
      </div>
      <div className="bd">
        <div className="cbm-loading">Loading...</div>
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
        {/* KPI Row */}
        <div className="cbm-kpi-row">
          <div className="kpi cbm-kpi-targets">
            <div className="lab">{copy(pageContract, "command_board.kpi.targets")}</div>
            <div className="val">{board.kpis.targets}</div>
          </div>
          <div className="kpi cbm-kpi-verified">
            <div className="lab">{copy(pageContract, "command_board.kpi.verified")}</div>
            <div className="val">{board.kpis.dosesVerified}</div>
          </div>
          <div className="kpi cbm-kpi-awaiting">
            <div className="lab">{copy(pageContract, "command_board.kpi.awaiting_verification")}</div>
            <div className="val">{board.kpis.awaitingVerification}</div>
          </div>
          <div className="kpi cbm-kpi-overdue">
            <div className="lab">{copy(pageContract, "command_board.kpi.overdue")}</div>
            <div className="val">{board.kpis.overdueNotGiven}</div>
          </div>
          <div className="kpi cbm-kpi-scheduled">
            <div className="lab">{copy(pageContract, "command_board.kpi.scheduled_ahead")}</div>
            <div className="val">{board.kpis.scheduledAhead}</div>
          </div>
        </div>

        {/* Cohort Matrix Table */}
        {board.cohortMatrix.length > 0 && (
          <div className="cbm-cohort-section">
            <h3 className="cbm-cohort-title">{copy(pageContract, "command_board.cohort_matrix.title")}</h3>
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
                    <td className="cbm-cohort-number">{cell.cohort.animalCount}</td>
                    <td>{cell.vaccineLabel}</td>
                    <td className={`cbm-cohort-number ${cell.pendingCount > 0 ? "cbm-cohort-pending-active" : "cbm-cohort-pending-none"}`}>
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
          <div className="cbm-queue-section">
            <h3 className="cbm-queue-title">{copy(pageContract, "command_board.verification_queue.title")}</h3>
            <table className="cbm-queue-table">
              <thead>
                <tr>
                  <th>{copy(pageContract, "command_board.verification_queue.column.shed")}</th>
                  <th>{copy(pageContract, "command_board.verification_queue.column.dose")}</th>
                  <th>{copy(pageContract, "command_board.verification_queue.column.awaiting")}</th>
                  <th>{copy(pageContract, "command_board.verification_queue.column.total")}</th>
                  <th>{copy(pageContract, "command_board.verification_queue.column.days")}</th>
                </tr>
              </thead>
              <tbody>
                {board.verificationQueue.map((row, idx) => (
                  <tr key={idx}>
                    <td>{row.shedName}</td>
                    <td>{row.doseRule}</td>
                    <td className="cbm-queue-awaiting">{row.awaitingCount}</td>
                    <td className="cbm-queue-number">{row.totalCount}</td>
                    <td className="cbm-queue-number">{row.daysInQueue ?? "-"}</td>
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
