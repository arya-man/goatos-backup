/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import {
  getBirthsKPI,
  getKiddingEfficiency,
  getBirthCountLast10Days,
  getKiddingFrequency,
  getLitterSize,
} from "@/lib/data/births";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const [kpiRows, efficiencyRows, recentRows, frequencyRows, litterRows, avgRows] = await Promise.all([
  queryBigQuery(`SELECT COUNTIF(is_birth = 1) as total_births FROM ${table("mother_kid_facts")}`),
  queryBigQuery(`
  SELECT breed, delivered_last_8m_pct
  FROM ${table("breedwise_kidding_8m")}
  WHERE breed IS NOT NULL
  ORDER BY delivered_last_8m_pct DESC
`),
  queryBigQuery(`SELECT date, birth_count FROM ${table("v_birth_count_last_10_days")} ORDER BY date`),
  queryBigQuery(`SELECT breed, approx_months_between_births FROM ${table("kidding_frequency")} WHERE breed IS NOT NULL
    AND TRIM(breed) != ''
    AND LOWER(TRIM(breed)) != 'mixed'`),
  queryBigQuery(`
  SELECT
    breed,
    ROUND(AVG(avg_litter_size), 2) AS avg_litter_size
  FROM ${table("birth_analysis_view")}
  WHERE breed IS NOT NULL
    AND TRIM(breed) != ''
    AND LOWER(TRIM(breed)) != 'mixed'
  GROUP BY breed
  ORDER BY avg_litter_size DESC
`),
  queryBigQuery(`
  SELECT ROUND(AVG(avg_litter_size), 2) AS avg_kids_per_mother
  FROM ${table("birth_analysis_view")}
  WHERE breed IS NOT NULL
    AND TRIM(breed) != ''
    AND LOWER(TRIM(breed)) != 'mixed'
`),
]);
const round2 = (val: any) => Math.round(Number(val || 0) * 100) / 100;
const kpiRow = (kpiRows as any[])[0] || {};
const avgRow = (avgRows as any[])[0] || {};

return NextResponse.json({
    data: {
      kpi: {
        totalBirths: round2(kpiRow.total_births),
        avgKidsPerMother: round2(avgRow.avg_kids_per_mother),
      },

      kiddingEfficiency: (() => {
        const mapped = (efficiencyRows as any[]).map((r: any) => ({
          breed: r.breed,
          pct: round2(r.delivered_last_8m_pct),
        }));
        // If values look like decimals (0-1), convert to percentages (0-100)
        const allSmall = mapped.length > 0 && mapped.every((r) => r.pct <= 1);
        if (allSmall) return mapped.map((r) => ({ ...r, pct: round2(r.pct * 100) }));
        return mapped;
      })(),

      birthCountLast10Days: (recentRows as any[])
      .map((r: any) => ({
        date: String(r.date?.value ?? r.date),
        count: round2(r.birth_count),
      }))
      .sort((a, b) => a.date.localeCompare(b.date)),

      kiddingFrequency: (frequencyRows as any[]).map((r: any) => ({
        breed: r.breed,
        months: round2(r.approx_months_between_births),
      })),

      litterSize: (litterRows as any[]).map((r: any) => ({
        breed: r.breed,
        size: round2(r.avg_litter_size),
      })),
    },
  });
    }

    return NextResponse.json({
      data: {
        kpi: getBirthsKPI(),
        kiddingEfficiency: getKiddingEfficiency(),
        birthCountLast10Days: getBirthCountLast10Days(),
        kiddingFrequency: getKiddingFrequency(),
        litterSize: getLitterSize(),
      },
    });
  } catch (error) {
    console.error("Error fetching births data:", error);
    return NextResponse.json({ error: "Failed to fetch births data" }, { status: 500 });
  }
}
