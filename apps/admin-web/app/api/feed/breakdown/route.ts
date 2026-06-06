import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table, serializeDate } from "@/lib/bigquery";
import { EXPENDITURE_BY_TYPE } from "@/lib/data/feed";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT feed, date, total_spend
         FROM ${table("feed_daily_expense_feedwise")}
         WHERE date >= DATE_SUB(CURRENT_DATE('Asia/Kolkata'), INTERVAL 8 MONTH) AND date < CURRENT_DATE('Asia/Kolkata')
         ORDER BY date, total_spend DESC`
      );
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const data = (rows as any[]).map((r: any) => ({
        feed_type: r.feed || "",
        date: serializeDate(r.date),
        total_spend: Math.round(Number(r.total_spend || 0) * 100) / 100,
      }));
      return NextResponse.json({ data });
    }

    return NextResponse.json({ data: EXPENDITURE_BY_TYPE });
  } catch (error) {
    console.error("Error fetching feed breakdown data:", error);
    return NextResponse.json({ error: "Failed to fetch feed breakdown data" }, { status: 500 });
  }
}
