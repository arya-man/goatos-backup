import { SalesConfigPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Sales Config — the one place a sales fact is entered or changed: recording a sale and the
// animals it is made of, the buyer/farmer-group pipeline, market quotes, tag lists, weight checks,
// deal payments and status, and a purchased load's landed cost. The sales board and Purchase and Born read
// these facts back and declare no write of their own.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <SalesConfigPage
      searchParams={await searchParams}
      pageContract={await requireAdminWebPageContract("sales-config")}
    />
  );
}
