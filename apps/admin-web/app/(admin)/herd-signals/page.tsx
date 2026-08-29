import { Suspense } from "react";
import { HerdSignalsBoard, HerdSignalsSkeleton } from "@/features/herd-signals";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [sp, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("herd-signals")]);
  // NO key on the Suspense boundary below. key={JSON.stringify(sp)} minted a NEW boundary on every
  // searchParam change, so each filter/KPI/pagination click UNMOUNTED the whole board and showed
  // the skeleton -- which is exactly the "changing a filter refreshes the whole page" the
  // maintainer reported repeatedly. Without the key React reuses the boundary and keeps the
  // previous content on screen through the transition, which is what .wfbusy/.wfspin are for: the
  // old answer stays readable and visibly held back until the new one lands.
  // (A `{/* ... */}` JSX comment cannot be the direct child of `return (` -- as written it was a
  // syntax error and the whole route failed to compile.)
  return (
    <Suspense fallback={<HerdSignalsSkeleton />}>
      <HerdSignalsBoard searchParams={sp} pageContract={pageContract} />
    </Suspense>
  );
}
