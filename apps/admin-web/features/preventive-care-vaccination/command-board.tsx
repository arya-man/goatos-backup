import { Suspense } from "react";
import { getVaccinationCommandBoard } from "@/lib/api/server";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { copy } from "@/lib/admin-ui-contract";
import { parseScope } from "@/lib/scope";
import type { RouteSearchParams } from "@/lib/search-params";
import { CommandBoardView } from "./command-board-view";
import { resolveSelectedDrive } from "./command-board-future-drives";
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
  // than a client-side slice of a wider payload. Park travels with the batch because the
  // API's drive-option grain is (batch, park), not batch alone.
  driveBatchId?: string;
  driveParkId?: string;
}

function CommandBoardUnavailable({ pageContract }: VaccinationCommandBoardSkeletonProps) {
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

async function VaccinationCommandBoardContent({
  pageContract,
  searchParams,
  driveBatchId,
  driveParkId,
}: VaccinationCommandBoardProps) {
  const { parkId } = vaccinationCurrentViewScope(parseScope(searchParams ?? {}));
  const result = await getVaccinationCommandBoard({ parkId });
  // telemetry: covered by parent /vaccination page-level Faro tracking
  if (!result.ok) return <CommandBoardUnavailable pageContract={pageContract} />;

  const driveOptions = result.data.driveOptions ?? [];
  // No URL selection means the real all-drives board. Previously the page silently selected the
  // newest batch, which reduced a 324-animal future programme to one operator day's 95/109 rows.
  const selectedDrive = resolveSelectedDrive(driveOptions, driveBatchId, driveParkId);

  if (!selectedDrive) {
    return <CommandBoardView board={result.data} pageContract={pageContract} />;
  }

  const driveResult = await getVaccinationCommandBoard({
    driveBatchId: selectedDrive.driveBatchId,
    parkId: selectedDrive.parkId || parkId,
  });
  // A FAILED narrowed read used to fall through to the still-loaded all-drives payload while the
  // selector kept showing the chosen drive: the heading named one operator day and every number
  // underneath was the whole programme's. A wrong number that looks right is worse than no number,
  // so the failure is surfaced instead of being dressed up as a successful narrow.
  if (!driveResult.ok) return <CommandBoardUnavailable pageContract={pageContract} />;

  return (
    <CommandBoardView
      // The narrowed read is park-scoped, so ITS driveOptions only cover the selected drive's park.
      // The option list is a selector catalogue rather than board data, so it keeps the wider
      // scope's list -- otherwise picking one park's drive would delete every other park's drive
      // from the dropdown and strand the user on that park.
      board={{ ...driveResult.data, driveOptions }}
      pageContract={pageContract}
      driveBatchId={selectedDrive.driveBatchId}
      driveParkId={selectedDrive.parkId ?? ""}
    />
  );
}

export function VaccinationCommandBoard({ pageContract, searchParams, driveBatchId, driveParkId }: VaccinationCommandBoardProps) {
  return (
    <Suspense
      key={`${driveBatchId ?? ""}:${driveParkId ?? ""}:${parseScope(searchParams ?? {}).parkId ?? ""}`}
      fallback={<VaccinationCommandBoardSkeleton pageContract={pageContract} />}
    >
      <VaccinationCommandBoardContent
        pageContract={pageContract}
        searchParams={searchParams}
        driveBatchId={driveBatchId}
        driveParkId={driveParkId}
      />
    </Suspense>
  );
}
