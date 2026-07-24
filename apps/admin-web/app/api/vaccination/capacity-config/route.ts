import { getVaccinationCapacityConfig, putVaccinationCapacityConfig } from "@/lib/api/server";
import { NextRequest, NextResponse } from "next/server";

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

export async function PUT(request: NextRequest) {
  const body = await request.json();
  const result = await putVaccinationCapacityConfig(body);
  if (!result.ok) {
    return NextResponse.json(result.error, { status: result.error.status });
  }
  return NextResponse.json(result.data, {
    headers: {
      "Cache-Control": "no-store",
    },
  });
}
