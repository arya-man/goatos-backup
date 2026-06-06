import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, serializeDate } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        "SELECT goatid, farm, breed, date, no_of_babies, kid_ids FROM `goatos-sheets.ceo_dashboard.parent_stock_table` ORDER BY date DESC, goatid"
      );
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const data = (rows as any[]).map((r: any) => ({
        goatid: String(r.goatid || ""),
        farm: String(r.farm || ""),
        breed: String(r.breed || ""),
        date: serializeDate(r.date),
        no_of_babies: Number(r.no_of_babies || 0),
        kid_ids: String(r.kid_ids || ""),
      }));
      return NextResponse.json({ data });
    }

    return NextResponse.json({ data: [] });
  } catch (error) {
    console.error("Error fetching parent stock data:", error);
    return NextResponse.json(
      { error: "Failed to fetch parent stock data" },
      { status: 500 }
    );
  }
}
