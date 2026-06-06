import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getKidsWeighedPct } from "@/lib/data/fattening";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT week_label, pct_weighed FROM ${table("kids_counting_vs_weighing")} ORDER BY week_label`
      );
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({ data: getKidsWeighedPct() });
  } catch (error) {
    console.error("Error fetching weighing data:", error);
    return NextResponse.json(
      { error: "Failed to fetch weighing data" },
      { status: 500 }
    );
  }
}
