import { OperatorsPage } from "@/features/operators";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <OperatorsPage searchParams={await searchParams} />;
}
