import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getSeasonsData } from "@/lib/data-loader";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT Region, Crop, Start_Date, End_Date FROM ${table("seasons_clean")}`
      );
      return NextResponse.json({ data: rows });
    }

    const seasons = await getSeasonsData();
    return NextResponse.json({ data: seasons });
  } catch (error) {
    console.error("Error fetching seasons data:", error);
    return NextResponse.json({ error: "Failed to fetch seasons data" }, { status: 500 });
  }
}
