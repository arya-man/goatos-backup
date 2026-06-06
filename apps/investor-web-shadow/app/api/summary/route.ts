/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getDailySummary, getWeeklySummary, getMonthlySummary } from "@/lib/data/summary";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const t = table("goats_db_clean_dev");
      const [dailyRows, weeklyRows, monthlyRows] = await Promise.all([
        queryBigQuery(`SELECT
          COUNTIF(LOWER(event) = 'birth') as births,
          COUNTIF(LOWER(event) = 'death') as deaths,
          COUNTIF(LOWER(event) = 'purchase') as purchases,
          COUNTIF(LOWER(event) = 'sale') as sales,
          COUNTIF(LOWER(event) = 'abortion') as abortions
        FROM ${t} WHERE date = DATE_SUB(CURRENT_DATE('Asia/Kolkata'), INTERVAL 1 DAY)`),
        queryBigQuery(`SELECT
          COUNTIF(LOWER(event) = 'birth') as births,
          COUNTIF(LOWER(event) = 'death') as deaths,
          COUNTIF(LOWER(event) = 'purchase') as purchases,
          COUNTIF(LOWER(event) = 'sale') as sales,
          COUNTIF(LOWER(event) = 'abortion') as abortions
        FROM ${t} WHERE date >= DATE_SUB(CURRENT_DATE('Asia/Kolkata'), INTERVAL 7 DAY)`),
        queryBigQuery(`SELECT
          COUNTIF(LOWER(event) = 'birth') as births,
          COUNTIF(LOWER(event) = 'death') as deaths,
          COUNTIF(LOWER(event) = 'purchase') as purchases,
          COUNTIF(LOWER(event) = 'sale') as sales,
          COUNTIF(LOWER(event) = 'abortion') as abortions
        FROM ${t} WHERE date >= DATE_SUB(CURRENT_DATE('Asia/Kolkata'), INTERVAL 30 DAY)`),
      ]);

      const toSummary = (rows: any[]) => {
        const r = rows[0] || {};
        return {
          births: Number(r.births || 0),
          deaths: Number(r.deaths || 0),
          purchases: Number(r.purchases || 0),
          sales: Number(r.sales || 0),
          abortions: Number(r.abortions || 0),
        };
      };

      return NextResponse.json({
        data: {
          daily: toSummary(dailyRows as any[]),
          weekly: toSummary(weeklyRows as any[]),
          monthly: toSummary(monthlyRows as any[]),
        },
      });
    }

    // Mock fallback
    return NextResponse.json({
      data: {
        daily: getDailySummary(),
        weekly: getWeeklySummary(),
        monthly: getMonthlySummary(),
      },
    });
  } catch (error) {
    console.error("Error fetching summary data:", error);
    return NextResponse.json({ error: "Failed to fetch summary data" }, { status: 500 });
  }
}
