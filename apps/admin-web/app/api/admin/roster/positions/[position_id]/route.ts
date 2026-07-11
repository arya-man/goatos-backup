import { NextResponse } from "next/server";
import { getStaffPositionProfile } from "@/lib/api/server";

export const dynamic = "force-dynamic";

// Backs the People "Position & Coverage" row-click profile drawer
// (getStaffPositionProfile in lib/api/client.ts -> this proxy -> backend
// GET /admin/roster/positions/{position_id}).
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ position_id: string }> },
) {
  const { position_id } = await params;
  const result = await getStaffPositionProfile(position_id);

  if (!result.ok) {
    return NextResponse.json(
      { error: result.error.message },
      { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } },
    );
  }

  return NextResponse.json(result.data, {
    headers: { "Cache-Control": "no-store" },
  });
}
