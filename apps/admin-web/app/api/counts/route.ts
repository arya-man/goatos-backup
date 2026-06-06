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

      // Fetch age breakdown from counting_kpis_daily
      let ageBreakdown: { animal_type: string; total_count: number }[] = [];
      try {
        const ageConditions: string[] = [
          "as_of_date = @date",
          "metric_name = 'age_wise_count'",
        ];
        const ageParams: Record<string, unknown> = { date: filterDate };

        if (farm) {
          const fk = farm.toLowerCase();
          if (fk === "cbe") {
            ageConditions.push("LOWER(farm) = 'cbe'");
          } else if (fk === "cpt") {
            ageConditions.push("LOWER(farm) = 'cpt'");
          } else if (fk === "holdings") {
            ageConditions.push("LOWER(farm) = 'holding farm'");
          } else if (fk === "core" || fk === "core-farms") {
            ageConditions.push("LOWER(farm) IN ('cbe', 'cpt')");
          }
          // overall: no farm filter
        }

        const ageRows = await queryBigQuery<{ animal_type: string; total_count: number }>(
          `SELECT animal_type, total_count FROM ${table("counting_kpis_daily")} WHERE ${ageConditions.join(" AND ")}`,
          ageParams
        );

        // Aggregate across farms by animal_type
        const ageMap = new Map<string, number>();
        for (const r of ageRows) {
          const key = (r.animal_type || "").toLowerCase();
          ageMap.set(key, (ageMap.get(key) ?? 0) + Number(r.total_count || 0));
        }
        ageBreakdown = Array.from(ageMap, ([animal_type, total_count]) => ({ animal_type, total_count }));
      } catch (e) {
        console.error("Error fetching age breakdown:", e);
      }

      // Fetch adults_by_gender, kids_by_gender and genderwise_fattening_count from counting_kpis_daily
      let adultsGender: { fattening_gender: string; total_count: number }[] = [];
      let kidsGender: { fattening_gender: string; total_count: number }[] = [];
      let fatteningGender: { fattening_gender: string; total_count: number }[] = [];
      try {
        const genderKpiConditions = (metricName: string) => {
          const conds: string[] = ["as_of_date = @date", `metric_name = '${metricName}'`];
          if (farm) {
            const fk = farm.toLowerCase();
            if (fk === "cbe") conds.push("LOWER(farm) = 'cbe'");
            else if (fk === "cpt") conds.push("LOWER(farm) = 'cpt'");
            else if (fk === "holdings") conds.push("LOWER(farm) NOT IN ('cbe', 'cpt')");
            else if (fk === "core" || fk === "core-farms") conds.push("LOWER(farm) IN ('cbe', 'cpt')");
          }
          return conds;
        };

        const [agRows, kgRows, fgRows] = await Promise.all([
          queryBigQuery<{ fattening_gender: string; total_count: number }>(
            `SELECT fattening_gender, SUM(total_count) AS total_count FROM ${table("counting_kpis_daily")} WHERE ${genderKpiConditions("adults_by_gender").join(" AND ")} GROUP BY fattening_gender`,
            { date: filterDate }
          ),
          queryBigQuery<{ fattening_gender: string; total_count: number }>(
            `SELECT fattening_gender, SUM(total_count) AS total_count FROM ${table("counting_kpis_daily")} WHERE ${genderKpiConditions("kids_by_gender").join(" AND ")} GROUP BY fattening_gender`,
            { date: filterDate }
          ),
          queryBigQuery<{ fattening_gender: string; total_count: number }>(
            `SELECT fattening_gender, SUM(total_count) AS total_count FROM ${table("counting_kpis_daily")} WHERE ${genderKpiConditions("genderwise_fattening_count").join(" AND ")} GROUP BY fattening_gender`,
            { date: filterDate }
          ),
        ]);
        adultsGender = agRows;
        kidsGender = kgRows;
        fatteningGender = fgRows;
      } catch (e) {
        console.error("Error fetching adults/kids/fattening by gender:", e);
      }

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
