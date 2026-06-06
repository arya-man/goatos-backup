import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const shed = searchParams.get("shed");
    const farm = searchParams.get("farm");

    if (USE_BIGQUERY) {
      const t = table("counting_shed_capacity_status_dev");
      const conditions: string[] = [];
      const params: Record<string, unknown> = {};

      if (shed) {
        conditions.push("shed = @shed");
        params.shed = shed;
      }
      if (farm) {
        conditions.push("farm = @farm");
        params.farm = farm;
      }

      let sql = `SELECT * FROM ${t}`;
      if (conditions.length > 0) {
        sql += ` WHERE ${conditions.join(" AND ")}`;
      }

      const rows = await queryBigQuery(sql, params);
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({ data: [] });
  } catch (error) {
    console.error("Error fetching infra housing data:", error);
    return NextResponse.json({ error: "Failed to fetch infra housing data" }, { status: 500 });
  }
}