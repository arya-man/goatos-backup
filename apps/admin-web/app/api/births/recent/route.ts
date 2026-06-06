import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getBirthCountLast10Days } from "@/lib/data/births";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery<{ date: string; birth_count: number }>(
        `SELECT date, birth_count FROM ${table("v_birth_count_last_10_days")} ORDER BY date`
      );
      return NextResponse.json({ data: rows });
    }

    const data = getBirthCountLast10Days();
    return NextResponse.json({ data });
  } catch (error) {
    console.error("Error fetching recent birth count data:", error);
    return NextResponse.json({ error: "Failed to fetch recent birth count data" }, { status: 500 });
  }
}
