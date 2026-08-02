import { Suspense } from "react";
import { getVaccinationCommandBoard } from "@/lib/api/server";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { copy } from "@/lib/admin-ui-contract";
import { parseScope } from "@/lib/scope";
import type { RouteSearchParams } from "@/lib/search-params";
import { CommandBoardView } from "./command-board-view";
import { vaccinationCurrentViewScope } from "@/features/vaccination-sheds";

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
  searchParams?: RouteSearchParams;
  // Selected drive, carried in the URL so the narrowed board is a real server read rather
  // than a client-side slice of a wider payload.
  driveBatchId?: string;
}

async function VaccinationCommandBoardContent({ pageContract, searchParams, driveBatchId }: VaccinationCommandBoardProps) {
  let selectedDriveBatchId = driveBatchId;
  const { parkId } = vaccinationCurrentViewScope(parseScope(searchParams ?? {}));
  let result = await getVaccinationCommandBoard({ parkId });
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

  const driveOptions = result.data.driveOptions ?? [];
  const requestedDriveIsInScope = selectedDriveBatchId
    ? driveOptions.some((drive) => drive.driveBatchId === selectedDriveBatchId)
    : false;

  // No URL selection means the real all-drives board. Previously the page silently selected the
  // newest batch, which reduced a 324-animal future programme to one operator day's 95/109 rows.
  if (selectedDriveBatchId && !requestedDriveIsInScope) selectedDriveBatchId = undefined;

  if (selectedDriveBatchId) {
    const driveResult = await getVaccinationCommandBoard({ driveBatchId: selectedDriveBatchId, parkId });
    if (driveResult.ok) result = driveResult;
  }

  return <CommandBoardView board={result.data} pageContract={pageContract} driveBatchId={selectedDriveBatchId} />;
}

export function VaccinationCommandBoard({ pageContract, searchParams, driveBatchId }: VaccinationCommandBoardProps) {
  return (
    <Suspense
      key={`${driveBatchId ?? ""}:${parseScope(searchParams ?? {}).parkId ?? ""}`}
      fallback={<VaccinationCommandBoardSkeleton pageContract={pageContract} />}
    >
      <VaccinationCommandBoardContent pageContract={pageContract} searchParams={searchParams} driveBatchId={driveBatchId} />
    </Suspense>
  );
}
