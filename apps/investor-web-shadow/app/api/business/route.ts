/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table, serializeDate } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const farm = searchParams.get("farm") ?? "TOTAL";

    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT date, farm, feed_spend, kid_count, daily_gain_value FROM ${table("feed_daily_business")} WHERE farm = '${farm}' ORDER BY date ASC`
      );

      const chartData = (rows as any[]).map((r: any) => ({
        date: serializeDate(r.date),
        feed_spend: parseFloat(Number(r.feed_spend || 0).toFixed(2)),
        daily_gain_value: parseFloat(Number(r.daily_gain_value || 0).toFixed(2)),
        kid_count: parseFloat(Number(r.kid_count || 0).toFixed(2)),
      }));

      return NextResponse.json({ data: { chartData } });
    }

    return NextResponse.json({ data: { chartData: [] } });
  } catch (error) {
    console.error("[business] API error:", error);
    return NextResponse.json({ error: "Failed to fetch business data" }, { status: 500 });
  }
}
