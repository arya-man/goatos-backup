import { WeighingPage } from "@/features/weighing/page";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <WeighingPage searchParams={await searchParams} />;
}
