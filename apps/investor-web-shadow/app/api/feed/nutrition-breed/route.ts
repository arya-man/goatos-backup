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

      // Return distinct breeds for the given farm
      if (searchParams.get("list") === "breeds") {
        const farmFilter = farm ? "WHERE farm = @farm" : "";
        const rows = await queryBigQuery(
          `SELECT DISTINCT breed FROM ${table("last_7_days_feed_per_animal")} ${farmFilter} ORDER BY breed`,
          farm ? { farm } : undefined
        );
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const breeds = (rows as any[]).map((r: any) => String(r.breed || ""));
        return NextResponse.json({ breeds });
      }

      const feedType = searchParams.get("feed_type");
      const breed = searchParams.get("breed");

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
      if (breed) {
        conditions.push("LOWER(breed) = LOWER(@breed)");
        params.breed = breed;
      }

      const whereClause = conditions.length > 0 ? `WHERE ${conditions.join(" AND ")}` : "";

      const rows = await queryBigQuery(
        `SELECT date, status, feed, feed_gms_per_animal_per_day
         FROM ${table("last_7_days_feed_per_animal")}
         ${whereClause}
         ORDER BY date`,
        Object.keys(params).length > 0 ? params : undefined
      );
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const data = (rows as any[]).map((r: any) => ({
        status: r.status || "",
        date: serializeDate(r.date),
        feed_per_animal: Math.round(Number(r.feed_gms_per_animal_per_day ?? 0) * 100) / 100,
      }));
      return NextResponse.json({ data });
    }

    return NextResponse.json({ data: TOTAL_FEED_DATA });
  } catch (error) {
    console.error("Error fetching nutrition breed data:", error);
    return NextResponse.json({ error: "Failed to fetch nutrition breed data" }, { status: 500 });
  }
}
