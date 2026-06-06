import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { MOTHER_MORTALITY_BREED } from "@/lib/data/mortality";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT * FROM ${table("mother_litter_size_dev_breedwise")}`
      );
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({ data: MOTHER_MORTALITY_BREED });
  } catch (error) {
    console.error("Error fetching delivery mortality by breed:", error);
    return NextResponse.json(
      { error: "Failed to fetch delivery mortality data" },
      { status: 500 }
    );
  }
}
