/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table, serializeDate } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const farm = searchParams.get("farm") || "TOTAL";

    if (USE_BIGQUERY) {
      const round2 = (v: any) => parseFloat(Number(v || 0).toFixed(2));

      const rows = await queryBigQuery(`
        SELECT date, farm, feed_spend, kid_count, daily_gain_value
        FROM ${table("feed_daily_business")}
        WHERE farm = '${farm}'
        ORDER BY date ASC
      `);

      return NextResponse.json({
        data: {
          chartData: (rows as any[]).map((r: any) => ({
            date: serializeDate(r.date),
            feed_spend: round2(r.feed_spend),
            daily_gain_value: round2(r.daily_gain_value),
            kid_count: Number(r.kid_count || 0),
          })),
        },
      });
    }

    // Mock data fallback
    return NextResponse.json({
      data: {
        chartData: [],
      },
    });
  } catch (error) {
    console.error("[business] API error:", error);
    return NextResponse.json(
      { error: "Failed to fetch business data" },
      { status: 500 }
    );
  }
}
