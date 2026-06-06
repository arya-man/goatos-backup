import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getInfraKPI } from "@/lib/data/infra";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const farm = searchParams.get("farm") || "CBE";

    if (USE_BIGQUERY) {
      const t = table("shed_capacity_count_dev");
      const sql = `SELECT total_capacity_farm, total_vacancy_farm FROM ${t} WHERE LOWER(farm) = LOWER(@farm) LIMIT 1`;
      const rows = await queryBigQuery<{ total_capacity_farm: number; total_vacancy_farm: number }>(sql, { farm });
      const row = rows[0];
      return NextResponse.json({
        data: {
          capacity: Number(row?.total_capacity_farm || 0),
          vacancy: Number(row?.total_vacancy_farm || 0),
        },
      });
    }

    // Mock fallback
    const farmKey = (farm === "CPT" ? "CPT" : "CBE") as "CBE" | "CPT";
    return NextResponse.json({ data: getInfraKPI(farmKey) });
  } catch (error) {
    console.error("Error fetching infra capacity data:", error);
    return NextResponse.json({ error: "Failed to fetch infra capacity data" }, { status: 500 });
  }
}
