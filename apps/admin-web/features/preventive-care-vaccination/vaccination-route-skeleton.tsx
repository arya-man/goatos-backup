"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";

import { PageHeaderSkeleton, PageSkeleton, StackSkeleton } from "@/components/app/skeletons";
import { usePendingRouteSearch } from "@/components/app/pending-route";
import { VACCINATION_HEADER_ACTION_WIDTHS, isVaccinationScheduleView } from "./command-board-layout";
import {
  VaccinationCommandBoardSkeleton,
  VaccinationFullScheduleSkeleton,
  VaccinationInventorySkeleton,
  VaccinationShedBoardSkeleton,
} from "./vaccination-skeletons";

/**
 * /vaccination board: header + Full schedule action, then the SAME three panel skeletons the page
 * streams its sections behind (command board, inventory progress, pen board).
 */
export function VaccinationBoardRouteSkeleton() {
  return (
    <PageSkeleton gap={3}>
      <PageHeaderSkeleton crumbLink={false} actionWidths={VACCINATION_HEADER_ACTION_WIDTHS} />
      <StackSkeleton spacing={2}>
        <VaccinationCommandBoardSkeleton />
        <VaccinationInventorySkeleton />
        <VaccinationShedBoardSkeleton />
      </StackSkeleton>
    </PageSkeleton>
  );
}

/** `?view=schedule`: the header has no action (the page hides Full schedule there), then the schedule card. */
export function VaccinationScheduleRouteSkeleton() {
  return (
    <PageSkeleton gap={3}>
      <PageHeaderSkeleton crumbLink={false} />
      <VaccinationFullScheduleSkeleton />
    </PageSkeleton>
  );
}

// loading.tsx gets no props: the branch reads the TARGET url (the shell's pending search while a
// click is in flight, else the current one) with the page's own predicate, inside a Suspense.
function VaccinationLoadingBranch() {
  const current = useSearchParams();
  const pending = usePendingRouteSearch();
  return isVaccinationScheduleView((pending ?? current).get("view")) ? <VaccinationScheduleRouteSkeleton /> : <VaccinationBoardRouteSkeleton />;
}

export function VaccinationRouteSkeleton() {
  return (
    <Suspense fallback={<VaccinationBoardRouteSkeleton />}>
      <VaccinationLoadingBranch />
    </Suspense>
  );
}
