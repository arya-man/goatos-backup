import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getAvgWeightByLoad } from "@/lib/data/fattening";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT load, week_label, avg_weight FROM ${table("weighingprogression_loadwise")}`
      );
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({ data: getAvgWeightByLoad() });
  } catch (error) {
    console.error("Error fetching loadwise weight data:", error);
    return NextResponse.json(
      { error: "Failed to fetch loadwise weight data" },
      { status: 500 }
    );
  }
}
