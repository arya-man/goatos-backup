import { Suspense } from "react";
import { getVaccinationCommandBoard } from "@/lib/api/server";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { copy } from "@/lib/admin-ui-contract";
import { CommandBoardView } from "./command-board-view";

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

  return <CommandBoardView board={result.data} pageContract={pageContract} />;
}

export function VaccinationCommandBoard({ pageContract }: VaccinationCommandBoardProps) {
  return (
    <Suspense fallback={<VaccinationCommandBoardSkeleton pageContract={pageContract} />}>
      <VaccinationCommandBoardContent pageContract={pageContract} />
    </Suspense>
  );
}
