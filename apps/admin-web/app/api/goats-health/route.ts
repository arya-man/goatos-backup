/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery } from "@/lib/bigquery";

export const dynamic = 'force-dynamic';

const T = '`goatos-sheets.healthDB.health_db_clean_dev`';

function formatRows(rows: any[]): { date: string; count: number }[] {
  return rows.map((r: any) => ({
    date: String(r.date?.value ?? r.date),
    count: Number(r.count || 0),
  }));
}

function diseaseRows(rows: any[]): { name: string; value: number }[] {
  return rows.map((r: any) => ({
    name: String(r.name),
    value: Number(r.value || 0),
  }));
}

function sickGoatRows(rows: any[]) {
  return rows.map((r: any) => ({
    goatId: r.goat_id != null ? String(r.goat_id) : "No tag",
    problemId: String(r.problem_id ?? "—"),
    problemName: String(r.problem_name ?? "—"),
    farm: String(r.farm ?? "—"),
    date: String(r.date?.value ?? r.date),
  }));
}

const sickGoatListQuery = (farmFilter?: string) => `
  SELECT goat_id, problem_id, problem_name, farm, date
  FROM (
    SELECT *,
      ROW_NUMBER() OVER (
        PARTITION BY COALESCE(CAST(goat_id AS STRING), CAST(problem_id AS STRING))
        ORDER BY date DESC, problem_id DESC
      ) AS rn
    FROM ${T}
    WHERE status = 'Open'${farmFilter ? `\n      AND farm = '${farmFilter}'` : ""}
  )
  WHERE rn = 1
  ORDER BY date DESC
`;

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const [
        sickTotalCountRows,
        sickCbeCountRows,
        sickCptCountRows,
        sickGoatListTotalRows,
        sickGoatListCbeRows,
        sickGoatListCptRows,
        sickTotalRows,
        sickCbeRows,
        sickCptRows,
        diseaseTotalRows,
        diseaseCbeRows,
        diseaseCptRows,
      ] = await Promise.all([
        queryBigQuery(`SELECT COUNT(DISTINCT goat_id) AS currently_sick FROM ${T} WHERE status = 'Open'`),
        queryBigQuery(`SELECT COUNT(DISTINCT goat_id) AS currently_sick FROM ${T} WHERE status = 'Open' AND farm = 'CBE'`),
        queryBigQuery(`SELECT COUNT(DISTINCT goat_id) AS currently_sick FROM ${T} WHERE status = 'Open' AND farm = 'CPT'`),
        queryBigQuery(sickGoatListQuery()),
        queryBigQuery(sickGoatListQuery("CBE")),
        queryBigQuery(sickGoatListQuery("CPT")),
        queryBigQuery(`
          SELECT first_date AS date, COUNT(DISTINCT goat_id) AS count
          FROM (
            SELECT goat_id, MIN(date) AS first_date
            FROM ${T}
            GROUP BY goat_id
          )
          WHERE first_date >= DATE_SUB(CURRENT_DATE(), INTERVAL 9 DAY)
          GROUP BY first_date
          ORDER BY first_date
        `),
        queryBigQuery(`
          SELECT first_date AS date, COUNT(DISTINCT goat_id) AS count
          FROM (
            SELECT goat_id, MIN(date) AS first_date
            FROM ${T}
            WHERE farm = 'CBE'
            GROUP BY goat_id
          )
          WHERE first_date >= DATE_SUB(CURRENT_DATE(), INTERVAL 9 DAY)
          GROUP BY first_date
          ORDER BY first_date
        `),
        queryBigQuery(`
          SELECT first_date AS date, COUNT(DISTINCT goat_id) AS count
          FROM (
            SELECT goat_id, MIN(date) AS first_date
            FROM ${T}
            WHERE farm = 'CPT'
            GROUP BY goat_id
          )
          WHERE first_date >= DATE_SUB(CURRENT_DATE(), INTERVAL 9 DAY)
          GROUP BY first_date
          ORDER BY first_date
        `),
        queryBigQuery(`
          SELECT problem_name AS name, COUNT(*) AS value
          FROM ${T}
          WHERE date >= DATE_SUB(CURRENT_DATE(), INTERVAL 9 DAY)
          GROUP BY problem_name
          ORDER BY value DESC
          LIMIT 10
        `),
        queryBigQuery(`
          SELECT problem_name AS name, COUNT(*) AS value
          FROM ${T}
          WHERE date >= DATE_SUB(CURRENT_DATE(), INTERVAL 9 DAY)
            AND farm = 'CBE'
          GROUP BY problem_name
          ORDER BY value DESC
          LIMIT 10
        `),
        queryBigQuery(`
          SELECT problem_name AS name, COUNT(*) AS value
          FROM ${T}
          WHERE date >= DATE_SUB(CURRENT_DATE(), INTERVAL 9 DAY)
            AND farm = 'CPT'
          GROUP BY problem_name
          ORDER BY value DESC
          LIMIT 10
        `),
      ]);

      return NextResponse.json({
        data: {
          kpi: {
            total: Number((sickTotalCountRows as any[])[0]?.currently_sick || 0),
            cbe: Number((sickCbeCountRows as any[])[0]?.currently_sick || 0),
            cpt: Number((sickCptCountRows as any[])[0]?.currently_sick || 0),
          },
          sickGoatsList: {
            total: sickGoatRows(sickGoatListTotalRows as any[]),
            cbe: sickGoatRows(sickGoatListCbeRows as any[]),
            cpt: sickGoatRows(sickGoatListCptRows as any[]),
          },
          sickPerDay: {
            total: formatRows(sickTotalRows as any[]),
            cbe: formatRows(sickCbeRows as any[]),
            cpt: formatRows(sickCptRows as any[]),
          },
          topDiseases: {
            total: diseaseRows(diseaseTotalRows as any[]),
            cbe: diseaseRows(diseaseCbeRows as any[]),
            cpt: diseaseRows(diseaseCptRows as any[]),
          },
        },
      });
    }

    // Mock data fallback
    return NextResponse.json({
      data: {
        kpi: { total: 0, cbe: 0, cpt: 0 },
        sickGoatsList: { total: [], cbe: [], cpt: [] },
        sickPerDay: { total: [], cbe: [], cpt: [] },
        topDiseases: { total: [], cbe: [], cpt: [] },
      },
    });
  } catch (error) {
    console.error("[goats-health] API error:", error);
    return NextResponse.json(
      { error: "Failed to fetch goats health data" },
      { status: 500 }
    );
  }
}
