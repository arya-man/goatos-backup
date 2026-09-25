import { Suspense } from "react";
import { CareCoverageBoard, CareCoverageSkeleton } from "@/features/vaccination-care-coverage";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Care Coverage: the vaccination status matrix's layout for the five PC Care jobs — pens down the
// left, one column per job, a tick where the job is done.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [sp, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("vaccination-care-coverage")]);
  return (
    <Suspense fallback={<CareCoverageSkeleton pageContract={pageContract} />}>
      <CareCoverageBoard searchParams={sp} pageContract={pageContract} />
    </Suspense>
  );
}
