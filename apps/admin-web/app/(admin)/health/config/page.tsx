import { HealthConfigPage } from "@/features/health";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <HealthConfigPage
      searchParams={await searchParams}
      pageContract={await requireAdminWebPageContract("health-config")}
    />
  );
}
