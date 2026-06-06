import { NextResponse } from "next/server";
import { getCountingData } from "@/lib/data-loader";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const farm = searchParams.get("farm") || "CBE";

    if (USE_BIGQUERY) {
      const t = table("counting_db_with_holding_dev");
      const rows = await queryBigQuery<{
        shed: string;
        shed_tag: string;
        breed: string;
        goat_count: number;
      }>(
        `SELECT shed, shed_tag, breed, SUM(goat_count) as goat_count
          FROM ${t}
          WHERE date = DATE_SUB(CURRENT_DATE('Asia/Kolkata'), INTERVAL 1 DAY)
            AND LOWER(farm) = LOWER(@farm)
          GROUP BY shed, shed_tag, breed`,
        { farm }
      );
      return NextResponse.json({ date: null, farm, data: rows });
    }

    // CSV fallback
    let records = await getCountingData();

    // Latest date
    const allDates = [...new Set(records.map((r) => r.date))].sort();
    const latestDate = allDates[allDates.length - 1];

    records = records.filter((r) => r.date === latestDate && r.farm === farm && r.shed);

    return NextResponse.json({ date: latestDate, farm, data: records });
  } catch (error) {
    console.error("Error fetching infra data:", error);
    return NextResponse.json({ error: "Failed to fetch infra data" }, { status: 500 });
  }
}
