import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { MORTALITY_SUMMARY_MONTH } from "@/lib/data/mortality";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT * FROM ${table("mortality_this_month_dev")}`
      );
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({ data: MORTALITY_SUMMARY_MONTH });
  } catch (error) {
    console.error("Error fetching this-month mortality:", error);
    return NextResponse.json(
      { error: "Failed to fetch this-month mortality data" },
      { status: 500 }
    );
  }
}
