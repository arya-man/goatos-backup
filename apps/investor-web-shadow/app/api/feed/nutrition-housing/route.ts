import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table, serializeDate } from "@/lib/bigquery";
import { TOTAL_FEED_DATA } from "@/lib/data/feed";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    if (USE_BIGQUERY) {
      const { searchParams } = new URL(request.url);
      const farm = searchParams.get("farm");

      // Return distinct shed prefixes for the given farm
      if (searchParams.get("list") === "sheds") {
        const farmFilter = farm ? "WHERE farm = @farm" : "";
        const rows = await queryBigQuery(
          `SELECT DISTINCT shed FROM ${table("last_7_days_feed_per_animal_shedwise")} ${farmFilter} ORDER BY shed`,
          farm ? { farm } : undefined
        );
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const sheds = (rows as any[]).map((r: any) => String(r.shed || ""));
        return NextResponse.json({ sheds });
      }

      const feedType = searchParams.get("feed_type");
      const shed = searchParams.get("shed");

      const conditions: string[] = [];
      const params: Record<string, unknown> = {};

      if (feedType) {
        conditions.push("LOWER(feed) = LOWER(@feed_type)");
        params.feed_type = feedType;
      }
      if (farm) {
        conditions.push("farm = @farm");
        params.farm = farm;
      }
      if (shed) {
        conditions.push("STARTS_WITH(LOWER(shed), LOWER(@shed))");
        params.shed = shed;
      }

      const whereClause = conditions.length > 0 ? `WHERE ${conditions.join(" AND ")}` : "";

      // Fetch feed data and shed_tag mapping in parallel
      const countingTable = table("counting_db_with_holding_dev");
      const [rows, tagRows] = await Promise.all([
        queryBigQuery(
          `SELECT *
           FROM ${table("last_7_days_feed_per_animal_shedwise")}
           ${whereClause}
           ORDER BY date`,
          Object.keys(params).length > 0 ? params : undefined
        ),
        queryBigQuery(
          `SELECT DISTINCT shed, shed_tag FROM ${countingTable} WHERE date = (SELECT MAX(date) FROM ${countingTable})${farm ? " AND farm = @farm" : ""} AND shed IS NOT NULL AND shed_tag IS NOT NULL`,
          farm ? { farm } : undefined
        ),
      ]);
      // Build shed → shed_tag lookup
      const tagMap = new Map<string, string>();
      for (const t of tagRows as Record<string, unknown>[]) {
        if (t.shed && t.shed_tag) tagMap.set(String(t.shed), String(t.shed_tag));
      }
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const data = (rows as any[]).map((r: any) => {
        const shedName = r.shed || r.Shed || r.housing_unit || "";
        return {
          housing_unit: shedName,
          shed_tag: tagMap.get(shedName) || "",
          date: serializeDate(r.date),
          feed_per_animal: Math.round(Number(r.feed_gms_per_animal_per_day ?? r.feed_per_animal ?? 0) * 100) / 100,
        };
      });
      return NextResponse.json({ data });
    }

    return NextResponse.json({ data: TOTAL_FEED_DATA });
  } catch (error) {
    console.error("Error fetching nutrition housing data:", error);
    return NextResponse.json({ error: "Failed to fetch nutrition housing data" }, { status: 500 });
  }
}
