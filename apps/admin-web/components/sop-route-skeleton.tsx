"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { Skeleton } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import { skeletonClasses } from "@/components/app/page-skeletons";

/**
 * Route-shaped shimmer for the module SOP library.
 *
 * Shared by all five `app/(admin)/{counts,feed,milk,procurement,weighing}/sops/loading.tsx` so the
 * shape cannot drift per module. It mirrors `sop-library.tsx` exactly: pagehead (crumbs + title +
 * subtitle + right-hand "+ Add" button), the segmented stat strip, the filter bar row, the g3 card
 * grid, then the table footer strip. There is no KPI deck and no chart on the real page, so there
 * is none here.
 */
export function SopLibrarySkeleton() {
  return (
    <div className="screen on sop-kit" aria-busy="true">
      <PageHeaderSkeleton action />

      {/* Stat strip (template invoice-analytic row in a Card): Total / Published / Draft / Retired. */}
      <Card sx={{ mb: 1.75, py: 2, display: "grid", gridTemplateColumns: { xs: "repeat(2, minmax(0, 1fr))", md: "repeat(4, minmax(0, 1fr))" }, rowGap: 2 }}>
        {[0, 1, 2, 3].map((i) => (
          <Box key={i} sx={{ display: "flex", alignItems: "center", justifyContent: "center", gap: 2.5, px: 2, minWidth: 0 }}>
            <Skeleton width={56} height={56} radius="50%" />
            <Box sx={{ display: "grid", gap: 0.75 }}>
              <Skeleton width={66} height={16} />
              <Skeleton width={40} height={12} />
            </Box>
          </Box>
        ))}
      </Card>

      {/* FilterBar (template list toolbar card): two selects + search, then trailing actions. */}
      <Card sx={{ p: 2.5, mb: 1.75, display: "flex", flexWrap: "wrap", gap: 2, alignItems: "center" }}>
        <Skeleton width={160} height={56} radius={8} />
        <Skeleton width={160} height={56} radius={8} />
        <Skeleton width={260} height={56} radius={8} style={{ flex: "1 1 240px" }} />
        {[84, 74, 84].map((w, wi) => (
          <Skeleton key={`${wi}-${w}`} width={w} height={36} radius={8} />
        ))}
      </Card>

      <div className="grid g3">
        {Array.from({ length: 6 }, (_, i) => (
          <div key={i} className={skeletonClasses.card} style={{ gap: 10 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
              <Skeleton width={26} height={26} radius={8} />
              <Skeleton width="58%" height={16} />
            </div>
            <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
              {[70, 54, 88, 62, 76].map((w, wi) => (
                <Skeleton key={`${wi}-${w}`} width={w} height={22} radius={6} />
              ))}
            </div>
            <Skeleton width="80%" height={12} />
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <Skeleton width={96} height={11} radius={5} />
              <div style={{ flex: 1 }} />
              <Skeleton width={28} height={28} radius={8} />
            </div>
          </div>
        ))}
      </div>

      {/* Template table pagination row: rows-per-page, range, two arrow buttons. */}
      <Box sx={{ display: "flex", alignItems: "center", justifyContent: "flex-end", gap: 2, px: 2, py: 1.5 }}>
        <Skeleton width={150} height={20} radius={6} />
        <Skeleton width={120} height={12} radius={5} />
        <Skeleton width={32} height={32} radius={16} />
        <Skeleton width={32} height={32} radius={16} />
      </Box>
    </div>
  );
}

/**
 * Editor-shaped shimmer for `?compose=1`: the full-page builder is a header + section cards + a
 * sticky action bar, nothing like the card grid, so the library skeleton flashing first reads as a
 * layout jump.
 */
export function SopEditorSkeleton() {
  return (
    <div className="screen on sop-kit" aria-busy="true">
      <div className="kit-pagehead" style={{ marginBottom: 20 }}>
        <div className="kit-pagehead-title">
          <Skeleton width={220} height={12} radius={6} />
          <Skeleton width={300} height={30} radius={10} style={{ marginTop: 10 }} />
          <Skeleton width={440} height={14} style={{ marginTop: 10, maxWidth: "100%" }} />
        </div>
        <Skeleton width={110} height={40} radius={10} />
        <Skeleton width={92} height={40} radius={10} />
      </div>
      {[0, 1, 2].map((i) => (
        <div key={i} className={skeletonClasses.card} style={{ gap: 14, marginBottom: 16 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <Skeleton width={190} height={18} />
            <div style={{ flex: 1 }} />
            <Skeleton width={20} height={20} radius={6} />
          </div>
          <Skeleton width={340} height={12} style={{ maxWidth: "100%" }} />
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(220px,1fr))", gap: 12 }}>
            {[0, 1, 2, 3].map((f) => (
              <Skeleton key={f} height={52} radius={10} />
            ))}
          </div>
        </div>
      ))}
      <div className={skeletonClasses.card} style={{ flexDirection: "row", alignItems: "center", gap: 10 }}>
        <Skeleton width={150} height={14} />
        <div style={{ flex: 1 }} />
        <Skeleton width={104} height={40} radius={10} />
        <Skeleton width={120} height={40} radius={10} />
      </div>
    </div>
  );
}

/**
 * `loading.tsx` gets no props, so the compose/library branch is read from the URL in a client
 * component. It is wrapped in its own Suspense boundary (with the library shape as the fallback)
 * so `useSearchParams` can never force the route out of static rendering.
 */
function SopLoadingBranch() {
  const compose = useSearchParams().get("compose");
  return compose === "1" ? <SopEditorSkeleton /> : <SopLibrarySkeleton />;
}

export function SopRouteSkeleton() {
  return (
    <Suspense fallback={<SopLibrarySkeleton />}>
      <SopLoadingBranch />
    </Suspense>
  );
}
