import { Suspense } from "react";
import { HerdSignalsBoard } from "@/features/herd-signals";
import Loading from "./loading";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [sp, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("herd-signals")]);
  // NO key on the Suspense boundary below. key={JSON.stringify(sp)} minted a NEW boundary on every
  // searchParam change, so each filter/KPI/pagination click UNMOUNTED the whole board and showed
  // the skeleton -- which is exactly the "changing a filter refreshes the whole page" the
  // maintainer reported repeatedly. Without the key React reuses this boundary; the board's own
  // URL-keyed panels (UrlSuspense) swap only the KPIs + table to their skeleton on a filter / KPI /
  // sort / page change, while header, tabs and filters stay. Nothing is dimmed and no stale rows
  // stay on screen (REVIEW-43, guard: pending-dim).
  // (A `{/* ... */}` JSX comment cannot be the direct child of `return (` -- as written it was a
  // syntax error and the whole route failed to compile.)
  return (
    <Suspense fallback={<Loading />}>
      <HerdSignalsBoard searchParams={sp} pageContract={pageContract} />
    </Suspense>
  );
}
