"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import {
  CardGridSkeleton,
  DetailCardSkeleton,
  FilterCardSkeleton,
  PageHeaderSkeleton,
  PageSkeleton,
  StackSkeleton,
  BlockSkeleton,
} from "@/components/app/skeletons";
import { SOP_SKELETON_CARDS, isSopEditorUrl } from "./sop-library-layout";
import { usePendingRouteSearch } from "@/components/app/pending-route";

/**
 * The module SOP libraries' loading shape (all SOP routes share `sop-library.tsx`, so they share
 * this): header + New SOP, the bare FilterBar (status + trigger selects, search, the ⋮ menu), the
 * job-list card grid (one page of cards). No KPI row (TR1-#33: the template job list has none).
 */
export function SopLibrarySkeleton({ titleWidth = 180 }: { titleWidth?: number } = {}) {
  return (
    <PageSkeleton gap={3}>
      {/* Module SOP titles are short: on a phone New SOP sits beside the title block (a longer title,
          "Work instructions", passes its own width and New SOP wraps under it). */}
      <PageHeaderSkeleton crumbLink={false} titleWidth={titleWidth} actionWidths={[112]} />
      {/* FilterBar bare + fold: Status / Trigger selects from md, search, Filters button below md, ⋮. */}
      <FilterCardSkeleton bare fold fields={[160, 160, "search"]} actionWidths={[36]} />
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
      <PageHeaderSkeleton crumbLink={false} actions={2} />
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
// The shell's pending-route skeleton passes the TARGET url's params (useSearchParams still answers for
// the page being left); the predicate is the page's own (isSopEditorUrl: compose=1 or new=1).
function SopLoadingBranch({ titleWidth }: { titleWidth?: number }) {
  const current = useSearchParams();
  const pending = usePendingRouteSearch();
  return isSopEditorUrl(pending ?? current) ? <SopEditorSkeleton /> : <SopLibrarySkeleton titleWidth={titleWidth} />;
}

export function SopRouteSkeleton({ titleWidth }: { titleWidth?: number } = {}) {
  return (
    <Suspense fallback={<SopLibrarySkeleton titleWidth={titleWidth} />}>
      <SopLoadingBranch titleWidth={titleWidth} />
    </Suspense>
  );
}
