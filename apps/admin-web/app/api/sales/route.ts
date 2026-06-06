/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, serializeDate, table } from "@/lib/bigquery";

export const dynamic = "force-dynamic";

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const farm = searchParams.get("farm") || "TOTAL";

    if (USE_BIGQUERY) {
      const [rows, feedVsSalesRows] = await Promise.all([
        queryBigQuery(
        `SELECT
          FORMAT_DATE('%b %Y', DATE_TRUNC(date, MONTH)) AS month,
          DATE_TRUNC(date, MONTH) AS month_start,
          SUM(
            CASE
              WHEN number_of_animals_loads IS NOT NULL AND number_of_animals_loads > 0
                THEN number_of_animals_loads
              ELSE COALESCE(number_of_males, 0) + COALESCE(number_of_females, 0)
            END
          ) AS sale_count,
          SUM(total_sales_value) AS sales_amount
        FROM ${table("salesDB_clean")}
        WHERE date IS NOT NULL
          AND (@farm = 'TOTAL' OR UPPER(farm) = @farm)
        GROUP BY month, month_start
        ORDER BY month_start`,
        { farm }
        ),
        queryBigQuery(
          `SELECT
            FORMAT_DATE('%b %Y', month) AS month_label,
            month,
            farm,
            feed_spend,
            sales_revenue
          FROM ${table("monthly_feed_vs_sales")}
          WHERE farm = @farm
          ORDER BY month ASC`,
          { farm }
        ),
      ]);

      const data = (rows as any[]).map((r: any) => ({
        month: String(r.month ?? ""),
        saleCount: Number(r.sale_count ?? 0),
        salesAmount: Number(r.sales_amount ?? 0),
      }));

      const feedVsSales = (feedVsSalesRows as any[]).map((r: any) => ({
        month: serializeDate(r.month),
        monthLabel: String(r.month_label ?? ""),
        farm: String(r.farm ?? farm),
        feedSpend: Number(r.feed_spend ?? 0),
        salesRevenue: Number(r.sales_revenue ?? 0),
      }));

      return NextResponse.json({ data, feedVsSales });
    }

    return NextResponse.json({ data: [], feedVsSales: [] });
  } catch (error) {
    console.error("Error fetching sales data:", error);
    return NextResponse.json(
      { error: "Failed to fetch sales data" },
      { status: 500 }
    );
  }
}
