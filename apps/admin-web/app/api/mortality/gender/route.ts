import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { TRENDS_BY_GENDER } from "@/lib/data/mortality";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT * FROM ${table("mortality_genderwise")}`
      );
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({ data: TRENDS_BY_GENDER });
  } catch (error) {
    console.error("Error fetching gender-wise mortality:", error);
    return NextResponse.json(
      { error: "Failed to fetch gender-wise mortality data" },
      { status: 500 }
    );
  }
}
