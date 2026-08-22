import { Suspense } from "react";
import { HerdSignalsBoard, HerdSignalsSkeleton } from "@/features/herd-signals";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [sp, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("herd-signals")]);
  return (
    <Suspense key={JSON.stringify(sp)} fallback={<HerdSignalsSkeleton />}>
      <HerdSignalsBoard searchParams={sp} pageContract={pageContract} />
    </Suspense>
  );
}
