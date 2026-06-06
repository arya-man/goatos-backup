import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { TRENDS_BY_SEASON } from "@/lib/data/mortality";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT * FROM ${table("deaths_monthly_trend_v")}`
      );
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({ data: TRENDS_BY_SEASON });
  } catch (error) {
    console.error("Error fetching mortality trends:", error);
    return NextResponse.json(
      { error: "Failed to fetch mortality trends data" },
      { status: 500 }
    );
  }
}
