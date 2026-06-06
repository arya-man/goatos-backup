import { NextResponse } from "next/server";
import { getFeedData, getSeasonsData } from "@/lib/data-loader";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table, serializeDate } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const farm = searchParams.get("farm");
    const type = searchParams.get("type");

    if (type === "seasons") {
      if (USE_BIGQUERY) {
        const rows = await queryBigQuery(`SELECT * FROM ${table("seasons_clean")}`);
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const data = (rows as any[]).map((r: any) => ({
          Region: r.Region || r.region || "",
          Crop: r.Crop || r.crop || "",
          Start_Date: serializeDate(r.Start_Date || r.start_date),
          End_Date: serializeDate(r.End_Date || r.end_date),
        }));
        return NextResponse.json({ data });
      }
      const seasons = await getSeasonsData();
      return NextResponse.json({ data: seasons });
    }

    if (USE_BIGQUERY) {
      const farmFilter = farm ? `WHERE farm = @farm` : "";
      const rows = await queryBigQuery(
        `SELECT * FROM ${table("last_10_loads_feedwise")} ${farmFilter} ORDER BY purchase_date DESC`,
        farm ? { farm } : undefined
      );
      // Map BigQuery snake_case columns to FeedLoadRecord fields
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const data = (rows as any[]).map((r: any) => ({
        farm: r.farm || '',
        load_id: Number(r.load_id || 0),
        load_type: r.load_type || '',
        purchase_date: serializeDate(r.purchase_date),
        total_purchased_qty: Math.round(Number(r.total_purchased_qty || 0) * 100) / 100,
        consumption_per_day: Math.round(Number(r.consumption_per_day || 0) * 100) / 100,
        current_stock: Math.round(Number(r.current_stock || 0) * 100) / 100,
        days_stock_can_last: Math.round(Number(r.days_stock_can_last || 0) * 100) / 100,
        days_consumed_so_far: Math.round(Number(r.days_consumed_so_far || 0) * 100) / 100,
        farm_load_total_consumed_qty: Math.round(Number(r.farm_load_total_consumed_qty || 0) * 100) / 100,
        last_load_total_cost: Math.round(Number(r.last_load_total_cost || 0) * 100) / 100,
        paid_so_far: Math.round(Number(r.paid_so_far || 0) * 100) / 100,
        pending_amount: Math.round(Number(r.pending_amount || 0) * 100) / 100,
        payment_status: r.payment_status || '',
        last_payment_date: serializeDate(r.last_payment_date),
        last_payment_amount: Math.round(Number(r.last_payment_amount || 0) * 100) / 100,
        purchase_cost: Math.round(Number(r.purchase_cost || 0) * 100) / 100,
        transport_cost: Math.round(Number(r.transport_cost || 0) * 100) / 100,
      }));
      return NextResponse.json({ data });
    }

    let records = await getFeedData();

    if (farm) {
      records = records.filter((r) => r.farm === farm);
    }

    return NextResponse.json({ data: records });
  } catch (error) {
    console.error("Error fetching feed data:", error);
    return NextResponse.json({ error: "Failed to fetch feed data" }, { status: 500 });
  }
}
