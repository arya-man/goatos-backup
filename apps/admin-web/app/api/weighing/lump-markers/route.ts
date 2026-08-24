import { NextResponse } from "next/server";

import { getShedWeights } from "@/lib/api/server";

export const dynamic = "force-dynamic";

const BUSINESS_DAY = /^\d{4}-\d{2}-\d{2}$/;

export async function GET(request: Request) {
  const url = new URL(request.url);
  const from = url.searchParams.get("from")?.trim() ?? "";
  const to = url.searchParams.get("to")?.trim() ?? "";
  const parkID = url.searchParams.get("park_id")?.trim() ?? "";

  if (!BUSINESS_DAY.test(from) || !BUSINESS_DAY.test(to) || from > to) {
    return NextResponse.json(
      { dates: [] },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }

  const result = await getShedWeights({
    park_id: parkID || undefined,
    from,
    to,
  });
  if (!result.ok) {
    return NextResponse.json(
      { dates: [] },
      { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } },
    );
  }

  return NextResponse.json(
    { dates: result.data.lump_weighing_dates },
    { headers: { "Cache-Control": "no-store" } },
  );
}
