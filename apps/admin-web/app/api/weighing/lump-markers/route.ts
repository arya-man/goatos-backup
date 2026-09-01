import { NextResponse } from "next/server";

import { getWeighingDates } from "@/lib/api/server";

export const dynamic = "force-dynamic";

const BUSINESS_DAY = /^\d{4}-\d{2}-\d{2}$/;

export async function GET(request: Request) {
  const url = new URL(request.url);
  const from = url.searchParams.get("from")?.trim() ?? "";
  const to = url.searchParams.get("to")?.trim() ?? "";
  const parkID = url.searchParams.get("park_id")?.trim() ?? "";
  const sex = url.searchParams.get("sex")?.trim() ?? "";
  const origin = url.searchParams.get("origin")?.trim() ?? "";
  const weighing = url.searchParams.get("weighing")?.trim() ?? "";

  if (!BUSINESS_DAY.test(from) || !BUSINESS_DAY.test(to) || from > to) {
    return NextResponse.json(
      { dates: [] },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }

  // The narrow read: this route wants the marker DATES and nothing else, and the date picker fires
  // it on every calendar open. Going through the full shed read meant four queries and a whole
  // shed table thrown away each time.
  const result = await getWeighingDates({
    park_id: parkID || undefined,
    from,
    to,
    sex: sex === "male" || sex === "female" ? sex : undefined,
    origin: origin === "farm_born" || origin === "purchased" ? origin : undefined,
    weighing_category:
      weighing === "individual_animal" || weighing === "per_shed_partition" ? weighing : undefined,
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
