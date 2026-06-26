import { type NextRequest, NextResponse } from "next/server";
import { getVaccinationActionCenter } from "@/lib/api/server";
import { backendScope, parseScope } from "@/lib/scope";

export const dynamic = "force-dynamic";

type NavCountsPayload = {
  actionCenter: number | null;
  phc: number | null;
};

export async function GET(request: NextRequest) {
  const scopeParams = Object.fromEntries(request.nextUrl.searchParams.entries());
  const { parkId, asOf } = backendScope(parseScope(scopeParams));
  const actionCenter = await getVaccinationActionCenter({ parkId, asOf, limit: 1 });

  if (!actionCenter.ok) {
    return NextResponse.json(
      { actionCenter: null, phc: null } satisfies NavCountsPayload,
      { headers: { "Cache-Control": "no-store" } },
    );
  }

  const openVaccinationWork = actionCenter.data.counts_by_work_state.reduce((total, row) => {
    return row.work_state === "completed" ? total : total + row.count;
  }, 0);

  return NextResponse.json(
    {
      actionCenter: openVaccinationWork,
      phc: openVaccinationWork,
    } satisfies NavCountsPayload,
    { headers: { "Cache-Control": "no-store" } },
  );
}
