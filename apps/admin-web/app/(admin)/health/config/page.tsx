import { HealthConfigPage } from "@/features/health";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("health-config")]);
  return (
    <HealthConfigPage
      searchParams={params}
      pageContract={pageContract}
    />
  );
}
