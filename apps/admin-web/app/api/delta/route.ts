/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, serializeDate } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

const DELTA_TABLE = 'goatos-sheets.ceo_dashboard.delta_monthly_summary_dev';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const farm = searchParams.get("farm") || "TOTAL";

    if (USE_BIGQUERY) {
      const round2 = (v: any) => parseFloat(Number(v || 0).toFixed(2));

      const rows = await queryBigQuery(`
        SELECT
          month,
          month_end,
          month_label,
          farm,
          feed_spend,
          hr_expense,
          total_expense,
          meat_gain_value,
          milk_revenue,
          total_revenue,
          farm_value,
          net_delta
        FROM \`${DELTA_TABLE}\`
        WHERE farm = '${farm}'
        ORDER BY month DESC
      `);

      return NextResponse.json({
        data: {
          rows: (rows as any[]).map((r: any) => ({
            month: serializeDate(r.month),
            month_end: serializeDate(r.month_end),
            month_label: r.month_label ?? '',
            farm: r.farm ?? farm,
            feed_spend: round2(r.feed_spend),
            hr_expense: round2(r.hr_expense),
            total_expense: round2(r.total_expense),
            meat_gain_value: round2(r.meat_gain_value),
            milk_revenue: round2(r.milk_revenue),
            total_revenue: round2(r.total_revenue),
            farm_value: round2(r.farm_value),
            net_delta: round2(r.net_delta),
          })),
        },
      });
    }

    // Mock data fallback
    return NextResponse.json({ data: { rows: [] } });
  } catch (error) {
    console.error("[delta] API error:", error);
    return NextResponse.json(
      { error: "Failed to fetch delta data" },
      { status: 500 }
    );
  }
}
