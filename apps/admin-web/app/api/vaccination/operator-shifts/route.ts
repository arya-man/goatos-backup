import {
  deleteVaccinationOperatorShift,
  listVaccinationOperatorShifts,
  putVaccinationOperatorShift,
} from "@/lib/api/server";
import { NextRequest, NextResponse } from "next/server";

// Backs the Vaccination operators screen's shift column and its Set / Clear shift form
// (lib/api/client.ts -> this proxy -> backend /vaccination/operator-shifts).

const NO_STORE = { "Cache-Control": "no-store" };

export async function GET(request: NextRequest) {
  const parkId = request.nextUrl.searchParams.get("park_id") ?? "";
  const result = await listVaccinationOperatorShifts(parkId);
  if (!result.ok) {
    return NextResponse.json(result.error, { status: result.error.status ?? 500, headers: NO_STORE });
  }
  return NextResponse.json(result.data, { headers: NO_STORE });
}

export async function PUT(request: NextRequest) {
  const body = await request.json().catch(() => null);
  if (!body || typeof body !== "object") {
    return NextResponse.json(
      { code: "invalid_body", message: "The shift form could not be read. Reload and try again." },
      { status: 400, headers: NO_STORE },
    );
  }
  const idempotencyKey = request.headers.get("Idempotency-Key") ?? `operator-shift-${crypto.randomUUID()}`;
  const result = await putVaccinationOperatorShift(body, idempotencyKey);
  if (!result.ok) {
    return NextResponse.json(result.error, { status: result.error.status ?? 500, headers: NO_STORE });
  }
  return NextResponse.json(result.data, { headers: NO_STORE });
}

export async function DELETE(request: NextRequest) {
  const parkId = request.nextUrl.searchParams.get("park_id") ?? "";
  const operatorId = request.nextUrl.searchParams.get("operator_id") ?? "";
  const idempotencyKey = request.headers.get("Idempotency-Key") ?? `operator-shift-clear-${crypto.randomUUID()}`;
  const result = await deleteVaccinationOperatorShift(parkId, operatorId, idempotencyKey);
  if (!result.ok) {
    return NextResponse.json(result.error, { status: result.error.status ?? 500, headers: NO_STORE });
  }
  return NextResponse.json(result.data, { headers: NO_STORE });
}
