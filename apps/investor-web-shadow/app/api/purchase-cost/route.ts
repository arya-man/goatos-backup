/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getGoatPurchaseCosts, getSheepPurchaseCosts } from "@/lib/data/purchase-cost";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const type = searchParams.get("type") || "goat";
    const normalizedType = type.replace(/s$/, ""); // "goats" → "goat", "sheep" → "sheep"

    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT load_display_label, landing_cost_per_kg, vendor, breed
        FROM ${table("procurement_db_loadwise_cost")}
        WHERE LOWER(TRIM(animal_type)) = LOWER(TRIM(@type)) AND landing_cost_per_kg != 0
        ORDER BY load_display_label`,
        { type: normalizedType }
      );
      const data = (rows as any[]).map((r: any) => ({
        label: r.load_display_label || '',
        costPerKg: Number(Number(r.landing_cost_per_kg || 0).toFixed(2)),
        vendor: r.vendor || '',
        breed: r.breed || '',
      }));
      return NextResponse.json({ data });
    }

    // Mock fallback
    const costs = type === "sheep" ? getSheepPurchaseCosts() : getGoatPurchaseCosts();

    return NextResponse.json({ data: costs });
  } catch (error) {
    console.error("Error fetching purchase cost data:", error);
    return NextResponse.json({ error: "Failed to fetch purchase cost data" }, { status: 500 });
  }
}
