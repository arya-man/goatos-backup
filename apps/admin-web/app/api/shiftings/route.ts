/* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unused-vars */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getShiftings } from "@/lib/data/shiftings";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const _stage = searchParams.get("stage");

    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT goat_id, stage, days_in_stage FROM ${table("cbe_kids_current_stage_days")} ORDER BY days_in_stage DESC`
      );
      const data = (rows as any[]).map((r: any, i: number) => ({
        rowNum: i + 1,
        goatId: r.goat_id || '',
        stage: r.stage || '',
        daysInStage: Number(r.days_in_stage || 0),
      }));
      return NextResponse.json({ data });
    }

    // Mock fallback
    return NextResponse.json({ data: getShiftings() });
  } catch (error) {
    console.error("Error fetching shiftings data:", error);
    return NextResponse.json({ error: "Failed to fetch shiftings data" }, { status: 500 });
  }
}
