import { getVaccinationCapacityConfig } from "@/lib/api/server";
import { NextResponse } from "next/server";

export async function GET() {
  const result = await getVaccinationCapacityConfig();
  if (!result.ok) {
    return NextResponse.json(result.error, { status: result.error.status });
  }
  return NextResponse.json(result.data, {
    headers: {
      "Cache-Control": "no-store",
    },
  });
}
