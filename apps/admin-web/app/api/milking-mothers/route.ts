import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT farm, goatid, breed FROM ${table("milking_goats_list")} ORDER BY farm, goatid`
      );
      return NextResponse.json({ data: rows });
    }

    return NextResponse.json({ data: [] });
  } catch (error) {
    console.error("Error fetching milking mothers data:", error);
    return NextResponse.json({ error: "Failed to fetch milking mothers data" }, { status: 500 });
  }
}
