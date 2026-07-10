import { type NextRequest, NextResponse } from "next/server";
import { listBackupConfig, type BackupConfigQuery } from "@/lib/api/server";
import { positiveIntParam, stringParam } from "../query";

export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  const result = await listBackupConfig(rosterQuery(request));

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

function rosterQuery(request: NextRequest): BackupConfigQuery {
  return {
    scope_type: stringParam(request, "scope_type") as BackupConfigQuery["scope_type"],
    scope_id: stringParam(request, "scope_id"),
    limit: positiveIntParam(request, "limit"),
  };
}
