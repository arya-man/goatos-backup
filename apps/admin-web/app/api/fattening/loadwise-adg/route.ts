import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getLoadADGByBreed } from "@/lib/data/fattening";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT load, breed, gender, adg FROM ${table("adg_goat_last2")}`
      );
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({ data: getLoadADGByBreed() });
  } catch (error) {
    console.error("Error fetching loadwise ADG data:", error);
    return NextResponse.json(
      { error: "Failed to fetch loadwise ADG data" },
      { status: 500 }
    );
  }
}
