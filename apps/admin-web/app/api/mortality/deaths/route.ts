import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { TRENDS_BY_STATUS } from "@/lib/data/mortality";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const status = searchParams.get("status");

    if (USE_BIGQUERY) {
      let sql = `SELECT * FROM ${table("deaths_fact_dev")}`;
      const params: Record<string, unknown> = {};

      if (status) {
        sql += ` WHERE status = @status`;
        params.status = status;
      }

      const rows = await queryBigQuery(sql, Object.keys(params).length > 0 ? params : undefined);
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    const mockData = status
      ? TRENDS_BY_STATUS.filter((row) => row.name === status)
      : TRENDS_BY_STATUS;
    return NextResponse.json({ data: mockData });
  } catch (error) {
    console.error("Error fetching deaths data:", error);
    return NextResponse.json(
      { error: "Failed to fetch deaths data" },
      { status: 500 }
    );
  }
}
