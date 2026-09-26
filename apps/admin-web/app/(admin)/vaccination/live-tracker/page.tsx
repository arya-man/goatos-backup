import { Suspense } from "react";
import { LiveTrackerBoard } from "@/features/vaccination-live-tracker";
import Loading from "./loading";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [sp, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("vaccination-live-tracker")]);
  return (
    <Suspense fallback={<Loading />}>
      <LiveTrackerBoard searchParams={sp} pageContract={pageContract} />
    </Suspense>
  );
}
