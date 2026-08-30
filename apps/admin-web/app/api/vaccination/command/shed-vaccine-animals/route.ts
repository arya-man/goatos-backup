import { NextResponse } from "next/server";
import { getCommandBoardShedVaccineAnimals } from "@/lib/api/server";
import { drilldownScopeFromRequest, jsonOrError } from "../scope";

export const dynamic = "force-dynamic";

// The animals behind ONE shed x vaccine cell, with that shed's proof videos.
export async function GET(request: Request) {
  const params = new URL(request.url).searchParams;
  const shedId = params.get("shed_id");
  const vaccineCode = params.get("vaccine_code");
  if (!shedId || !vaccineCode) {
    // Without the cell this would be the tenant-wide scan the split exists to remove, so it is a
    // 400 rather than a slow 200.
    return NextResponse.json(
      { error: "shed_id and vaccine_code identify the cell and are required" },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }
  return jsonOrError(
    await getCommandBoardShedVaccineAnimals({
      ...drilldownScopeFromRequest(request),
      shedId,
      vaccineCode,
      // An unpartitioned shed's cell key IS the empty label.
      partitionLabel: params.get("partition_label") ?? "",
    }),
  );
}
