import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { AGE_DISTRIBUTION } from "@/lib/data/feed";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    if (USE_BIGQUERY) {
      const { searchParams } = new URL(request.url);
      const nowIST = new Date(new Date().toLocaleString("en-US", { timeZone: "Asia/Kolkata" }));
      nowIST.setDate(nowIST.getDate() - 1);
      const date = searchParams.get("date") || `${nowIST.getFullYear()}-${String(nowIST.getMonth() + 1).padStart(2, "0")}-${String(nowIST.getDate()).padStart(2, "0")}`;

      const rows = await queryBigQuery(
        `SELECT age as breed_age, SUM(feed_for_breed_age_today) as feed_amount FROM ${table("feed_breed_age_daily")} WHERE date = @date AND feed_for_breed_age_today IS NOT NULL GROUP BY age ORDER BY age`,
        { date }
      );
      return NextResponse.json({ data: rows });
    }

    return NextResponse.json({ data: AGE_DISTRIBUTION });
  } catch (error) {
    console.error("Error fetching feed age data:", error);
    return NextResponse.json({ error: "Failed to fetch feed age data" }, { status: 500 });
  }
}
