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
      const overallT = table("total_summary_breeding");
      const [dailyRows, weeklyRows, monthlyRows, overallRows] = await Promise.all([
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
        queryBigQuery<{ label: string; total: number }>(
          `SELECT label, total FROM ${overallT}`
        ),
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

      const overallMap: Record<string, number> = {};
      for (const r of overallRows as any[]) {
        overallMap[r.label] = Number(r.total || 0);
      }
      const overall = {
        births: overallMap["Birth"] ?? 0,
        deaths: overallMap["Death"] ?? 0,
        purchases: overallMap["Purchase (Breeding)"] ?? 0,
        sales: overallMap["Sale"] ?? 0,
        abortions: overallMap["Abortion"] ?? 0,
      };

      return NextResponse.json({
        data: {
          daily: toSummary(dailyRows as any[]),
          weekly: toSummary(weeklyRows as any[]),
          monthly: toSummary(monthlyRows as any[]),
          overall,
        },
      });
    }

    // Mock fallback
    return NextResponse.json({
      data: {
        daily: getDailySummary(),
        weekly: getWeeklySummary(),
        monthly: getMonthlySummary(),
        overall: { births: 0, deaths: 0, purchases: 0, sales: 0, abortions: 0 },
      },
    });
  } catch (error) {
    console.error("Error fetching summary data:", error);
    return NextResponse.json({ error: "Failed to fetch summary data" }, { status: 500 });
  }
}
