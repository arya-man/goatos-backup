import { WorkBoardPage } from "@/features/work-board";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Work Board — every module's work for one park and one business day, on one board.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("work-board")]);
  return <WorkBoardPage searchParams={params} pageContract={pageContract} />;
}
