/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, serializeDate, table } from "@/lib/bigquery";

export const dynamic = "force-dynamic";

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT
           month,
           FORMAT_DATE('%b %Y', month) AS month_label,
           farm,
           avg_animal_count,
           feed_spend
         FROM ${table("monthly_animals_vs_feed")}
         WHERE farm IN ('TOTAL', 'CBE', 'CPT')
         ORDER BY month ASC, farm`
      );

      const data = (rows as any[]).map((r: any) => ({
        month: serializeDate(r.month),
        monthLabel: String(r.month_label ?? ""),
        farm: String(r.farm ?? ""),
        avgAnimalCount: Number(r.avg_animal_count ?? 0),
        feedSpend: Number(r.feed_spend ?? 0),
      }));

      return NextResponse.json({ data });
    }

    return NextResponse.json({ data: [] });
  } catch (error) {
    console.error("Error fetching monthly animals vs feed data:", error);
    return NextResponse.json(
      { error: "Failed to fetch monthly animals vs feed data" },
      { status: 500 }
    );
  }
}
