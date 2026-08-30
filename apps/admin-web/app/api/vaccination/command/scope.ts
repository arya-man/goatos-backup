import { NextResponse } from "next/server";
import type { CommandBoardCohortCellParams, CommandBoardDrilldownScope } from "@/lib/api/server";

// Shared parsing for the command board's drilldown route handlers.
//
// Park scope is passed through, NOT trusted: the backend clamps park_id to the caller's grants on
// every one of these routes exactly as it does on /vaccination/command. A drilldown is a different
// route, not a different trust boundary.

export function drilldownScopeFromRequest(request: Request): CommandBoardDrilldownScope {
  const params = new URL(request.url).searchParams;
  const limit = Number(params.get("limit"));
  return {
    driveBatchId: params.get("drive_batch_id") ?? undefined,
    parkId: params.get("park_id") ?? undefined,
    asOf: params.get("as_of") ?? undefined,
    limit: Number.isFinite(limit) && limit > 0 ? limit : undefined,
    cursor: params.get("cursor") ?? undefined,
  };
}

// cohortCellFromRequest returns null when the cell keys are absent, which is a 400 rather than an
// unscoped read: without the cell this is the tenant-wide scan the split exists to remove.
export function cohortCellFromRequest(request: Request): CommandBoardCohortCellParams | null {
  const params = new URL(request.url).searchParams;
  const managementStage = params.get("management_stage");
  const sex = params.get("sex");
  const doseCodes = (params.get("dose_codes") ?? "")
    .split(",")
    .map((code) => code.trim())
    .filter(Boolean);
  if (!managementStage || !sex || doseCodes.length === 0) return null;
  return {
    ...drilldownScopeFromRequest(request),
    // "" is a real cell key — the park-less cohort — so it is passed through rather than treated
    // as missing.
    cohortParkId: params.get("cohort_park_id") ?? "",
    managementStage,
    sex,
    doseCodes,
  };
}

export function jsonOrError<T>(result: { ok: true; data: T } | { ok: false; error: { message: string; status?: number } }) {
  if (!result.ok) {
    return NextResponse.json(
      { error: result.error.message },
      { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } },
    );
  }
  return NextResponse.json(result.data, { headers: { "Cache-Control": "no-store" } });
}

export function badCell() {
  return NextResponse.json(
    { error: "management_stage, sex and dose_codes identify the cohort cell and are required" },
    { status: 400, headers: { "Cache-Control": "no-store" } },
  );
}
