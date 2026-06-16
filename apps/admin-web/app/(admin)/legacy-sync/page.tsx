import { LegacySyncPage } from "@/features/legacy-sync";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <LegacySyncPage searchParams={await searchParams} />;
}
