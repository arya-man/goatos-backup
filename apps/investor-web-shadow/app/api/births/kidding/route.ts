import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getKiddingEfficiency } from "@/lib/data/births";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery<{ breed: string; delivered_last_8m_pct: number }>(
        `SELECT breed, delivered_last_8m_pct FROM ${table("breedwise_kidding_8m")} WHERE breed IS NOT NULL ORDER BY delivered_last_8m_pct DESC`
      );
      return NextResponse.json({ data: rows });
    }

    const data = getKiddingEfficiency();
    return NextResponse.json({ data });
  } catch (error) {
    console.error("Error fetching kidding efficiency data:", error);
    return NextResponse.json({ error: "Failed to fetch kidding efficiency data" }, { status: 500 });
  }
}
