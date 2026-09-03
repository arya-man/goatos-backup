import { VaccinationDriveDetail } from "@/features/calendar";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Full-screen drive detail (owner-directed replacement for the calendar drive drawer). Thin route:
// the feature component owns all fetching + rendering; nested drilldown under the Calendar command
// lens is allow-listed in check-ia-guard.mjs (like /workflows/{param}).
export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ eventId: string }>;
  searchParams: Promise<RouteSearchParams>;
}) {
  const [{ eventId }, sp, pageContract] = await Promise.all([
    params,
    searchParams,
    requireAdminWebPageContract("calendar"),
  ]);
  return (
    <VaccinationDriveDetail
      eventId={decodeURIComponent(eventId)}
      searchParams={sp}
      pageContract={pageContract}
    />
  );
}
