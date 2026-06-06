import { NextResponse } from "next/server";
import { queryBigQuery, table } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export interface CoreFarmGenderwiseRow {
  farm: string;
  breed: string;
  gender: string;
  total_count: number;
}

export async function GET() {
  try {
    const rows = await queryBigQuery<CoreFarmGenderwiseRow>(
      `SELECT farm, breed, gender, total_count FROM ${table("core_farm_genderwise")}`,
      {}
    );
    return NextResponse.json({ data: rows });
  } catch (error) {
    console.error("Error fetching core farm genderwise data:", error);
    return NextResponse.json({ error: "Failed to fetch data" }, { status: 500 });
  }
}
