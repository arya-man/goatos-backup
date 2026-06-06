import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { FEED_DISTRIBUTION } from "@/lib/data/feed";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT
           Feed AS feed_type,
           SUM(Consumed_Qty) AS total_consumed
         FROM ${table("feedDB_clean")}
         WHERE
           Record_Type = 'Consumption'
           AND Consumed_Date >= DATE_SUB(CURRENT_DATE('Asia/Kolkata'), INTERVAL 30 DAY)
           AND Consumed_Date < CURRENT_DATE('Asia/Kolkata')
         GROUP BY Feed
         ORDER BY total_consumed DESC`
      );
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const data = (rows as any[]).map((r: any) => ({
        name: String(r.feed_type || ""),
        value: Math.round(Number(r.total_consumed || 0) * 100) / 100,
      }));
      return NextResponse.json({ data });
    }

    return NextResponse.json({ data: FEED_DISTRIBUTION });
  } catch (error) {
    console.error("Error fetching feed distribution data:", error);
    return NextResponse.json({ error: "Failed to fetch feed distribution data" }, { status: 500 });
  }
}
