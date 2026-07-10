import { type NextRequest, NextResponse } from "next/server";
import { listStaffPositions, type StaffPositionsQuery } from "@/lib/api/server";
import { positiveIntParam, stringParam } from "../query";

export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  const result = await listStaffPositions(rosterQuery(request));

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

function rosterQuery(request: NextRequest): StaffPositionsQuery {
  return {
    workforce_member_id: stringParam(request, "workforce_member_id"),
    scope_type: stringParam(request, "scope_type") as StaffPositionsQuery["scope_type"],
    scope_id: stringParam(request, "scope_id"),
    position_code: stringParam(request, "position_code"),
    status: stringParam(request, "status") as StaffPositionsQuery["status"],
    limit: positiveIntParam(request, "limit"),
  };
}
