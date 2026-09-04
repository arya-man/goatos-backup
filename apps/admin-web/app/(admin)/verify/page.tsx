import { VerificationReviewPage } from "@/features/verification-review";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Admin / Data Ops -> Actions. The backend page contract owns title, table, filters, drawer copy,
// and nav visibility; the verification read model owns action types, statuses, rows, and media.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("verification-review")]);
  return (
    <VerificationReviewPage
      searchParams={params}
      pageContract={pageContract}
    />
  );
}
