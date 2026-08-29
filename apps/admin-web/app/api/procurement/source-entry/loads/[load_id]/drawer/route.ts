import { NextResponse } from "next/server";

import { getProcurementLoad } from "@/lib/api/procurement-server";

export const dynamic = "force-dynamic";

export async function GET(
  _request: Request,
  { params }: { params: Promise<{ load_id: string }> },
) {
  const { load_id: loadID } = await params;
  // serial-await: allow getProcurementLoad depends on the dynamic route param.
  const result = await getProcurementLoad(loadID);
  if (!result.ok) {
    return NextResponse.json(
      { error: result.error.code ?? result.error.kind },
      { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } },
    );
  }
  return NextResponse.json(
    { detail: result.data.detail },
    { headers: { "Cache-Control": "no-store" } },
  );
}
