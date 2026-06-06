import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const farm = searchParams.get("farm") || "TOTAL"; // TOTAL | CBE | CPT
    const date = searchParams.get("date"); // optional YYYY-MM-DD filter

    if (USE_BIGQUERY) {
      const query = date
        ? `SELECT date, farm, no_of_kids, milk_quantity_litres, milk_fed_litres FROM ${table("milk_fed_kids_with_quantity")} WHERE UPPER(farm) = @farm AND date = @date ORDER BY date ASC`
        : `SELECT date, farm, no_of_kids, milk_quantity_litres, milk_fed_litres FROM ${table("milk_fed_kids_with_quantity")} WHERE UPPER(farm) = @farm ORDER BY date ASC`;
      const params = date ? { farm: farm.toUpperCase(), date } : { farm: farm.toUpperCase() };

      const rows = await queryBigQuery<{
        date: { value: string } | string;
        farm: string;
        no_of_kids: number;
        milk_quantity_litres: number;
        milk_fed_litres: number;
      }>(query, params);

      const data = rows.map((r) => ({
        date: String((r.date as { value: string })?.value ?? r.date),
        farm: r.farm,
        no_of_kids: Number(r.no_of_kids || 0),
        milk_quantity_litres: Number(r.milk_quantity_litres || 0),
        milk_fed_litres: Number(r.milk_fed_litres || 0),
      }));

      return NextResponse.json({ data });
    }

    return NextResponse.json({ data: [] });
  } catch (error) {
    console.error("Error fetching milk fed kids data:", error);
    return NextResponse.json({ error: "Failed to fetch milk fed kids data" }, { status: 500 });
  }
}
