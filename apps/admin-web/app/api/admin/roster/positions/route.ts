import { type NextRequest, NextResponse } from "next/server";
import { listStaffPositions } from "@/lib/api/server";

export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  const result = await listStaffPositions();

  if (!result.ok) {
    return NextResponse.json(
      { error: result.error.message },
      { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } }
    );
  }

  return NextResponse.json(result.data, {
    headers: { "Cache-Control": "no-store" },
  });
}
