import { NextResponse } from "next/server";
import { getStaffPositionProfile, updateStaffPosition } from "@/lib/api/server";

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

export async function PATCH(
  request: Request,
  { params }: { params: Promise<{ position_id: string }> },
) {
  const [{ position_id }, body] = await Promise.all([
    params,
    request.json().catch(() => null),
  ]);
  if (!body || typeof body !== "object") {
    return NextResponse.json(
      { error: "invalid_position_update_body" },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }

  const idempotencyKey = request.headers.get("Idempotency-Key") ?? `position-update-${crypto.randomUUID()}`;
  const result = await updateStaffPosition(position_id, body, idempotencyKey);

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
