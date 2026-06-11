import { ImportReviewPage } from "@/features/import-review";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <ImportReviewPage searchParams={await searchParams} />;
}
