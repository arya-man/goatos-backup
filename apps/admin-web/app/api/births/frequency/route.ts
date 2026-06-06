import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getKiddingFrequency } from "@/lib/data/births";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery<{ breed: string; approx_months_between_births: number }>(
        `SELECT breed, approx_months_between_births FROM ${table("kidding_frequency")}`
      );
      return NextResponse.json({ data: rows });
    }

    const data = getKiddingFrequency();
    return NextResponse.json({ data });
  } catch (error) {
    console.error("Error fetching kidding frequency data:", error);
    return NextResponse.json({ error: "Failed to fetch kidding frequency data" }, { status: 500 });
  }
}
