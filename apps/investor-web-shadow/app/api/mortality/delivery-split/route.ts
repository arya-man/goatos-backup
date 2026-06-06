import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { KIDS_MORTALITY_SPLIT } from "@/lib/data/mortality";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT * FROM ${table("mortality_by_litter_size_overall_dev")}`
      );
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({ data: KIDS_MORTALITY_SPLIT });
  } catch (error) {
    console.error("Error fetching delivery-split mortality:", error);
    return NextResponse.json(
      { error: "Failed to fetch delivery-split mortality data" },
      { status: 500 }
    );
  }
}
