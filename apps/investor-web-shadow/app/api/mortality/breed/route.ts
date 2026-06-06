import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { MORTALITY_BY_BREED } from "@/lib/data/mortality";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT * FROM ${table("mortality_overall_breedwise_dev")}`
      );
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({ data: MORTALITY_BY_BREED });
  } catch (error) {
    console.error("Error fetching breed-wise mortality:", error);
    return NextResponse.json(
      { error: "Failed to fetch breed-wise mortality data" },
      { status: 500 }
    );
  }
}
