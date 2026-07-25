import { type NextRequest, NextResponse } from "next/server";
import {
  approveStaffLeave,
  type ApproveStaffLeaveRequest,
} from "@/lib/api/server";

export const dynamic = "force-dynamic";

export async function POST(
  request: NextRequest,
  context: { params: Promise<{ absence_id: string }> },
) {
  const [{ absence_id: absenceId }, body] = await Promise.all([
    context.params,
    request.json() as Promise<ApproveStaffLeaveRequest>,
  ]);
  const result = await approveStaffLeave(absenceId, body, request.headers.get("Idempotency-Key") ?? undefined);

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
