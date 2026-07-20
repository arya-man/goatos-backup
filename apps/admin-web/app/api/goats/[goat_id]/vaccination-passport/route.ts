import { NextResponse } from "next/server";
import { getGoatVaccinationPassport } from "@/lib/api/server";

export const dynamic = "force-dynamic";

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Authenticated same-origin vaccination read for the client-local shed drawer. */
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ goat_id: string }> },
) {
  const { goat_id } = await params;
  if (!UUID_RE.test(goat_id)) {
    return NextResponse.json(
      { error: "invalid_goat_id" },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }

  const result = await getGoatVaccinationPassport(goat_id);
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
