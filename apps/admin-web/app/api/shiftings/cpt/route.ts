/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT goat_id, stage, days_in_stage FROM ${table("cpt_kids_current_stage_days")} ORDER BY days_in_stage DESC`
      );
      const data = (rows as any[]).map((r: any, i: number) => ({
        rowNum: i + 1,
        goatId: r.goat_id || '',
        stage: r.stage || '',
        daysInStage: Number(r.days_in_stage || 0),
      }));
      return NextResponse.json({ data });
    }

    // Mock fallback — empty until local mock data is available
    return NextResponse.json({ data: [] });
  } catch (error) {
    console.error("Error fetching CPT shiftings data:", error);
    return NextResponse.json({ error: "Failed to fetch CPT shiftings data" }, { status: 500 });
  }
}
