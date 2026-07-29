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
  // Selected drive, carried in the URL so the narrowed board is a real server read rather
  // than a client-side slice of a wider payload.
  driveBatchId?: string;
}

async function VaccinationCommandBoardContent({ pageContract, driveBatchId }: VaccinationCommandBoardProps) {
  let selectedDriveBatchId = driveBatchId;
  let result = await getVaccinationCommandBoard({ driveBatchId: selectedDriveBatchId });
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

  if (!selectedDriveBatchId && result.data.driveOptions?.length) {
    selectedDriveBatchId = result.data.driveOptions[0].driveBatchId;
    const driveResult = await getVaccinationCommandBoard({ driveBatchId: selectedDriveBatchId });
    if (driveResult.ok) result = driveResult;
  }

  return <CommandBoardView board={result.data} pageContract={pageContract} driveBatchId={selectedDriveBatchId} />;
}

export function VaccinationCommandBoard({ pageContract, driveBatchId }: VaccinationCommandBoardProps) {
  return (
    <Suspense
      key={driveBatchId ?? ""}
      fallback={<VaccinationCommandBoardSkeleton pageContract={pageContract} />}
    >
      <VaccinationCommandBoardContent pageContract={pageContract} driveBatchId={driveBatchId} />
    </Suspense>
  );
}
