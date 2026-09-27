"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import {
  CardGridSkeleton,
  DetailCardSkeleton,
  FilterCardSkeleton,
  PageHeaderSkeleton,
  PageSkeleton,
  KpiRowSkeleton,
  StackSkeleton,
  BlockSkeleton,
} from "@/components/app/skeletons";
import { SOP_SKELETON_CARDS, SOP_STAT_CELLS } from "./sop-library-layout";

/**
 * The module SOP libraries' loading shape (all SOP routes share `sop-library.tsx`, so they share
 * this): header + New SOP, the stat strip card, the FilterBar (status + trigger selects, search,
 * Columns / Export / more), the job-list card grid (one page of cards).
 */
export function SopLibrarySkeleton() {
  return (
    <PageSkeleton className="sop-kit">
      <PageHeaderSkeleton actions={1} />
      {/* The KPI row is KpiGrid + KpiWidget (template CourseWidgetSummary): its loading twin is the
          KPI card row, not the retired StatStrip ring strip (twice as tall; audit skeleton IoU). */}
      <KpiRowSkeleton count={SOP_STAT_CELLS} />
      <FilterCardSkeleton fields={[160, 160, "search"]} actions={3} />
      {/* One row of the 1/2/3 grid: most module libraries hold one to three SOPs, so a full page
          of twelve placeholder cards was three rows taller than the page it stands in for. */}
      <CardGridSkeleton count={SOP_SKELETON_CARDS} />
    </PageSkeleton>
  );
}

/** `?compose=1` / `?edit=`: the full-page builder (header + section cards + the action bar). */
export function SopEditorSkeleton() {
  return (
    <PageSkeleton className="sop-kit">
      <PageHeaderSkeleton actions={2} />
      <StackSkeleton>
        <DetailCardSkeleton rows={4} columns={2} />
        <DetailCardSkeleton rows={4} columns={2} />
        <DetailCardSkeleton rows={4} columns={2} />
      </StackSkeleton>
      <BlockSkeleton height={72} />
    </PageSkeleton>
  );
}

// loading.tsx gets no props, so the builder / library branch is read from the URL in a client
// component, inside its own Suspense (library shape as the fallback) so useSearchParams never forces
// the route out of static rendering.
function SopLoadingBranch() {
  const sp = useSearchParams();
  return sp.get("compose") === "1" || sp.get("edit") ? <SopEditorSkeleton /> : <SopLibrarySkeleton />;
}

export function SopRouteSkeleton() {
  return (
    <Suspense fallback={<SopLibrarySkeleton />}>
      <SopLoadingBranch />
    </Suspense>
  );
}
