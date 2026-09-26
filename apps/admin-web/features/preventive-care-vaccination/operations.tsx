import { Suspense } from "react";

import { PageHeader } from "@/components/app/page-header";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseScope } from "@/lib/scope";
import { VaccinationShedBoard, VaccinationShedBoardSkeleton } from "@/features/vaccination-sheds";
import { VaccinationFullScheduleButton } from "./vaccination-action-dialogs";
import { VaccinationFullSchedule, VaccinationFullScheduleSkeleton, vaccinationScheduleYear } from "./full-vaccine-schedule";
import { VaccinationCommandBoard, VaccinationCommandBoardSkeleton } from "./command-board";
import { InventoryVaccineProgressSection } from "./inventory-vaccine-progress";

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

  return (
    <div className="screen on">
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
        actions={isFullSchedule ? null : <VaccinationFullScheduleButton scope={scope} pageContract={pageContract} active={isFullSchedule} year={scheduleYear} />}
      />

      {isFullSchedule ? (
        <Suspense fallback={<VaccinationFullScheduleSkeleton pageContract={pageContract} />}>
          <VaccinationFullSchedule searchParams={sp} scope={scope} pageContract={pageContract} />
        </Suspense>
      ) : (
        <div className="kit-enter" style={{ display: "grid", gap: 16, gridTemplateColumns: "minmax(0,1fr)" }}>
      {/* CEO command board — KPIs, cohort matrix, shed dose matrix, weekly given, verification queue. */}
      <div style={{ minWidth: 0 }}>
        <Suspense fallback={<VaccinationCommandBoardSkeleton pageContract={pageContract} />}>
          <VaccinationCommandBoard pageContract={pageContract} searchParams={sp} driveBatchId={one(sp, "cb_drive")} driveParkId={one(sp, "cb_drive_park")} />
        </Suspense>
      </div>

      <div style={{ minWidth: 0 }}>
      <Suspense
        fallback={
          <section className="card" id="pc-care-inventory-progress">
            <div className="hd">
              <h2>{copy(pageContract, "section.inventory_progress.title")}</h2>
            </div>
            <div className="bd">
              <div className="muted">{copy(pageContract, "inventory_progress.loading")}</div>
            </div>
          </section>
        }
      >
        <InventoryVaccineProgressSection searchParams={sp} pageContract={pageContract} />
      </Suspense>
      </div>

      {/* Shed-wise vaccination table — one row per shed, animal-level due/done, planned sessions, capacity,
          and merged status. Rows deep-link to the shed detail. This is the MAIN vaccination table. */}
      <div style={{ minWidth: 0 }}>
        <Suspense fallback={<VaccinationShedBoardSkeleton pageContract={pageContract} />}>
          <VaccinationShedBoard searchParams={sp} pageContract={pageContract} />
        </Suspense>
      </div>
        </div>
      )}
    </div>
  );
}
