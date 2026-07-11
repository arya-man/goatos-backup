import { type NextRequest, NextResponse } from "next/server";
import { listCoverage, type CoverageQuery } from "@/lib/api/server";
import { booleanParam, positiveIntParam, stringParam } from "../query";

export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  const result = await listCoverage(rosterQuery(request));

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

function rosterQuery(request: NextRequest): CoverageQuery {
  return {
    scope_type: stringParam(request, "scope_type") as CoverageQuery["scope_type"],
    scope_id: stringParam(request, "scope_id"),
    active: booleanParam(request, "active"),
    limit: positiveIntParam(request, "limit"),
  };
}
