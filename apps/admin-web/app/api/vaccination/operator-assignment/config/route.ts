import { getVaccinationOperatorAssignmentConfig, putVaccinationOperatorAssignmentConfig } from "@/lib/api/server";
import { NextRequest, NextResponse } from "next/server";

export async function GET(request: NextRequest) {
  // park_id is optional: the backend owns park-scope resolution and echoes the
  // resolved parkId back to the client (BUG-019).
  const parkId = request.nextUrl.searchParams.get("park_id") ?? undefined;

  const result = await getVaccinationOperatorAssignmentConfig(parkId);
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
  const result = await putVaccinationOperatorAssignmentConfig(body);
  if (!result.ok) {
    return NextResponse.json(result.error, { status: result.error.status });
  }
  return NextResponse.json(result.data, {
    headers: {
      "Cache-Control": "no-store",
    },
  });
}
