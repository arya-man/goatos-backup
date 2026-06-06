import { NextResponse } from "next/server";
import { queryBigQuery, table, serializeDate } from "@/lib/bigquery";
import { normalizeFeedName } from "@/lib/constants";

export const dynamic = 'force-dynamic';

// Must stay in sync with ALLOWED_STOCK_FEEDS in feed/page.tsx
const ALLOWED_FEEDS = [
  'masoor dhal bhusa',
  'concentrate',
  'vgoats grain mix',
  'baking soda',
  'uht milk',
  'toor dal bhusa pellet',
];

export async function GET(request: Request) {
  const { searchParams } = new URL(request.url);
  const farm = searchParams.get("farm") || "";

  try {
    const farmFilter = farm ? `AND farm = @farm` : "";
    const feedList = ALLOWED_FEEDS.map((f) => `'${f}'`).join(", ");

    const rows = await queryBigQuery(
      `WITH ranked AS (
         SELECT
           farm,
           load_id,
           load_type AS feed,
           purchase_date,
           consumption_start_date,
           consumption_per_day AS avg_consumption_per_day,
           days_consumed_so_far,
           IFNULL(days_stock_can_last, 0) AS days_stock_can_last,
           total_purchased_qty,
           ROW_NUMBER() OVER (PARTITION BY load_type ORDER BY load_id DESC) AS rn
         FROM ${table("feedDB_load_summary")}
         WHERE LOWER(load_type) IN (${feedList})
         ${farmFilter}
       )
       SELECT farm, load_id, feed, purchase_date, consumption_start_date,
              avg_consumption_per_day, days_consumed_so_far, days_stock_can_last, total_purchased_qty
       FROM ranked
       WHERE rn <= 10
       ORDER BY feed, load_id DESC`,
      farm ? { farm } : undefined
    );

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const data = (rows as any[]).map((r: any) => ({
      farm: String(r.farm || ""),
      load_id: Number(r.load_id || 0),
      feed: normalizeFeedName(String(r.feed || "")),
      purchase_date: serializeDate(r.purchase_date),
      consumption_start_date: serializeDate(r.consumption_start_date),
      avg_consumption_per_day: Math.round(Number(r.avg_consumption_per_day || 0) * 100) / 100,
      days_consumed_so_far: Number(r.days_consumed_so_far || 0),
      days_stock_can_last: Number(r.days_stock_can_last || 0),
      total_purchased_qty: Math.round(Number(r.total_purchased_qty || 0) * 100) / 100,
    }));

    return NextResponse.json({ data });
  } catch (error) {
    console.error("Error fetching feed load summary:", error);
    return NextResponse.json({ error: "Failed to fetch feed load summary" }, { status: 500 });
  }
}
