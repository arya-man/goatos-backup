import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getLitterSize } from "@/lib/data/births";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery<{ breed: string; avg_litter_size: number }>(
        `SELECT breed, avg_litter_size FROM ${table("birth_analysis_view")}`
      );
      return NextResponse.json({ data: rows });
    }

    const data = getLitterSize();
    return NextResponse.json({ data });
  } catch (error) {
    console.error("Error fetching birth analysis data:", error);
    return NextResponse.json({ error: "Failed to fetch birth analysis data" }, { status: 500 });
  }
}
