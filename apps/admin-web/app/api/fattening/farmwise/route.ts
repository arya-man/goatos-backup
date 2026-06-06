import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { getFatteningStages } from "@/lib/data/fattening";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    if (USE_BIGQUERY) {
      const { searchParams } = new URL(request.url);
      const farm = searchParams.get("farm");
      const includeTotal = searchParams.get("include_total") === "true";

      const conditions: string[] = [
        "shed_tag_out IN ('F1','F2','K0','K1','K2','K3')",
      ];
      const params: Record<string, unknown> = {};

      if (farm) {
        conditions.push("farm = @farm");
        params.farm = farm;
      }

      const whereClause = conditions.length
        ? `WHERE ${conditions.join(" AND ")}`
        : "";

      const rows = await queryBigQuery(
        `SELECT * FROM ${table("growth_farmwise_weighing")} ${whereClause}`,
        Object.keys(params).length ? params : undefined
      );

      return NextResponse.json({ data: rows, include_total: includeTotal });
    }

    // Mock fallback
    return NextResponse.json({ data: getFatteningStages() });
  } catch (error) {
    console.error("Error fetching farmwise fattening data:", error);
    return NextResponse.json(
      { error: "Failed to fetch farmwise fattening data" },
      { status: 500 }
    );
  }
}
