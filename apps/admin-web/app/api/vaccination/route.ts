import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, serializeDate } from "@/lib/bigquery";

export const dynamic = "force-dynamic";

export interface VaccinationRecord {
  farm: string;
  shed: string;
  shed_tag: string;
  age: string;
  animal_type: string;
  animal_count: number;
  count_date: string;
  vaccine: string;
  last_vaccination_date: string | null;
  next_due_date: string | null;
  days_until_due: number | null;
  vaccination_status: string | null;
}

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const farm = searchParams.get("farm") || "CBE";

    if (USE_BIGQUERY) {
      const rows = await queryBigQuery<Record<string, unknown>>(
        `SELECT
          farm,
          shed,
          shed_tag,
          age,
          animal_type,
          animal_count,
          count_date,
          vaccine,
          last_vaccination_date,
          next_due_date,
          days_until_due,
          vaccination_status
        FROM \`goatos-sheets.ceo_dashboard.vaccination_dashboard\`
        WHERE LOWER(farm) = LOWER(@farm)
        ORDER BY shed, shed_tag, age, animal_type, vaccine`,
        { farm }
      );

      const data: VaccinationRecord[] = rows.map((r) => ({
        farm: String(r.farm ?? ""),
        shed: String(r.shed ?? ""),
        shed_tag: String(r.shed_tag ?? ""),
        age: String(r.age ?? ""),
        animal_type: String(r.animal_type ?? ""),
        animal_count: Number(r.animal_count ?? 0),
        count_date: serializeDate(r.count_date),
        vaccine: String(r.vaccine ?? ""),
        last_vaccination_date: r.last_vaccination_date ? serializeDate(r.last_vaccination_date) : null,
        next_due_date: r.next_due_date ? serializeDate(r.next_due_date) : null,
        days_until_due: r.days_until_due != null ? Number(r.days_until_due) : null,
        vaccination_status: r.vaccination_status ? String(r.vaccination_status) : null,
      }));

      return NextResponse.json({ farm, data });
    }

    // Fallback: return empty data when BigQuery is not enabled
    return NextResponse.json({ farm, data: [] });
  } catch (error) {
    console.error("Error fetching vaccination data:", error);
    return NextResponse.json({ error: "Failed to fetch vaccination data" }, { status: 500 });
  }
}
