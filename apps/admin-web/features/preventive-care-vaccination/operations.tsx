import { Suspense } from "react";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseScope } from "@/lib/scope";
import { VaccinationCommandBoard } from "./command-board";
import { VaccinationFullScheduleButton } from "./vaccination-action-dialogs";
import { VaccinationFullSchedule, VaccinationFullScheduleSkeleton, vaccinationScheduleYear } from "./full-vaccine-schedule";
import { VaccinationCommandBoard, VaccinationCommandBoardSkeleton } from "./command-board";

// Preventive Care (PC) · Vaccination — the SHED-WISE operations floor:
//   header (SOP · Full Schedule) → drive-mechanic band (Target → Group → Route → Execute)
//   → shed-wise vaccination table (one row per shed, animal-level due/done, planned sessions, capacity,
//     merged status), which links to the shed detail at /vaccination/execution/sheds/{shed_id}.
// The old cohort/vaccine-wise status matrix + per-cohort detail + drive shed-event board are replaced by
// the shed-wise table per docs/runbooks/staging-vaccination-seed-preflight.md — no cohort/vaccine-wise
// rows appear on the main page. Command lenses (Control Tower / Action Center / Protocol Adherence /
// Workflows) stay top-level; this screen does not embed or shortcut them. Park scope comes from the shell
// top bar (?park).
export function VaccinationOperationsPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const isFullSchedule = one(sp, "view") === "schedule";
  const scheduleYear = vaccinationScheduleYear(sp);

  const driveSteps = optionGroup(pageContract, "drive_steps").map((step) => {
    const [title, detail] = (step.title || "").split("|");
    return { key: step.key, step: step.label, title, detail };
  });

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pageContract, "crumb")}
          </div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {isFullSchedule ? null : <VaccinationFullScheduleButton scope={scope} pageContract={pageContract} active={isFullSchedule} year={scheduleYear} />}
      </div>

      {isFullSchedule ? (
        <Suspense fallback={<VaccinationFullScheduleSkeleton pageContract={pageContract} />}>
          <VaccinationFullSchedule searchParams={sp} scope={scope} pageContract={pageContract} />
        </Suspense>
      ) : (
        <>
      {/* CEO command board — KPIs, cohort matrix, shed dose matrix, weekly given, verification queue. */}
      <Suspense fallback={<VaccinationCommandBoardSkeleton pageContract={pageContract} />}>
        <VaccinationCommandBoard pageContract={pageContract} driveBatchId={one(sp, "cb_drive")} />
      </Suspense>

      {/* PROTOTYPE (local-only, do not push): Vaccination Command closure board replaces the
          shed-wise table for maintainer visual review. Restore VaccinationShedBoard before landing. */}
      <VaccinationCommandBoard />
        </>
      )}
    </div>
  );
}
