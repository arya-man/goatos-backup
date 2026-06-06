import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table, serializeDate } from "@/lib/bigquery";
import { DAILY_QUANTITY } from "@/lib/data/feed";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT date, SUM(Consumed_Qty) as value FROM ${table("feedDB_clean")} WHERE date >= DATE_SUB(CURRENT_DATE('Asia/Kolkata'), INTERVAL 8 MONTH) AND date < CURRENT_DATE('Asia/Kolkata') GROUP BY date ORDER BY date`
      );
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const data = (rows as any[]).map((r: any) => ({
        date: serializeDate(r.date),
        value: Math.round(Number(r.value || 0) * 100) / 100,
      }));
      return NextResponse.json({ data });
    }

    return NextResponse.json({ data: DAILY_QUANTITY });
  } catch (error) {
    console.error("Error fetching feed consumption data:", error);
    return NextResponse.json({ error: "Failed to fetch feed consumption data" }, { status: 500 });
  }
}
