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

  // ONE request, whether or not a drive is selected.
  //
  // It used to be two, awaited in sequence: a wide read kept ONLY for its drive catalogue, then a
  // park-narrowed read for the numbers. That cost the sum of two ~1s server reads on every filter
  // change and built the endpoint's most expensive query — the catalogue — twice, discarding one
  // copy. The backend now takes the drive's park separately (drive_park_id), so the sections narrow
  // to the selected operator day while driveOptions stays at the top bar's scope and keeps offering
  // the other parks' drives.
  const result = await getVaccinationCommandBoard({
    parkId,
    driveBatchId,
    driveParkId: driveBatchId ? driveParkId : undefined,
  });
  // telemetry: covered by parent /vaccination page-level Faro tracking
  if (!result.ok) return <CommandBoardUnavailable pageContract={pageContract} />;

  const driveOptions = result.data.driveOptions ?? [];
  // No URL selection means the real all-drives board. Previously the page silently selected the
  // newest batch, which reduced a 324-animal future programme to one operator day's 95/109 rows.
  const selectedDrive = resolveSelectedDrive(driveOptions, driveBatchId, driveParkId);

  if (!selectedDrive) {
    return <CommandBoardView board={result.data} pageContract={pageContract} />;
  }

  // The catalogue can resolve a batch to a park the URL did not carry. When it does, the board just
  // rendered is scoped to the WRONG park (or to every park), and a board narrowed to the wrong park
  // is a wrong board rather than a slow one — so that one case re-reads. An in-app click always
  // carries the park, so this is the typed/stale-URL path, not the normal one.
  const resolvedParkId = selectedDrive.parkId || driveParkId || parkId;
  const driveResult =
    resolvedParkId === (driveParkId || parkId)
      ? result
      : await getVaccinationCommandBoard({
          parkId,
          driveBatchId: selectedDrive.driveBatchId,
          driveParkId: resolvedParkId,
        });
  // A FAILED narrowed read used to fall through to the still-loaded all-drives payload while the
  // selector kept showing the chosen drive: the heading named one operator day and every number
  // underneath was the whole programme's. A wrong number that looks right is worse than no number,
  // so the failure is surfaced instead of being dressed up as a successful narrow.
  if (!driveResult.ok) return <CommandBoardUnavailable pageContract={pageContract} />;

  return (
    <CommandBoardView
      // driveOptions travels on the SAME response now: the backend scopes the catalogue to the top
      // bar while narrowing the sections to the drive's park, so picking one park's drive can no
      // longer delete every other park's drive from the dropdown.
      board={{ ...driveResult.data, driveOptions }}
      pageContract={pageContract}
      driveBatchId={selectedDrive.driveBatchId}
      // The RESOLVED park (the catalogue's answer for this batch), not the URL's — the selector
      // must echo the park whose numbers are on screen.
      driveParkId={resolvedParkId}
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
