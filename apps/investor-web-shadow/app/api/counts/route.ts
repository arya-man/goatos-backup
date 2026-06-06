import { NextResponse } from "next/server";
import { getCountingData } from "@/lib/data-loader";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const farm = searchParams.get("farm");
    const date = searchParams.get("date");

    if (USE_BIGQUERY) {
      const tableName = table("counting_db_with_holding_dev");

      // Build farm filter conditions (reused for both latest-date lookup and main query)
      const farmConditions: string[] = [];
      const farmParams: Record<string, unknown> = {};

      if (farm) {
        if (farm.toLowerCase() === "core" || farm.toLowerCase() === "core-farms") {
          farmConditions.push("LOWER(farm) IN ('cbe', 'cpt')");
        } else if (farm.toLowerCase() === "holdings") {
          farmConditions.push("farm NOT IN ('CBE', 'CPT', 'cbe','cpt')");
        } else {
          farmConditions.push("LOWER(farm) = LOWER(@farm)");
          farmParams.farm = farm;
        }
      }

      // Compute yesterday's date in YYYY-MM-DD format (IST)
      const nowIST = new Date(new Date().toLocaleString("en-US", { timeZone: "Asia/Kolkata" }));
      nowIST.setDate(nowIST.getDate() - 1);
      const yesterdayStr = `${nowIST.getFullYear()}-${String(nowIST.getMonth() + 1).padStart(2, "0")}-${String(nowIST.getDate()).padStart(2, "0")}`;

      let filterDate = date;
      if (!filterDate) {
        filterDate = yesterdayStr;
      }

      const conditions: string[] = ["date = @date", ...farmConditions];
      const params: Record<string, unknown> = { date: filterDate, ...farmParams };
      const sql = `SELECT * FROM ${tableName} WHERE ${conditions.join(" AND ")}`;
      const rows = await queryBigQuery(sql, params);

      // Fetch KPI summary from daily_summary_dev (yesterday's row)
      let kpiSummary = null;
      try {
        const summaryRows = await queryBigQuery(
          `SELECT * FROM ${table("daily_summary_dev")} WHERE date = @yesterday`,
          { yesterday: yesterdayStr }
        );
        if (summaryRows.length > 0) {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          const s = summaryRows[0] as any;
          const farmKey = farm?.toLowerCase();
          if (farmKey === "cbe") {
            kpiSummary = {
              totalActive: Number(s.cbe_summary_count || 0),
              farmValue: Number(s.cbe_summary_weight_value || 0),
              totalWeight: Number(s.cbe_summary_weight || 0),
            };
          } else if (farmKey === "cpt") {
            kpiSummary = {
              totalActive: Number(s.cpt_summary_count || 0),
              farmValue: Number(s.cpt_summary_weight_value || 0),
              totalWeight: Number(s.cpt_summary_weight || 0),
            };
          } else if (farmKey === "core" || farmKey === "core-farms") {
            kpiSummary = {
              totalActive: Number(s.cbe_summary_count || 0) + Number(s.cpt_summary_count || 0),
              farmValue: Number(s.cbe_summary_weight_value || 0) + Number(s.cpt_summary_weight_value || 0),
              totalWeight: Number(s.cbe_summary_weight || 0) + Number(s.cpt_summary_weight || 0),
            };
          } else if (farmKey === "holdings") {
            kpiSummary = {
              totalActive: Number(s.procurement_summary_count || 0),
              farmValue: Number(s.procurement_summary_weight_value || 0),
              totalWeight: Number(s.procurement_summary_weight || 0),
            };
          } else {
            // overall
            kpiSummary = {
              totalActive: Number(s.farm_total_count || 0),
              farmValue: Number(s.farm_total_weight_value || 0),
              totalWeight: Number(s.farm_total_weight || 0),
            };
          }
        }
      } catch (e) {
        console.error("Error fetching KPI summary:", e);
      }

      const kpiTable = table("counting_kpis_daily");

      // Build farm filter clause for counting_kpis_daily (shared by all three parallel queries)
      const buildKpisFarmClause = (farmKey: string | null): string => {
        if (!farmKey) return "";
        const k = farmKey.toLowerCase();
        if (k === "cbe") return ` AND LOWER(farm) = 'cbe'`;
        if (k === "cpt") return ` AND LOWER(farm) = 'cpt'`;
        if (k === "holdings") return ` AND LOWER(farm) = 'holding farm'`;
        if (k === "core" || k === "core-farms") return ` AND LOWER(farm) IN ('cbe', 'cpt')`;
        return "";
      };
      const kpiFarmClause = buildKpisFarmClause(farm);
      const kpisParams: Record<string, unknown> = { date: filterDate };

      // Run age breakdown, adults gender, and fattening gender queries in parallel
      const [ageRowsResult, adultsGenderResult, kidsGenderResult, fatteningGenderResult] = await Promise.allSettled([
        queryBigQuery<{ animal_type: string; total_count: number }>(
          `SELECT animal_type, total_count FROM ${kpiTable} WHERE as_of_date = @date AND metric_name = 'age_wise_count'${kpiFarmClause}`,
          kpisParams
        ),
        queryBigQuery<{ fattening_gender: string; total_count: number }>(
          `SELECT fattening_gender, total_count FROM ${kpiTable} WHERE as_of_date = @date AND metric_name = 'adults_by_gender'${kpiFarmClause}`,
          kpisParams
        ),
        queryBigQuery<{ fattening_gender: string; total_count: number }>(
          `SELECT fattening_gender, total_count FROM ${kpiTable} WHERE as_of_date = @date AND metric_name = 'kids_by_gender'${kpiFarmClause}`,
          kpisParams
        ),
        queryBigQuery<{ fattening_gender: string; total_count: number }>(
          `SELECT fattening_gender, total_count FROM ${kpiTable} WHERE as_of_date = @date AND metric_name = 'genderwise_fattening_count'${kpiFarmClause}`,
          kpisParams
        ),
      ]);

      let ageBreakdown = null;
      if (ageRowsResult.status === "fulfilled" && ageRowsResult.value.length > 0) {
        const map = new Map<string, number>();
        for (const r of ageRowsResult.value) {
          const key = (r.animal_type || "").toLowerCase();
          map.set(key, (map.get(key) ?? 0) + Number(r.total_count || 0));
        }
        ageBreakdown = Array.from(map, ([animal_type, total_count]) => ({ animal_type, total_count }));
      } else if (ageRowsResult.status === "rejected") {
        console.error("Error fetching age breakdown:", ageRowsResult.reason);
      }

      const adultsGender = adultsGenderResult.status === "fulfilled" ? adultsGenderResult.value : [];
      if (adultsGenderResult.status === "rejected") console.error("Error fetching adults gender:", adultsGenderResult.reason);

      const kidsGender = kidsGenderResult.status === "fulfilled" ? kidsGenderResult.value : [];
      if (kidsGenderResult.status === "rejected") console.error("Error fetching kids gender:", kidsGenderResult.reason);

      const fatteningGender = fatteningGenderResult.status === "fulfilled" ? fatteningGenderResult.value : [];
      if (fatteningGenderResult.status === "rejected") console.error("Error fetching fattening gender:", fatteningGenderResult.reason);

      return NextResponse.json({ date: filterDate, farm: farm || "all", data: rows, kpiSummary, ageBreakdown, adultsGender, kidsGender, fatteningGender });
    } else {
      let records = await getCountingData();

      // Apply farm filter first so the latest date is scoped to the relevant farms
      if (farm) {
        if (farm.toLowerCase() === "core" || farm.toLowerCase() === "core-farms") {
          records = records.filter((r) => r.farm === "CBE" || r.farm === "CPT");
        } else if (farm.toLowerCase() === "holdings") {
          records = records.filter((r) => r.farm !== "CBE" && r.farm !== "CPT");
        } else {
          records = records.filter((r) => r.farm === farm);
        }
      }

      let filterDate = date;
      if (!filterDate && farm) {
        // Per-farm latest date
        const allDates = [...new Set(records.map((r) => r.date))].sort();
        filterDate = allDates[allDates.length - 1];
        records = records.filter((r) => r.date === filterDate);
      } else if (!filterDate) {
        // Overall: keep each farm's latest date
        const latestByFarm = new Map<string, string>();
        for (const r of records) {
          const cur = latestByFarm.get(r.farm);
          if (!cur || r.date > cur) latestByFarm.set(r.farm, r.date);
        }
        records = records.filter((r) => r.date === latestByFarm.get(r.farm));
        const dates = [...latestByFarm.values()].sort();
        filterDate = dates[dates.length - 1];
      } else {
        records = records.filter((r) => r.date === filterDate);
      }

      return NextResponse.json({ date: filterDate, farm: farm || "all", data: records });
    }
  } catch (error) {
    console.error("Error fetching counting data:", error);
    return NextResponse.json({ error: "Failed to fetch counting data" }, { status: 500 });
  }
}
