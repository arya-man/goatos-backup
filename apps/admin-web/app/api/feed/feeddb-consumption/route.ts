import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table, serializeDate } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  const { searchParams } = new URL(request.url);
  const feed = searchParams.get("feed") || "";

  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT
           Consumed_Date AS date,
           Farm,
           SUM(Consumed_Qty) AS value
         FROM ${table("feedDB_clean")}
         WHERE
           Record_Type = 'Consumption'
           AND Feed = @feed
           AND Consumed_Date >= DATE_SUB(CURRENT_DATE('Asia/Kolkata'), INTERVAL 30 DAY)
           AND Consumed_Date < CURRENT_DATE('Asia/Kolkata')
         GROUP BY date, Farm
         ORDER BY date`,
        { feed }
      );
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const data = (rows as any[]).map((r: any) => ({
        date: serializeDate(r.date),
        farm: String(r.Farm || ""),
        value: Math.round(Number(r.value || 0) * 100) / 100,
      }));
      return NextResponse.json({ data });
    }

    return NextResponse.json({ data: [] });
  } catch (error) {
    console.error("Error fetching feeddb consumption data:", error);
    return NextResponse.json({ error: "Failed to fetch feeddb consumption data" }, { status: 500 });
  }
}
