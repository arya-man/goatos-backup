import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { MORTALITY_BY_LOAD } from "@/lib/data/mortality";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT * FROM ${table("load_Wise_pct_data")}`
      );
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({ data: MORTALITY_BY_LOAD });
  } catch (error) {
    console.error("Error fetching load-wise mortality:", error);
    return NextResponse.json(
      { error: "Failed to fetch load-wise mortality data" },
      { status: 500 }
    );
  }
}
