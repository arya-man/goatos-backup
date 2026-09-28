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
import { SOP_HEADER_ACTIONS, SOP_HEADER_LAYOUT, SOP_SKELETON_CARDS, isSopEditorUrl } from "./sop-library-layout";
import { usePendingRouteSearch } from "@/components/app/pending-route";
import { FILTER_SEARCH_FOLD_BASIS } from "@/components/app/filter-field-widths";

/**
 * The module SOP libraries' loading shape (all SOP routes share `sop-library.tsx`, so they share
 * this): header + New SOP, the bare FilterBar (status + trigger selects, search, the ⋮ menu), the
 * job-list card grid (one page of cards). No KPI row (TR1-#33: the template job list has none).
 */
type SopSkeletonProps = { titleWidth?: number; actionWidths?: readonly number[] };

export function SopLibrarySkeleton({ titleWidth = 180, actionWidths = SOP_HEADER_ACTIONS.newSop }: SopSkeletonProps = {}) {
  return (
    <PageSkeleton gap={3}>
      {/* SOP_HEADER_LAYOUT (shared with SopLibrary's PageHeader): below md the actions always take their
          own row under the title block, so neither the crumb width nor the Assumptions permission moves it. */}
      <PageHeaderSkeleton layout={SOP_HEADER_LAYOUT} crumbLink={false} titleWidth={titleWidth} actionWidths={[...actionWidths]} />
      {/* FilterBar bare + fold: Status / Trigger selects from md, search, Filters button below md, ⋮ -- all on
          ONE row at 390 (the fold search basis, shared with FilterBar; guard: filter-bar-fold-one-row). */}
      <FilterCardSkeleton bare fold fields={[160, 160, "search"]} actionWidths={[36]} searchBasis={{ ...FILTER_SEARCH_FOLD_BASIS }} />
      {/* One row of the 1/2/3 grid at every width (1 card on a phone, TR3-P0-3): most module libraries
          hold one to three SOPs, so a full page of twelve placeholder cards was three rows taller than
          the page it stands in for, and three stacked phone cards were two more than a 1-SOP library. */}
      <CardGridSkeleton count={SOP_SKELETON_CARDS} oneRow />
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
function SopLoadingBranch(props: SopSkeletonProps) {
  const current = useSearchParams();
  const pending = usePendingRouteSearch();
  return isSopEditorUrl(pending ?? current) ? <SopEditorSkeleton /> : <SopLibrarySkeleton {...props} />;
}

export function SopRouteSkeleton(props: SopSkeletonProps = {}) {
  return (
    <Suspense fallback={<SopLibrarySkeleton {...props} />}>
      <SopLoadingBranch {...props} />
    </Suspense>
  );
}
