import { Suspense } from "react";
import { LiveTrackerBoard, LiveTrackerSkeleton } from "@/features/vaccination-live-tracker";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [sp, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("vaccination-live-tracker")]);
  return (
    <Suspense key={JSON.stringify(sp)} fallback={<LiveTrackerSkeleton pageContract={pageContract} />}>
      <LiveTrackerBoard searchParams={sp} pageContract={pageContract} />
    </Suspense>
  );
}
