import { type NextRequest, NextResponse } from "next/server";
import {
  listStaffLeave,
  applyStaffLeave,
  type StaffLeaveQuery,
  type ApplyStaffLeaveRequest,
} from "@/lib/api/server";
import { positiveIntParam, stringParam } from "../query";

export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  const result = await listStaffLeave(leaveQuery(request));

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

export async function POST(request: NextRequest) {
  const body = (await request.json()) as ApplyStaffLeaveRequest;
  const result = await applyStaffLeave(body);

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

function leaveQuery(request: NextRequest): StaffLeaveQuery {
  return {
    workforce_member_id: stringParam(request, "workforce_member_id"),
    scope_type: stringParam(request, "scope_type") as StaffLeaveQuery["scope_type"],
    scope_id: stringParam(request, "scope_id"),
    status: stringParam(request, "status") as StaffLeaveQuery["status"],
    limit: positiveIntParam(request, "limit"),
  };
}
